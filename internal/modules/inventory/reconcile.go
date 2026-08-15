package inventory

import (
	"context"
	"log/slog"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
)

// MappingInventory is the account role that holds stock value.
//
// A KEY, not an account code. Which account holds stock is a decision an accountant makes in a
// seed file (§20.3), and naming "1300" here would break the moment a country's chart differed.
const MappingInventory = "INVENTORY"

// Ledger reports what the books say.
//
// A PORT, like Products: inventory needs one number — the balance of whichever account fills the
// INVENTORY role — and §10.3 says a module reaches another only through its contract. The
// composition root satisfies this from accounting.
type Ledger interface {
	BalanceOfMapping(ctx context.Context, companyID id.ID, mappingKey string) (int64, error)
}

// Reconciliation is what the check found.
type Reconciliation struct {
	CompanyID id.ID
	// StockValueMinor is what the shelves are worth: every level's quantity at its average cost.
	StockValueMinor int64
	// LedgerValueMinor is what the inventory account says.
	LedgerValueMinor int64
	// DifferenceMinor is the first, less the second. Zero is the only acceptable answer.
	DifferenceMinor int64
	// LedgerChecked reports whether the books were actually consulted.
	//
	// Without it, a build with no Ledger port produced a difference of zero and no discrepancies
	// — and Balanced() said yes. That is a false clean bill of health, and the test that found it
	// is the only reason this field exists: "I did not check" and "I checked and it was fine" are
	// different answers, and only one of them should reassure anybody.
	LedgerChecked bool
	// Discrepancies are places the stock PROJECTION disagrees with its own movement ledger,
	// carried alongside because they are the usual cause of the first difference and reporting
	// them together saves a second investigation.
	Discrepancies []Discrepancy
}

// Balanced reports whether the shelves and the books agree.
//
// False when the books were never consulted: an unasked question has not been answered.
func (r Reconciliation) Balanced() bool {
	return r.LedgerChecked && r.DifferenceMinor == 0 && len(r.Discrepancies) == 0
}

// Reconcile proves that what is on the shelf is worth what the inventory account says.
//
// # Why this is the most important check in the phase
//
// Every other guarantee here is about one side of the system. This is the one that holds the two
// together, and its failure mode is the worst in the module: stock value and the inventory
// account drift apart, gross margin is quietly wrong, and nothing says so until somebody
// reconciles by hand — which in most businesses is never, or once a year at an audit.
//
// It REPORTS. The Phase 2 precedent, unchanged: a repair that runs automatically fixes the
// symptom nightly and hides the bug forever.
func (s *Service) Reconcile(ctx context.Context, companyID id.ID) (Reconciliation, error) {
	levels, err := s.repos.Levels(ctx, companyID, id.ID(""), id.ID(""))
	if err != nil {
		return Reconciliation{}, err
	}

	var stockValue int64
	for _, level := range levels {
		stockValue += valueOfStock(level.State.OnHandMicro, level.State.AverageMicro)
	}

	result := Reconciliation{CompanyID: companyID, StockValueMinor: stockValue}

	// The projection-versus-ledger check comes along, because a drift there is the usual cause
	// of a difference here and reporting both saves a second investigation.
	discrepancies, err := s.VerifyLedger(ctx, companyID)
	if err != nil {
		return Reconciliation{}, err
	}
	result.Discrepancies = discrepancies

	if s.ledger == nil {
		// No books wired: the stock half is still worth reporting, and claiming a difference of
		// zero against a ledger nobody asked would be a false clean bill of health.
		return result, nil
	}
	ledgerValue, err := s.ledger.BalanceOfMapping(ctx, companyID, MappingInventory)
	if err != nil {
		return Reconciliation{}, err
	}
	result.LedgerValueMinor = ledgerValue
	result.DifferenceMinor = stockValue - ledgerValue
	result.LedgerChecked = true
	return result, nil
}

// valueOfStock is what a quantity at an average cost is worth, in whole minor units.
//
// Both figures are ×10⁶, so the product is ×10¹². Rounded once, the same way the domain does it.
func valueOfStock(quantityMicro, averageMicro int64) int64 {
	const scale = 1_000_000_000_000
	product := quantityMicro * averageMicro
	if product < 0 {
		return (product - scale/2) / scale
	}
	return (product + scale/2) / scale
}

// Jobs registers the nightly reconciliation.
//
// # Nightly, and why not at boot
//
// The drift this looks for is introduced by restores, by repairs, and by bugs in the incremental
// path — not by ordinary trading. Running it at boot would catch nothing a shop notices and would
// delay opening; running it hourly would read every level and every movement twelve times a day
// for an answer that changes rarely. The accounting integrity job (Phase 2) made the same call
// for the same reasons.
func (m *Module) Jobs() []jobs.Registration {
	return []jobs.Registration{{
		Def: jobs.Def{
			Key:         "inventory.stock_reconciliation",
			Schedule:    jobs.Every(24 * time.Hour),
			CatchUp:     jobs.Skip,
			Timeout:     10 * time.Minute,
			Description: "jobs.inventory.stock_reconciliation",
		},
		Handler: func(ctx context.Context, _ jobs.RunContext) error {
			return m.svc.ReconcileAll(ctx)
		},
	}}
}

// ReconcileAll checks every company and reports what it finds.
//
// # It returns nil for a difference, and that is deliberate
//
// A failing job retries and eventually alerts as an OUTAGE. A stock reconciliation that does not
// balance is not an outage — it is a finding, and one nobody can fix by running the job again.
// Returning an error would turn a bookkeeping problem into a red light on an operations
// dashboard, and the two need different people.
//
// So the finding is LOGGED at warning level with both figures, and the job succeeds. A genuine
// failure — the database is gone, the mapping is missing — still returns an error, because that
// one is an outage.
func (s *Service) ReconcileAll(ctx context.Context) error {
	// From this module's own table: a company with no stock has nothing to reconcile, which
	// makes it the more precise question as well as the one that needs no port.
	companies, err := s.repos.CompaniesWithStock(ctx)
	if err != nil {
		return err
	}

	for _, companyID := range companies {
		result, reconcileErr := s.Reconcile(ctx, companyID)
		if reconcileErr != nil {
			return reconcileErr
		}
		// A build with no books wired has nothing to compare against, and logging "unbalanced"
		// every night for that would be noise rather than a finding.
		if !result.LedgerChecked || result.Balanced() || s.logger == nil {
			continue
		}
		s.logger.WarnContext(ctx, "stock does not reconcile to the inventory account",
			slog.String("company_id", string(companyID)),
			slog.Int64("stock_value_minor", result.StockValueMinor),
			slog.Int64("ledger_value_minor", result.LedgerValueMinor),
			slog.Int64("difference_minor", result.DifferenceMinor),
			slog.Int("drifted_levels", len(result.Discrepancies)))
	}
	return nil
}

// Series is empty: this module numbers no documents of its own.
func (m *Module) Series() []numbering.SeriesSpec { return nil }
