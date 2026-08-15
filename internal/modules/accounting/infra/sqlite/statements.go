package sqlite

import (
	"context"
	"database/sql"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
)

// AccountPosition is one account's figure for a statement, with the tree columns needed to
// place it.
//
// # Debit-positive, like everything else the ledger returns
//
// `Amount` is signed in the DEBIT direction: revenue, which normally carries a credit balance,
// reads NEGATIVE here. Converting at the query would mean each query deciding for itself, which
// is 0012's warning — the statement converts once, at the point where it knows what it is
// presenting.
type AccountPosition struct {
	AccountID  id.ID
	ParentID   id.ID
	Code       string
	Name       string
	Type       domain.AccountType
	Normal     domain.Side
	Depth      int
	Path       string
	IsPostable bool
	Amount     int64
}

// CoveredRange is the span a statement actually reports on, which need not be the span asked for.
//
// # Why a report says what it covers
//
// `account_balances` is a per-PERIOD projection, so the finest resolution any statement built on
// it can have is one fiscal period. A request for 1–15 March cannot be answered exactly; the
// honest answers are to refuse, to silently widen, or to widen and SAY SO.
//
// Refusing makes the common case — "this month", where the period is the month — fail for people
// whose fiscal calendar does not start on the 1st. Silently widening reports March's figures
// under a heading that says two weeks, which is how somebody concludes their sales doubled.
//
// So the statement widens to whole periods and returns both ranges. A caller that finds them
// different can say so on the screen, and one that does not is at least not lying in its data.
type CoveredRange struct {
	// From and To are the earliest start and latest end of the periods included. Empty when no
	// period overlapped the request, which is not an error: a company in its first week has no
	// closed period and no figures, and that is an empty report rather than a failure.
	From string
	To   string
	// Periods counts what was included, so a caller can distinguish "no periods" from "periods
	// with no postings".
	Periods int
}

// PositionsInRange sums each account's movement over every period overlapping the dates.
//
// Used for the PROFIT AND LOSS, where the question is "what happened between these dates".
func (r *Repos) PositionsInRange(
	ctx context.Context, companyID id.ID, from, to string,
) ([]AccountPosition, CoveredRange, error) {
	covered, err := r.coveredRange(ctx, companyID, from, to)
	if err != nil {
		return nil, CoveredRange{}, err
	}

	// A period OVERLAPS the request when it starts before the request ends and ends after the
	// request starts. The obvious `start >= from AND end <= to` is containment, not overlap, and
	// it silently drops the period a mid-month date falls in — the whole month, not part of it.
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT a.id, COALESCE(a.parent_id, ''), a.code, a.name, a.account_type,
		       a.normal_balance, a.depth, a.path, a.is_postable,
		       COALESCE(SUM(b.debit_minor - b.credit_minor), 0)
		  FROM accounts a
		  LEFT JOIN account_balances b
		         ON b.account_id = a.id AND b.branch_id IS NULL
		        AND b.fiscal_period_id IN (
		              SELECT p.id FROM fiscal_periods p
		                JOIN fiscal_years y ON y.id = p.fiscal_year_id
		               WHERE y.company_id = ? AND p.start_date <= ? AND p.end_date >= ?)
		 WHERE a.company_id = ?
		 GROUP BY a.id, a.parent_id, a.code, a.name, a.account_type,
		          a.normal_balance, a.depth, a.path, a.is_postable
		 ORDER BY a.path`,
		string(companyID), to, from, string(companyID))
	if err != nil {
		return nil, CoveredRange{}, r.wrap(err, "reading account positions for a range")
	}
	positions, err := scanPositions(rows)
	if err != nil {
		return nil, CoveredRange{}, r.wrap(err, "reading account positions for a range")
	}
	return positions, covered, nil
}

// PositionsAsAt sums each account's movement from the beginning of time to the date.
//
// Used for the BALANCE SHEET, where the question is "what is the position now". An asset account
// has no opening and closing to distinguish: its balance IS everything that ever happened to it.
func (r *Repos) PositionsAsAt(
	ctx context.Context, companyID id.ID, asAt string,
) ([]AccountPosition, CoveredRange, error) {
	covered, err := r.coveredRange(ctx, companyID, "", asAt)
	if err != nil {
		return nil, CoveredRange{}, err
	}

	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT a.id, COALESCE(a.parent_id, ''), a.code, a.name, a.account_type,
		       a.normal_balance, a.depth, a.path, a.is_postable,
		       COALESCE(SUM(b.debit_minor - b.credit_minor), 0)
		  FROM accounts a
		  LEFT JOIN account_balances b
		         ON b.account_id = a.id AND b.branch_id IS NULL
		        AND b.fiscal_period_id IN (
		              SELECT p.id FROM fiscal_periods p
		                JOIN fiscal_years y ON y.id = p.fiscal_year_id
		               WHERE y.company_id = ? AND p.start_date <= ?)
		 WHERE a.company_id = ?
		 GROUP BY a.id, a.parent_id, a.code, a.name, a.account_type,
		          a.normal_balance, a.depth, a.path, a.is_postable
		 ORDER BY a.path`,
		string(companyID), asAt, string(companyID))
	if err != nil {
		return nil, CoveredRange{}, r.wrap(err, "reading account positions as at a date")
	}
	positions, err := scanPositions(rows)
	if err != nil {
		return nil, CoveredRange{}, r.wrap(err, "reading account positions as at a date")
	}
	return positions, covered, nil
}

// coveredRange reports which whole periods a request actually reaches.
//
// An empty `from` means "from the beginning", which is what a balance sheet asks for.
func (r *Repos) coveredRange(
	ctx context.Context, companyID id.ID, from, to string,
) (CoveredRange, error) {
	var covered CoveredRange

	// COALESCE on the aggregates, not on the row: MIN over no rows is one row holding NULL, and
	// scanning that into a string fails. The empty answer must be a value, because "no periods
	// yet" is an ordinary state for a company in its first week.
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(MIN(p.start_date), ''), COALESCE(MAX(p.end_date), ''), COUNT(*)
		  FROM fiscal_periods p
		  JOIN fiscal_years y ON y.id = p.fiscal_year_id
		 WHERE y.company_id = ? AND p.start_date <= ? AND (? = '' OR p.end_date >= ?)`,
		string(companyID), to, from, from)

	if err := row.Scan(&covered.From, &covered.To, &covered.Periods); err != nil {
		return CoveredRange{}, r.wrap(err, "reading the periods a statement covers")
	}
	return covered, nil
}

func scanPositions(rows *sql.Rows) ([]AccountPosition, error) {
	defer func() { _ = rows.Close() }()

	out := make([]AccountPosition, 0, 64)
	for rows.Next() {
		var (
			position AccountPosition
			postable int
		)
		if err := rows.Scan(&position.AccountID, &position.ParentID, &position.Code,
			&position.Name, &position.Type, &position.Normal, &position.Depth,
			&position.Path, &postable, &position.Amount); err != nil {
			return nil, err
		}
		position.IsPostable = postable == 1
		out = append(out, position)
	}
	return out, rows.Err()
}
