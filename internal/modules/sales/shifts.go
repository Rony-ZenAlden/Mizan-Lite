package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The audited and postable actions this step adds.
const (
	ActionShiftOpened = "pos.shift.opened"
	ActionShiftClosed = "pos.shift.closed"

	// The till was SHORT: less money than the system expected. A separate action from an
	// overage, for the reason every other pair in this codebase is separate — the posting engine
	// refuses negative amounts, and a negative would flip a line's side silently.
	ActionCashShort = "pos.cash.short"
	ActionCashOver  = "pos.cash.over"

	EntityShift = "pos.shift"

	CodeUnknownShift = "sales.unknown_shift"
	CodeNoOpenShift  = "sales.no_open_shift"
)

// The permissions a till needs.
const (
	PermShiftOpen  = "pos.shift.open"
	PermShiftClose = "pos.shift.close"
)

// OpenShiftInput starts a till's trading session.
type OpenShiftInput struct {
	CompanyID  id.ID
	BranchID   id.ID
	Terminal   string
	FloatMinor int64
	Notes      string
}

// OpenShift starts a till.
//
// One open shift per till, kept by a partial unique index as well as by this check: two would
// make "which shift did this payment belong to" unanswerable, and that answer is the whole point
// of the table.
func (s *Service) OpenShift(ctx context.Context, in OpenShiftInput) (domain.Shift, error) {
	var opened domain.Shift

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.OpenShift(
			txCtx, in.BranchID, in.Terminal); err != nil {
			return err
		} else if found {
			return errs.Conflict(domain.CodeShiftOpen,
				"this till already has an open shift").WithParam("terminal", in.Terminal)
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		shift, err := domain.NewShift(identifier, in.BranchID, in.Terminal,
			clock.Format(s.clk.Now()), in.FloatMinor)
		if err != nil {
			return err
		}
		shift.OpenedBy = s.actorOf(txCtx)
		shift.Notes = in.Notes

		if err = s.repos.InsertShift(txCtx, in.CompanyID, shift); err != nil {
			return err
		}
		opened = shift

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionShiftOpened, EntityType: EntityShift, EntityID: shift.ID,
			After: map[string]any{
				"terminal": shift.Terminal, "float_minor": shift.OpeningFloatMinor,
			},
		})
	})
	if err != nil {
		return domain.Shift{}, err
	}
	return opened, nil
}

// CloseShift reconciles a till.
//
// # The difference is recorded and POSTED, not absorbed
//
// A shop's owner learns more from "the till was 300 short on Tuesday" than from most reports this
// system produces. A POS that quietly adjusts the expectation to match the count destroys exactly
// that, and does it silently.
//
// So the difference goes to the books as well as to the shift row — through an action, so the
// account it lands in is a seed file's decision rather than this module's (§20.3).
func (s *Service) CloseShift(
	ctx context.Context, companyID, shiftID id.ID, countedMinor int64, notes string,
) (domain.Shift, error) {
	if s.bus == nil {
		return domain.Shift{}, errs.Internal(CodePublisherMissing,
			"sales cannot close a shift without an event publisher")
	}

	var closed domain.Shift

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		shift, found, err := s.repos.ShiftByID(txCtx, shiftID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownShift,
				"there is no shift with that identity").WithParam("id", string(shiftID))
		}

		// What the drawer should hold: the float plus the CASH taken. Read at close and frozen,
		// so a payment recorded later cannot silently change a closed shift's arithmetic.
		cash, err := s.repos.CashTakenIn(txCtx, shiftID)
		if err != nil {
			return err
		}

		reconciled, err := shift.Close(clock.Format(s.clk.Now()), countedMinor, cash)
		if err != nil {
			return err
		}
		reconciled.Notes = notes

		if err = s.repos.CloseShift(txCtx, reconciled, s.actorOf(txCtx)); err != nil {
			return err
		}
		if err = s.publishDifference(txCtx, companyID, reconciled); err != nil {
			return err
		}
		closed = reconciled

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionShiftClosed, EntityType: EntityShift, EntityID: shiftID,
			After: map[string]any{
				"expected_minor":   reconciled.ExpectedMinor,
				"counted_minor":    reconciled.CountedMinor,
				"difference_minor": reconciled.DifferenceMinor,
			},
		})
	})
	if err != nil {
		return domain.Shift{}, err
	}
	return closed, nil
}

// publishDifference books a till's overage or shortfall.
//
// A shift that balanced posts NOTHING: an entry of zero on both sides is noise in a ledger
// somebody has to read, and 4.3 established that a movement worth nothing posts nothing.
func (s *Service) publishDifference(
	ctx context.Context, companyID id.ID, shift domain.Shift,
) error {
	if shift.Balanced() {
		return nil
	}

	date, ok := clock.ParseTimestamp(shift.ClosedAt)
	if !ok {
		return errs.Internal(domain.CodeInvalidShift,
			"a shift has an unreadable closing time").WithParam("closed_at", shift.ClosedAt)
	}

	action := ActionCashOver
	amount := shift.DifferenceMinor
	if shift.IsShort() {
		action = ActionCashShort
		// The engine refuses negatives, so the SIGN chooses the action and the amount is always
		// positive — the same shape inventory's increase/decrease split uses (4.3).
		amount = -amount
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    action,
		CompanyID: companyID,
		BranchID:  shift.BranchID,
		Date:      date,

		DocumentType:   EntityShift,
		DocumentID:     shift.ID,
		DocumentNumber: shift.Terminal,

		Amounts: map[string]int64{accountingc.AmountTotal: amount},
		Memo:    shift.Notes,
	})
}

// Shift reads one shift.
func (s *Service) Shift(ctx context.Context, shiftID id.ID) (domain.Shift, error) {
	shift, found, err := s.repos.ShiftByID(ctx, shiftID)
	if err != nil {
		return domain.Shift{}, err
	}
	if !found {
		return domain.Shift{}, errs.NotFound(CodeUnknownShift,
			"there is no shift with that identity")
	}
	return shift, nil
}

// CurrentShift finds the shift a till is trading under.
func (s *Service) CurrentShift(
	ctx context.Context, branchID id.ID, terminal string,
) (domain.Shift, error) {
	shift, found, err := s.repos.OpenShift(ctx, branchID, terminal)
	if err != nil {
		return domain.Shift{}, err
	}
	if !found {
		return domain.Shift{}, errs.NotFound(CodeNoOpenShift,
			"this till has no open shift").WithParam("terminal", terminal)
	}
	return shift, nil
}
