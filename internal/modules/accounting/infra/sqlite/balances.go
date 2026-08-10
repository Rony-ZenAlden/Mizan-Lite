package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
)

// ApplyMovement adds one line's amounts to the balance rows it affects.
//
// Called inside the posting transaction, so the ledger and the balances derived from it commit
// together or not at all. Balances written afterwards, outside the transaction, would drift the
// moment a posting rolled back — which is the exact failure the rebuild job exists to catch and
// a much better one to prevent.
//
// TWO rows are touched when a line carries a branch: the aggregate (branch NULL) and the
// branch's own. A branch profit-and-loss then reads the same query shape as a company one,
// rather than needing a sum over branches that would silently omit lines carrying no branch.
func (r *Repos) ApplyMovement(
	ctx context.Context, companyID, periodID id.ID, line domain.Line,
) error {
	if err := r.addMovement(ctx, companyID, periodID, line.AccountID, id.ID(""), line); err != nil {
		return err
	}
	if line.BranchID.IsZero() {
		return nil
	}
	return r.addMovement(ctx, companyID, periodID, line.AccountID, line.BranchID, line)
}

// addMovement upserts one balance row.
//
// An UPSERT rather than a read-modify-write: the increment happens inside the database, so two
// postings in the same transaction cannot read the same stale total and each write it back
// plus their own. SQLite has one writer, which makes the race impossible today — but the
// portable form costs nothing and survives the move to an engine where it is possible (§8).
func (r *Repos) addMovement(
	ctx context.Context, companyID, periodID, accountID, branchID id.ID, line domain.Line,
) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO account_balances (
			id, company_id, account_id, fiscal_period_id, branch_id,
			debit_minor, credit_minor, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, fiscal_period_id, COALESCE(branch_id, '')) DO UPDATE SET
			debit_minor  = debit_minor  + excluded.debit_minor,
			credit_minor = credit_minor + excluded.credit_minor,
			updated_at   = excluded.updated_at`,
		string(identifier), string(companyID), string(accountID), string(periodID),
		nullable(string(branchID)), line.Debit, line.Credit, r.now())
	if err != nil {
		return r.wrap(err, "updating an account balance")
	}
	return nil
}

// Balance is one account's movement and running position within a period.
type Balance struct {
	AccountID   id.ID
	AccountCode string
	AccountName string
	AccountType domain.AccountType
	Normal      domain.Side

	// Opening is the position at the START of the period, debit-positive: the sum of every
	// earlier period's movement. Derived, never stored (0012).
	Opening int64
	Debit   int64
	Credit  int64
	Closing int64
}

// Movement is the debit-positive net for the period.
func (b Balance) Movement() int64 { return b.Debit - b.Credit }

// TrialBalance reads every account's position for a period.
//
// # Debit-positive throughout
//
// Every amount here is signed in the DEBIT direction: a liability holding a credit balance
// reads negative. One convention, converted once at the presentation edge by consulting the
// account's normal balance — rather than each query deciding for itself, which is how a report
// ends up showing revenue as a negative number to an accountant who then does not trust it.
//
// The consequence worth stating: a correct trial balance sums to ZERO under this convention,
// not to "total debits = total credits". Same fact, and the zero is cheaper to assert.
func (r *Repos) TrialBalance(
	ctx context.Context, companyID, periodID id.ID,
) ([]Balance, error) {
	// One query, two correlated sums: the movement inside this period, and everything before
	// it. `b.start` orders periods by their start date, which is what "earlier" means on a
	// fiscal calendar that need not start in January.
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		WITH target AS (
			SELECT p.id, p.start_date FROM fiscal_periods p WHERE p.id = ?
		)
		SELECT a.id, a.code, a.name, a.account_type, a.normal_balance,
		       COALESCE(SUM(CASE WHEN bp.id = t.id THEN b.debit_minor  ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN bp.id = t.id THEN b.credit_minor ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN bp.start_date < t.start_date
		                         THEN b.debit_minor - b.credit_minor ELSE 0 END), 0)
		  FROM accounts a
		  CROSS JOIN target t
		  LEFT JOIN account_balances b
		         ON b.account_id = a.id AND b.branch_id IS NULL
		  LEFT JOIN fiscal_periods bp ON bp.id = b.fiscal_period_id
		 WHERE a.company_id = ?
		 GROUP BY a.id, a.code, a.name, a.account_type, a.normal_balance, a.path
		 ORDER BY a.path`,
		string(periodID), string(companyID))
	if err != nil {
		return nil, r.wrap(err, "reading the trial balance")
	}
	defer func() { _ = rows.Close() }()

	var out []Balance
	for rows.Next() {
		var (
			b                   Balance
			accountType, normal string
		)
		if scanErr := rows.Scan(
			&b.AccountID, &b.AccountCode, &b.AccountName, &accountType, &normal,
			&b.Debit, &b.Credit, &b.Opening,
		); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a balance")
		}
		b.AccountType = domain.AccountType(accountType)
		b.Normal = domain.Side(normal)
		b.Closing = b.Opening + b.Debit - b.Credit
		out = append(out, b)
	}
	return out, rows.Err()
}

// RebuildBalances recomputes every balance row from the journal.
//
// The safety net §20.5 asks for. It deletes and recreates rather than reconciling: reconciliation
// would need its own arithmetic, which is one more thing that can be wrong in the same way the
// incremental path can — and the point of a rebuild is to be an INDEPENDENT answer.
//
// Draft entries are excluded, the same as the incremental path, so a rebuild and a live total
// are computed from the same set of facts.
func (r *Repos) RebuildBalances(ctx context.Context, companyID id.ID) error {
	if _, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM account_balances WHERE company_id = ?`, string(companyID)); err != nil {
		return r.wrap(err, "clearing account balances")
	}

	now := r.now()
	// The aggregate rows.
	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO account_balances (
			id, company_id, account_id, fiscal_period_id, branch_id,
			debit_minor, credit_minor, updated_at
		)
		SELECT lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-' ||
		       lower(hex(randomblob(2))) || '-' || lower(hex(randomblob(2))) || '-' ||
		       lower(hex(randomblob(6))),
		       e.company_id, l.account_id, e.fiscal_period_id, NULL,
		       SUM(l.debit_minor), SUM(l.credit_minor), ?
		  FROM journal_lines l
		  JOIN journal_entries e ON e.id = l.journal_entry_id
		 WHERE e.company_id = ? AND e.status <> 'draft'
		 GROUP BY e.company_id, l.account_id, e.fiscal_period_id`,
		now, string(companyID)); err != nil {
		return r.wrap(err, "rebuilding account balances")
	}

	// The per-branch rows.
	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO account_balances (
			id, company_id, account_id, fiscal_period_id, branch_id,
			debit_minor, credit_minor, updated_at
		)
		SELECT lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-' ||
		       lower(hex(randomblob(2))) || '-' || lower(hex(randomblob(2))) || '-' ||
		       lower(hex(randomblob(6))),
		       e.company_id, l.account_id, e.fiscal_period_id, l.branch_id,
		       SUM(l.debit_minor), SUM(l.credit_minor), ?
		  FROM journal_lines l
		  JOIN journal_entries e ON e.id = l.journal_entry_id
		 WHERE e.company_id = ? AND e.status <> 'draft' AND l.branch_id IS NOT NULL
		 GROUP BY e.company_id, l.account_id, e.fiscal_period_id, l.branch_id`,
		now, string(companyID)); err != nil {
		return r.wrap(err, "rebuilding branch account balances")
	}
	return nil
}

// BalanceSnapshot is every stored balance row, for comparison.
type BalanceSnapshot struct {
	AccountID id.ID
	PeriodID  id.ID
	BranchID  id.ID
	Debit     int64
	Credit    int64
}

// BalanceRows reads every stored balance, ordered so two snapshots compare element by element.
func (r *Repos) BalanceRows(ctx context.Context, companyID id.ID) ([]BalanceSnapshot, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT account_id, fiscal_period_id, COALESCE(branch_id, ''), debit_minor, credit_minor
		  FROM account_balances
		 WHERE company_id = ?
		 ORDER BY account_id, fiscal_period_id, COALESCE(branch_id, '')`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "reading account balances")
	}
	defer func() { _ = rows.Close() }()

	var out []BalanceSnapshot
	for rows.Next() {
		var b BalanceSnapshot
		if scanErr := rows.Scan(&b.AccountID, &b.PeriodID, &b.BranchID, &b.Debit, &b.Credit); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a balance row")
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CompaniesWithLedgers lists every company that has posted anything.
//
// The integrity job iterates these rather than every company: a company with no journal entries
// has nothing to check, and on a single-company installation this is one row either way.
func (r *Repos) CompaniesWithLedgers(ctx context.Context) ([]id.ID, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT DISTINCT company_id FROM journal_entries`)
	if err != nil {
		return nil, r.wrap(err, "listing companies with ledgers")
	}
	defer func() { _ = rows.Close() }()

	var out []id.ID
	for rows.Next() {
		var companyID id.ID
		if scanErr := rows.Scan(&companyID); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a company")
		}
		out = append(out, companyID)
	}
	return out, rows.Err()
}
