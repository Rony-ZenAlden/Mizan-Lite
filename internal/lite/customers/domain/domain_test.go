package domain_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
)

var (
	syp = domain.Currency{Code: "SYP", Decimals: 0}
	usd = domain.Currency{Code: "USD", Decimals: 2}
)

const r15000 = int64(15_000_000_000_000)

func newID(t testing.TB) id.ID {
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func cashContext(t testing.TB, balance int64) domain.CashContext {
	return domain.CashContext{Local: syp, USD: usd, RateID: newID(t), RateNano: r15000, CashNote: 500, CustomerID: newID(t),
		Place: domain.Place{Seq: 3, BalanceMinor: balance}}
}

func code(err error) string { return errs.CodeOf(err) }

func TestNamesAreUniqueAfterNormalisation(t *testing.T) {
	a, err := domain.NewCustomer(newID(t), domain.Draft{Name: " أبو محمد ", Phone: "٠٩٣٣ ١٢٣ ٤٥٦", Note: "الحلاق"})
	if err != nil || a.Name != "أبو محمد" || a.Phone != "0933 123 456" || !a.Active || a.RowVersion != 1 {
		t.Fatalf("customer = %+v, %v", a, err)
	}
	b, _ := domain.NewCustomer(newID(t), domain.Draft{Name: "ابو محمد"})
	if a.NameKey() != b.NameKey() {
		t.Fatalf("%q and %q are one name to a reader: keys %q, %q", a.Name, b.Name, a.NameKey(), b.NameKey())
	}
	c, _ := domain.NewCustomer(newID(t), domain.Draft{Name: "أبو محمد - الحلاق"})
	if c.NameKey() == a.NameKey() {
		t.Fatal("a nickname does not tell two customers apart")
	}
	for _, bad := range []struct {
		d    domain.Draft
		code string
	}{
		{domain.Draft{Name: "  "}, domain.CodeNameRequired},
		{domain.Draft{Name: "ـــ"}, domain.CodeNameRequired},
		{domain.Draft{Name: "سمير", Phone: "0933x"}, domain.CodePhoneInvalid},
		{domain.Draft{Name: "سمير", Phone: "123456789012345678901"}, domain.CodePhoneInvalid},
	} {
		if _, err := domain.NewCustomer(newID(t), bad.d); code(err) != bad.code {
			t.Errorf("%+v: %v, want %s", bad.d, err, bad.code)
		}
	}
	long := ""
	for range 101 {
		long += "م"
	}
	if _, err := domain.NewCustomer(newID(t), domain.Draft{Name: long}); code(err) != domain.CodeNameTooLong {
		t.Errorf("a long name: %v", err)
	}
}

func TestTheNextEntryContinuesTheChain(t *testing.T) {
	var place domain.Place
	opening, err := place.Append(domain.Entry{Kind: domain.KindOpening, AmountMinor: 1_500})
	if err != nil || opening.Seq != 1 || opening.BalanceBeforeMinor != 0 || opening.BalanceAfterMinor != 1_500 {
		t.Fatalf("opening = %+v, %v", opening, err)
	}
	place = domain.Place{Seq: opening.Seq, BalanceMinor: opening.BalanceAfterMinor}
	charge, err := place.Append(domain.Entry{Kind: domain.KindCharge, AmountMinor: 625})
	if err != nil || charge.Seq != 2 || charge.BalanceBeforeMinor != 1_500 || charge.BalanceAfterMinor != 2_125 {
		t.Fatalf("charge = %+v, %v", charge, err)
	}
	if _, err := place.Append(domain.Entry{Kind: domain.KindOpening, AmountMinor: 0}); code(err) != domain.CodeAmountRequired {
		t.Fatalf("a zero entry: %v", err)
	}
	if _, err := (domain.Place{BalanceMinor: 9_000_000_000_000_000_000}).Append(domain.Entry{Kind: domain.KindCharge, AmountMinor: 9_000_000_000_000_000_000}); code(err) != domain.CodeAmountTooLarge {
		t.Fatalf("an overflowing balance: %v", err)
	}
}

func TestAPaymentNeverTakesABalanceBelowZero(t *testing.T) {
	owed := domain.Place{Seq: 1, BalanceMinor: 1_000}
	if _, err := owed.Append(domain.Entry{Kind: domain.KindPayment, AmountMinor: -1_001}); code(err) != domain.CodeAmountTooLarge {
		t.Fatalf("an overpayment written as a payment: %v", err)
	}
	if _, err := owed.Append(domain.Entry{Kind: domain.KindWriteOff, AmountMinor: -1_001}); code(err) != domain.CodeWriteOffTooLarge {
		t.Fatalf("a write-off beyond the balance: %v", err)
	}
	if _, err := (domain.Place{Seq: 1}).Append(domain.Entry{Kind: domain.KindPayment, AmountMinor: -1}); code(err) != domain.CodeNothingOwed {
		t.Fatalf("a payment of nothing owed: %v", err)
	}
	if e, err := owed.Append(domain.Entry{Kind: domain.KindPayment, AmountMinor: -1_000}); err != nil || e.BalanceAfterMinor != 0 {
		t.Fatalf("paying it all = %+v, %v", e, err)
	}
	if _, err := domain.PricePayment(domain.CashInput{Currency: "USD", Amount: "5"}, cashContext(t, 0)); code(err) != domain.CodeNothingOwed {
		t.Fatalf("a payment quote with nothing owed: %v", err)
	}
}

// TestAnAmountHandedOverSettlesAtTodaysRate is L5 §6.2's table.
func TestAnAmountHandedOverSettlesAtTodaysRate(t *testing.T) {
	for _, c := range []struct {
		name                   string
		balance                int64
		in                     domain.CashInput
		settled, after, change int64
		changeCurrency         string
	}{
		{"16.25 USD, 100,000 pounds handed over", 1_625, domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "100000"}, 667, 958, 0, "SYP"},
		{"9.58 USD, $20 handed over", 958, domain.CashInput{Currency: "USD", TenderCurrency: "USD", Amount: "20"}, 958, 0, 1_042, "USD"},
		{"250,000 pounds, $10 handed over", 250_000, domain.CashInput{Currency: "SYP", TenderCurrency: "USD", Amount: "10"}, 150_000, 100_000, 0, "SYP"},
		{"100,000 pounds, $10 handed over", 100_000, domain.CashInput{Currency: "SYP", TenderCurrency: "USD", Amount: "10"}, 100_000, 0, 50_000, "SYP"},
		{"pounds for pounds", 100_000, domain.CashInput{Currency: "SYP", Amount: "٤٠٠٠٠"}, 40_000, 60_000, 0, "SYP"},
	} {
		q, err := domain.PricePayment(c.in, cashContext(t, c.balance))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if q.SettledMinor != c.settled || q.BalanceAfterMinor != c.after || q.ChangeMinor != c.change || q.Change.Code != c.changeCurrency || q.Token == "" {
			t.Errorf("%s: settled %d after %d change %d %s", c.name, q.SettledMinor, q.BalanceAfterMinor, q.ChangeMinor, q.Change.Code)
		}
	}
	for _, bad := range []struct {
		in   domain.CashInput
		code string
	}{
		{domain.CashInput{Currency: "USD", Amount: ""}, domain.CodeAmountRequired},
		{domain.CashInput{Currency: "USD", Amount: "0"}, domain.CodeAmountRequired},
		{domain.CashInput{Currency: "USD", Amount: "1.005"}, domain.CodeAmountDecimals},
		{domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "10.5"}, domain.CodeAmountDecimals},
		{domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "50"}, domain.CodeBelowNote}, // a third of a cent
		{domain.CashInput{Currency: "EUR", Amount: "1"}, domain.CodeUnknownCurrency},
		{domain.CashInput{Currency: "USD", TenderCurrency: "EUR", Amount: "1"}, domain.CodeUnknownCurrency},
		{domain.CashInput{Currency: "USD", Amount: "99999999999999999"}, domain.CodeAmountTooLarge},
	} {
		if _, err := domain.PricePayment(bad.in, cashContext(t, 1_625)); code(err) != bad.code {
			t.Errorf("%+v: %v, want %s", bad.in, err, bad.code)
		}
	}
	noRate := cashContext(t, 1_625)
	noRate.RateNano = 0
	if _, err := domain.PricePayment(domain.CashInput{Currency: "USD", Amount: "1"}, noRate); code(err) != domain.CodeNoRate {
		t.Fatalf("no rate: %v", err)
	}
}

func TestChangeIsInPoundsUnlessDebtAndTenderAreDollars(t *testing.T) {
	ctx := cashContext(t, 958)
	for _, c := range []struct {
		in   domain.CashInput
		want string
		code string
	}{
		{domain.CashInput{Currency: "USD", Amount: "20"}, "USD", ""},
		{domain.CashInput{Currency: "USD", Amount: "20", ChangeCurrency: "SYP"}, "", domain.CodeChangeCurrency},
		{domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "200000"}, "SYP", ""},
		{domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "200000", ChangeCurrency: "USD"}, "USD", ""},
	} {
		q, err := domain.PricePayment(c.in, ctx)
		if code(err) != c.code || (err == nil && q.Change.Code != c.want) {
			t.Errorf("%+v: %s, %v", c.in, q.Change.Code, err)
		}
	}
	// 200,000 pounds on 9.58 USD: 143,700 of it settles the debt, 56,300 back — 56,500 to the note.
	q, _ := domain.PricePayment(domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "200000"}, ctx)
	if q.ChangeMinor != 56_500 || q.SettledMinor != 958 {
		t.Fatalf("pound change = %+v", q)
	}
}

// TestPayAllClearsTheBalanceExactly is §6.3's table, then the property: whatever the balance and rate, pay all leaves
// zero, and the cash taken is within the verifier's bound.
func TestPayAllClearsTheBalanceExactly(t *testing.T) {
	q, err := domain.PricePayment(domain.CashInput{Currency: "USD", TenderCurrency: "SYP", All: true}, cashContext(t, 958))
	if err != nil || q.TenderedMinor != 143_500 || q.SettledMinor != 958 || q.BalanceAfterMinor != 0 || q.ChangeMinor != 0 {
		t.Fatalf("9.58 USD in pounds = %+v, %v", q, err)
	}
	q, err = domain.PricePayment(domain.CashInput{Currency: "SYP", TenderCurrency: "USD", All: true}, cashContext(t, 100_000))
	if err != nil || q.TenderedMinor != 667 || q.SettledMinor != 100_000 || q.BalanceAfterMinor != 0 {
		t.Fatalf("100,000 pounds in dollars = %+v, %v", q, err)
	}
	if _, err := domain.PricePayment(domain.CashInput{Currency: "USD", TenderCurrency: "SYP", All: true}, cashContext(t, 1)); code(err) != domain.CodeBelowNote {
		t.Fatalf("one cent in pounds: %v", err)
	}

	rapid.Check(t, func(rt *rapid.T) {
		balance := rapid.Int64Range(1, 100_000_000).Draw(rt, "balance")
		rate := rapid.Int64Range(100_000_000_000, 50_000_000_000_000).Draw(rt, "rate")
		debt := rapid.SampledFrom([]string{"USD", "SYP"}).Draw(rt, "debt")
		paid := rapid.SampledFrom([]string{"USD", "SYP"}).Draw(rt, "paid")
		c := cashContext(t, balance)
		c.RateNano = rate
		q, err := domain.PricePayment(domain.CashInput{Currency: debt, TenderCurrency: paid, All: true}, c)
		if code(err) == domain.CodeBelowNote {
			return
		}
		if err != nil || q.BalanceAfterMinor != 0 || q.SettledMinor != balance {
			rt.Fatalf("pay all = %+v, %v", q, err)
		}
		customer := newID(t)
		e, err := (domain.Place{BalanceMinor: balance}).Append(q.Entry(customer, "x", ""))
		if err != nil {
			rt.Fatal(err)
		}
		e.ID, e.CustomerID = newID(t), customer
		opening := domain.Entry{ID: newID(t), CustomerID: customer, Currency: debt, Seq: 1, Kind: domain.KindOpening, AmountMinor: balance, BalanceAfterMinor: balance}
		e.Seq = 2
		if f := domain.Verify([]domain.Entry{opening, e}, []domain.Currency{syp, usd}); len(f) != 0 {
			rt.Fatalf("the verifier refused pay all: %+v, entry %+v", f, e)
		}
	})
}

func TestARefundOnlyReturnsWhatIsOwedBack(t *testing.T) {
	if _, err := domain.PriceRefund(domain.CashInput{Currency: "SYP", Amount: "1"}, cashContext(t, 0)); code(err) != domain.CodeNothingOwedBack {
		t.Fatalf("a refund of nothing: %v", err)
	}
	c := cashContext(t, -50_000)
	if _, err := domain.PriceRefund(domain.CashInput{Currency: "SYP", Amount: "50500"}, c); code(err) != domain.CodeRefundTooLarge {
		t.Fatalf("a refund past zero: %v", err)
	}
	q, err := domain.PriceRefund(domain.CashInput{Currency: "SYP", Amount: "20000"}, c)
	if err != nil || q.SettledMinor != 20_000 || q.BalanceAfterMinor != -30_000 || q.ChangeMinor != 0 {
		t.Fatalf("part refund = %+v, %v", q, err)
	}
	all, err := domain.PriceRefund(domain.CashInput{Currency: "SYP", TenderCurrency: "USD", All: true}, c)
	if err != nil || all.TenderedMinor != 333 || all.SettledMinor != 50_000 || all.BalanceAfterMinor != 0 {
		t.Fatalf("refund all in dollars = %+v, %v", all, err)
	}
	e := all.Entry(c.CustomerID, "سمير", "أعيد")
	if e.Kind != domain.KindRefund || e.AmountMinor != 50_000 || e.Cash.TenderedCurrency != "USD" {
		t.Fatalf("refund entry = %+v", e)
	}
	if _, err := (domain.Place{BalanceMinor: -50_000}).Append(domain.Entry{Kind: domain.KindRefund, AmountMinor: 50_001}); code(err) != domain.CodeRefundTooLarge {
		t.Fatalf("the chain's refund rule: %v", err)
	}
}

func TestOwedSinceIsTheDayTheCurrentDebtStarted(t *testing.T) {
	chain := []domain.Entry{
		{Seq: 1, BusinessDate: "2026-08-01", Kind: domain.KindCharge, AmountMinor: 500, BalanceAfterMinor: 500},
		{Seq: 2, BusinessDate: "2026-08-10", Kind: domain.KindPayment, AmountMinor: -500, BalanceBeforeMinor: 500},
		{Seq: 3, BusinessDate: "2026-09-12", Kind: domain.KindCharge, AmountMinor: 700, BalanceAfterMinor: 700},
		{Seq: 4, BusinessDate: "2026-09-13", Kind: domain.KindCharge, AmountMinor: 300, BalanceBeforeMinor: 700, BalanceAfterMinor: 1_000},
	}
	s := domain.Summarise("USD", chain)
	if s.OwedSince != "2026-09-12" || s.LastPayment != "2026-08-10" || s.BalanceMinor != 1_000 || s.Place != (domain.Place{Seq: 4, BalanceMinor: 1_000}) {
		t.Fatalf("summary = %+v", s)
	}
	if s := domain.Summarise("USD", chain[:2]); s.OwedSince != "" || s.BalanceMinor != 0 {
		t.Fatalf("paid off = %+v", s)
	}
	if s := domain.Summarise("USD", nil); s.Place != (domain.Place{}) {
		t.Fatalf("an empty chain = %+v", s)
	}
}

func TestReversalRules(t *testing.T) {
	payment := domain.Entry{ID: newID(t), CustomerID: newID(t), Currency: "USD", Kind: domain.KindPayment, AmountMinor: -500, CustomerName: "سمير"}
	r, err := domain.Reverse(payment, " خطأ ", false)
	if err != nil || r.AmountMinor != 500 || r.ReversesID != payment.ID || r.Kind != domain.KindReversal || r.Note != "خطأ" || r.CustomerName != "سمير" {
		t.Fatalf("reversal = %+v, %v", r, err)
	}
	if _, err := domain.Reverse(payment, " ", false); code(err) != domain.CodeReasonRequired {
		t.Fatalf("no reason: %v", err)
	}
	if _, err := domain.Reverse(r, "x", false); code(err) != domain.CodeNotReversible {
		t.Fatalf("a reversal reversed: %v", err)
	}
	charge := domain.Entry{ID: newID(t), Kind: domain.KindCharge, AmountMinor: 800}
	if _, err := domain.Reverse(charge, "x", false); code(err) != domain.CodeNotReversible {
		t.Fatalf("a charge reversed by hand: %v", err)
	}
	if _, err := domain.Reverse(charge, "void", true); err != nil {
		t.Fatalf("a charge reversed by its sale's void: %v", err)
	}
	// A reversal may take a balance below zero (L5 H4).
	if e, err := (domain.Place{Seq: 2, BalanceMinor: 300}).Append(domain.Entry{Kind: domain.KindReversal, AmountMinor: -800}); err != nil || e.BalanceAfterMinor != -500 {
		t.Fatalf("a reversal below zero = %+v, %v", e, err)
	}
}

func TestAPaymentTokenMovesWithThePlaceAndTheRate(t *testing.T) {
	c := cashContext(t, 1_625)
	in := domain.CashInput{Currency: "USD", TenderCurrency: "SYP", Amount: "100000"}
	a, _ := domain.PricePayment(in, c)
	b, _ := domain.PricePayment(in, c)
	if a.Token != b.Token {
		t.Fatal("the same payment quoted twice")
	}
	moved := c
	moved.Place.Seq++
	rated := c
	rated.RateID, rated.RateNano = newID(t), 15_500_000_000_000
	other := in
	other.Amount = "100500"
	for name, pair := range map[string]struct {
		c  domain.CashContext
		in domain.CashInput
	}{"the place": {moved, in}, "the rate": {rated, in}, "the amount": {c, other}} {
		if q, _ := domain.PricePayment(pair.in, pair.c); q.Token == a.Token {
			t.Errorf("%s changed and the token did not", name)
		}
	}
}

// TestTheVerifier plants one discrepancy per L5 §9.1 row into a clean book.
func TestTheVerifier(t *testing.T) {
	customer := newID(t)
	e := func(seq int64, kind domain.Kind, amount, before int64) domain.Entry {
		return domain.Entry{ID: newID(t), CustomerID: customer, Currency: "USD", Seq: seq, Kind: kind, AmountMinor: amount,
			BalanceBeforeMinor: before, BalanceAfterMinor: before + amount, CustomerName: "سمير"}
	}
	opening := e(1, domain.KindOpening, 1_625, 0)
	payment := e(2, domain.KindPayment, -667, 1_625)
	payment.Cash = domain.Cash{RateNano: r15000, CashNoteMinor: 500, TenderedCurrency: "SYP", TenderedMinor: 100_000, ChangeCurrency: "SYP"}
	reversal := e(3, domain.KindReversal, 667, 958)
	reversal.ReversesID = payment.ID
	book := func() []domain.Entry { return []domain.Entry{opening, payment, reversal} }
	currencies := []domain.Currency{syp, usd}
	if f := domain.Verify(book(), currencies); len(f) != 0 {
		t.Fatalf("a clean book: %+v", f)
	}
	plants := map[string]func([]domain.Entry){
		domain.FindingChainBroken: func(b []domain.Entry) {
			b[1].BalanceBeforeMinor++
			b[1].BalanceAfterMinor++
			b[2].BalanceBeforeMinor++
			b[2].BalanceAfterMinor++
		},
		domain.FindingBadReversal: func(b []domain.Entry) { b[2].ReversesID = opening.ID },
		domain.FindingCashWrong:   func(b []domain.Entry) { b[1].Cash.TenderedMinor = 90_000 },
	}
	for want, plant := range plants {
		b := book()
		plant(b)
		f := domain.Verify(b, currencies)
		if len(f) == 0 || f[0].Code != want {
			t.Errorf("planted %s, found %+v", want, f)
		}
	}
	gap := book()
	gap[2].Seq = 4
	if f := domain.Verify(gap, currencies); len(f) != 1 || f[0].Code != domain.FindingChainBroken {
		t.Errorf("a missing place: %+v", f)
	}
}
