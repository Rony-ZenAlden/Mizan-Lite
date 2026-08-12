package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// InsertCount writes a stock count.
func (r *Repos) InsertCount(
	ctx context.Context, companyID id.ID, c domain.StockCount, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_counts (
			id, company_id, warehouse_id, reference, description, status, is_blind,
			snapshot_at, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(c.ID), string(companyID), string(c.WarehouseID), c.Reference,
		nullable(c.Description), string(c.Status), boolToInt(c.IsBlind),
		nullable(c.SnapshotAt), now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "creating a stock count")
	}
	return nil
}

// UpdateCount writes a count's mutable fields.
func (r *Repos) UpdateCount(
	ctx context.Context, c domain.StockCount, actorID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE stock_counts SET status = ?, snapshot_at = ?, applied_at = ?, applied_by = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		string(c.Status), nullable(c.SnapshotAt), nullable(c.AppliedAt),
		nullableID(actorID), r.now(), string(c.ID))
	if err != nil {
		return r.wrap(err, "updating a stock count")
	}
	return nil
}

// CountByReference finds one count.
func (r *Repos) CountByReference(
	ctx context.Context, companyID id.ID, reference string,
) (domain.StockCount, bool, error) {
	var (
		c                 domain.StockCount
		description       any
		snapshot, applied any
		status            string
		blind             int
	)
	err := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT id, warehouse_id, reference, description, status, is_blind,
		       snapshot_at, applied_at
		FROM stock_counts WHERE company_id = ? AND reference = ?`,
		string(companyID), reference).
		Scan(&c.ID, &c.WarehouseID, &c.Reference, &description, &status, &blind,
			&snapshot, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.StockCount{}, false, nil
	}
	if err != nil {
		return domain.StockCount{}, false, r.wrap(err, "reading a stock count")
	}
	c.Description = text(description)
	c.Status = domain.CountStatus(status)
	c.IsBlind = blind == 1
	c.SnapshotAt = text(snapshot)
	c.AppliedAt = text(applied)
	return c, true, nil
}

// InsertCountLine adds a thing to be counted, with the expectation frozen at this moment.
func (r *Repos) InsertCountLine(
	ctx context.Context, countID id.ID, l domain.CountLine,
) error {
	now := r.now()
	var counted any
	if l.CountedMicro != nil {
		counted = *l.CountedMicro
	}
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO stock_count_lines (
			id, count_id, product_id, variant_id, lot_id, expected_micro, counted_micro,
			note, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(countID), string(l.ProductID), string(l.VariantID),
		nullableID(l.LotID), l.ExpectedMicro, counted, nullable(l.Note), now, now)
	if err != nil {
		return r.wrap(err, "adding a stock count line")
	}
	return nil
}

// RecordCounted enters what was found on the shelf.
func (r *Repos) RecordCounted(
	ctx context.Context, lineID id.ID, countedMicro int64, note string, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE stock_count_lines SET counted_micro = ?, note = ?, counted_at = ?,
			counted_by = ?, row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		countedMicro, nullable(note), now, nullableID(actorID), now, string(lineID))
	if err != nil {
		return r.wrap(err, "recording a counted quantity")
	}
	return nil
}

// RecordVariance stores the arithmetic that produced a movement, so the line keeps it.
func (r *Repos) RecordVariance(
	ctx context.Context, lineID id.ID, varianceMicro int64,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE stock_count_lines SET variance_micro = ?, row_version = row_version + 1,
			updated_at = ? WHERE id = ?`,
		varianceMicro, r.now(), string(lineID))
	if err != nil {
		return r.wrap(err, "recording a count variance")
	}
	return nil
}

// CountLines lists a count's lines.
func (r *Repos) CountLines(ctx context.Context, countID id.ID) ([]domain.CountLine, error) {
	rows, err := r.db.Writer(ctx).QueryContext(ctx, `
		SELECT id, product_id, variant_id, lot_id, expected_micro, counted_micro, note
		FROM stock_count_lines WHERE count_id = ? ORDER BY created_at, id`, string(countID))
	if err != nil {
		return nil, r.wrap(err, "listing stock count lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.CountLine, 0, 32)
	for rows.Next() {
		var (
			l         domain.CountLine
			lot, note any
			counted   sql.NullInt64
		)
		if err = rows.Scan(&l.ID, &l.ProductID, &l.VariantID, &lot, &l.ExpectedMicro,
			&counted, &note); err != nil {
			return nil, r.wrap(err, "reading a stock count line")
		}
		l.LotID = id.ID(text(lot))
		l.Note = text(note)
		if counted.Valid {
			// A pointer, because "nobody counted this" and "the counter found none" are
			// different facts and only one of them may be applied.
			value := counted.Int64
			l.CountedMicro = &value
		}
		out = append(out, l)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing stock count lines")
	}
	return out, nil
}

// CountLineByID reads one line, for entering a figure against it.
func (r *Repos) CountLineByID(
	ctx context.Context, lineID id.ID,
) (domain.CountLine, id.ID, bool, error) {
	var (
		l         domain.CountLine
		countID   id.ID
		lot, note any
		counted   sql.NullInt64
	)
	err := r.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT id, count_id, product_id, variant_id, lot_id, expected_micro, counted_micro, note
		FROM stock_count_lines WHERE id = ?`, string(lineID)).
		Scan(&l.ID, &countID, &l.ProductID, &l.VariantID, &lot, &l.ExpectedMicro,
			&counted, &note)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CountLine{}, id.ID(""), false, nil
	}
	if err != nil {
		return domain.CountLine{}, id.ID(""), false, r.wrap(err, "reading a stock count line")
	}
	l.LotID = id.ID(text(lot))
	l.Note = text(note)
	if counted.Valid {
		value := counted.Int64
		l.CountedMicro = &value
	}
	return l, countID, true, nil
}
