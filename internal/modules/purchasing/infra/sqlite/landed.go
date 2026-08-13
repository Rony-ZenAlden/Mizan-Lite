package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

const landedColumns = `
	id, company_id, receipt_id, charge_type, description, partner_id, basis,
	currency_code, amount_minor, status`

// InsertLandedCost writes a charge.
func (r *Repos) InsertLandedCost(
	ctx context.Context, c domain.LandedCost, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO landed_costs (
			id, company_id, receipt_id, charge_type, description, partner_id, basis,
			currency_code, amount_minor, status, row_version, created_at, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		string(c.ID), string(c.CompanyID), string(c.ReceiptID), c.ChargeType,
		nullable(c.Description), nullableID(c.PartnerID), string(c.Basis),
		c.CurrencyCode, c.AmountMinor, string(c.Status), now, nullableID(actorID), now)
	if err != nil {
		return r.wrap(err, "inserting a landed cost")
	}
	return nil
}

// LandedCostByID reads one charge.
func (r *Repos) LandedCostByID(
	ctx context.Context, chargeID id.ID,
) (domain.LandedCost, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+landedColumns+` FROM landed_costs WHERE id = ?`, string(chargeID))

	charge, err := scanLandedCost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LandedCost{}, false, nil
	}
	if err != nil {
		return domain.LandedCost{}, false, r.wrap(err, "reading a landed cost")
	}
	return charge, true, nil
}

// LandedCostsFor lists a delivery's charges.
func (r *Repos) LandedCostsFor(
	ctx context.Context, receiptID id.ID,
) ([]domain.LandedCost, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+landedColumns+` FROM landed_costs WHERE receipt_id = ? ORDER BY created_at`,
		string(receiptID))
	if err != nil {
		return nil, r.wrap(err, "listing landed costs")
	}
	defer func() { _ = rows.Close() }()

	charges := make([]domain.LandedCost, 0, 4)
	for rows.Next() {
		charge, scanErr := scanLandedCost(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "listing landed costs")
		}
		charges = append(charges, charge)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing landed costs")
	}
	return charges, nil
}

// MarkLandedCostApplied closes a charge off.
func (r *Repos) MarkLandedCostApplied(
	ctx context.Context, chargeID id.ID, actorID id.ID,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE landed_costs
		   SET status = 'applied', applied_at = ?, applied_by = ?,
		       row_version = row_version + 1, updated_at = ?
		 WHERE id = ?`,
		now, nullableID(actorID), now, string(chargeID))
	if err != nil {
		return r.wrap(err, "applying a landed cost")
	}
	return nil
}

// InsertAllocation records one line's share.
func (r *Repos) InsertAllocation(
	ctx context.Context, chargeID id.ID, a domain.LandedAllocation,
) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO landed_cost_allocations (
			id, landed_cost_id, receipt_line_id, weight_micro, amount_minor,
			movement_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(identifier), string(chargeID), string(a.ReceiptLineID),
		a.WeightMicro, a.AmountMinor, nullableID(a.MovementID), r.now()); err != nil {
		return r.wrap(err, "recording a landed cost allocation")
	}
	return nil
}

// AllocationsFor reads what each line absorbed.
func (r *Repos) AllocationsFor(
	ctx context.Context, chargeID id.ID,
) ([]domain.LandedAllocation, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT receipt_line_id, weight_micro, amount_minor, movement_id
		   FROM landed_cost_allocations WHERE landed_cost_id = ?`, string(chargeID))
	if err != nil {
		return nil, r.wrap(err, "reading landed cost allocations")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.LandedAllocation, 0, 8)
	for rows.Next() {
		var (
			allocation domain.LandedAllocation
			movementID any
		)
		if err = rows.Scan(&allocation.ReceiptLineID, &allocation.WeightMicro,
			&allocation.AmountMinor, &movementID); err != nil {
			return nil, r.wrap(err, "reading landed cost allocations")
		}
		allocation.MovementID = id.ID(text(movementID))
		out = append(out, allocation)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading landed cost allocations")
	}
	return out, nil
}

func scanLandedCost(s scanner) (domain.LandedCost, error) {
	var (
		charge      domain.LandedCost
		description any
		partnerID   any
	)
	if err := s.Scan(
		&charge.ID, &charge.CompanyID, &charge.ReceiptID, &charge.ChargeType,
		&description, &partnerID, &charge.Basis,
		&charge.CurrencyCode, &charge.AmountMinor, &charge.Status,
	); err != nil {
		return domain.LandedCost{}, err
	}
	charge.Description = text(description)
	charge.PartnerID = id.ID(text(partnerID))
	return charge, nil
}
