package purchasing_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// billed runs the whole chain to a POSTED bill, which is what a payment settles.
func (f fixture) billed(t *testing.T, invoiceNumber string) purchasingdomain.Bill {
	t.Helper()
	_, _, receiptLines := f.delivered(t, 10_000_000)

	billID := f.draftBill(t, invoiceNumber)
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}
	posted, err := f.svc.PostBill(f.ctx, billID)
	if err != nil {
		t.Fatalf("PostBill: %v", err)
	}
	return posted
}

func TestTheMethodDecidesWhichAccountTheMoneyLeaves(t *testing.T) {
	// # The defect Phase 2 seeded, on the paying side
	//
	// One `supplier_payment` rule credited CASH whatever the method, so a bank transfer to a
	// supplier would have reduced the till — and the till would have been short at every close
	// with no transaction to explain it. The identical defect existed on the receiving side and
	// 5.5 found it there.
	//
	// Fixed entirely in seed data: four actions, four rules, and no Go that knows which account
	// any of them touches.
	for _, test := range []struct {
		method  purchasingdomain.Method
		account string
		other   string
	}{
		{purchasingdomain.Cash, "1110", "1120"},
		{purchasingdomain.BankTransfer, "1120", "1110"},
		{purchasingdomain.Cheque, "1120", "1110"},
		{purchasingdomain.Card, "1120", "1110"},
	} {
		t.Run(string(test.method), func(t *testing.T) {
			f := newStockedFixture(t)
			books := f.booked(t)
			bill := f.billed(t, "ACME-"+string(test.method))

			before := f.balances(t, books)
			if _, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
				CompanyID: f.companyID, BranchID: f.branchID,
				PartnerID: f.partnerID, PartnerName: "Acme Supplies",
				PaymentDate: "2026-08-28", Method: test.method, Currency: "SAR",
				AmountMinor: bill.TotalMinor,
				Settle: []purchasingdomain.Allocation{
					{BillID: bill.ID, AmountMinor: bill.TotalMinor},
				},
			}); err != nil {
				t.Fatalf("Pay: %v", err)
			}
			after := f.balances(t, books)

			if after[test.account]-before[test.account] != -bill.TotalMinor {
				t.Errorf("%s moved %d, want -%d",
					test.account, after[test.account]-before[test.account], bill.TotalMinor)
			}
			if after[test.other] != before[test.other] {
				t.Errorf("paying by %s moved %s, which it must not touch",
					test.method, test.other)
			}
			// And what we owe falls, whatever the method.
			if after["2100"]-before["2100"] != bill.TotalMinor {
				t.Errorf("payable moved %d, want %d",
					after["2100"]-before["2100"], bill.TotalMinor)
			}
		})
	}
}

func TestOnePaymentCanSettleSeveralBills(t *testing.T) {
	// Clearing a supplier's statement. The shape a `paid_minor` column on a bill cannot express.
	f := newStockedFixture(t)

	first := f.billed(t, "ACME-9101")
	second := f.billed(t, "ACME-9102")
	total := first.TotalMinor + second.TotalMinor

	if _, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.BankTransfer,
		Currency: "SAR", AmountMinor: total,
		Settle: []purchasingdomain.Allocation{
			{BillID: first.ID, AmountMinor: first.TotalMinor},
			{BillID: second.ID, AmountMinor: second.TotalMinor},
		},
	}); err != nil {
		t.Fatalf("Pay: %v", err)
	}

	for _, bill := range []purchasingdomain.Bill{first, second} {
		outstanding, err := f.svc.OutstandingOnBill(f.ctx, bill.ID)
		if err != nil {
			t.Fatalf("OutstandingOnBill: %v", err)
		}
		if outstanding != 0 {
			t.Errorf("bill %s still owes %d", bill.Number, outstanding)
		}
	}
}

func TestOneBillCanTakeSeveralPayments(t *testing.T) {
	// A deposit, then the balance. The other shape the column cannot express.
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-9201")

	half := bill.TotalMinor / 2
	for _, amount := range []int64{half, bill.TotalMinor - half} {
		if _, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
			CompanyID: f.companyID, BranchID: f.branchID,
			PartnerID: f.partnerID, PartnerName: "Acme Supplies",
			PaymentDate: "2026-08-28", Method: purchasingdomain.Cash,
			Currency: "SAR", AmountMinor: amount,
			Settle: []purchasingdomain.Allocation{
				{BillID: bill.ID, AmountMinor: amount},
			},
		}); err != nil {
			t.Fatalf("Pay(%d): %v", amount, err)
		}
	}

	outstanding, err := f.svc.OutstandingOnBill(f.ctx, bill.ID)
	if err != nil {
		t.Fatalf("OutstandingOnBill: %v", err)
	}
	if outstanding != 0 {
		t.Errorf("outstanding = %d, want 0", outstanding)
	}
}

func TestABillCannotBeOverpaid(t *testing.T) {
	// Not generosity: a keying error that leaves the payable overdrawn and reconciles against
	// nothing.
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-9301")

	_, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: bill.TotalMinor * 2,
		Settle: []purchasingdomain.Allocation{
			{BillID: bill.ID, AmountMinor: bill.TotalMinor * 2},
		},
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeOverSettled {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeOverSettled)
	}
}

func TestAPaymentCannotAllocateMoreThanItIsWorth(t *testing.T) {
	// Arithmetic that cannot be true: the money does not exist.
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-9401")

	_, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: 100,
		Settle: []purchasingdomain.Allocation{
			{BillID: bill.ID, AmountMinor: 500},
		},
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeOverAllocated {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeOverAllocated)
	}
}

func TestAPrepaymentNeedsNoAllocation(t *testing.T) {
	// Money against future orders is a real thing. Forcing it to balance would mean inventing an
	// allocation against a bill that does not exist yet.
	f := newStockedFixture(t)
	books := f.booked(t)
	before := f.balances(t, books)

	if _, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.BankTransfer,
		Currency: "SAR", AmountMinor: 5_000,
	}); err != nil {
		t.Fatalf("Pay: %v", err)
	}

	after := f.balances(t, books)
	// The money has left the bank and the supplier's account carries the credit.
	if after["1120"]-before["1120"] != -5_000 {
		t.Errorf("bank moved %d, want -5000", after["1120"]-before["1120"])
	}
	if after["2100"]-before["2100"] != 5_000 {
		t.Errorf("payable moved %d, want 5000", after["2100"]-before["2100"])
	}
}

func TestADraftBillCannotBePaid(t *testing.T) {
	// Paying one would settle a debt the books do not carry, leaving the payable negative and
	// the bill still unposted.
	f := newStockedFixture(t)
	_, _, receiptLines := f.delivered(t, 10_000_000)

	billID := f.draftBill(t, "ACME-9501")
	if _, err := f.svc.AddBillLine(f.ctx, purchasing.BillLineInput{
		CompanyID: f.companyID, BillID: billID, ReceiptLineID: receiptLines[0].ID,
	}); err != nil {
		t.Fatalf("AddBillLine: %v", err)
	}

	_, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: f.partnerID, PartnerName: "Acme Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: 100,
		Settle:      []purchasingdomain.Allocation{{BillID: billID, AmountMinor: 100}},
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeInvalidBill {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeInvalidBill)
	}
}

func TestAPaymentNeedsAPayee(t *testing.T) {
	// Unlike a customer payment's walk-in. Money leaving the business goes to somebody, and a
	// payment with no payee is a hole in the cash position nobody can chase.
	f := newStockedFixture(t)

	_, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: id.ID(""), PaymentDate: "2026-08-28",
		Method: purchasingdomain.Cash, Currency: "SAR", AmountMinor: 100,
	})
	if code := errs.CodeOf(err); code != purchasingdomain.CodeNoSupplier {
		t.Fatalf("code = %q, want %q", code, purchasingdomain.CodeNoSupplier)
	}
}

func TestAFailedAllocationLeavesNoPaymentAndNoNumber(t *testing.T) {
	// One transaction. A payment that reached the books without its allocations would show the
	// supplier paid and every bill still outstanding — so somebody chases them for money already
	// sent.
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-9601")

	other, _ := id.New()
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, 'OTHER3', 'Other Supplies', 0, 1, 'company', 30, 0, 1, 0, 1, ?, ?)`,
		string(other), string(f.companyID), "2026-08-01", "2026-08-01"); err != nil {
		t.Fatalf("creating a second supplier: %v", err)
	}

	// Paying the WRONG supplier's bill fails partway through.
	if _, err := f.svc.Pay(f.ctx, purchasing.NewPaymentInput{
		CompanyID: f.companyID, BranchID: f.branchID,
		PartnerID: other, PartnerName: "Other Supplies",
		PaymentDate: "2026-08-28", Method: purchasingdomain.Cash, Currency: "SAR",
		AmountMinor: bill.TotalMinor,
		Settle: []purchasingdomain.Allocation{
			{BillID: bill.ID, AmountMinor: bill.TotalMinor},
		},
	}); err == nil {
		t.Fatal("one supplier's payment settled another's bill")
	}

	payments, err := f.svc.Payments(f.ctx, f.companyID, "")
	if err != nil {
		t.Fatalf("Payments: %v", err)
	}
	if len(payments) != 0 {
		t.Errorf("%d payments survived a failed allocation", len(payments))
	}

	outstanding, err := f.svc.OutstandingOnBill(f.ctx, bill.ID)
	if err != nil {
		t.Fatalf("OutstandingOnBill: %v", err)
	}
	if outstanding != bill.TotalMinor {
		t.Errorf("the bill owes %d, want the whole %d", outstanding, bill.TotalMinor)
	}
}

// TestADraftPaymentSettlesNothing
//
// # Why this test writes straight to the table
//
// `Pay` drafts and posts in ONE transaction, so no draft payment ever carries allocations through
// the service — which makes the `status = 'posted'` filter unreachable from the API, and a drill
// removing it changed nothing.
//
// The filter is still right, and the day a draft-payment flow is added is the day it matters:
// allocations against a payment somebody is still typing would show bills settled by money that
// has not left. So it is asserted where it can only pass if the REPOSITORY keeps it — the Phase 3
// rule, applied to a guard the service cannot reach.
func TestADraftPaymentSettlesNothing(t *testing.T) {
	f := newStockedFixture(t)
	bill := f.billed(t, "ACME-9701")

	paymentID, _ := id.New()
	allocationID, _ := id.New()
	now := "2026-08-28"

	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO supplier_payments (
			id, company_id, branch_id, partner_id, partner_name, payment_date,
			method, currency_code, exchange_rate_micro, amount_minor, status,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'Acme Supplies', ?, 'cash', 'SAR', 1000000, ?, 'draft', 1, ?, ?)`,
		string(paymentID), string(f.companyID), string(f.branchID), string(f.partnerID),
		now, bill.TotalMinor, now, now); err != nil {
		t.Fatalf("inserting a draft payment: %v", err)
	}
	if _, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO supplier_payment_allocations (
			id, payment_id, bill_id, amount_minor, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(allocationID), string(paymentID), string(bill.ID),
		bill.TotalMinor, now); err != nil {
		t.Fatalf("inserting a draft allocation: %v", err)
	}

	outstanding, err := f.svc.OutstandingOnBill(f.ctx, bill.ID)
	if err != nil {
		t.Fatalf("OutstandingOnBill: %v", err)
	}
	if outstanding != bill.TotalMinor {
		t.Fatalf("outstanding = %d, want the whole %d — a draft payment was counted as settled",
			outstanding, bill.TotalMinor)
	}
}
