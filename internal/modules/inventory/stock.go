package inventory

import (
	"context"

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
