package inventory

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// The audited actions and codes for tracking.
const (
	ActionLotCreated    = "inventory.lot.created"
	ActionSerialCreated = "inventory.serial.created"

	EntityLot    = "inventory.lot"
	EntitySerial = "inventory.serial"

	CodeUnknownLot    = "inventory.unknown_lot"
	CodeUnknownSerial = "inventory.unknown_serial"
	CodeDuplicateLot  = "inventory.duplicate_lot"
)

// NewLotInput records a batch.
type NewLotInput struct {
	CompanyID      id.ID
	VariantID      id.ID
	Number         string
	ExpiresOn      string
	ManufacturedOn string
	SupplierID     id.ID
}

// CreateLot records a batch.
func (s *Service) CreateLot(ctx context.Context, in NewLotInput) (domain.Lot, error) {
	var created domain.Lot

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.LotByNumber(txCtx, in.VariantID, in.Number); err != nil {
			return err
		} else if found {
			// The same number twice for one variant is a duplicate nobody can tell apart at a
			// recall — which is the one moment lot tracking has to work.
			return errs.Conflict(CodeDuplicateLot,
				"this variant already has a lot with that number").
				WithParam("lot", in.Number)
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		lot, err := domain.NewLot(identifier, in.VariantID, in.Number)
		if err != nil {
			return err
		}
		lot.ExpiresOn = in.ExpiresOn
		lot.ManufacturedOn = in.ManufacturedOn
		lot.SupplierID = in.SupplierID

		if err = s.repos.InsertLot(txCtx, in.CompanyID, lot); err != nil {
			return err
		}
		created = lot

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionLotCreated, EntityType: EntityLot, EntityID: lot.ID,
			After: map[string]any{
				"lot": lot.Number, "expires_on": lot.ExpiresOn,
			},
		})
	})
	if err != nil {
		return domain.Lot{}, err
	}
	return created, nil
}

// LotByNumber finds a batch.
func (s *Service) LotByNumber(
	ctx context.Context, variantID id.ID, number string,
) (domain.Lot, error) {
	lot, found, err := s.repos.LotByNumber(ctx, variantID, number)
	if err != nil {
		return domain.Lot{}, err
	}
	if !found {
		return domain.Lot{}, errs.NotFound(CodeUnknownLot,
			"there is no lot with that number").WithParam("lot", number)
	}
	return lot, nil
}

// LotStock lists a variant's lots in a warehouse, with what is left of each.
func (s *Service) LotStock(
	ctx context.Context, variantID, warehouseID id.ID,
) ([]domain.LotStock, error) {
	return s.repos.LotStock(ctx, variantID, warehouseID)
}

// PickLots chooses which lots to draw a quantity from, first-expired-first-out.
//
// The RULE is the domain's; this loads the candidates and supplies the date. A caller that wants
// to know what a pick would take — a picking list, a screen — calls this and gets an answer it
// can show before anything moves.
func (s *Service) PickLots(
	ctx context.Context, variantID, warehouseID id.ID, quantityMicro int64, on string,
) ([]domain.LotUse, error) {
	available, err := s.repos.LotStock(ctx, variantID, warehouseID)
	if err != nil {
		return nil, err
	}
	if on == "" {
		on = clock.FormatDate(s.clk.Now())
	}
	return domain.Pick(available, quantityMicro, on)
}

// NewSerialInput records one physical unit.
type NewSerialInput struct {
	CompanyID   id.ID
	VariantID   id.ID
	Number      string
	LotID       id.ID
	WarehouseID id.ID
}

// CreateSerial records one physical unit.
func (s *Service) CreateSerial(ctx context.Context, in NewSerialInput) (domain.Serial, error) {
	var created domain.Serial

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		identifier, err := id.New()
		if err != nil {
			return err
		}
		serial, err := domain.NewSerial(identifier, in.VariantID, in.Number)
		if err != nil {
			return err
		}
		serial.LotID = in.LotID
		serial.WarehouseID = in.WarehouseID

		if err = s.repos.InsertSerial(txCtx, in.CompanyID, serial); err != nil {
			return err
		}
		created = serial

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionSerialCreated, EntityType: EntitySerial, EntityID: serial.ID,
			After: map[string]any{"serial": serial.Number},
		})
	})
	if err != nil {
		return domain.Serial{}, err
	}
	return created, nil
}

// SerialByNumber finds one physical unit.
//
// The lookup a repair counter makes: scan the device, find what it is and who bought it. It
// finds SOLD serials too, deliberately — a warranty claim two years later is the whole reason
// the row is kept.
func (s *Service) SerialByNumber(
	ctx context.Context, companyID id.ID, number string,
) (domain.Serial, error) {
	serial, found, err := s.repos.SerialByNumber(ctx, companyID, number)
	if err != nil {
		return domain.Serial{}, err
	}
	if !found {
		return domain.Serial{}, errs.NotFound(CodeUnknownSerial,
			"there is no unit with that serial number").WithParam("serial", number)
	}
	return serial, nil
}

// MoveSerial changes a serial's state and location.
//
// Refuses to issue one that is not in stock. A serial that has been sold keeps its row, so
// "does it exist" and "can we sell it" are different questions and only the second is asked
// here.
func (s *Service) MoveSerial(
	ctx context.Context, companyID id.ID, number, status string,
	warehouseID, partnerID id.ID,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		serial, found, err := s.repos.SerialByNumber(txCtx, companyID, number)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownSerial,
				"there is no unit with that serial number").WithParam("serial", number)
		}
		// Leaving stock requires being in it. Arriving back does not.
		if status == domain.SerialSold || status == domain.SerialInTransit {
			if err = serial.RequireIssuable(); err != nil {
				return err
			}
		}
		return s.repos.SetSerialStatus(txCtx, serial.ID, status, warehouseID, partnerID)
	})
}
