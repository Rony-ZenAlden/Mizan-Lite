package inventory_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// fakeLedger stands in for accounting, which inventory cannot import.
//
// It reports whatever it is told to, which is the point: the tests below need the two paths to
// AGREE in one case and DISAGREE in another, and a real ledger that always agreed could only
// ever demonstrate the first.
type fakeLedger struct {
	valueMinor int64
	err        error
}

func (l *fakeLedger) StockValueMinor(context.Context, id.ID) (int64, error) {
	return l.valueMinor, l.err
}

// ── what the stock is worth ─────────────────────────────────────────────────────

// TestAValuationIsQuantityTimesTheCostTheStockIsCarriedAt
func TestAValuationIsQuantityTimesTheCostTheStockIsCarriedAt(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 60_000_000) // ten at 60

	valuation, err := f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if len(valuation.Lines) != 1 {
		t.Fatalf("%d lines, want 1", len(valuation.Lines))
	}
	if valuation.TotalMinor != 600 {
		t.Errorf("total = %d, want 600 — ten at 60", valuation.TotalMinor)
	}
	if valuation.Lines[0].QuantityMicro != 10_000_000 {
		t.Errorf("quantity = %d, want 10000000", valuation.Lines[0].QuantityMicro)
	}
	if valuation.Lines[0].ProductName == "" || valuation.Lines[0].WarehouseName == "" {
		t.Error("a valuation row that names neither the product nor the warehouse cannot be " +
			"acted on")
	}

	// Nothing is attached, so nothing is claimed about the ledger. "The books agree" and "nobody
	// asked the books" must not look the same.
	if valuation.HasLedger {
		t.Error("a valuation with no control ledger claims to have compared one")
	}
}

// TestAValuationIsExactInACurrencyWithMinorUnits
//
// 6.6's lesson, applied rather than rediscovered: **a scale conversion tested only at scale 1 is
// a conversion nobody has tested.** The inventory fixture uses SYP, which has no minor unit, so
// every other test in this package would pass with the currency scale omitted entirely — which
// is exactly the state the codebase was in for two phases.
func TestAValuationIsExactInACurrencyWithMinorUnits(t *testing.T) {
	f := newFixture(t)
	f.useCurrency(t, "SAR", 2)
	f.move(t, domain.Receipt, 3_000_000, 12_340_000) // three at 12.34

	valuation, err := f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	// Three at 12.34 is 37.02, which is 3,702 minor units. Omitting the scale gives 37.
	if valuation.TotalMinor != 3_702 {
		t.Errorf("total = %d, want 3702 — a value of 37 means the currency's scale was dropped",
			valuation.TotalMinor)
	}
}

// TestStockAtZeroIsNotAValuationLine
//
// A shop that has ever stocked a thousand products has a level row for each of them. Nine hundred
// lines worth nothing bury the ninety that matter.
func TestStockAtZeroIsNotAValuationLine(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 5_000_000, 60_000_000)
	f.move(t, domain.Issue, 5_000_000, 0)

	valuation, err := f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if len(valuation.Lines) != 0 {
		t.Errorf("%d lines for stock that is all gone: %+v", len(valuation.Lines),
			valuation.Lines)
	}
	if valuation.TotalMinor != 0 {
		t.Errorf("total = %d, want 0", valuation.TotalMinor)
	}
}

// ── the criterion: the books must agree ─────────────────────────────────────────

// TestAValuationThatDisagreesWithTheLedgerSaysSo
//
// DoD criterion 5. Two completely different paths to one number: this one multiplies each level's
// quantity by its maintained average, the other sums journal lines written by posting rules from
// values computed when each movement happened.
//
// They agree only if every movement that changed the stock also posted, and posted the same
// figure. When they do not, **nothing else in the system will say so** — Phase 6.6 is the
// argument, where a costing conversion was wrong by a factor of a hundred for two phases while
// every level, every movement and every journal entry stayed internally consistent.
//
// The disagreement is tested first, because a detector tested only where it should stay silent is
// a detector nobody has heard (8.1's D169).
func TestAValuationThatDisagreesWithTheLedgerSaysSo(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 60_000_000) // 600 on the shelf

	ledger := &fakeLedger{valueMinor: 540} // the books say 540
	f.svc.AttachControlLedger(ledger)

	valuation, err := f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if !valuation.HasLedger {
		t.Fatal("a control ledger is attached and the valuation did not consult it")
	}
	if valuation.DifferenceMinor != 60 {
		t.Errorf("difference = %d, want 60 — the shelf says %d and the books say %d",
			valuation.DifferenceMinor, valuation.TotalMinor, valuation.LedgerMinor)
	}
	// And it still REPORTED. Refusing would hide which side is wrong.
	if len(valuation.Lines) == 0 {
		t.Error("the valuation refused to render, which hides the evidence")
	}

	// Agreement is the other case, and must read as zero rather than as absence.
	ledger.valueMinor = 600
	valuation, err = f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if valuation.DifferenceMinor != 0 {
		t.Errorf("difference = %d when the two agree", valuation.DifferenceMinor)
	}
}

// TestVerifyingWithNoLedgerAttachedIsRefusedRatherThanAnsweredZero
//
// A scheduled check that only reports failures would read a zero as "the books agree". They were
// never asked.
func TestVerifyingWithNoLedgerAttachedIsRefusedRatherThanAnsweredZero(t *testing.T) {
	f := newFixture(t)
	f.move(t, domain.Receipt, 10_000_000, 60_000_000)

	_, err := f.svc.VerifyValuation(f.ctx, f.companyID)
	if code := errs.CodeOf(err); code != inventory.CodeControlMissing {
		t.Errorf("code = %q, want %q", code, inventory.CodeControlMissing)
	}

	f.svc.AttachControlLedger(&fakeLedger{valueMinor: 600})
	difference, err := f.svc.VerifyValuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("VerifyValuation: %v", err)
	}
	if difference != 0 {
		t.Errorf("difference = %d, want 0", difference)
	}
}

// TestAValuationOverAnEmptyCompanyIsEmptyAndNotAnError
//
// DoD criterion 11.
func TestAValuationOverAnEmptyCompanyIsEmptyAndNotAnError(t *testing.T) {
	f := newFixture(t)

	valuation, err := f.svc.Valuation(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Valuation over a company with no stock: %v", err)
	}
	if len(valuation.Lines) != 0 || valuation.TotalMinor != 0 {
		t.Errorf("an empty company values at %d over %d lines",
			valuation.TotalMinor, len(valuation.Lines))
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// useCurrency switches the company's functional currency, straight to the table.
//
// There is no service method for it, correctly: changing the currency a ledger is kept in is not
// an operation — it is a migration nobody has asked for. What the test needs is a company whose
// money has minor units, and this is the shortest honest way to have one.
func (f fixture) useCurrency(t *testing.T, code string, decimals int) {
	t.Helper()
	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(identifier), code, code, code, decimals, now, now); err != nil {
		t.Fatalf("seeding %s: %v", code, err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE companies SET functional_currency = ? WHERE id = ?`,
		code, string(f.companyID)); err != nil {
		t.Fatalf("switching to %s: %v", code, err)
	}
}
