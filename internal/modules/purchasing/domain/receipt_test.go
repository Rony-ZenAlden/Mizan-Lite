package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// twoPercent is the tolerance a general trading business might set.
var twoPercent = domain.TolerancePolicy{PercentMicro: 20_000}

func TestADeliveryWithinToleranceIsAccepted(t *testing.T) {
	// 100 outstanding, 102 arriving: 2% over, exactly at the allowance. Bulk goods are cut,
	// weighed, or counted by machine, and exactness costs more than the difference.
	if err := domain.CheckOverReceipt(100_000_000, 102_000_000, twoPercent); err != nil {
		t.Fatalf("a 2%% over-delivery against a 2%% tolerance was refused: %v", err)
	}
}

func TestADeliveryBeyondToleranceIsRefused(t *testing.T) {
	// A supplier who over-delivers by 40% and invoices for it has been paid for goods nobody
	// ordered. That is what the control exists to catch.
	err := domain.CheckOverReceipt(100_000_000, 140_000_000, twoPercent)
	if code := errs.CodeOf(err); code != domain.CodeOverReceipt {
		t.Fatalf("code = %q, want %q", code, domain.CodeOverReceipt)
	}
}

func TestZeroToleranceRefusesAnySurplus(t *testing.T) {
	// A pharmacy dispensing controlled drugs takes exactly what it ordered. The setting must
	// reach zero and mean it — an off-by-one that let one extra through would be a controlled
	// substance nobody can account for.
	strict := domain.TolerancePolicy{}
	if err := domain.CheckOverReceipt(100_000_000, 100_000_000, strict); err != nil {
		t.Fatalf("an exact delivery was refused under zero tolerance: %v", err)
	}
	if err := domain.CheckOverReceipt(100_000_000, 100_000_001, strict); err == nil {
		t.Fatal("a surplus was accepted under zero tolerance")
	}
}

func TestToleranceIsMeasuredAgainstWhatIsOUTSTANDING(t *testing.T) {
	// # The arithmetic a supplier who wants to over-ship relies on
	//
	// An order for 100 delivered as 60 then 45 is not a 5% over-delivery on the second note; it
	// is 5 more than the 40 still owed, which is 12.5% and must be refused.
	//
	// Measured against the ORDERED quantity instead, each of five deliveries could be 2% over
	// and the total 10% over, with every individual note passing.
	err := domain.CheckOverReceipt(40_000_000, 45_000_000, twoPercent)
	if errs.CodeOf(err) != domain.CodeOverReceipt {
		t.Fatalf("5 over an outstanding 40 was accepted under a 2%% tolerance: %v", err)
	}

	// And the same 45 against a full 100 outstanding is simply a partial delivery.
	if err = domain.CheckOverReceipt(100_000_000, 45_000_000, twoPercent); err != nil {
		t.Fatalf("a partial delivery was refused: %v", err)
	}
}

func TestADeliveryAgainstAFilledLineIsRefusedWhateverTheTolerance(t *testing.T) {
	// Nothing outstanding means a percentage of it is nothing. This is either a duplicate
	// delivery or one meant for somebody else, and no tolerance makes it acceptable.
	generous := domain.TolerancePolicy{PercentMicro: 500_000} // 50%
	err := domain.CheckOverReceipt(0, 1_000_000, generous)
	if code := errs.CodeOf(err); code != domain.CodeNothingOutstanding {
		t.Fatalf("code = %q, want %q", code, domain.CodeNothingOutstanding)
	}
}

func TestTheToleranceIsExactOnAFractionalQuantity(t *testing.T) {
	// # Why this test is about a fraction
	//
	// The allowance is `outstanding × percent / 10⁶`. Dividing FIRST — `outstanding / 10⁶ ×
	// percent` — gives the same answer for every whole-unit quantity, which is why a drill that
	// swapped the order passed against tests that all ordered whole widgets.
	//
	// They diverge exactly where tolerance matters most: goods measured rather than counted.
	// 1.5 kg outstanding at a 2% tolerance allows 0.03 kg. Dividing first truncates 1.5 to 1 and
	// allows 0.02 — so a delivery of 1.53 kg, which the business said was acceptable, is
	// refused at the loading bay.
	if err := domain.CheckOverReceipt(1_500_000, 1_530_000, twoPercent); err != nil {
		t.Fatalf("1.53 against an outstanding 1.5 at 2%% was refused: %v", err)
	}
	// And 1.54 is genuinely over.
	if err := domain.CheckOverReceipt(1_500_000, 1_540_000, twoPercent); err == nil {
		t.Fatal("1.54 against an outstanding 1.5 at 2% was accepted")
	}
}

func TestASmallExcessAgainstALargeOutstandingDoesNotTruncateToZero(t *testing.T) {
	// The allowance is computed multiplication-first. Dividing first — excess/outstanding — is
	// integer division that gives 0 for every excess smaller than the outstanding quantity, so
	// every over-delivery would pass a zero-tolerance check.
	strict := domain.TolerancePolicy{}
	if err := domain.CheckOverReceipt(1_000_000_000_000, 1_000_000_000_001, strict); err == nil {
		t.Fatal("a one-micro excess against a huge outstanding quantity was accepted")
	}
}

// ── what may still change ───────────────────────────────────────────────────────

func TestAConfirmedDeliveryCannotBeEdited(t *testing.T) {
	// Stock has moved and the books have accrued. A correction is a supplier return or an
	// adjustment, both of which leave a record — editing the receipt would leave the stock
	// ledger describing a delivery that no longer says it happened.
	receipt := domain.Receipt{Status: domain.ReceiptDraft}
	if err := receipt.RequireDraft(); err != nil {
		t.Fatalf("a draft refused editing: %v", err)
	}

	receipt.Status = domain.ReceiptConfirmed
	if code := errs.CodeOf(receipt.RequireDraft()); code != domain.CodeAlreadyConfirmed {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyConfirmed)
	}
}

func TestADeliveryCannotBeBilledTwice(t *testing.T) {
	// Billing one twice pays a supplier twice for one delivery — the failure mode that costs
	// real money rather than tidiness, and the one the three-way match exists for.
	receipt := domain.Receipt{Status: domain.ReceiptConfirmed}
	if err := receipt.RequireBillable(); err != nil {
		t.Fatalf("a confirmed unbilled receipt refused billing: %v", err)
	}

	receipt.BilledAt = "2026-08-20T09:00:00Z"
	receipt.BillID = "bill-1"
	if code := errs.CodeOf(receipt.RequireBillable()); code != domain.CodeAlreadyBilled {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyBilled)
	}

	// A draft cannot be billed either: nothing has arrived yet as far as the books know.
	draft := domain.Receipt{Status: domain.ReceiptDraft}
	if code := errs.CodeOf(draft.RequireBillable()); code != domain.CodeNotConfirmed {
		t.Errorf("code = %q, want %q", code, domain.CodeNotConfirmed)
	}
}

func TestReceivingNothingOrLessThanNothingIsRefused(t *testing.T) {
	for _, quantity := range []int64{0, -1_000_000} {
		_, err := domain.NewReceiptLine(newID(t), newID(t), newID(t), newID(t), 1,
			quantity, quantity)
		if errs.CodeOf(err) != domain.CodeNonPositiveQty {
			t.Errorf("quantity %d was accepted as a delivery", quantity)
		}
	}
}
