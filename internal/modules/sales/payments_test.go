package sales_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

func (f fixture) pay(
	t *testing.T, method domain.Method, amount int64, settle ...sales.SettleInput,
) domain.Payment {
	t.Helper()
	taken, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
		Method: method, Currency: "SYP", AmountMinor: amount, Settle: settle,
	})
	if err != nil {
		t.Fatalf("TakePayment: %v", err)
	}
	return taken
}

// ── the three shapes a `paid_minor` column cannot express (§2.4) ────────────────

// One payment settling SEVERAL invoices: a customer clearing their account.
func TestOnePaymentCanSettleSeveralInvoices(t *testing.T) {
	f := newPayingFixture(t)
	first, _ := f.sold(t, 2_000_000)  // 200
	second, _ := f.sold(t, 3_000_000) // 300

	f.pay(t, domain.Cash, 500,
		sales.SettleInput{DocumentID: first.ID, AmountMinor: 200},
		sales.SettleInput{DocumentID: second.ID, AmountMinor: 300})

	for _, invoice := range []string{string(first.ID), string(second.ID)} {
		outstanding, err := f.svc.Outstanding(f.ctx, idOf(invoice))
		if err != nil {
			t.Fatalf("Outstanding: %v", err)
		}
		if outstanding != 0 {
			t.Errorf("invoice still owes %d, want 0", outstanding)
		}
	}
}

// One invoice taking SEVERAL payments: a deposit, then the balance.
func TestOneInvoiceCanTakeSeveralPayments(t *testing.T) {
	f := newPayingFixture(t)
	invoice, _ := f.sold(t, 5_000_000) // 500

	f.pay(t, domain.Cash, 200, sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 200})

	outstanding, err := f.svc.Outstanding(f.ctx, invoice.ID)
	if err != nil {
		t.Fatalf("Outstanding: %v", err)
	}
	if outstanding != 300 {
		t.Errorf("outstanding = %d after a deposit of 200, want 300", outstanding)
	}

	f.pay(t, domain.Cash, 300, sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 300})

	if outstanding, err = f.svc.Outstanding(f.ctx, invoice.ID); err != nil {
		t.Fatalf("Outstanding: %v", err)
	}
	if outstanding != 0 {
		t.Errorf("outstanding = %d after the balance, want 0", outstanding)
	}
}

// A payment allocated to NOTHING: a deposit against future orders, which a shop genuinely takes.
func TestAPaymentCanBeTakenAgainstNothing(t *testing.T) {
	f := newPayingFixture(t)

	deposit := f.pay(t, domain.Cash, 1000)

	if deposit.Status != domain.Posted {
		t.Errorf("status = %q, want posted", deposit.Status)
	}
	_, allocations, err := f.svc.Payment(f.ctx, deposit.ID)
	if err != nil {
		t.Fatalf("Payment: %v", err)
	}
	if len(allocations) != 0 {
		t.Errorf("%d allocations on a deposit, want none", len(allocations))
	}
}

// ── what a payment refuses ──────────────────────────────────────────────────────

// Settling invoices with money nobody handed over would leave the receivables ledger showing
// customers as paid up while the till is short.
func TestAPaymentCannotSettleMoreThanWasPaid(t *testing.T) {
	f := newPayingFixture(t)
	invoice, _ := f.sold(t, 5_000_000)

	_, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
		Method: domain.Cash, Currency: "SYP", AmountMinor: 100,
		Settle: []sales.SettleInput{{DocumentID: invoice.ID, AmountMinor: 500}},
	})
	if err == nil {
		t.Fatal("500 was settled with a payment of 100")
	}
	if code := errs.CodeOf(err); code != domain.CodeOverAllocated {
		t.Errorf("code = %q, want %q", code, domain.CodeOverAllocated)
	}
}

// Two payments cannot each clear the same invoice in full.
func TestAnInvoiceCannotBeSettledTwice(t *testing.T) {
	f := newPayingFixture(t)
	invoice, _ := f.sold(t, 2_000_000) // 200

	f.pay(t, domain.Cash, 200, sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 200})

	_, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
		Method: domain.Cash, Currency: "SYP", AmountMinor: 200,
		Settle: []sales.SettleInput{{DocumentID: invoice.ID, AmountMinor: 200}},
	})
	if err == nil {
		t.Fatal("an invoice was settled twice")
	}
	if code := errs.CodeOf(err); code != domain.CodeOverSettled {
		t.Errorf("code = %q, want %q", code, domain.CodeOverSettled)
	}
}

func TestADraftInvoiceCannotBeSettled(t *testing.T) {
	f := newPayingFixture(t)
	document := f.draft(t, domain.Invoice)
	f.addLine(t, document.ID, 2_000_000)

	_, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
		Method: domain.Cash, Currency: "SYP", AmountMinor: 200,
		Settle: []sales.SettleInput{{DocumentID: document.ID, AmountMinor: 200}},
	})
	if err == nil {
		t.Fatal("a draft invoice was settled")
	}
}

func TestAPaymentMustBeForAPositiveAmount(t *testing.T) {
	f := newPayingFixture(t)

	for _, amount := range []int64{0, -100} {
		if _, err := f.svc.TakePayment(f.ctx, sales.NewPaymentInput{
			CompanyID: f.companyID, BranchID: f.branchID, Date: "2026-06-15",
			Method: domain.Cash, Currency: "SYP", AmountMinor: amount,
		}); err == nil {
			t.Errorf("a payment of %d was accepted", amount)
		}
	}
}

// ── the method decides where the money lands ────────────────────────────────────

// Cash goes to the till; a card goes to the bank. Phase 2's seeded rule debited CASH for every
// payment — right for a market stall, wrong for anybody who takes cards, and it had looked
// correct for three phases because nothing had ever fired it.
//
// The fix is a distinct ACTION per method, so the rules differ and sales still names no account.
func TestCashGoesToTheTillAndACardGoesToTheBank(t *testing.T) {
	f := newPayingFixture(t)
	books := f.booked(t)

	first, _ := f.sold(t, 2_000_000)  // 200
	second, _ := f.sold(t, 3_000_000) // 300

	f.pay(t, domain.Cash, 200, sales.SettleInput{DocumentID: first.ID, AmountMinor: 200})
	f.pay(t, domain.Card, 300, sales.SettleInput{DocumentID: second.ID, AmountMinor: 300})

	balances := f.balances(t, books)
	if balances["1110"] != 200 {
		t.Errorf("cash = %d, want 200 — only the cash payment belongs here", balances["1110"])
	}
	if balances["1120"] != 300 {
		t.Errorf("bank = %d, want 300 — the card payment landed elsewhere", balances["1120"])
	}
	// And both cleared the customer's balance.
	if balances["1200"] != 0 {
		t.Errorf("receivables = %d, want 0", balances["1200"])
	}
}

// A credit sale moves no money: the customer will pay later, and the invoice stays outstanding.
// Numbering it and posting an entry would show a shop as having been paid for everything it had
// ever sold.
func TestASaleOnCreditSettlesNothing(t *testing.T) {
	f := newPayingFixture(t)
	books := f.booked(t)
	invoice, _ := f.sold(t, 2_000_000)

	taken := f.pay(t, domain.OnCredit, 200,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 200})

	if taken.Status == domain.Posted {
		t.Error("a credit sale was posted as a payment")
	}
	if taken.Number != "" {
		t.Errorf("a credit sale took receipt number %q", taken.Number)
	}

	balances := f.balances(t, books)
	if balances["1110"] != 0 || balances["1120"] != 0 {
		t.Errorf("cash %d and bank %d — a credit sale moved money",
			balances["1110"], balances["1120"])
	}
	// The customer still owes it.
	if balances["1200"] != 200 {
		t.Errorf("receivables = %d, want 200 — the invoice was cleared without payment",
			balances["1200"])
	}

	// And the OUTSTANDING figure agrees: a draft payment settles nothing.
	outstanding, err := f.svc.Outstanding(f.ctx, invoice.ID)
	if err != nil {
		t.Fatalf("Outstanding: %v", err)
	}
	if outstanding != 200 {
		t.Errorf("outstanding = %d, want 200", outstanding)
	}
}

// ── numbering ───────────────────────────────────────────────────────────────────

func TestReceiptsHaveTheirOwnSequence(t *testing.T) {
	f := newPayingFixture(t)
	invoice, _ := f.sold(t, 2_000_000)

	receipt := f.pay(t, domain.Cash, 200,
		sales.SettleInput{DocumentID: invoice.ID, AmountMinor: 200})

	if receipt.Number != "RCT-000001" {
		t.Errorf("receipt number = %q, want RCT-000001", receipt.Number)
	}
	// The invoice sequence is untouched.
	next, err := f.svc.PreviewNumber(f.ctx, f.branchID, sales.SeriesInvoice)
	if err != nil {
		t.Fatalf("PreviewNumber: %v", err)
	}
	if next != "INV-000002" {
		t.Errorf("next invoice = %q — a receipt took an invoice number", next)
	}
}
