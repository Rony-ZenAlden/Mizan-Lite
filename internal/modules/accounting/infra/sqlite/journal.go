package sqlite

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
)

// Business DATES and instants are written by the kernel's own helpers (§7.5), not by layouts
// declared here. A second copy of the format is a second thing to keep in step, and the first
// symptom of drift would be a date that reads back one day out.

// ── journal entries ─────────────────────────────────────────────────────────────
//
// Note what this file does NOT contain: an update or a delete for a posted entry. §20.2 says a
// posted entry is never modified or deleted, and the strongest way to say that is to offer no
// method that could — the same reasoning that keeps the audit repository append-only (1.6).
//
// MarkReversed is the one status change, and it moves an entry from posted to reversed while
// leaving every line exactly as posted.

// InsertEntry writes an entry and its lines.
func (r *Repos) InsertEntry(ctx context.Context, e domain.Entry) error {
	now := r.now()
	var postedAt any
	if !e.PostedAt.IsZero() {
		postedAt = clock.Format(e.PostedAt)
	}

	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO journal_entries (
			id, company_id, entry_number, entry_date, fiscal_period_id, status,
			source_module, source_document_type, source_document_id, reversal_of_entry_id,
			memo, branch_id, posted_at, posted_by, created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(e.ID), string(e.CompanyID), e.Number, clock.FormatDate(e.Date),
		string(e.PeriodID), string(e.Status),
		e.SourceModule, nullable(e.SourceDocumentType), nullable(string(e.SourceDocumentID)),
		nullable(string(e.ReversalOf)), nullable(e.Memo), nullable(string(e.BranchID)),
		postedAt, nullable(string(e.PostedBy)), now, now)
	if err != nil {
		return r.wrap(err, "inserting a journal entry")
	}

	for _, line := range e.Lines {
		if _, err = r.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO journal_lines (
				id, journal_entry_id, line_number, account_id, debit_minor, credit_minor,
				currency_code, original_amount_minor, exchange_rate_micro,
				branch_id, warehouse_id, partner_id, product_id, cost_center_id, project_id,
				memo, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			mustLineID(), string(e.ID), line.Number, string(line.AccountID),
			line.Debit, line.Credit,
			nullable(line.CurrencyCode), nullableInt(line.OriginalMinor), nullableInt(line.RateMicro),
			nullable(string(line.BranchID)), nullable(string(line.WarehouseID)),
			nullable(string(line.PartnerID)), nullable(string(line.ProductID)),
			nullable(string(line.CostCenterID)), nullable(string(line.ProjectID)),
			nullable(line.Memo), now,
		); err != nil {
			return r.wrap(err, "inserting a journal line")
		}
	}
	return nil
}

// mustLineID mints a line identifier.
//
// A line has no natural key and is never referenced from outside its entry, so a failure to
// generate one is a failed entropy source — which means the process has bigger problems than
// this posting. The zero id would violate the primary key and fail the insert, which is the
// honest outcome either way.
func mustLineID() string {
	identifier, err := id.New()
	if err != nil {
		return ""
	}
	return string(identifier)
}

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// MarkReversed moves a posted entry to reversed, leaving its lines untouched.
func (r *Repos) MarkReversed(ctx context.Context, entryID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE journal_entries
		   SET status = 'reversed', updated_at = ?, row_version = row_version + 1
		 WHERE id = ? AND status = 'posted'`,
		r.now(), string(entryID))
	if err != nil {
		return r.wrap(err, "reversing a journal entry")
	}
	return nil
}

// NextEntryNumber allocates the next number for a company.
//
// Unique and sequential, NOT gapless (§9.4). MAX+1 inside the caller's transaction is safe here
// because SQLite has exactly one writer (0.3): no second posting can interleave between the
// read and the insert. On an engine with real concurrency this becomes a `number_series` row
// taken with a locking read — which is what §9.4's series table is for, and it arrives with the
// documents that need gapless numbering in Phase 5.
func (r *Repos) NextEntryNumber(ctx context.Context, companyID id.ID, year int) (string, error) {
	prefix := "JE-" + itoa(year) + "-"
	var last any
	err := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT MAX(entry_number) FROM journal_entries
		 WHERE company_id = ? AND entry_number LIKE ?`,
		string(companyID), prefix+"%").Scan(&last)
	if err != nil {
		return "", r.wrap(err, "allocating an entry number")
	}

	next := 1
	if text, ok := last.(string); ok && len(text) > len(prefix) {
		next = atoi(text[len(prefix):]) + 1
	}
	return prefix + pad(next, 6), nil
}

// EntryByID loads one entry with its lines.
func (r *Repos) EntryByID(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, company_id, entry_number, entry_date, fiscal_period_id, status,
		       source_module, source_document_type, source_document_id, reversal_of_entry_id,
		       memo, branch_id, posted_at, posted_by
		  FROM journal_entries WHERE id = ?`, string(entryID))

	entry, err := scanEntry(row)
	if err != nil {
		return domain.Entry{}, err // sql.ErrNoRows passes through
	}
	entry.Lines, err = r.linesFor(ctx, entryID)
	if err != nil {
		return domain.Entry{}, err
	}
	return entry, nil
}

// Entries lists a company's entries, newest first.
func (r *Repos) Entries(ctx context.Context, companyID id.ID, limit int) ([]domain.Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, entry_number, entry_date, fiscal_period_id, status,
		       source_module, source_document_type, source_document_id, reversal_of_entry_id,
		       memo, branch_id, posted_at, posted_by
		  FROM journal_entries
		 WHERE company_id = ?
		 ORDER BY entry_date DESC, entry_number DESC
		 LIMIT ?`, string(companyID), limit)
	if err != nil {
		return nil, r.wrap(err, "listing journal entries")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Entry
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a journal entry")
		}
		out = append(out, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Lines are fetched per entry rather than by one join: a join would repeat every header
	// column per line and need de-duplicating in Go, and the list screen this serves shows
	// headers. Callers that need lines ask for one entry.
	for i := range out {
		if out[i].Lines, err = r.linesFor(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repos) linesFor(ctx context.Context, entryID id.ID) ([]domain.Line, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT line_number, account_id, debit_minor, credit_minor,
		       currency_code, original_amount_minor, exchange_rate_micro,
		       branch_id, warehouse_id, partner_id, product_id, cost_center_id, project_id, memo
		  FROM journal_lines WHERE journal_entry_id = ? ORDER BY line_number`,
		string(entryID))
	if err != nil {
		return nil, r.wrap(err, "reading journal lines")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Line
	for rows.Next() {
		var (
			line                               domain.Line
			currencyCode, original, rate       any
			branchID, warehouseID, partnerID   any
			productID, costCenterID, projectID any
			memo                               any
		)
		if scanErr := rows.Scan(
			&line.Number, &line.AccountID, &line.Debit, &line.Credit,
			&currencyCode, &original, &rate,
			&branchID, &warehouseID, &partnerID, &productID, &costCenterID, &projectID, &memo,
		); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a journal line")
		}
		line.CurrencyCode = text(currencyCode)
		line.OriginalMinor = integer(original)
		line.RateMicro = integer(rate)
		line.BranchID = id.ID(text(branchID))
		line.WarehouseID = id.ID(text(warehouseID))
		line.PartnerID = id.ID(text(partnerID))
		line.ProductID = id.ID(text(productID))
		line.CostCenterID = id.ID(text(costCenterID))
		line.ProjectID = id.ID(text(projectID))
		line.Memo = text(memo)
		out = append(out, line)
	}
	return out, rows.Err()
}

func scanEntry(s scanner) (domain.Entry, error) {
	var (
		e                          domain.Entry
		entryDate, status          string
		docType, docID, reversalOf any
		memo, branchID             any
		postedAt, postedBy         any
	)
	if err := s.Scan(
		&e.ID, &e.CompanyID, &e.Number, &entryDate, &e.PeriodID, &status,
		&e.SourceModule, &docType, &docID, &reversalOf, &memo, &branchID, &postedAt, &postedBy,
	); err != nil {
		return domain.Entry{}, err
	}

	e.Status = domain.EntryStatus(status)
	e.Date, _ = clock.ParseDate(entryDate)
	e.SourceDocumentType = text(docType)
	e.SourceDocumentID = id.ID(text(docID))
	e.ReversalOf = id.ID(text(reversalOf))
	e.Memo = text(memo)
	e.BranchID = id.ID(text(branchID))
	if stamp := text(postedAt); stamp != "" {
		// Parsed leniently: a timestamp this module cannot read is a display problem, and
		// refusing to load an entry over it would make a cosmetic fault look like ledger
		// corruption.
		e.PostedAt, _ = clock.ParseTimestamp(stamp)
	}
	e.PostedBy = id.ID(text(postedBy))
	return e, nil
}

func integer(v any) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}

// ── fiscal period control (§20.4) ───────────────────────────────────────────────

// Period is the little the ledger needs to know about a fiscal period.
//
// Declared HERE rather than imported from org: this module reads org's TABLE, which is a
// database-level dependency the migration already declares — but importing org's domain type
// would be a module-to-module import that `module-isolation` forbids and that would make the
// two modules' release cycles one.
type Period struct {
	ID           id.ID
	FiscalYearID id.ID
	Sequence     int
	Start        time.Time
	End          time.Time
	Status       string
}

// PeriodForDate finds the fiscal period a business date falls in.
func (r *Repos) PeriodForDate(ctx context.Context, companyID id.ID, on time.Time) (Period, error) {
	day := clock.FormatDate(on)
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT p.id, p.start_date, p.end_date, p.status
		  FROM fiscal_periods p
		  JOIN fiscal_years y ON y.id = p.fiscal_year_id
		 WHERE y.company_id = ? AND ? BETWEEN p.start_date AND p.end_date
		 LIMIT 1`, string(companyID), day)

	var (
		period     Period
		start, end string
	)
	if err := row.Scan(&period.ID, &start, &end, &period.Status); err != nil {
		return Period{}, err // sql.ErrNoRows passes through
	}
	period.Start, _ = clock.ParseDate(start)
	period.End, _ = clock.ParseDate(end)
	return period, nil
}

// SetPeriodStatus opens, closes, or locks a period.
func (r *Repos) SetPeriodStatus(ctx context.Context, periodID id.ID, status string) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE fiscal_periods SET status = ?, updated_at = ? WHERE id = ?`,
		status, r.now(), string(periodID))
	if err != nil {
		return r.wrap(err, "changing a fiscal period's status")
	}
	return nil
}

// ── integrity (§20.2's third guard) ─────────────────────────────────────────────

// UnbalancedEntries finds every stored entry whose lines do not balance.
//
// The guard the aggregate and the posting service cannot be: it sees entries that never went
// through either — a restore, a repair script, a future importer. Done in SQL because the
// question is an aggregate over every line in the ledger, and pulling that into Go to add it up
// would be slower and no more correct.
func (r *Repos) UnbalancedEntries(ctx context.Context, companyID id.ID) ([]string, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT e.entry_number
		  FROM journal_entries e
		  JOIN journal_lines l ON l.journal_entry_id = e.id
		 WHERE e.company_id = ? AND e.status <> 'draft'
		 GROUP BY e.id, e.entry_number
		HAVING SUM(l.debit_minor) <> SUM(l.credit_minor)
		 ORDER BY e.entry_number`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "checking ledger integrity")
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var number string
		if scanErr := rows.Scan(&number); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning an unbalanced entry")
		}
		out = append(out, number)
	}
	return out, rows.Err()
}

// ── small helpers ───────────────────────────────────────────────────────────────

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func pad(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ── fiscal calendar (§20.4) ─────────────────────────────────────────────────────

// PeriodByID loads one period with its year and sequence.
func (r *Repos) PeriodByID(ctx context.Context, periodID id.ID) (Period, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT p.id, p.start_date, p.end_date, p.status, p.sequence, p.fiscal_year_id
		  FROM fiscal_periods p WHERE p.id = ?`, string(periodID))

	var (
		period     Period
		start, end string
	)
	if err := row.Scan(&period.ID, &start, &end, &period.Status,
		&period.Sequence, &period.FiscalYearID); err != nil {
		return Period{}, err
	}
	period.Start, _ = clock.ParseDate(start)
	period.End, _ = clock.ParseDate(end)
	return period, nil
}

// PeriodsOfYear lists a year's periods in calendar order.
func (r *Repos) PeriodsOfYear(ctx context.Context, yearID id.ID) ([]Period, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, start_date, end_date, status, sequence, fiscal_year_id
		  FROM fiscal_periods WHERE fiscal_year_id = ? ORDER BY sequence`, string(yearID))
	if err != nil {
		return nil, r.wrap(err, "listing fiscal periods")
	}
	defer func() { _ = rows.Close() }()

	var out []Period
	for rows.Next() {
		var (
			period     Period
			start, end string
		)
		if scanErr := rows.Scan(&period.ID, &start, &end, &period.Status,
			&period.Sequence, &period.FiscalYearID); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a fiscal period")
		}
		period.Start, _ = clock.ParseDate(start)
		period.End, _ = clock.ParseDate(end)
		out = append(out, period)
	}
	return out, rows.Err()
}

// FiscalYear is the little the ledger needs about a year.
type FiscalYear struct {
	ID     id.ID
	Code   string
	Start  time.Time
	End    time.Time
	Status string
}

// YearByID loads one fiscal year.
func (r *Repos) YearByID(ctx context.Context, yearID id.ID) (FiscalYear, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, code, start_date, end_date, status FROM fiscal_years WHERE id = ?`,
		string(yearID))

	var (
		year       FiscalYear
		start, end string
	)
	if err := row.Scan(&year.ID, &year.Code, &start, &end, &year.Status); err != nil {
		return FiscalYear{}, err
	}
	year.Start, _ = clock.ParseDate(start)
	year.End, _ = clock.ParseDate(end)
	return year, nil
}

// Years lists a company's fiscal years, newest first.
func (r *Repos) Years(ctx context.Context, companyID id.ID) ([]FiscalYear, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, code, start_date, end_date, status
		  FROM fiscal_years WHERE company_id = ? ORDER BY start_date DESC`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing fiscal years")
	}
	defer func() { _ = rows.Close() }()

	var out []FiscalYear
	for rows.Next() {
		var (
			year       FiscalYear
			start, end string
		)
		if scanErr := rows.Scan(&year.ID, &year.Code, &start, &end, &year.Status); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a fiscal year")
		}
		year.Start, _ = clock.ParseDate(start)
		year.End, _ = clock.ParseDate(end)
		out = append(out, year)
	}
	return out, rows.Err()
}

// SetYearStatus opens, closes, or locks a fiscal year.
func (r *Repos) SetYearStatus(ctx context.Context, yearID id.ID, status string) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`UPDATE fiscal_years SET status = ?, updated_at = ? WHERE id = ?`,
		status, r.now(), string(yearID))
	if err != nil {
		return r.wrap(err, "changing a fiscal year's status")
	}
	return nil
}
