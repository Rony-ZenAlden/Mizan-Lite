package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

func billLine(quantityMicro, netMinor, accruedMinor int64) domain.BillLine {
	return domain.BillLine{
		QuantityMicro: quantityMicro, NetMinor: netMinor, AccruedMinor: accruedMinor,
	}
}

// ── the supplier's own invoice number ───────────────────────────────────────────

func TestABillNeedsTheSuppliersOwnInvoiceNumber(t *testing.T) {
	// Required, not optional. Without it a payment cannot quote a reference the supplier
	// recognises, a statement cannot be reconciled, and the duplicate check has nothing to
	// compare — which is the check that stops a business paying twice.
	_, err := domain.NewBill(newID(t), newID(t), newID(t), newID(t),
		"Acme Supplies", "2026-08-20", "", "SAR")
	if code := errs.CodeOf(err); code != domain.CodeNoSupplierInvoice {
		t.Fatalf("code = %q, want %q", code, domain.CodeNoSupplierInvoice)
	}
}

// ── the match ───────────────────────────────────────────────────────────────────

func TestQuantityIsAHardRuleAndPriceIsASoftOne(t *testing.T) {
	// # The asymmetry is the whole design
	//
	// Being invoiced for goods that never arrived is theft or error, and there is no version of
	// it a business should absorb silently — so it is REFUSED.
	//
	// Being invoiced at a different price is ordinary: a surcharge, a currency movement, a price
	// agreed by telephone and never recorded. Refusing it would leave the goods on the shelf,
	// the GRNI accrued, and no way to close the loop except by editing the order retrospectively
	// — which is worse than the variance.

	// Ten arrived, ten invoiced, at the price ordered: everything agrees.
	exact := billLine(10_000_000, 1_000, 1_000)
	if match := domain.Match(exact, 10_000_000); !match.Matched() {
		t.Fatalf("an exact bill did not match: %+v", match)
	}

	// Ten arrived, twelve invoiced: reported as a quantity mismatch. In practice the schema
	// cannot produce this — a bill line takes its receipt line IN FULL — which is why nothing
	// refuses it at runtime. It is reported so a screen has all three sides, and so a change
	// that made the quantity settable would have somewhere to be caught.
	overBilled := billLine(12_000_000, 1_200, 1_000)
	if match := domain.Match(overBilled, 10_000_000); match.QuantityMatches {
		t.Error("twelve invoiced against ten received was reported as matching")
	}

	// Ten arrived, ten invoiced, at a higher price: ACCEPTED, and reported.
	dearer := billLine(10_000_000, 1_100, 1_000)
	match := domain.Match(dearer, 10_000_000)
	if match.Matched() {
		t.Error("a price difference was reported as a clean match")
	}
	if !match.QuantityMatches {
		t.Error("the quantity was reported as mismatched")
	}
	if match.PriceVarianceMinor != 100 {
		t.Errorf("variance = %d, want 100", match.PriceVarianceMinor)
	}
}

func TestUnderBillingIsReportedToo(t *testing.T) {
	// A supplier invoicing for LESS than arrived looks like a gift and is usually an error —
	// most often a second invoice on the way for the rest. Both directions are reported, because
	// a report that only flagged one would leave the other invisible.
	if match := domain.Match(billLine(8_000_000, 800, 1_000), 10_000_000); match.QuantityMatches {
		t.Fatal("eight invoiced against ten received was reported as matching")
	}
}

func TestAPriceVarianceKeepsItsSign(t *testing.T) {
	// A supplier honouring a discount nobody recorded is just as real a variance as a surcharge,
	// and the books must move the other way.
	dearer := billLine(10_000_000, 1_150, 1_000)
	if got := dearer.PriceVarianceMinor(); got != 150 {
		t.Errorf("variance = %d, want 150", got)
	}
	cheaper := billLine(10_000_000, 900, 1_000)
	if got := cheaper.PriceVarianceMinor(); got != -100 {
		t.Errorf("variance = %d, want -100", got)
	}
}

// ── the posting split ───────────────────────────────────────────────────────────

func TestAVarianceSplitsIntoTwoNonNegativeHalves(t *testing.T) {
	// The posting engine refuses negative amounts, because a negative would silently flip a line
	// to the other side of the entry — a credit meant to be a debit, balancing perfectly and
	// meaning the opposite. So exactly one half is ever non-zero.
	over, under := domain.SplitVariance(150)
	if over != 150 || under != 0 {
		t.Errorf("over=%d under=%d, want 150/0", over, under)
	}

	over, under = domain.SplitVariance(-100)
	if over != 0 || under != 100 {
		t.Errorf("over=%d under=%d, want 0/100", over, under)
	}

	over, under = domain.SplitVariance(0)
	if over != 0 || under != 0 {
		t.Errorf("over=%d under=%d, want 0/0", over, under)
	}

	// Neither half is ever negative, whatever it is given. A negative reaching a rule line is
	// the failure this function exists to make impossible.
	for _, variance := range []int64{-9_223_372_036_854_775_807, -1, 0, 1, 1 << 40} {
		over, under = domain.SplitVariance(variance)
		if over < 0 || under < 0 {
			t.Errorf("SplitVariance(%d) = %d/%d, which has a negative half",
				variance, over, under)
		}
	}
}

func TestBillTotalsTieToTheirLines(t *testing.T) {
	lines := []domain.BillLine{
		{NetMinor: 1_000, TaxAmountMinor: 150, TotalMinor: 1_150, AccruedMinor: 1_000},
		{NetMinor: 500, TaxAmountMinor: 75, TotalMinor: 575, AccruedMinor: 450, DiscountMinor: 20},
	}
	net, tax, discount, total, accrued := domain.BillTotals(lines)

	if net != 1_500 || tax != 225 || discount != 20 || total != 1_725 || accrued != 1_450 {
		t.Fatalf("net=%d tax=%d discount=%d total=%d accrued=%d",
			net, tax, discount, total, accrued)
	}
	if total != net+tax {
		t.Error("the total does not tie to net plus tax")
	}
	// The variance is what the posting must book so the entry balances against a GRNI clearing
	// of exactly what was accrued.
	if net-accrued != 50 {
		t.Errorf("variance = %d, want 50", net-accrued)
	}
}

// ── what may still change ───────────────────────────────────────────────────────

func TestAPostedBillCannotBeEdited(t *testing.T) {
	bill := domain.Bill{Status: domain.BillDraft}
	if err := bill.RequireDraft(); err != nil {
		t.Fatalf("a draft refused editing: %v", err)
	}
	bill.Status = domain.BillPosted
	if code := errs.CodeOf(bill.RequireDraft()); code != domain.CodeBillPosted {
		t.Errorf("code = %q, want %q", code, domain.CodeBillPosted)
	}
}
