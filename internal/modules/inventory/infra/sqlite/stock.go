// Package sqlite is the inventory module's persistence.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "inventory.storage"

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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ── movements: append only ──────────────────────────────────────────────────────

// InsertMovement appends to the ledger.
//
// There is deliberately NO UpdateMovement and no DeleteMovement in this file. The ledger is the
// source of truth, and a source of truth that can be edited is a source of truth nobody can
// rely on — the same rule the audit trail follows (1.7) and the journal follows (Phase 2). A
// mistake is corrected by a compensating movement, which leaves both the error and the
// correction visible.
func (r *Repos) InsertMovement(
	ctx context.Context, companyID id.ID, m domain.Movement, actorID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_movements (
			id, company_id, warehouse_id, product_id, variant_id, movement_type,
			quantity_micro, unit_cost_micro, value_minor,
			balance_after_micro, average_after_micro, source_movement_id,
			document_type, document_id, document_line_id, reason_code, reason,
			occurred_at, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(m.ID), string(companyID), string(m.WarehouseID), string(m.ProductID),
		string(m.VariantID), string(m.Type),
		m.QuantityMicro, m.UnitCostMicro, m.ValueMinor,
		m.BalanceAfterMicro, m.AverageAfterMicro, nullableID(m.SourceMovementID),
		nullable(m.DocumentType), nullableID(m.DocumentID), nullableID(m.DocumentLineID),
		nullable(m.ReasonCode), nullable(m.Reason),
		m.OccurredAt, r.now(), nullableID(actorID))
	if err != nil {
		return r.wrap(err, "appending a stock movement")
	}
	return nil
}

const movementColumns = `
	id, warehouse_id, product_id, variant_id, movement_type,
	quantity_micro, unit_cost_micro, value_minor,
	balance_after_micro, average_after_micro, source_movement_id,
	document_type, document_id, document_line_id, reason_code, reason, occurred_at`

// Movements lists one variant's ledger in a warehouse, in occurrence order.
//
// Ordered by `occurred_at` then `created_at`: when the stock actually moved decides the
// valuation, and the write order breaks the tie for two movements on the same instant. A replay
// in the wrong order produces a different average, so the ordering is part of the contract
// rather than a convenience.
func (r *Repos) Movements(
	ctx context.Context, variantID, warehouseID id.ID,
) ([]domain.Movement, error) {
	return r.movements(ctx,
		`SELECT `+movementColumns+` FROM stock_movements
		 WHERE variant_id = ? AND warehouse_id = ?
		 ORDER BY occurred_at, created_at, id`,
		string(variantID), string(warehouseID))
}

// MovementsForCompany lists a company's whole ledger, for the verifier.
func (r *Repos) MovementsForCompany(
	ctx context.Context, companyID id.ID,
) ([]domain.Movement, error) {
	return r.movements(ctx,
		`SELECT `+movementColumns+` FROM stock_movements
		 WHERE company_id = ? ORDER BY variant_id, warehouse_id, occurred_at, created_at, id`,
		string(companyID))
}

// MovementByID reads one movement — what a return needs to find its original.
func (r *Repos) MovementByID(
	ctx context.Context, movementID id.ID,
) (domain.Movement, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+movementColumns+` FROM stock_movements WHERE id = ?`, string(movementID))

	m, err := scanMovement(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Movement{}, false, nil
	}
	if err != nil {
		return domain.Movement{}, false, r.wrap(err, "reading a stock movement")
	}
	return m, true, nil
}

func (r *Repos) movements(
	ctx context.Context, query string, args ...any,
) ([]domain.Movement, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing stock movements")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Movement, 0, 32)
	for rows.Next() {
		m, scanErr := scanMovement(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a stock movement")
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing stock movements")
	}
	return out, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanMovement(s scanner) (domain.Movement, error) {
	var (
		m                             domain.Movement
		movementType                  string
		source, docType, docID        any
		docLineID, reasonCode, reason any
	)
	if err := s.Scan(&m.ID, &m.WarehouseID, &m.ProductID, &m.VariantID, &movementType,
		&m.QuantityMicro, &m.UnitCostMicro, &m.ValueMinor,
		&m.BalanceAfterMicro, &m.AverageAfterMicro, &source,
		&docType, &docID, &docLineID, &reasonCode, &reason, &m.OccurredAt); err != nil {
		return domain.Movement{}, err
	}
	m.Type = domain.Type(movementType)
	m.SourceMovementID = id.ID(text(source))
	m.DocumentType = text(docType)
	m.DocumentID = id.ID(text(docID))
	m.DocumentLineID = id.ID(text(docLineID))
	m.ReasonCode = text(reasonCode)
	m.Reason = text(reason)
	return m, nil
}

// ── levels: the projection ──────────────────────────────────────────────────────

// Level is a stored stock level.
type Level struct {
	ID          id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	State       domain.State
}

// LevelFor reads a level, locking it for the transaction.
//
// # The level row is the lock
//
// Two concurrent issues of the last item must not both succeed. SQLite serialises writers, so
// reading the level inside the write transaction and updating it there is enough — but the read
// must happen through the WRITER connection, not the reader, or the second transaction sees a
// stale on-hand and oversells.
func (r *Repos) LevelFor(
	ctx context.Context, variantID, warehouseID id.ID,
) (Level, bool, error) {
	row := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT id, warehouse_id, product_id, variant_id,
		       qty_on_hand_micro, qty_reserved_micro, avg_cost_micro
		FROM stock_levels WHERE variant_id = ? AND warehouse_id = ?`,
		string(variantID), string(warehouseID))

	var level Level
	err := row.Scan(&level.ID, &level.WarehouseID, &level.ProductID, &level.VariantID,
		&level.State.OnHandMicro, &level.State.ReservedMicro, &level.State.AverageMicro)
	if errors.Is(err, sql.ErrNoRows) {
		// A variant that has never moved in this warehouse. Not an error — it is the ordinary
		// case for most of a catalog.
		return Level{}, false, nil
	}
	if err != nil {
		return Level{}, false, r.wrap(err, "reading a stock level")
	}
	return level, true, nil
}

// UpsertLevel writes the projection after a movement.
func (r *Repos) UpsertLevel(
	ctx context.Context, companyID id.ID, level Level, lastMovementID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_levels (
			id, company_id, warehouse_id, product_id, variant_id,
			qty_on_hand_micro, qty_reserved_micro, avg_cost_micro, last_movement_id,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT (variant_id, warehouse_id) DO UPDATE SET
			qty_on_hand_micro = excluded.qty_on_hand_micro,
			qty_reserved_micro = excluded.qty_reserved_micro,
			avg_cost_micro = excluded.avg_cost_micro,
			last_movement_id = excluded.last_movement_id,
			row_version = stock_levels.row_version + 1,
			updated_at = excluded.updated_at`,
		string(level.ID), string(companyID), string(level.WarehouseID),
		string(level.ProductID), string(level.VariantID),
		level.State.OnHandMicro, level.State.ReservedMicro, level.State.AverageMicro,
		nullableID(lastMovementID), now, now)
	if err != nil {
		return r.wrap(err, "writing a stock level")
	}
	return nil
}

// Levels lists a company's stock, optionally narrowed to one warehouse or product.
func (r *Repos) Levels(
	ctx context.Context, companyID, warehouseID, productID id.ID,
) ([]Level, error) {
	query := `
		SELECT id, warehouse_id, product_id, variant_id,
		       qty_on_hand_micro, qty_reserved_micro, avg_cost_micro
		FROM stock_levels WHERE company_id = ?`
	args := []any{string(companyID)}

	if !warehouseID.IsZero() {
		query += ` AND warehouse_id = ?`
		args = append(args, string(warehouseID))
	}
	if !productID.IsZero() {
		query += ` AND product_id = ?`
		args = append(args, string(productID))
	}
	query += ` ORDER BY product_id, variant_id, warehouse_id`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing stock levels")
	}
	defer func() { _ = rows.Close() }()

	out := make([]Level, 0, 32)
	for rows.Next() {
		var level Level
		if err = rows.Scan(&level.ID, &level.WarehouseID, &level.ProductID, &level.VariantID,
			&level.State.OnHandMicro, &level.State.ReservedMicro,
			&level.State.AverageMicro); err != nil {
			return nil, r.wrap(err, "reading a stock level")
		}
		out = append(out, level)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing stock levels")
	}
	return out, nil
}

// ── layers: written always, read under FIFO (§D.2) ──────────────────────────────

// InsertLayer records a receipt's cost layer.
//
// Called on EVERY receipt, in average-cost mode exactly as in FIFO. Under WAC nothing reads
// these rows; they exist so that switching a company to FIFO is a configuration change plus a
// recompute rather than a migration that would have to reconstruct purchase history from
// movements — approximate at best, impossible once opening balances exist.
func (r *Repos) InsertLayer(
	ctx context.Context, layerID, companyID id.ID, m domain.Movement,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO inventory_layers (
			id, company_id, warehouse_id, variant_id, source_movement_id, received_at,
			quantity_micro, remaining_micro, unit_cost_micro, is_exhausted,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, ?, ?)`,
		string(layerID), string(companyID), string(m.WarehouseID), string(m.VariantID),
		string(m.ID), m.OccurredAt,
		m.QuantityMicro, m.QuantityMicro, m.UnitCostMicro, now, now)
	if err != nil {
		return r.wrap(err, "recording an inventory layer")
	}
	return nil
}

// Layer is a stored cost layer.
type Layer struct {
	ID             id.ID
	VariantID      id.ID
	WarehouseID    id.ID
	ReceivedAt     string
	QuantityMicro  int64
	RemainingMicro int64
	UnitCostMicro  int64
	IsExhausted    bool
}

// OpenLayers lists a variant's unexhausted layers in receipt order.
//
// The FIFO consumption order. Nothing reads it under WAC, but it is maintained and indexed now
// so that enabling FIFO is a setting rather than a performance investigation.
func (r *Repos) OpenLayers(
	ctx context.Context, variantID, warehouseID id.ID,
) ([]Layer, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, variant_id, warehouse_id, received_at,
		       quantity_micro, remaining_micro, unit_cost_micro, is_exhausted
		FROM inventory_layers
		WHERE variant_id = ? AND warehouse_id = ? AND is_exhausted = 0
		ORDER BY received_at, created_at, id`,
		string(variantID), string(warehouseID))
	if err != nil {
		return nil, r.wrap(err, "listing inventory layers")
	}
	defer func() { _ = rows.Close() }()

	out := make([]Layer, 0, 8)
	for rows.Next() {
		var (
			layer     Layer
			exhausted int
		)
		if err = rows.Scan(&layer.ID, &layer.VariantID, &layer.WarehouseID, &layer.ReceivedAt,
			&layer.QuantityMicro, &layer.RemainingMicro, &layer.UnitCostMicro,
			&exhausted); err != nil {
			return nil, r.wrap(err, "reading an inventory layer")
		}
		layer.IsExhausted = exhausted == 1
		out = append(out, layer)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing inventory layers")
	}
	return out, nil
}

// ConsumeLayers draws a quantity down the open layers in receipt order.
//
// Run on every ISSUE regardless of costing method, for the same reason layers are written on
// every receipt: a company switching to FIFO must start from a truthful remaining-quantity
// picture, not from full layers that were never drawn down. Under WAC the amounts consumed do
// not affect valuation; under FIFO they are the valuation.
func (r *Repos) ConsumeLayers(
	ctx context.Context, variantID, warehouseID id.ID, quantityMicro int64,
) error {
	layers, err := r.OpenLayers(ctx, variantID, warehouseID)
	if err != nil {
		return err
	}

	remaining := quantityMicro
	now := r.now()
	for _, layer := range layers {
		if remaining <= 0 {
			break
		}
		take := layer.RemainingMicro
		if take > remaining {
			take = remaining
		}
		left := layer.RemainingMicro - take
		exhausted := 0
		if left == 0 {
			exhausted = 1
		}
		if _, err = r.db.Writer(ctx).ExecContext(ctx, `
			UPDATE inventory_layers
			SET remaining_micro = ?, is_exhausted = ?, row_version = row_version + 1,
			    updated_at = ?
			WHERE id = ?`, left, exhausted, now, string(layer.ID)); err != nil {
			return r.wrap(err, "consuming an inventory layer")
		}
		remaining -= take
	}
	// A shortfall is NOT an error here. Negative stock is permitted for some companies, and the
	// costing strategy has already decided whether that was allowed and recorded the variance.
	// Refusing here would be a second, quieter rule contradicting the first.
	return nil
}

// WarehouseAllowsNegative reads whether a warehouse tolerates stock below zero.
//
// The column lives on `warehouses` because Phase 1 put it there (migration 0004) with the note
// "Read from Phase 4" — and per-warehouse is the right grain: a bonded store may permit what the
// shop floor must not.
//
// A warehouse that does not exist reads as FALSE rather than as an error. The movement will fail
// on its foreign key a moment later with a message about the warehouse, which is clearer than a
// costing question failing first.
func (r *Repos) WarehouseAllowsNegative(
	ctx context.Context, warehouseID id.ID,
) (bool, error) {
	var allows int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT allows_negative_stock FROM warehouses WHERE id = ?`,
		string(warehouseID)).Scan(&allows)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, r.wrap(err, "reading a warehouse's negative-stock policy")
	}
	return allows == 1, nil
}

// ── lots and serials ────────────────────────────────────────────────────────────

// InsertLot records a batch.
func (r *Repos) InsertLot(ctx context.Context, companyID id.ID, l domain.Lot) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_lots (
			id, company_id, variant_id, lot_number, expires_on, manufactured_on,
			supplier_id, is_quarantined, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(companyID), string(l.VariantID), l.Number,
		nullable(l.ExpiresOn), nullable(l.ManufacturedOn), nullableID(l.SupplierID),
		boolToInt(l.IsQuarantined), boolToInt(l.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "recording a lot")
	}
	return nil
}

// LotByNumber finds a batch by the number printed on the box.
func (r *Repos) LotByNumber(
	ctx context.Context, variantID id.ID, number string,
) (domain.Lot, bool, error) {
	var (
		l                       domain.Lot
		expires, made, supplier any
		quarantined, active     int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, variant_id, lot_number, expires_on, manufactured_on, supplier_id,
		       is_quarantined, is_active
		FROM stock_lots WHERE variant_id = ? AND lot_number = ?`,
		string(variantID), number).
		Scan(&l.ID, &l.VariantID, &l.Number, &expires, &made, &supplier,
			&quarantined, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Lot{}, false, nil
	}
	if err != nil {
		return domain.Lot{}, false, r.wrap(err, "reading a lot")
	}
	l.ExpiresOn = text(expires)
	l.ManufacturedOn = text(made)
	l.SupplierID = id.ID(text(supplier))
	l.IsQuarantined = quarantined == 1
	l.IsActive = active == 1
	return l, true, nil
}

// LotStock lists a variant's lots in a warehouse, with what is left of each.
//
// Ordered by expiry so that a caller which forgets to sort still picks sensibly — but the FEFO
// decision belongs to the domain's Pick, which sorts again. Two orderings sounds redundant; it
// is not. This one is an index-friendly default for the query, and that one is the RULE, tested
// against a table. If the query's order were the only one, the rule would be untestable without
// a database.
func (r *Repos) LotStock(
	ctx context.Context, variantID, warehouseID id.ID,
) ([]domain.LotStock, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.id, l.variant_id, l.lot_number, l.expires_on, l.manufactured_on,
		       l.supplier_id, l.is_quarantined, l.is_active, ll.qty_on_hand_micro
		FROM stock_lot_levels ll
		JOIN stock_lots l ON l.id = ll.lot_id
		WHERE ll.variant_id = ? AND ll.warehouse_id = ? AND ll.qty_on_hand_micro > 0
		ORDER BY l.expires_on IS NULL, l.expires_on, l.created_at`,
		string(variantID), string(warehouseID))
	if err != nil {
		return nil, r.wrap(err, "listing lot stock")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.LotStock, 0, 8)
	for rows.Next() {
		var (
			entry                   domain.LotStock
			expires, made, supplier any
			quarantined, active     int
		)
		if err = rows.Scan(&entry.Lot.ID, &entry.Lot.VariantID, &entry.Lot.Number,
			&expires, &made, &supplier, &quarantined, &active,
			&entry.QuantityMicro); err != nil {
			return nil, r.wrap(err, "reading lot stock")
		}
		entry.Lot.ExpiresOn = text(expires)
		entry.Lot.ManufacturedOn = text(made)
		entry.Lot.SupplierID = id.ID(text(supplier))
		entry.Lot.IsQuarantined = quarantined == 1
		entry.Lot.IsActive = active == 1
		out = append(out, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing lot stock")
	}
	return out, nil
}

// AdjustLotLevel folds a movement into the lot-grain projection.
func (r *Repos) AdjustLotLevel(
	ctx context.Context, levelID, companyID, warehouseID, variantID, lotID id.ID, delta int64,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_lot_levels (
			id, company_id, warehouse_id, variant_id, lot_id, qty_on_hand_micro,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT (lot_id, warehouse_id) DO UPDATE SET
			qty_on_hand_micro = stock_lot_levels.qty_on_hand_micro + excluded.qty_on_hand_micro,
			row_version = stock_lot_levels.row_version + 1,
			updated_at = excluded.updated_at`,
		string(levelID), string(companyID), string(warehouseID), string(variantID),
		string(lotID), delta, now, now)
	if err != nil {
		return r.wrap(err, "updating a lot's level")
	}
	return nil
}

// InsertSerial records one physical unit.
func (r *Repos) InsertSerial(ctx context.Context, companyID id.ID, s domain.Serial) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_serials (
			id, company_id, variant_id, serial_number, lot_id, warehouse_id, status,
			partner_id, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(s.ID), string(companyID), string(s.VariantID), s.Number,
		nullableID(s.LotID), nullableID(s.WarehouseID), s.Status,
		nullableID(s.PartnerID), now, now)
	if err != nil {
		return r.wrap(err, "recording a serial")
	}
	return nil
}

// SerialByNumber finds one physical unit.
func (r *Repos) SerialByNumber(
	ctx context.Context, companyID id.ID, number string,
) (domain.Serial, bool, error) {
	var (
		s                       domain.Serial
		lot, warehouse, partner any
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, variant_id, serial_number, lot_id, warehouse_id, status, partner_id
		FROM stock_serials WHERE company_id = ? AND serial_number = ?`,
		string(companyID), number).
		Scan(&s.ID, &s.VariantID, &s.Number, &lot, &warehouse, &s.Status, &partner)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Serial{}, false, nil
	}
	if err != nil {
		return domain.Serial{}, false, r.wrap(err, "reading a serial")
	}
	s.LotID = id.ID(text(lot))
	s.WarehouseID = id.ID(text(warehouse))
	s.PartnerID = id.ID(text(partner))
	return s, true, nil
}

// SetSerialStatus moves a serial's state and location.
//
// The row is never deleted: a serial that has been sold must still be findable for the warranty
// claim two years later, and a deleted row cannot be found.
func (r *Repos) SetSerialStatus(
	ctx context.Context, serialID id.ID, status string, warehouseID, partnerID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE stock_serials SET status = ?, warehouse_id = ?, partner_id = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		status, nullableID(warehouseID), nullableID(partnerID), r.now(), string(serialID))
	if err != nil {
		return r.wrap(err, "changing a serial's status")
	}
	return nil
}
