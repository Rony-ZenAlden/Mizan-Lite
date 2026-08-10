// Package sqlite is the accounting module's persistence.
//
// SQL lives here and nowhere else (`no-sql`, 1.1): the domain decides what a valid account is,
// and this decides how one is stored. Every method takes the executor from the CONTEXT, so a
// call inside a Unit of Work joins that transaction (0.3 §3.2) — which is what lets ApplyChart
// write nineteen accounts, thirteen mappings, and an audit entry as one atomic act.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "accounting.storage"

// Repos is the module's repository set.
type Repos struct {
	db  database.DB
	clk clock.Clock
}

// New builds the repositories.
func New(db database.DB, clk clock.Clock) *Repos {
	if clk == nil {
		clk = clock.System()
	}
	return &Repos{db: db, clk: clk}
}

func (r *Repos) now() string { return clock.Format(r.clk.Now()) }

func (r *Repos) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal, CodeStorage, what)
}

// nullable maps "" to NULL, so an absent value is stored as absent rather than as an empty
// string every later query would have to treat as a third case.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── accounts ────────────────────────────────────────────────────────────────────

// InsertAccount writes one account.
func (r *Repos) InsertAccount(ctx context.Context, a domain.Account) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO accounts (
			id, company_id, code, name, name_key, parent_id, path, depth,
			account_type, account_subtype, normal_balance, currency_code,
			is_postable, is_system, is_active, branch_id, description,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(a.ID), string(a.CompanyID), a.Code, a.Name, nullable(a.NameKey),
		nullable(string(a.ParentID)), a.Path, a.Depth,
		string(a.Type), nullable(a.Subtype), string(a.Normal), nullable(a.CurrencyCode),
		boolToInt(a.IsPostable), boolToInt(a.IsSystem), boolToInt(a.IsActive),
		nullable(string(a.BranchID)), nullable(a.Description), now, now)
	if err != nil {
		return r.wrap(err, "inserting an account")
	}
	return nil
}

// SetPostable switches an account's posting surface on or off.
//
// The only mutation this step exposes, and it exists for one reason: an account stops accepting
// postings the moment it acquires a child (§20.1). Everything else about an account is written
// once by ApplyChart.
func (r *Repos) SetPostable(ctx context.Context, accountID id.ID, postable bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE accounts
		   SET is_postable = ?, updated_at = ?, row_version = row_version + 1
		 WHERE id = ?`,
		boolToInt(postable), r.now(), string(accountID))
	if err != nil {
		return r.wrap(err, "updating an account")
	}
	return nil
}

// CountAccounts reports how many accounts a company has.
//
// Used to refuse a second chart. A count rather than a flag: there is no state to keep in step,
// and "does this company have a chart?" has exactly one answer that cannot drift.
func (r *Repos) CountAccounts(ctx context.Context, companyID id.ID) (int, error) {
	var count int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM accounts WHERE company_id = ?`, string(companyID)).Scan(&count)
	if err != nil {
		return 0, r.wrap(err, "counting accounts")
	}
	return count, nil
}

const accountColumns = `
	id, company_id, code, name, name_key, parent_id, path, depth,
	account_type, account_subtype, normal_balance, currency_code,
	is_postable, is_system, is_active, branch_id, description`

// Accounts lists a company's chart.
//
// Ordered by PATH, not by code: the path sorts a hierarchy into reading order — a parent
// immediately followed by its children — which is what a chart of accounts is meant to look
// like. Ordering by code would interleave levels.
func (r *Repos) Accounts(ctx context.Context, companyID id.ID) ([]domain.Account, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE company_id = ? ORDER BY path`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing accounts")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Account
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning an account")
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

// AccountByCode finds one account by its stable key.
func (r *Repos) AccountByCode(ctx context.Context, companyID id.ID, code string) (domain.Account, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE company_id = ? AND code = ?`,
		string(companyID), code)
	account, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, err // sql.ErrNoRows passes through
	}
	return account, nil
}

// AccountByID finds one account.
func (r *Repos) AccountByID(ctx context.Context, accountID id.ID) (domain.Account, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, string(accountID))
	account, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

// scanner is what QueryRow and Rows have in common, so one scan serves both.
type scanner interface{ Scan(dest ...any) error }

func scanAccount(s scanner) (domain.Account, error) {
	var (
		a                             domain.Account
		nameKey, parentID, subtype    any
		currencyCode, branchID, descr any
		accountType, normal           string
		postable, system, active      int
	)
	if err := s.Scan(
		&a.ID, &a.CompanyID, &a.Code, &a.Name, &nameKey, &parentID, &a.Path, &a.Depth,
		&accountType, &subtype, &normal, &currencyCode,
		&postable, &system, &active, &branchID, &descr,
	); err != nil {
		return domain.Account{}, err
	}

	a.Type = domain.AccountType(accountType)
	a.Normal = domain.Side(normal)
	a.IsPostable, a.IsSystem, a.IsActive = postable == 1, system == 1, active == 1
	a.NameKey = text(nameKey)
	a.ParentID = id.ID(text(parentID))
	a.Subtype = text(subtype)
	a.CurrencyCode = text(currencyCode)
	a.BranchID = id.ID(text(branchID))
	a.Description = text(descr)
	return a, nil
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ── mappings ────────────────────────────────────────────────────────────────────

// Mapping is one mapping-key-to-account row.
type Mapping struct {
	ID        id.ID
	CompanyID id.ID
	Key       string
	AccountID id.ID
	BranchID  id.ID
	ProfileID id.ID
}

// InsertMapping writes one mapping.
func (r *Repos) InsertMapping(ctx context.Context, m Mapping) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO account_mappings (
			id, company_id, mapping_key, account_id, branch_id, business_profile_id,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(m.ID), string(m.CompanyID), m.Key, string(m.AccountID),
		nullable(string(m.BranchID)), nullable(string(m.ProfileID)), now, now)
	if err != nil {
		return r.wrap(err, "inserting an account mapping")
	}
	return nil
}

// ResolveMapping answers "which account plays this role?", most specific first.
//
// The ORDER BY is the precedence: a row naming this branch beats the company-wide row. Done in
// SQL rather than by fetching both and choosing in Go, because "most specific wins" is one
// sortable expression and two round trips would be two chances to disagree about it.
func (r *Repos) ResolveMapping(
	ctx context.Context, companyID, branchID id.ID, key string,
) (domain.Account, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT `+qualify(accountColumns, "a.")+`
		  FROM account_mappings m
		  JOIN accounts a ON a.id = m.account_id
		 WHERE m.company_id = ?
		   AND m.mapping_key = ?
		   AND (m.branch_id IS NULL OR m.branch_id = ?)
		 ORDER BY CASE WHEN m.branch_id IS NULL THEN 1 ELSE 0 END
		 LIMIT 1`,
		string(companyID), key, string(branchID))

	account, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, errs.NotFound(domain.CodeUnknownMapping,
			"no account is mapped to that role").WithParam("mapping", key)
	}
	if err != nil {
		return domain.Account{}, r.wrap(err, "resolving an account mapping")
	}
	return account, nil
}

// Mappings lists a company's mappings, keyed by mapping key.
func (r *Repos) Mappings(ctx context.Context, companyID id.ID) (map[string]string, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT m.mapping_key, a.code
		  FROM account_mappings m
		  JOIN accounts a ON a.id = m.account_id
		 WHERE m.company_id = ? AND m.branch_id IS NULL
		 ORDER BY m.mapping_key`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing account mappings")
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var key, code string
		if scanErr := rows.Scan(&key, &code); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning an account mapping")
		}
		out[key] = code
	}
	return out, rows.Err()
}

// qualify prefixes every column in a select list with a table alias.
//
// One column list, used bare and joined, so a column added to `accounts` cannot be forgotten in
// the mapping query — which would surface as a scan error at the moment a posting needed the
// account, not at compile time.
func qualify(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
