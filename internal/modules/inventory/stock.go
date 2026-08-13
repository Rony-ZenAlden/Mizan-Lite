package inventory

import (
	"context"
	"math/big"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
	"github.com/mizan-erp/mizan/internal/modules/inventory/infra/sqlite"
)

// MoveInput is one stock movement.
//
// The caller says WHAT moved and why; costing decides what it was worth, and the ledger decides
// what the balance became. A caller cannot assert either.
type MoveInput struct {
	CompanyID   id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	Type        domain.Type

	// QuantityMicro is always positive; Type carries the direction.
	QuantityMicro int64
	// UnitCostMicro is what was paid, and is read only for movements that bring value IN. An
	// issue is costed at the average, not at whatever the caller passes — otherwise a sale
	// could set its own cost of goods and gross margin would become an opinion.
	UnitCostMicro int64

	// SourceMovementID is the issue a return reverses (§D.3). Required for a return.
	SourceMovementID id.ID

	// LotID and SerialID are required when the product's tracking mode demands them, and
	// refused when it does not.
	LotID    id.ID
	SerialID id.ID
	// CountedMicro is the figure a physical count found. Only read for a count.
	CountedMicro int64

	DocumentType   string
	DocumentID     id.ID
	DocumentLineID id.ID
	ReasonCode     string
	Reason         string
	// OccurredAt is when the stock actually moved, which is not when the row is written: a
	// delivery note entered on Monday for goods received on Friday belongs in Friday's
	// valuation. Empty means now.
	OccurredAt string
}

// Move records one stock movement and updates the projection.
//
// # One transaction, three writes
//
// The movement, the level, and the layer are written together. A crash between them would leave
// a ledger that says one thing and a projection that says another — the exact drift the verifier
// exists to catch, manufactured by the system itself.
//
// # The level row is the lock
//
// The level is read through the WRITER connection inside the transaction, so two concurrent
// issues of the last item serialise. Reading it from the reader would let the second transaction
// see a stale on-hand and oversell — and the oversell would be invisible, because both movements
// would record a plausible balance.
func (s *Service) Move(ctx context.Context, in MoveInput) (domain.Movement, error) {
	if err := s.requirePublisher(); err != nil {
		return domain.Movement{}, err
	}

	var recorded domain.Movement
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		movement, err := s.moveWithin(txCtx, in)
		if err != nil {
			return err
		}
		recorded = movement
		return nil
	})
	if err != nil {
		return domain.Movement{}, err
	}
	return recorded, nil
}

// actionFor maps a movement type to its audited action.
func actionFor(t domain.Type) string {
	switch t {
	case domain.Receipt, domain.ReturnIn, domain.TransferIn:
		return ActionStockReceived
	case domain.Issue, domain.TransferOut:
		return ActionStockIssued
	case domain.Count:
		return ActionStockCounted
	default:
		return ActionStockAdjusted
	}
}

func (s *Service) audit(ctx context.Context, a auditc.Auditable) error {
	if err := s.requirePublisher(); err != nil {
		return err
	}
	return s.bus.Publish(ctx, a)
}

// actorOf reads who is acting, for the movement's created_by.
//
// # Why a movement records its author as well as the audit trail
//
// The trail already says who did this, in the same transaction. But `created_by` on the movement
// is what a stock history screen shows beside each row, and a screen that has to join the audit
// trail to name the person who wrote off six units is a screen nobody builds.
//
// Nil-safe: a movement made by a job or by the setup wizard has no user, and recording no actor
// is the truth where a fabricated one is not — the same reasoning ActorResolver states.
func (s *Service) branchOf(ctx context.Context) id.ID {
	if s.actors == nil {
		return id.ID("")
	}
	actor, ok := s.actors.Actor(ctx)
	if !ok {
		return id.ID("")
	}
	return actor.BranchID
}

func (s *Service) actorOf(ctx context.Context) id.ID {
	if s.actors == nil {
		return id.ID("")
	}
	actor, ok := s.actors.Actor(ctx)
	if !ok {
		return id.ID("")
	}
	return actor.UserID
}

// ── reading ─────────────────────────────────────────────────────────────────────

// StockOf reads one variant's level in a warehouse.
func (s *Service) StockOf(
	ctx context.Context, variantID, warehouseID id.ID,
) (domain.State, error) {
	level, found, err := s.repos.LevelFor(ctx, variantID, warehouseID)
	if err != nil {
		return domain.State{}, err
	}
	if !found {
		// A variant that has never moved has no row and no stock, which is a fact rather than
		// an absence: the caller wants zero, not an error to handle.
		return domain.State{}, nil
	}
	return level.State, nil
}

// Levels lists a company's stock, optionally narrowed.
func (s *Service) Levels(
	ctx context.Context, companyID, warehouseID, productID id.ID,
) ([]sqlite.Level, error) {
	return s.repos.Levels(ctx, companyID, warehouseID, productID)
}

// Movements lists one variant's ledger in a warehouse, in occurrence order.
func (s *Service) Movements(
	ctx context.Context, variantID, warehouseID id.ID,
) ([]domain.Movement, error) {
	return s.repos.Movements(ctx, variantID, warehouseID)
}

// ── verification ────────────────────────────────────────────────────────────────

// VerifyLedger replays the movement ledger and compares it with the projection.
//
// # Reports, never repairs
//
// The Phase 2 precedent, unchanged, and the reason bears repeating: a projection that heals
// itself hides the bug that broke it. The next drift is then silent too, and by the time anybody
// looks the evidence has been overwritten by the repair.
//
// This is the job that stands between "our margin looks fine" and a year of quietly wrong
// profit.
func (s *Service) VerifyLedger(
	ctx context.Context, companyID id.ID,
) ([]domain.Discrepancy, error) {
	levels, err := s.repos.Levels(ctx, companyID, id.ID(""), id.ID(""))
	if err != nil {
		return nil, err
	}

	discrepancies := make([]domain.Discrepancy, 0)
	for _, level := range levels {
		movements, movErr := s.repos.Movements(ctx, level.VariantID, level.WarehouseID)
		if movErr != nil {
			return nil, movErr
		}
		discrepancy, differs, verifyErr := domain.Verify(level.State, movements)
		if verifyErr != nil {
			return nil, verifyErr
		}
		if differs {
			discrepancy.VariantID = level.VariantID
			discrepancy.WarehouseID = level.WarehouseID
			discrepancies = append(discrepancies, discrepancy)
		}
	}
	return discrepancies, nil
}

// RebuildLevels recomputes every projection from the ledger.
//
// Separate from VerifyLedger, deliberately, and available only behind its own permission: this
// is the repair, and it must be something a person chooses after seeing a discrepancy report —
// not something that happens automatically and erases the evidence.
func (s *Service) RebuildLevels(ctx context.Context, companyID id.ID) (int, error) {
	if err := s.requirePublisher(); err != nil {
		return 0, err
	}

	var rebuilt int
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		levels, err := s.repos.Levels(txCtx, companyID, id.ID(""), id.ID(""))
		if err != nil {
			return err
		}

		for _, level := range levels {
			movements, movErr := s.repos.Movements(txCtx, level.VariantID, level.WarehouseID)
			if movErr != nil {
				return movErr
			}
			replayed, replayErr := domain.Replay(movements)
			if replayErr != nil {
				return replayErr
			}
			// Reservations are NOT rebuilt: they belong to open orders, not to the stock ledger,
			// and replaying movements says nothing about what is currently promised.
			replayed.ReservedMicro = level.State.ReservedMicro

			var last id.ID
			if len(movements) > 0 {
				last = movements[len(movements)-1].ID
			}
			level.State = replayed
			if err = s.repos.UpsertLevel(txCtx, companyID, level, last); err != nil {
				return err
			}
			rebuilt++
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionStockRebuilt, EntityType: EntityStock, EntityID: companyID,
			After: map[string]any{"levels_rebuilt": rebuilt},
		})
	})
	if err != nil {
		return 0, err
	}
	return rebuilt, nil
}

// RevalueBy changes what stock is worth without moving any of it.
//
// # Why the caller states a VALUE, and inventory decides the average
//
// A purchase bill that disagrees with the order it bills knows one thing: these goods are worth
// N minor units more (or less) than we thought. It does not know — and must not learn — whether
// this business runs weighted average or FIFO, or what the current average is. That is the
// costing strategy's business, and the whole point of the port Phase 4 built.
//
// So the caller supplies a delta and this converts it into whatever the strategy needs. Under
// WAC that is a new average: the delta spread across what is actually on hand.
//
// # Nothing on hand means nothing to revalue
//
// If the goods have all been sold, there is no stock left carrying the wrong cost — the error is
// in a cost of sale that has already posted, and correcting THAT is the caller's decision, not a
// stock movement. Revaluing zero quantity would write a movement that changes nothing and
// implies a correction that did not happen.
func (s *Service) RevalueBy(ctx context.Context, in RevalueInput) (domain.Movement, error) {
	if err := s.requirePublisher(); err != nil {
		return domain.Movement{}, err
	}

	var recorded domain.Movement
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		state, err := s.StockOf(txCtx, in.VariantID, in.WarehouseID)
		if err != nil {
			return err
		}
		if state.OnHandMicro <= 0 {
			// Nothing to revalue. Not an error: it is the ordinary outcome when goods were sold
			// before their invoice arrived.
			return nil
		}

		// The delta, spread across what is on hand, added to the average it already carries.
		// Computed in the same scale the average lives in (10⁻⁶ of the major unit, §E).
		perUnit := deltaPerUnitMicro(in.DeltaMinor, state.OnHandMicro, in.Decimals)

		movement, err := s.moveWithin(txCtx, MoveInput{
			CompanyID: in.CompanyID, WarehouseID: in.WarehouseID,
			ProductID: in.ProductID, VariantID: in.VariantID,
			Type: domain.Revaluation,
			// A revaluation moves no quantity. Its direction is Neutral and the ledger's fold
			// leaves the balance alone — which is exactly why Phase 4 gave it its own direction
			// rather than treating it as an inward move of zero.
			QuantityMicro: 0,
			UnitCostMicro: state.AverageMicro + perUnit,
			DocumentType:  in.DocumentType, DocumentID: in.DocumentID,
			OccurredAt: in.OccurredAt,
		})
		if err != nil {
			return err
		}
		recorded = movement
		return nil
	})
	if err != nil {
		return domain.Movement{}, err
	}
	return recorded, nil
}

// RevalueInput asks for a revaluation.
type RevalueInput struct {
	CompanyID   id.ID
	WarehouseID id.ID
	ProductID   id.ID
	VariantID   id.ID
	// DeltaMinor is how much more (or less) the stock on hand is worth.
	DeltaMinor int64
	// Decimals is the currency's minor-unit scale, for converting money into a unit cost.
	Decimals     int
	DocumentType string
	DocumentID   id.ID
	OccurredAt   string
}

// deltaPerUnitMicro converts a money delta into a per-unit cost at the UnitAmount scale.
//
// Money is in minor units (10^decimals of the major unit) and a unit cost is 10⁻⁶ of the MAJOR
// unit, so the conversion is not a division alone — getting it wrong by the currency's scale is
// a hundred-fold error in the direction nobody checks.
// The scales this file converts between (§E).
const (
	quantityScale = 1_000_000
	unitScale     = 1_000_000
)

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func deltaPerUnitMicro(deltaMinor, onHandMicro int64, decimals int) int64 {
	if onHandMicro == 0 {
		return 0
	}
	// delta(minor) → major × 10⁶ → per unit, with the quantity's own 10⁶ cancelling.
	numerator := new(big.Int).Mul(big.NewInt(deltaMinor), big.NewInt(unitScale*quantityScale))
	numerator.Div(numerator, pow10(decimals))
	return new(big.Int).Div(numerator, big.NewInt(onHandMicro)).Int64()
}
