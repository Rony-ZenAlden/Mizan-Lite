package inventory

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// The audited actions and codes for counts.
const (
	ActionCountOpened    = "inventory.count.opened"
	ActionCountStarted   = "inventory.count.started"
	ActionCountSubmitted = "inventory.count.submitted"
	ActionCountApplied   = "inventory.count.applied"
	ActionCountCancelled = "inventory.count.cancelled"

	EntityCount = "inventory.count"

	CodeUnknownCount     = "inventory.unknown_count"
	CodeUnknownCountLine = "inventory.unknown_count_line"
	CodeDuplicateCount   = "inventory.duplicate_count"
)

// NewCountInput opens a stock count.
type NewCountInput struct {
	CompanyID   id.ID
	WarehouseID id.ID
	Reference   string
	Description string
	// ShowExpected turns OFF the blind count. Named for what it does rather than as
	// `IsBlind bool`, so that the dangerous option must be asked for by name — leaving the
	// field alone gives the safe behaviour.
	ShowExpected bool
}

// OpenCount starts a stock count in draft.
func (s *Service) OpenCount(ctx context.Context, in NewCountInput) (domain.StockCount, error) {
	var created domain.StockCount

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.CountByReference(
			txCtx, in.CompanyID, in.Reference); err != nil {
			return err
		} else if found {
			return errs.Conflict(CodeDuplicateCount,
				"a stock count with this reference already exists").
				WithParam("reference", in.Reference)
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		count, err := domain.NewCount(identifier, in.WarehouseID, in.Reference)
		if err != nil {
			return err
		}
		count.Description = in.Description
		count.IsBlind = !in.ShowExpected

		if err = s.repos.InsertCount(
			txCtx, in.CompanyID, count, s.actorOf(txCtx)); err != nil {
			return err
		}
		created = count

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCountOpened, EntityType: EntityCount, EntityID: count.ID,
			After: map[string]any{
				"reference": count.Reference, "blind": count.IsBlind,
			},
		})
	})
	if err != nil {
		return domain.StockCount{}, err
	}
	return created, nil
}

// BeginCounting freezes the expectations and sends the sheet out.
//
// # The snapshot is the point
//
// Every line's expected quantity is read HERE and frozen. Refreshing it later — at apply time,
// say — would compare the counter's figures against a moving target and turn every concurrent
// sale into a discrepancy the counter never saw.
//
// Lines are created for everything with stock in the warehouse. A count of a warehouse is a
// count of what is IN it, and asking the operator to list the variants first would make a full
// count an afternoon's data entry before anybody reaches a shelf.
func (s *Service) BeginCounting(
	ctx context.Context, companyID id.ID, reference string,
) (domain.StockCount, error) {
	var started domain.StockCount

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		count, err := s.requireCount(txCtx, companyID, reference)
		if err != nil {
			return err
		}

		now := clock.Format(s.clk.Now())
		started, err = count.BeginCounting(now)
		if err != nil {
			return err
		}

		levels, err := s.repos.Levels(txCtx, companyID, count.WarehouseID, id.ID(""))
		if err != nil {
			return err
		}
		for _, level := range levels {
			lineID, lineErr := id.New()
			if lineErr != nil {
				return lineErr
			}
			if err = s.repos.InsertCountLine(txCtx, count.ID, domain.CountLine{
				ID: lineID, ProductID: level.ProductID, VariantID: level.VariantID,
				// FROZEN. Never refreshed.
				ExpectedMicro: level.State.OnHandMicro,
			}); err != nil {
				return err
			}
		}

		if err = s.repos.UpdateCount(txCtx, started, id.ID("")); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCountStarted, EntityType: EntityCount, EntityID: count.ID,
			After: map[string]any{
				"reference": count.Reference, "lines": len(levels), "snapshot_at": now,
			},
		})
	})
	if err != nil {
		return domain.StockCount{}, err
	}
	return started, nil
}

// RecordCount enters what a counter found on one line.
func (s *Service) RecordCount(
	ctx context.Context, lineID id.ID, countedMicro int64, note string,
) error {
	if countedMicro < 0 {
		// A negative count is not a finding, it is a typo: nobody counts minus three boxes.
		return errs.Validation(domain.CodeNegativeCounted,
			"a counted quantity cannot be negative")
	}
	return s.db.Do(ctx, func(txCtx context.Context) error {
		_, _, found, err := s.repos.CountLineByID(txCtx, lineID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownCountLine,
				"there is no count line with that identity")
		}
		return s.repos.RecordCounted(txCtx, lineID, countedMicro, note, s.actorOf(txCtx))
	})
}

// SubmitCount closes entry and opens the variance review.
func (s *Service) SubmitCount(
	ctx context.Context, companyID id.ID, reference string,
) (domain.StockCount, error) {
	return s.transition(ctx, companyID, reference, ActionCountSubmitted,
		func(c domain.StockCount) (domain.StockCount, error) { return c.SubmitForReview() })
}

// CancelCount abandons a count, leaving no movements.
func (s *Service) CancelCount(
	ctx context.Context, companyID id.ID, reference string,
) (domain.StockCount, error) {
	return s.transition(ctx, companyID, reference, ActionCountCancelled,
		func(c domain.StockCount) (domain.StockCount, error) { return c.Cancel() })
}

func (s *Service) transition(
	ctx context.Context, companyID id.ID, reference, action string,
	change func(domain.StockCount) (domain.StockCount, error),
) (domain.StockCount, error) {
	var moved domain.StockCount

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		count, err := s.requireCount(txCtx, companyID, reference)
		if err != nil {
			return err
		}
		moved, err = change(count)
		if err != nil {
			return err
		}
		if err = s.repos.UpdateCount(txCtx, moved, id.ID("")); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: action, EntityType: EntityCount, EntityID: count.ID,
			Before: map[string]any{"status": string(count.Status)},
			After:  map[string]any{"status": string(moved.Status)},
		})
	})
	if err != nil {
		return domain.StockCount{}, err
	}
	return moved, nil
}

// PlanCount reports what applying a count would do, without changing anything.
//
// The preview half, in the shape 3.3's variant generation established. A review screen shows
// "412 agreed, 6 did not" and the six rows, and only then is anything written.
func (s *Service) PlanCount(
	ctx context.Context, companyID id.ID, reference string,
) (domain.CountPlan, error) {
	count, err := s.requireCount(ctx, companyID, reference)
	if err != nil {
		return domain.CountPlan{}, err
	}
	return s.planFor(ctx, companyID, count)
}

func (s *Service) planFor(
	ctx context.Context, companyID id.ID, count domain.StockCount,
) (domain.CountPlan, error) {
	lines, err := s.repos.CountLines(ctx, count.ID)
	if err != nil {
		return domain.CountPlan{}, err
	}
	levels, err := s.repos.Levels(ctx, companyID, count.WarehouseID, id.ID(""))
	if err != nil {
		return domain.CountPlan{}, err
	}
	// CURRENT stock, read now — not the frozen expectation. The variance is what the counter
	// established; this is what it gets added to.
	current := make(map[id.ID]int64, len(levels))
	for _, level := range levels {
		current[level.VariantID] = level.State.OnHandMicro
	}
	return domain.PlanCount(lines, current), nil
}

// ApplyCount writes the movements a count's variances call for.
//
// Re-planned INSIDE the transaction, for the reason 3.3's variant generation gives: a plan is a
// snapshot, and between the preview and the click somebody may have sold one of the items. The
// preview stays honest because it is the same function.
func (s *Service) ApplyCount(
	ctx context.Context, companyID id.ID, reference string,
) (domain.CountPlan, error) {
	var applied domain.CountPlan

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		count, err := s.requireCount(txCtx, companyID, reference)
		if err != nil {
			return err
		}
		if err = count.RequireApplicable(); err != nil {
			return err
		}

		plan, err := s.planFor(txCtx, companyID, count)
		if err != nil {
			return err
		}

		for _, adjustment := range plan.Adjustments {
			// A `count` movement, so the ledger shows where the change came from — and its
			// balance is the CURRENT stock plus the variance, never the counted figure.
			if _, err = s.moveWithin(txCtx, MoveInput{
				CompanyID: companyID, WarehouseID: count.WarehouseID,
				ProductID: adjustment.Line.ProductID, VariantID: adjustment.Line.VariantID,
				LotID: adjustment.Line.LotID,
				Type:  domain.Count,
				// The quantity a movement carries is always positive: the SIZE of the variance.
				QuantityMicro: abs(adjustment.VarianceMicro),
				CountedMicro:  adjustment.BalanceAfterMicro,
				DocumentType:  EntityCount, DocumentID: count.ID,
				DocumentLineID: adjustment.Line.ID,
				ReasonCode:     "stock_count", Reason: count.Reference,
			}); err != nil {
				return err
			}
			if err = s.repos.RecordVariance(
				txCtx, adjustment.Line.ID, adjustment.VarianceMicro); err != nil {
				return err
			}
		}

		finished := count.Applied(clock.Format(s.clk.Now()))
		if err = s.repos.UpdateCount(txCtx, finished, s.actorOf(txCtx)); err != nil {
			return err
		}
		applied = plan

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionCountApplied, EntityType: EntityCount, EntityID: count.ID,
			After: map[string]any{
				"reference": count.Reference, "adjusted": len(plan.Adjustments),
				"unchanged": plan.Unchanged, "uncounted": plan.Uncounted,
			},
		})
	})
	if err != nil {
		return domain.CountPlan{}, err
	}
	return applied, nil
}

// CountLines lists a count's lines.
//
// # Blind counts hide the expectation HERE
//
// The column is still populated in storage — the apply needs it — but a blind count does not
// hand it to whoever is counting. Doing this at the read is what makes the control real: a
// screen cannot show what it was never given.
func (s *Service) CountLines(
	ctx context.Context, companyID id.ID, reference string,
) ([]domain.CountLine, error) {
	count, err := s.requireCount(ctx, companyID, reference)
	if err != nil {
		return nil, err
	}
	lines, err := s.repos.CountLines(ctx, count.ID)
	if err != nil {
		return nil, err
	}

	if count.IsBlind && count.Status == domain.CountCounting {
		// Only WHILE COUNTING. In review the variances are the whole point, and hiding the
		// expectation would leave a reviewer looking at figures with nothing to compare them to.
		for i := range lines {
			lines[i].ExpectedMicro = 0
		}
	}
	return lines, nil
}

func (s *Service) requireCount(
	ctx context.Context, companyID id.ID, reference string,
) (domain.StockCount, error) {
	count, found, err := s.repos.CountByReference(ctx, companyID, reference)
	if err != nil {
		return domain.StockCount{}, err
	}
	if !found {
		return domain.StockCount{}, errs.NotFound(CodeUnknownCount,
			"there is no stock count with that reference").WithParam("reference", reference)
	}
	return count, nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
