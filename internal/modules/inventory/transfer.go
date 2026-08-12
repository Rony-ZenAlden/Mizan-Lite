package inventory

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/inventory/infra/sqlite"
)

// The audited action and codes for transfers.
const (
	ActionStockTransferred = "inventory.stock.transferred"

	CodeSameWarehouse = "inventory.same_warehouse"
)

// TransferInput moves stock between two warehouses of one company.
type TransferInput struct {
	CompanyID id.ID
	FromID    id.ID
	ToID      id.ID
	ProductID id.ID
	VariantID id.ID

	QuantityMicro int64
	Reason        string
	OccurredAt    string
}

// Transfer moves stock from one warehouse to another.
//
// # Two movements, one transaction
//
// A transfer is a `transfer_out` at the source and a `transfer_in` at the destination. They are
// written together, because a crash between them would be the worst state this module can
// produce: stock that has left one warehouse and arrived at none. The ledger would be internally
// consistent per warehouse and the company would simply own less than it did.
//
// # The inbound leg is costed at the SOURCE's average
//
// This is the whole subtlety. If warehouse A holds stock at 100 and warehouse B at 200, moving
// five units and costing the arrival at B's average would credit A with 500 and debit B with
// 1000 — creating 500 of inventory value out of moving a box across town.
//
// So the destination receives at what the goods actually cost where they came from, and blends
// that into its own average. Total company inventory value is then unchanged, which is DoD
// criterion 10 and the property that makes a transfer post nothing to the general ledger.
func (s *Service) Transfer(ctx context.Context, in TransferInput) (out, arrival domain.Movement, err error) {
	if in.FromID == in.ToID {
		// A transfer to the same warehouse would write two movements that cancel, leaving the
		// ledger noisier and the stock unchanged. It is a mistake, not a no-op.
		return domain.Movement{}, domain.Movement{}, errs.Validation(CodeSameWarehouse,
			"a transfer needs two different warehouses")
	}
	if in.FromID.IsZero() || in.ToID.IsZero() {
		return domain.Movement{}, domain.Movement{}, errs.Validation(CodeUnknownWarehouse,
			"a transfer needs a source and a destination")
	}

	occurredAt := in.OccurredAt
	if occurredAt == "" {
		occurredAt = clock.Format(s.clk.Now())
	}

	txErr := s.db.Do(ctx, func(txCtx context.Context) error {
		// The source's average BEFORE the issue, which is what the goods are worth. Read inside
		// the transaction, through the writer, so a concurrent movement cannot change it between
		// the two legs.
		source, found, err := s.repos.LevelFor(txCtx, in.VariantID, in.FromID)
		if err != nil {
			return err
		}
		if !found {
			// Nothing has ever moved here, so there is nothing to send. Reported as insufficient
			// stock rather than as a missing row, because that is what it is from the caller's
			// point of view.
			return errs.Conflict(domain.CodeInsufficient,
				"there is no stock in that warehouse to transfer").
				WithParam("requested", itoa(in.QuantityMicro)).WithParam("available", "0")
		}
		sourceCost := source.State.AverageMicro

		out, err = s.moveWithin(txCtx, MoveInput{
			CompanyID: in.CompanyID, WarehouseID: in.FromID,
			ProductID: in.ProductID, VariantID: in.VariantID,
			Type: domain.TransferOut, QuantityMicro: in.QuantityMicro,
			Reason: in.Reason, OccurredAt: occurredAt,
		})
		if err != nil {
			return err
		}

		arrival, err = s.moveWithin(txCtx, MoveInput{
			CompanyID: in.CompanyID, WarehouseID: in.ToID,
			ProductID: in.ProductID, VariantID: in.VariantID,
			Type: domain.TransferIn, QuantityMicro: in.QuantityMicro,
			// The source's cost, not the destination's. See the doc comment.
			UnitCostMicro: sourceCost,
			Reason:        in.Reason, OccurredAt: occurredAt,
		})
		if err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionStockTransferred, EntityType: EntityStock, EntityID: out.ID,
			After: map[string]any{
				"from": string(in.FromID), "to": string(in.ToID),
				"quantity_micro": in.QuantityMicro, "unit_cost_micro": sourceCost,
			},
		})
	})
	if txErr != nil {
		return domain.Movement{}, domain.Movement{}, txErr
	}
	return out, arrival, nil
}

// moveWithin records one movement inside an EXISTING transaction.
//
// The body Move wraps in `db.Do`, factored out so a transfer's two legs join the caller's
// transaction rather than opening their own. Nesting `db.Do` would either deadlock on the single
// writer or, worse, commit the first leg independently — which is exactly the half-transfer this
// design exists to prevent.
func (s *Service) moveWithin(ctx context.Context, in MoveInput) (domain.Movement, error) {
	strategy, err := s.strategy(ctx, in.WarehouseID)
	if err != nil {
		return domain.Movement{}, err
	}

	movementID, err := id.New()
	if err != nil {
		return domain.Movement{}, err
	}
	movement, err := domain.NewMovement(movementID, in.WarehouseID, in.ProductID,
		in.VariantID, in.Type, in.QuantityMicro)
	if err != nil {
		return domain.Movement{}, err
	}

	movement.UnitCostMicro = in.UnitCostMicro
	movement.SourceMovementID = in.SourceMovementID
	movement.LotID = in.LotID
	movement.SerialID = in.SerialID
	movement.DocumentType = in.DocumentType
	movement.DocumentID = in.DocumentID
	movement.DocumentLineID = in.DocumentLineID
	movement.ReasonCode = in.ReasonCode
	movement.Reason = in.Reason
	movement.OccurredAt = in.OccurredAt
	if movement.OccurredAt == "" {
		movement.OccurredAt = clock.Format(s.clk.Now())
	}
	if movement.Type == domain.Count {
		movement.BalanceAfterMicro = in.CountedMicro
	}

	// The tracking rules, in BOTH directions: a lot-tracked product moving without a lot is
	// untraceable at a recall, and an untracked one moving WITH a lot has recorded something
	// nothing reads while implying traceability that does not exist.
	if s.products != nil {
		mode, modeErr := s.products.TrackingOf(ctx, in.ProductID)
		if modeErr != nil {
			return domain.Movement{}, modeErr
		}
		if err = domain.RequireTracking(mode, movement); err != nil {
			return domain.Movement{}, err
		}
	}

	level, found, err := s.repos.LevelFor(ctx, in.VariantID, in.WarehouseID)
	if err != nil {
		return domain.Movement{}, err
	}
	if !found {
		levelID, idErr := id.New()
		if idErr != nil {
			return domain.Movement{}, idErr
		}
		level = sqlite.Level{
			ID: levelID, WarehouseID: in.WarehouseID,
			ProductID: in.ProductID, VariantID: in.VariantID,
		}
	}

	var original domain.Movement
	if !in.SourceMovementID.IsZero() {
		source, ok, sourceErr := s.repos.MovementByID(ctx, in.SourceMovementID)
		if sourceErr != nil {
			return domain.Movement{}, sourceErr
		}
		if !ok {
			return domain.Movement{}, errs.NotFound(domain.CodeReturnNeedsSource,
				"the movement this reverses does not exist").
				WithParam("id", string(in.SourceMovementID))
		}
		original = source
	}

	result, err := strategy.Cost(level.State, movement, original)
	if err != nil {
		return domain.Movement{}, err
	}
	movement.UnitCostMicro = result.UnitCostMicro
	movement.ValueMinor = result.ValueDeltaMinor
	movement.AverageAfterMicro = result.NewAverageMicro

	after, err := domain.Apply(level.State, movement)
	if err != nil {
		return domain.Movement{}, err
	}
	movement.BalanceAfterMicro = after.OnHandMicro
	after.AverageMicro = result.NewAverageMicro

	if err = s.repos.InsertMovement(ctx, in.CompanyID, movement, s.actorOf(ctx)); err != nil {
		return domain.Movement{}, err
	}

	level.State = after
	if err = s.repos.UpsertLevel(ctx, in.CompanyID, level, movement.ID); err != nil {
		return domain.Movement{}, err
	}

	if movement.Type.IsInward() {
		layerID, layerErr := id.New()
		if layerErr != nil {
			return domain.Movement{}, layerErr
		}
		if err = s.repos.InsertLayer(ctx, layerID, in.CompanyID, movement); err != nil {
			return domain.Movement{}, err
		}
	}
	if movement.Type.IsOutward() {
		if err = s.repos.ConsumeLayers(ctx, in.VariantID, in.WarehouseID,
			movement.QuantityMicro); err != nil {
			return domain.Movement{}, err
		}
	}

	// The lot-grain projection, maintained alongside the coarse one. Both are rebuildable from
	// the same ledger; neither is the truth.
	if !movement.LotID.IsZero() {
		direction, dirErr := domain.DirectionOf(movement.Type)
		if dirErr != nil {
			return domain.Movement{}, dirErr
		}
		if direction != domain.Neutral {
			levelID, levelErr := id.New()
			if levelErr != nil {
				return domain.Movement{}, levelErr
			}
			if err = s.repos.AdjustLotLevel(ctx, levelID, in.CompanyID, in.WarehouseID,
				in.VariantID, movement.LotID,
				int64(direction)*movement.QuantityMicro); err != nil {
				return domain.Movement{}, err
			}
		}
	}

	if err = s.publishPosting(ctx, in.CompanyID, s.branchOf(ctx), movement); err != nil {
		return domain.Movement{}, err
	}

	if err = s.audit(ctx, auditc.Auditable{
		Action: actionFor(movement.Type), EntityType: EntityStock, EntityID: movement.ID,
		After: map[string]any{
			"type": string(movement.Type), "quantity_micro": movement.QuantityMicro,
			"unit_cost_micro": movement.UnitCostMicro, "value_minor": movement.ValueMinor,
			"balance_after_micro": movement.BalanceAfterMicro,
			"variance_minor":      result.VarianceMinor,
		},
	}); err != nil {
		return domain.Movement{}, err
	}
	return movement, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
