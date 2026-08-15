// Package sqlite is the expenses module's storage.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the coded error a storage fault surfaces as.
const CodeStorage = "expenses.storage"

// Repos is the module's storage.
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

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return string(v)
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type scanner interface{ Scan(dest ...any) error }

// ── categories ──────────────────────────────────────────────────────────────────

const categoryColumns = `
	id, company_id, code, name, name_key, account_id, tax_recoverable,
	parent_id, sort_order, is_active`

// InsertCategory writes a category.
func (r *Repos) InsertCategory(ctx context.Context, c domain.Category) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO expense_categories (
			id, company_id, code, name, name_key, account_id, tax_recoverable,
			parent_id, sort_order, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(c.ID), string(c.CompanyID), c.Code, c.Name, nullable(c.NameKey),
		string(c.AccountID), boolToInt(c.TaxRecoverable), nullableID(c.ParentID),
		c.SortOrder, boolToInt(c.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting an expense category")
	}
	return nil
}

// CategoryByCode finds a category by its code.
func (r *Repos) CategoryByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Category, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+categoryColumns+` FROM expense_categories
		  WHERE company_id = ? AND code = ?`, string(companyID), code)

	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Category{}, false, nil
	}
	if err != nil {
		return domain.Category{}, false, r.wrap(err, "reading an expense category")
	}
	return category, true, nil
}

// CategoryByID reads one category.
func (r *Repos) CategoryByID(
	ctx context.Context, categoryID id.ID,
) (domain.Category, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+categoryColumns+` FROM expense_categories WHERE id = ?`, string(categoryID))

	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Category{}, false, nil
	}
	if err != nil {
		return domain.Category{}, false, r.wrap(err, "reading an expense category")
	}
	return category, true, nil
}

// Categories lists a company's categories.
func (r *Repos) Categories(
	ctx context.Context, companyID id.ID,
) ([]domain.Category, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+categoryColumns+` FROM expense_categories
		  WHERE company_id = ? ORDER BY sort_order, code`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing expense categories")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Category, 0, 16)
	for rows.Next() {
		category, scanErr := scanCategory(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing expense categories")
		}
		out = append(out, category)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing expense categories")
	}
	return out, nil
}

// AccountByCode finds an account to point a category at.
func (r *Repos) AccountByCode(
	ctx context.Context, companyID id.ID, code string,
) (id.ID, bool, error) {
	var accountID string
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM accounts WHERE company_id = ? AND code = ?`,
		string(companyID), code).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, r.wrap(err, "reading an account")
	}
	return id.ID(accountID), true, nil
}

func scanCategory(s scanner) (domain.Category, error) {
	var (
		category    domain.Category
		nameKey     any
		parentID    any
		recoverable int
		active      int
	)
	if err := s.Scan(
		&category.ID, &category.CompanyID, &category.Code, &category.Name, &nameKey,
		&category.AccountID, &recoverable, &parentID, &category.SortOrder, &active,
	); err != nil {
		return domain.Category{}, err
	}
	category.NameKey = text(nameKey)
	category.ParentID = id.ID(text(parentID))
	category.TaxRecoverable = recoverable == 1
	category.IsActive = active == 1
	return category, nil
}

// ── expenses ────────────────────────────────────────────────────────────────────

const expenseColumns = `
	id, company_id, branch_id, status, document_number, partner_id, payee_name,
	expense_date, reference, description, settlement, paid_method, due_date,
	currency_code, exchange_rate_micro, net_minor, tax_minor, total_minor,
	is_template, recurs_every_months`

// InsertExpense writes a draft expense.
func (r *Repos) InsertExpense(ctx context.Context, e domain.Expense, actorID id.ID) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO expenses (
			id, company_id, branch_id, status, document_number, partner_id, payee_name,
			expense_date, reference, description, settlement, paid_method, due_date,
			currency_code, exchange_rate_micro, net_minor, tax_minor, total_minor,
			is_template, recurs_every_months, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(e.ID), string(e.CompanyID), string(e.BranchID), string(e.Status),
		nullable(e.Number), nullableID(e.PartnerID), e.PayeeName,
		e.ExpenseDate, nullable(e.Reference), nullable(e.Description),
		string(e.Settlement), nullable(string(e.PaidMethod)), nullable(e.DueDate),
		e.CurrencyCode, e.ExchangeRateMicro, e.NetMinor, e.TaxMinor, e.TotalMinor,
		boolToInt(e.IsTemplate), nullableInt(e.RecursEveryMonths),
		now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting an expense")
	}
	return nil
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// ExpenseByID reads one expense.
func (r *Repos) ExpenseByID(
	ctx context.Context, expenseID id.ID,
) (domain.Expense, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+expenseColumns+` FROM expenses WHERE id = ?`, string(expenseID))

	expense, err := scanExpense(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Expense{}, false, nil
	}
	if err != nil {
		return domain.Expense{}, false, r.wrap(err, "reading an expense")
	}
	return expense, true, nil
}

// Expenses lists a company's expenses.
func (r *Repos) Expenses(
	ctx context.Context, companyID id.ID, status domain.Status, templates bool,
) ([]domain.Expense, error) {
	query := `SELECT ` + expenseColumns + ` FROM expenses
	           WHERE company_id = ? AND is_template = ?`
	args := []any{string(companyID), boolToInt(templates)}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY expense_date DESC, created_at DESC`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing expenses")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Expense, 0, 16)
	for rows.Next() {
		expense, scanErr := scanExpense(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing expenses")
		}
		out = append(out, expense)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing expenses")
	}
	return out, nil
}

// UpdateExpense writes an expense's mutable fields.
func (r *Repos) UpdateExpense(ctx context.Context, e domain.Expense, actorID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE expenses SET
			status = ?, document_number = ?, reference = ?, description = ?, due_date = ?,
			net_minor = ?, tax_minor = ?, total_minor = ?,
			posted_at = CASE WHEN ? = 'posted' AND posted_at IS NULL THEN ? ELSE posted_at END,
			posted_by = CASE WHEN ? = 'posted' AND posted_by IS NULL THEN ? ELSE posted_by END,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(e.Status), nullable(e.Number), nullable(e.Reference), nullable(e.Description),
		nullable(e.DueDate), e.NetMinor, e.TaxMinor, e.TotalMinor,
		string(e.Status), r.now(), string(e.Status), nullableID(actorID),
		r.now(), string(e.ID))
	if err != nil {
		return r.wrap(err, "updating an expense")
	}
	return nil
}

func scanExpense(s scanner) (domain.Expense, error) {
	var (
		expense     domain.Expense
		number      any
		partnerID   any
		reference   any
		description any
		method      any
		dueDate     any
		template    int
		recurs      any
	)
	if err := s.Scan(
		&expense.ID, &expense.CompanyID, &expense.BranchID, &expense.Status, &number,
		&partnerID, &expense.PayeeName, &expense.ExpenseDate, &reference, &description,
		&expense.Settlement, &method, &dueDate,
		&expense.CurrencyCode, &expense.ExchangeRateMicro,
		&expense.NetMinor, &expense.TaxMinor, &expense.TotalMinor, &template, &recurs,
	); err != nil {
		return domain.Expense{}, err
	}
	expense.Number = text(number)
	expense.PartnerID = id.ID(text(partnerID))
	expense.Reference = text(reference)
	expense.Description = text(description)
	expense.PaidMethod = domain.Method(text(method))
	expense.DueDate = text(dueDate)
	expense.IsTemplate = template == 1
	if v, ok := recurs.(int64); ok {
		expense.RecursEveryMonths = int(v)
	}
	return expense, nil
}

// ── lines ───────────────────────────────────────────────────────────────────────

const lineColumns = `
	id, line_number, category_id, account_id, category_name, description,
	net_minor, tax_rate_micro, tax_code, tax_amount_minor, tax_recoverable, total_minor`

// InsertLine writes an expense line.
func (r *Repos) InsertLine(ctx context.Context, expenseID id.ID, l domain.Line) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO expense_lines (
			id, expense_id, line_number, category_id, account_id, category_name, description,
			net_minor, tax_rate_micro, tax_code, tax_amount_minor, tax_recoverable, total_minor,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(expenseID), l.LineNumber, string(l.CategoryID),
		string(l.AccountID), l.CategoryName, nullable(l.Description),
		l.NetMinor, l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		boolToInt(l.TaxRecoverable), l.TotalMinor, now, now)
	if err != nil {
		return r.wrap(err, "inserting an expense line")
	}
	return nil
}

// Lines reads an expense's lines.
func (r *Repos) Lines(ctx context.Context, expenseID id.ID) ([]domain.Line, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+lineColumns+` FROM expense_lines
		  WHERE expense_id = ? ORDER BY line_number`, string(expenseID))
	if err != nil {
		return nil, r.wrap(err, "reading expense lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Line, 0, 8)
	for rows.Next() {
		var (
			line        domain.Line
			description any
			taxCode     any
			recoverable int
		)
		if err = rows.Scan(
			&line.ID, &line.LineNumber, &line.CategoryID, &line.AccountID,
			&line.CategoryName, &description, &line.NetMinor, &line.TaxRateMicro,
			&taxCode, &line.TaxAmountMinor, &recoverable, &line.TotalMinor,
		); err != nil {
			return nil, r.wrap(err, "reading expense lines")
		}
		line.Description = text(description)
		line.TaxCode = text(taxCode)
		line.TaxRecoverable = recoverable == 1
		out = append(out, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading expense lines")
	}
	return out, nil
}

// UpdateLine writes a line's computed tax back.
func (r *Repos) UpdateLine(ctx context.Context, l domain.Line) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE expense_lines SET
			tax_rate_micro = ?, tax_code = ?, tax_amount_minor = ?,
			tax_recoverable = ?, total_minor = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		l.TaxRateMicro, nullable(l.TaxCode), l.TaxAmountMinor,
		boolToInt(l.TaxRecoverable), l.TotalMinor, r.now(), string(l.ID))
	if err != nil {
		return r.wrap(err, "updating an expense line")
	}
	return nil
}

// DeleteLine removes a line from a draft.
func (r *Repos) DeleteLine(ctx context.Context, lineID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM expense_lines WHERE id = ?`, string(lineID))
	if err != nil {
		return r.wrap(err, "removing an expense line")
	}
	return nil
}

// NextLineNumber reports the next free position.
func (r *Repos) NextLineNumber(ctx context.Context, expenseID id.ID) (int, error) {
	var highest sql.NullInt64
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT MAX(line_number) FROM expense_lines WHERE expense_id = ?`,
		string(expenseID)).Scan(&highest)
	if err != nil {
		return 0, r.wrap(err, "reading the next line number")
	}
	return int(highest.Int64) + 1, nil
}

// CurrencyDecimals reads a currency's minor-unit scale.
func (r *Repos) CurrencyDecimals(ctx context.Context, code string) (int, error) {
	var decimals int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT decimal_places FROM currencies WHERE code = ?`, code).Scan(&decimals)
	if err != nil {
		return 0, r.wrap(err, "reading a currency's scale")
	}
	return decimals, nil
}
