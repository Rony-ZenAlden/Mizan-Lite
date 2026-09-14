// Package sqlite is the stock tables: stock_levels and the insert-only stock_ledger.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeCorrupt reports a stored row this build cannot read back.
const CodeCorrupt = "lite.stock.corrupt_row"

// Store implements stock.Store.
//
// The ledger is INSERT-only: TestLedgerIsInsertOnly reads this file and fails on any other statement against it.
type Store struct {
	db  database.DB
	clk clock.Clock
}

// NewStore builds the store.
func NewStore(db database.DB, clk clock.Clock) *Store { return &Store{db: db, clk: clk} }

const levelSelect = `
	SELECT l.product_id, l.on_hand_micro, l.avg_cost_usd_micro, l.last_movement_id, m.seq, l.row_version
	  FROM stock_levels l
	  JOIN stock_ledger m ON m.id = l.last_movement_id`

type scanner interface{ Scan(dest ...any) error }

func scanLevel(row scanner) (domain.Level, error) {
	var (
		l                       domain.Level
		rawProduct, rawMovement string
	)
	if err := row.Scan(&rawProduct, &l.OnHandMicro, &l.AvgCostMicro, &rawMovement, &l.LastSeq, &l.RowVersion); err != nil {
		return domain.Level{}, err
	}
	var err error
	if l.ProductID, err = id.Parse(rawProduct); err != nil {
		return domain.Level{}, corrupt(err)
	}
	if l.LastMovementID, err = id.Parse(rawMovement); err != nil {
		return domain.Level{}, corrupt(err)
	}
	return l, nil
}

func (s *Store) Level(ctx context.Context, productID id.ID) (domain.Level, error) {
	l, err := scanLevel(s.db.Reader(ctx).QueryRowContext(ctx, levelSelect+` WHERE l.product_id = ?`, productID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Level{ProductID: productID}, nil
	}
	if err != nil {
		return domain.Level{}, s.db.Dialect().TranslateError(err)
	}
	return l, nil
}

func (s *Store) Levels(ctx context.Context) ([]domain.Level, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, levelSelect+` ORDER BY l.product_id`)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Level
	for rows.Next() {
		l, err := scanLevel(rows)
		if err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out = append(out, l)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

// SaveLevel inserts a level at version 1, or updates one guarded by its version.
func (s *Store) SaveLevel(ctx context.Context, l domain.Level) (domain.Level, error) {
	now := clock.Format(s.clk.Now())
	if l.RowVersion == 0 {
		_, err := s.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO stock_levels (product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at)
			VALUES (?, ?, ?, ?, 1, ?)`,
			l.ProductID.String(), l.OnHandMicro, l.AvgCostMicro, l.LastMovementID.String(), now)
		if err != nil {
			return domain.Level{}, s.db.Dialect().TranslateError(err)
		}
		l.RowVersion = 1
		return l, nil
	}
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE stock_levels
		   SET on_hand_micro = ?, avg_cost_usd_micro = ?, last_movement_id = ?,
		       row_version = row_version + 1, updated_at = ?
		 WHERE product_id = ? AND row_version = ?`,
		l.OnHandMicro, l.AvgCostMicro, l.LastMovementID.String(), now, l.ProductID.String(), l.RowVersion)
	if err != nil {
		return domain.Level{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return domain.Level{}, err
	}
	l.RowVersion++
	return l, nil
}

const movementColumns = `id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
	on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro,
	entered_currency, entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id, sale_id, sale_line_id`

func scanMovement(row scanner) (domain.Movement, error) {
	var (
		m                                   domain.Movement
		rawID, rawProduct, occurred, kind   string
		currency, reason, note, rev, pairID sql.NullString
		saleID, saleLineID                  sql.NullString
		enteredCost, rate                   sql.NullInt64
	)
	if err := row.Scan(&rawID, &rawProduct, &m.Seq, &m.BusinessDate, &occurred, &kind, &m.QuantityMicro, &m.UnitCostMicro,
		&m.OnHandBeforeMicro, &m.AvgCostBeforeMicro, &m.OnHandAfterMicro, &m.AvgCostAfterMicro,
		&currency, &enteredCost, &rate, &reason, &note, &rev, &pairID, &saleID, &saleLineID); err != nil {
		return domain.Movement{}, err
	}
	var (
		err error
		ok  bool
	)
	if m.ID, err = id.Parse(rawID); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	if m.ProductID, err = id.Parse(rawProduct); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	if m.OccurredAt, ok = clock.ParseTimestamp(occurred); !ok {
		return domain.Movement{}, errs.Internal(CodeCorrupt, "a movement's time does not parse").WithParam("value", occurred)
	}
	if m.ReversesID, err = optionalID(rev); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	if m.PairID, err = optionalID(pairID); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	if m.SaleID, err = optionalID(saleID); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	if m.SaleLineID, err = optionalID(saleLineID); err != nil {
		return domain.Movement{}, corrupt(err)
	}
	m.Kind = domain.Kind(kind)
	m.Entered = domain.Entered{Currency: currency.String, UnitCostMicro: enteredCost.Int64, LocalPerUSDNano: rate.Int64}
	m.Reason = domain.Reason(reason.String)
	m.Note = note.String
	return m, nil
}

func (s *Store) Movement(ctx context.Context, movementID id.ID) (domain.Movement, error) {
	m, err := scanMovement(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+movementColumns+` FROM stock_ledger WHERE id = ?`, movementID.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Movement{}, domain.ErrMovementNotFound()
	}
	if err != nil {
		return domain.Movement{}, s.db.Dialect().TranslateError(err)
	}
	return m, nil
}

func (s *Store) Movements(ctx context.Context, productID id.ID, limit int) ([]domain.Movement, error) {
	var out []domain.Movement
	err := s.each(ctx, `SELECT `+movementColumns+` FROM stock_ledger WHERE product_id = ? ORDER BY seq DESC LIMIT ?`,
		func(m domain.Movement) error { out = append(out, m); return nil }, productID.String(), limit)
	return out, err
}

// SaleMovement returns the movement of a kind (sale or sale_void) for a sale line, or found=false.
func (s *Store) SaleMovement(ctx context.Context, saleLineID id.ID, kind domain.Kind) (domain.Movement, bool, error) {
	m, err := scanMovement(s.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+movementColumns+` FROM stock_ledger WHERE sale_line_id = ? AND kind = ?`, saleLineID.String(), string(kind)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Movement{}, false, nil
	}
	if err != nil {
		return domain.Movement{}, false, s.db.Dialect().TranslateError(err)
	}
	return m, true, nil
}

// EachSaleMovement streams every sale and sale-void movement — the sales verifier's walk.
func (s *Store) EachSaleMovement(ctx context.Context, fn func(domain.Movement) error) error {
	return s.each(ctx, `SELECT `+movementColumns+` FROM stock_ledger WHERE sale_id IS NOT NULL ORDER BY product_id, seq`, fn)
}

// EachMovement streams the ledger in the verifier's order without holding it in memory.
func (s *Store) EachMovement(ctx context.Context, fn func(domain.Movement) error) error {
	return s.each(ctx, `SELECT `+movementColumns+` FROM stock_ledger ORDER BY product_id, seq`, fn)
}

func (s *Store) each(ctx context.Context, q string, fn func(domain.Movement) error, args ...any) error {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, q, args...)
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return s.db.Dialect().TranslateError(err)
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) Append(ctx context.Context, m domain.Movement) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_ledger (`+movementColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID.String(), m.ProductID.String(), m.Seq, m.BusinessDate, clock.Format(m.OccurredAt), string(m.Kind),
		m.QuantityMicro, m.UnitCostMicro, m.OnHandBeforeMicro, m.AvgCostBeforeMicro, m.OnHandAfterMicro,
		m.AvgCostAfterMicro, nullableText(m.Entered.Currency), nullableInt(m.Entered.Currency != "", m.Entered.UnitCostMicro),
		nullableInt(m.Entered.LocalPerUSDNano != 0, m.Entered.LocalPerUSDNano), nullableText(string(m.Reason)),
		nullableText(m.Note), nullableText(m.ReversesID.String()), nullableText(m.PairID.String()),
		nullableText(m.SaleID.String()), nullableText(m.SaleLineID.String()), clock.Format(s.clk.Now()))
	return s.db.Dialect().TranslateError(err)
}

func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableInt(present bool, v int64) any {
	if !present {
		return nil
	}
	return v
}

func optionalID(v sql.NullString) (id.ID, error) {
	if !v.Valid {
		return "", nil
	}
	return id.Parse(v.String)
}

func corrupt(err error) error {
	return errs.Wrap(err, errs.CategoryInternal, CodeCorrupt, "a stock row does not read back")
}
