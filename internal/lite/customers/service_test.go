package customers_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	"github.com/mizan-erp/mizan/internal/lite/customers/customerstest"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

var ctx = context.Background()

var damascus = time.FixedZone("Damascus", 3*3600)

type fixture struct {
	svc   *customers.Service
	store *customerstest.Fake
	rates *customerstest.Rates
	gate  *customerstest.Gate
	clk   *clock.Fixed
}

func newFixture() fixture {
	f := fixture{store: customerstest.NewFake(), rates: customerstest.NewRates(), gate: &customerstest.Gate{},
		clk: clock.NewFixed(time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC))}
	f.svc = customers.NewService(litetest.Immediate{}, f.store, f.rates, customerstest.Settings{Note: 500}, customerstest.Currencies{}, f.gate, f.clk, damascus)
	return f
}

func code(err error) string { return errs.CodeOf(err) }

func (f fixture) customer(t *testing.T, name string) domain.Customer {
	t.Helper()
	c, err := f.svc.Create(ctx, domain.Draft{Name: name, Phone: "0933 123 456"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// charge adds a credit sale's charge, as the till would.
func (f fixture) charge(t *testing.T, c domain.Customer, currency string, amount int64) domain.Entry {
	t.Helper()
	saleID, _ := id.New()
	e, err := f.svc.Charge(ctx, customers.ChargeInput{CustomerID: c.ID, SaleID: saleID, Currency: currency, AmountMinor: amount,
		BusinessDate: "2026-09-14", At: f.clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f fixture) pay(t *testing.T, c domain.Customer, in domain.CashInput) domain.Entry {
	t.Helper()
	q, err := f.svc.QuotePayment(ctx, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	e, err := f.svc.RecordPayment(ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCustomersAreCreatedAndEditedWithUniqueNames(t *testing.T) {
	f := newFixture()
	a := f.customer(t, "أبو محمد")
	if _, err := f.svc.Create(ctx, domain.Draft{Name: "ابو محمد"}); code(err) != domain.CodeDuplicateName {
		t.Fatalf("a second أبو محمد: %v", err)
	}
	b := f.customer(t, "أبو محمد - الحلاق")
	if _, err := f.svc.Update(ctx, customers.UpdateInput{ID: b.ID, RowVersion: b.RowVersion, Draft: domain.Draft{Name: "ابو محمد"}}); code(err) != domain.CodeDuplicateName {
		t.Fatalf("renamed onto another: %v", err)
	}
	edited, err := f.svc.Update(ctx, customers.UpdateInput{ID: a.ID, RowVersion: a.RowVersion, Draft: domain.Draft{Name: "أبو محمد", Phone: "٠٩٤٤", Note: "يدفع أول الشهر"}})
	if err != nil || edited.Phone != "0944" || edited.RowVersion != 2 {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	if _, err := f.svc.Update(ctx, customers.UpdateInput{ID: a.ID, RowVersion: 1, Draft: domain.Draft{Name: "x"}}); code(err) != "database.concurrent_modification" {
		t.Fatalf("a stale edit: %v", err)
	}
	if found, _ := f.svc.Search(ctx, "٠٩٤٤", false, false); len(found) != 1 || found[0].Customer.ID != a.ID {
		t.Fatalf("by phone in Arabic-Indic digits = %+v", found)
	}
	if found, _ := f.svc.Search(ctx, "الحلاق", false, false); len(found) != 1 || found[0].Customer.ID != b.ID {
		t.Fatalf("by nickname = %+v", found)
	}
}

func TestBalancesAreNeverSummedAcrossCurrencies(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 1_625)
	f.charge(t, c, "SYP", 150_000)
	found, err := f.svc.Search(ctx, "", false, false)
	if err != nil || len(found) != 1 || len(found[0].Summaries) != 2 {
		t.Fatalf("search = %+v, %v", found, err)
	}
	syp, usd := found[0].Summaries[0], found[0].Summaries[1]
	if syp.Currency != "SYP" || syp.BalanceMinor != 150_000 || usd.Currency != "USD" || usd.BalanceMinor != 1_625 {
		t.Fatalf("summaries = %+v", found[0].Summaries)
	}
	// Paying dollars in pounds moves only the dollar chain.
	f.pay(t, c, domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "100000"})
	if b, _ := f.svc.Balance(ctx, c.ID, "SYP"); b != 150_000 {
		t.Fatalf("the pounds balance moved: %d", b)
	}
	if b, _ := f.svc.Balance(ctx, c.ID, "USD"); b != 958 {
		t.Fatalf("the dollar balance = %d", b)
	}
	out, err := f.svc.Outstanding(ctx)
	if err != nil || len(out.Customers) != 1 || out.BusinessDate != "2026-09-14" {
		t.Fatalf("outstanding = %+v, %v", out, err)
	}
	if u, s := out.Today["USD"], out.Today["SYP"]; u.Payments != 1 || u.SettledMinor != 667 || u.ChargedMinor != 1_625 || s.CashInMinor != 100_000 || s.ChargedMinor != 150_000 {
		t.Fatalf("today = USD %+v SYP %+v", *out.Today["USD"], *out.Today["SYP"])
	}
}

func TestAStalePaymentQuoteIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 1_625)
	in := domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "100000"}
	q, _ := f.svc.QuotePayment(ctx, c.ID, in)

	f.rates.Change(15_500_000_000_000)
	if _, err := f.svc.RecordPayment(ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token}); code(err) != domain.CodePaymentStale {
		t.Fatalf("a payment at the old rate: %v", err)
	}
	q2, _ := f.svc.QuotePayment(ctx, c.ID, in)
	f.pay(t, c, domain.CashInput{Currency: "USD", Amount: "1"}) // another payment on the same debt
	if _, err := f.svc.RecordPayment(ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q2.Token}); code(err) != domain.CodePaymentStale {
		t.Fatalf("a payment after another moved the balance: %v", err)
	}
	if st, _ := f.svc.Statement(ctx, c.ID, "USD"); len(st.Entries) != 2 {
		t.Fatalf("a refused payment wrote: %+v", st.Entries)
	}
}

func TestNoRateNoPayment(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 1_625)
	f.rates.Set = false
	if _, err := f.svc.QuotePayment(ctx, c.ID, domain.CashInput{Currency: "USD", Amount: "1"}); code(err) != domain.CodeNoRate {
		t.Fatalf("a same-currency payment with no rate: %v", err)
	}
}

func TestOwnerActsNeedThePINAndAReason(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	opening := customers.AmountInput{CustomerID: c.ID, Currency: "USD", Amount: "20", Note: "صفحة 12"}
	if _, err := f.svc.Opening(ctx, opening); code(err) != customers.CodeOwnerRequired {
		t.Fatalf("an opening without the owner: %v", err)
	}
	f.gate.Elevated = true
	o, err := f.svc.Opening(ctx, opening)
	if err != nil || o.AmountMinor != 2_000 || o.Kind != domain.KindOpening || o.Note != "صفحة 12" {
		t.Fatalf("opening = %+v, %v", o, err)
	}
	if _, err = f.svc.Opening(ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", Amount: "1.005"}); code(err) != domain.CodeAmountDecimals {
		t.Fatalf("an opening of a tenth of a cent: %v", err)
	}
	if _, err = f.svc.WriteOff(ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", Amount: "5"}); code(err) != domain.CodeReasonRequired {
		t.Fatalf("a write-off without a reason: %v", err)
	}
	if _, err = f.svc.WriteOff(ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", Amount: "25", Note: "سافر"}); code(err) != domain.CodeWriteOffTooLarge {
		t.Fatalf("a write-off beyond the balance: %v", err)
	}
	w, err := f.svc.WriteOff(ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", All: true, Note: "سافر"})
	if err != nil || w.AmountMinor != -2_000 || w.BalanceAfterMinor != 0 {
		t.Fatalf("write-off all = %+v, %v", w, err)
	}
	if _, err := f.svc.WriteOff(ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", All: true, Note: "x"}); code(err) != domain.CodeNothingOwed {
		t.Fatalf("writing off nothing: %v", err)
	}
	f.gate.Elevated = false
	if _, err := f.svc.Refund(ctx, customers.RefundInput{CustomerID: c.ID, Cash: domain.CashInput{Currency: "USD", All: true}, Reason: "x"}); code(err) != domain.CodeNothingOwedBack {
		t.Fatalf("a refund is decided before the owner is asked: %v", err)
	}
	actions := []string{}
	for _, a := range f.gate.Acts {
		actions = append(actions, a.Action)
	}
	if len(actions) != 2 || actions[0] != customers.ActOpening || actions[1] != customers.ActWriteOff || f.gate.Acts[0].After != "20.00 USD صفحة 12" {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}
}

func TestAnEntryIsReversedOnceAndAChargeOnlyByItsSale(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	charge := f.charge(t, c, "USD", 1_625)
	payment := f.pay(t, c, domain.CashInput{Currency: "USD", Amount: "10"})

	if _, err := f.svc.Reverse(ctx, payment.ID, "خطأ"); code(err) != customers.CodeOwnerRequired {
		t.Fatalf("a reversal without the owner: %v", err)
	}
	f.gate.Elevated = true
	if _, err := f.svc.Reverse(ctx, charge.ID, "خطأ"); code(err) != domain.CodeNotReversible {
		t.Fatalf("a charge reversed by hand: %v", err)
	}
	r, err := f.svc.Reverse(ctx, payment.ID, "دفعة مكررة")
	if err != nil || r.AmountMinor != 1_000 || r.BalanceAfterMinor != 1_625 || r.Seq != 3 {
		t.Fatalf("reversal = %+v, %v", r, err)
	}
	if _, err := f.svc.Reverse(ctx, payment.ID, "again"); code(err) != domain.CodeAlreadyReversed {
		t.Fatalf("reversed twice: %v", err)
	}
	if _, err := f.svc.Reverse(ctx, r.ID, "x"); code(err) != domain.CodeNotReversible {
		t.Fatalf("a reversal reversed: %v", err)
	}
	st, _ := f.svc.Statement(ctx, c.ID, "USD")
	if !st.Reversed[payment.ID] || st.Reversed[charge.ID] {
		t.Fatalf("statement reversals = %v", st.Reversed)
	}
}

// TestACreditSaleVoidedAfterAPaymentLeavesTheShopOwing is L5 H4 and Q-L5.6: the charge reversed, the payment standing,
// the balance below zero, refunded by the owner — or used by the next charge.
func TestACreditSaleVoidedAfterAPaymentLeavesTheShopOwing(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	saleID, _ := id.New()
	if _, err := f.svc.Charge(ctx, customers.ChargeInput{CustomerID: c.ID, SaleID: saleID, Currency: "SYP", AmountMinor: 80_000, BusinessDate: "2026-09-14", At: f.clk.Now()}); err != nil {
		t.Fatal(err)
	}
	f.pay(t, c, domain.CashInput{Currency: "SYP", Amount: "50000"})
	f.clk.Advance(24 * time.Hour)
	r, err := f.svc.ReverseCharge(ctx, saleID, "أعاد البضاعة", f.clk.Now(), "2026-09-15")
	if err != nil || r.BalanceAfterMinor != -50_000 || r.BusinessDate != "2026-09-15" {
		t.Fatalf("reverse charge = %+v, %v", r, err)
	}
	if _, err = f.svc.ReverseCharge(ctx, saleID, "again", f.clk.Now(), "2026-09-15"); code(err) != domain.CodeAlreadyReversed {
		t.Fatalf("reversed twice: %v", err)
	}
	if view, found, _ := f.svc.ChargeOf(ctx, saleID); !found || !view.Reversed {
		t.Fatalf("charge view = %+v %v", view, found)
	}
	if _, err = f.svc.QuotePayment(ctx, c.ID, domain.CashInput{Currency: "SYP", Amount: "1"}); code(err) != domain.CodeNothingOwed {
		t.Fatalf("a payment while the shop owes: %v", err)
	}
	f.gate.Elevated = true
	part, err := f.svc.Refund(ctx, customers.RefundInput{CustomerID: c.ID, Cash: domain.CashInput{Currency: "SYP", Amount: "20000"}, Reason: "نقداً"})
	if err != nil || part.BalanceAfterMinor != -30_000 || part.Cash.TenderedMinor != 20_000 {
		t.Fatalf("refund = %+v, %v", part, err)
	}
	// The next credit sale uses what is left.
	f.charge(t, c, "SYP", 45_000)
	if b, _ := f.svc.Balance(ctx, c.ID, "SYP"); b != 15_000 {
		t.Fatalf("balance after the next charge = %d", b)
	}
	if findings, err := f.svc.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}

func TestDeactivatingACustomerWhoOwesNeedsTheOwner(t *testing.T) {
	f := newFixture()
	settled := f.customer(t, "خالد")
	if off, err := f.svc.SetActive(ctx, settled.ID, settled.RowVersion, false); err != nil || off.Active {
		t.Fatalf("a customer who owes nothing = %+v, %v", off, err)
	}
	owing := f.customer(t, "سمير")
	f.charge(t, owing, "USD", 500)
	if _, err := f.svc.SetActive(ctx, owing.ID, owing.RowVersion, false); code(err) != customers.CodeOwnerRequired {
		t.Fatalf("hiding a debt without the owner: %v", err)
	}
	f.gate.Elevated = true
	if off, err := f.svc.SetActive(ctx, owing.ID, owing.RowVersion, false); err != nil || off.Active || f.gate.Acts[0].Action != customers.ActDeactivateOwing {
		t.Fatalf("deactivated = %+v, %v, %+v", off, err, f.gate.Acts)
	}
	if found, _ := f.svc.Search(ctx, "", true, false); len(found) != 0 {
		t.Fatal("an inactive customer in the till's search")
	}
	if found, _ := f.svc.Search(ctx, "", true, true); len(found) != 1 {
		t.Fatal("owing only, inactive included")
	}
}

func TestAnInactiveCustomerCannotBeChargedButCanPay(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 1_000)
	f.gate.Elevated = true
	off, _ := f.svc.SetActive(ctx, c.ID, c.RowVersion, false)
	saleID, _ := id.New()
	if _, err := f.svc.Charge(ctx, customers.ChargeInput{CustomerID: off.ID, SaleID: saleID, Currency: "USD", AmountMinor: 100, BusinessDate: "2026-09-14", At: f.clk.Now()}); code(err) != domain.CodeInactive {
		t.Fatalf("charging an inactive customer: %v", err)
	}
	if e := f.pay(t, off, domain.CashInput{Currency: "USD", All: true}); e.BalanceAfterMinor != 0 {
		t.Fatalf("an inactive customer paying = %+v", e)
	}
}

func TestVerifyIsTheOwners(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.Verify(ctx); code(err) != customers.CodeOwnerRequired {
		t.Fatalf("verify outside owner mode: %v", err)
	}
	var walked int
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 100)
	if err := f.svc.EachCharge(ctx, func(customers.ChargeView) error { walked++; return nil }); err != nil || walked != 1 {
		t.Fatalf("EachCharge walked %d, %v", walked, err)
	}
}

// TestARepaymentConvertsAtTodaysRate is Q-L5.5: a dollar debt run up at 15,000 and repaid when the dollar is 18,000 is
// worth its dollars today — 180,000 pounds settle 10.00 USD, not the 12.00 they were worth when the debt was made.
func TestARepaymentConvertsAtTodaysRate(t *testing.T) {
	f := newFixture()
	c := f.customer(t, "سمير")
	f.charge(t, c, "USD", 1_625) // at 15,000
	f.rates.Change(18_000_000_000_000)
	e := f.pay(t, c, domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "180000"})
	if e.AmountMinor != -1_000 || e.BalanceAfterMinor != 625 || e.Cash.RateNano != 18_000_000_000_000 {
		t.Fatalf("payment = %+v", e)
	}
	q, err := f.svc.QuotePayment(ctx, c.ID, domain.CashInput{Currency: "USD", TenderCurrency: "SYP", All: true})
	if err != nil || q.TenderedMinor != 112_500 { // 6.25 × 18,000
		t.Fatalf("pay all at 18,000 = %+v, %v", q, err)
	}
}
