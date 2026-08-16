package bootstrap

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/notify"
)

// The rules this build ships.
//
// # Why they live in the composition root
//
// Each reads across modules — an overdue invoice is sales' data judged against today, a
// reconciliation failure is accounting's compared with inventory's — and a rule that imported two
// modules would break `module-isolation`. The root is where cross-module questions are already
// assembled, for search and for the dashboard.
//
// Each rule is small enough to read in one sitting, which is deliberate: a rule nobody can check
// is a rule that will fire wrongly and be ignored, and an ignored notification centre is worse
// than none.

// ── the books disagree with the shelf ───────────────────────────────────────────

// valuationRule reports stock whose value the ledger does not agree with.
//
// The most serious thing this application can notice about itself. Phase 6.6 is the argument: a
// costing conversion was wrong by a factor of a hundred for two phases while every level, every
// movement and every journal entry stayed internally consistent. Nothing but this comparison
// would have said so.
type valuationRule struct{ app *App }

func (r valuationRule) Name() string { return "inventory.valuation" }

func (r valuationRule) Check(
	ctx context.Context, companyID id.ID,
) ([]notify.Notice, error) {
	valuation, err := r.app.Inventory.Valuation(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if !valuation.HasLedger || valuation.DifferenceMinor == 0 {
		return nil, nil
	}
	return []notify.Notice{{
		// ONE key for the condition, not one per variant. A shop whose books are out by a
		// hundred does not need four hundred notices, and a key per variant would let somebody
		// dismiss the difference away one line at a time.
		Key:        "difference",
		Severity:   notify.Danger,
		MessageKey: "notices.valuation.difference",
		Params: map[string]string{
			"difference": minorString(valuation.DifferenceMinor),
			"shelf":      minorString(valuation.TotalMinor),
			"books":      minorString(valuation.LedgerMinor),
		},
	}}, nil
}

// ── the books do not balance ────────────────────────────────────────────────────

// ledgerRule reports a balance sheet that does not balance.
type ledgerRule struct{ app *App }

func (r ledgerRule) Name() string { return "accounting.ledger" }

func (r ledgerRule) Check(
	ctx context.Context, companyID id.ID,
) ([]notify.Notice, error) {
	// Unbalanced ENTRIES first: a stored entry whose lines do not sum is a defect the balance
	// sheet cannot show, because it is wrong in both directions at once.
	unbalanced, err := r.app.Accounting.CheckIntegrity(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if len(unbalanced) > 0 {
		return []notify.Notice{{
			Key: "unbalanced_entries", Severity: notify.Danger,
			MessageKey: "notices.ledger.unbalanced",
			Params:     map[string]string{"count": itoaInt(len(unbalanced))},
		}}, nil
	}

	sheet, err := r.app.Accounting.BalanceSheet(ctx, companyID, r.app.Today())
	if err != nil {
		return nil, err
	}
	if sheet.OutOfBalanceMinor == 0 {
		return nil, nil
	}
	return []notify.Notice{{
		Key: "out_of_balance", Severity: notify.Danger,
		MessageKey: "notices.ledger.out_of_balance",
		Params:     map[string]string{"difference": minorString(sheet.OutOfBalanceMinor)},
	}}, nil
}

// ── nobody has backed up ────────────────────────────────────────────────────────

// backupRule reports a shop whose most recent backup is old, or which has none.
//
// # Why the threshold is generous
//
// A notice that fires every other day teaches its reader to dismiss it, and a dismissed notice is
// silence with extra steps. Three days is long enough that a working scheduled backup never
// triggers it, and short enough that a broken one is noticed within the week.
type backupRule struct {
	app   *App
	stale time.Duration
}

func (r backupRule) Name() string { return "ops.backup" }

func (r backupRule) Check(_ context.Context, _ id.ID) ([]notify.Notice, error) {
	if r.app.Backups == nil {
		return nil, nil
	}
	taken, err := r.app.Backups.List()
	if err != nil {
		return nil, err
	}
	if len(taken) == 0 {
		return []notify.Notice{{
			Key: "never", Severity: notify.Warning, MessageKey: "notices.backup.never",
		}}, nil
	}

	// The list is newest first, and every entry carries a manifest — a file without one is not
	// listed at all, so there is no "unknown age" case to handle.
	// An unparseable timestamp means the manifest is malformed, which `List` cannot happen upon
	// — a file without a readable manifest is not listed at all. Treating it as "no notice"
	// rather than as a failure would hide a backup directory somebody had edited by hand, so it
	// is reported like any other rule failure.
	newest, err := time.Parse(time.RFC3339, taken[0].Manifest.TakenAt)
	if err != nil {
		return nil, err
	}
	age := r.app.opts.Clock.Now().Sub(newest)
	if age < r.stale {
		return nil, nil
	}
	return []notify.Notice{{
		Key: "stale", Severity: notify.Warning, MessageKey: "notices.backup.stale",
		Params: map[string]string{"days": itoaInt(int(age.Hours() / 24))},
	}}, nil
}

// ── a scheduled job failed ──────────────────────────────────────────────────────

// jobRule reports jobs whose most recent run failed.
//
// The one rule that reports something that HAPPENED rather than something that IS. 9.6's design
// noted the gap: a rule cannot tell a user about a backup that failed last night, because the
// condition is over. This is the answer — the job history already records it, and a rule over that
// history is still derived from current state.
type jobRule struct{ app *App }

func (r jobRule) Name() string { return "ops.jobs" }

func (r jobRule) Check(ctx context.Context, _ id.ID) ([]notify.Notice, error) {
	states, err := r.app.Scheduler.JobStates(ctx)
	if err != nil {
		return nil, err
	}

	notices := make([]notify.Notice, 0, 2)
	for _, state := range states {
		// The MOST RECENT outcome, not a count of failures. A job that failed at midnight and
		// succeeded at one o'clock is working, and reporting it would teach the reader that
		// these notices do not mean anything.
		if state.LastStatus != "failed" && state.LastStatus != "timeout" {
			continue
		}
		key := state.Key
		// One notice PER JOB, unlike the valuation rule's single key. Here the distinction is
		// real: a failing backup and a failing outbox dispatch are different problems with
		// different fixes, and dismissing one must not silence the other.
		notices = append(notices, notify.Notice{
			Key: "failed." + key, Severity: notify.Danger,
			MessageKey: "notices.jobs.failed",
			Params:     map[string]string{"job": key},
		})
	}
	return notices, nil
}

// ── assembly ────────────────────────────────────────────────────────────────────

// buildNoticeCentre wires every rule this build ships.
func (a *App) buildNoticeCentre(dismissals notify.Dismissals) *notify.Centre {
	return notify.New(dismissals,
		ledgerRule{app: a},
		valuationRule{app: a},
		backupRule{app: a, stale: 72 * time.Hour},
		jobRule{app: a},
	)
}

// minorString renders a minor-unit amount without float arithmetic.
func minorString(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func itoaInt(n int) string { return minorString(int64(n)) }
