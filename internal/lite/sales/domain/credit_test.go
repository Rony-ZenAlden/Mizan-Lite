package domain_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// creditShop is newShop with a customer, سمير, who owes 12.00 USD and nothing in pounds.
func creditShop(t *testing.T) (shop, domain.Customer) {
	s := newShop(t)
	c := domain.Customer{ID: newID(t), Name: "سمير", Active: true}
	s.ctx.Customer, s.ctx.Balances = c, map[string]int64{"USD": 1_200}
	return s, c
}

func onCredit(c domain.Customer, settle, tenderCur, tendered string, lines ...domain.LineInput) domain.CartInput {
	return domain.CartInput{Lines: lines, Settlement: settle, TenderCurrency: tenderCur, Tendered: tendered, Payment: domain.PaymentCredit, CustomerID: c.ID}
}

func TestACreditSaleNeedsACustomer(t *testing.T) {
	s, c := creditShop(t)
	noOne := onCredit(domain.Customer{}, "", "", "", line(s.jar, "1"))
	q := price(t, noOne, s.ctx)
	if !q.NeedsCustomer || q.DebtMinor != 45_000 || q.Payment != domain.PaymentCredit {
		t.Fatalf("a credit quote before a customer is chosen = %+v", q)
	}
	stranger := onCredit(domain.Customer{ID: newID(t)}, "", "", "", line(s.jar, "1"))
	if _, err := domain.Price(stranger, s.ctx); code(err) != domain.CodeCustomerNotFound {
		t.Fatalf("a customer the book does not have: %v", err)
	}
	s.ctx.Customer.Active = false
	if _, err := domain.Price(onCredit(c, "", "", "", line(s.jar, "1")), s.ctx); code(err) != domain.CodeCustomerInactive {
		t.Fatalf("an inactive customer: %v", err)
	}
	if _, err := domain.Price(domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}, Payment: "cheque"}, s.ctx); code(err) != domain.CodeUnknownPayment {
		t.Fatalf("an unknown payment: %v", err)
	}
}

// TestPaidNowReducesTheDebtAndPaidInFullIsCash is L5 §5.2's table: a jar of molasses at 45,000 and a litre of oil at $6.50,
// at 15,000 and a 500-pound note.
func TestPaidNowReducesTheDebtAndPaidInFullIsCash(t *testing.T) {
	s, c := creditShop(t)
	jar, oil := line(s.jar, "1"), line(s.oil, "1")
	for _, row := range []struct {
		name string
		in   domain.CartInput
		debt int64
	}{
		{"pounds, nothing paid", onCredit(c, "SYP", "", "", jar), 45_000},
		{"pounds, pounds paid", onCredit(c, "SYP", "SYP", "20000", jar), 25_000},
		{"pounds, dollars paid: 45,000 − 31,950, to the note", onCredit(c, "SYP", "USD", "2.13", jar), 13_000},
		{"dollars, nothing paid", onCredit(c, "USD", "", "", oil), 650},
		{"dollars, dollars paid", onCredit(c, "USD", "USD", "2.50", oil), 400},
		{"dollars, pounds paid: 6.50 − 3.3333, to the cent", onCredit(c, "USD", "SYP", "50000", oil), 317},
		{"dollars, nearly all paid in pounds", onCredit(c, "USD", "SYP", "97000", oil), 3},
	} {
		q, err := domain.Price(row.in, s.ctx)
		if err != nil || q.DebtMinor != row.debt || q.ChangeMinor != 0 || q.Customer.ID != c.ID {
			t.Errorf("%s: debt %d change %d, %v", row.name, q.DebtMinor, q.ChangeMinor, err)
		}
	}
	for name, in := range map[string]domain.CartInput{
		"pounds paid in full":            onCredit(c, "SYP", "SYP", "45000", jar),
		"more than the total":            onCredit(c, "SYP", "USD", "10", jar),
		"dollars paid in full in pounds": onCredit(c, "USD", "SYP", "97500", oil),
	} {
		if _, err := domain.Price(in, s.ctx); code(err) != domain.CodeCreditPaidInFull {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestTheDebtIsInTheCurrencyTheSaleIsChargedIn is A-L5.2, with the balance the till shows before Pay.
func TestTheDebtIsInTheCurrencyTheSaleIsChargedIn(t *testing.T) {
	s, c := creditShop(t)
	dollars := price(t, onCredit(c, "USD", "", "", line(s.jar, "1")), s.ctx)
	if dollars.Settlement.Code != "USD" || dollars.DebtMinor != 300 || dollars.BalanceBeforeMinor != 1_200 || dollars.BalanceAfterMinor != 1_500 {
		t.Fatalf("a pounds-priced jar charged in dollars = %+v", dollars)
	}
	pounds := price(t, onCredit(c, "SYP", "", "", line(s.jar, "1")), s.ctx)
	if pounds.Settlement.Code != "SYP" || pounds.DebtMinor != 45_000 || pounds.BalanceBeforeMinor != 0 || pounds.BalanceAfterMinor != 45_000 {
		t.Fatalf("charged in pounds = %+v", pounds)
	}
}

func TestACreditSaleTotalIsTheCashTotal(t *testing.T) {
	s, c := creditShop(t)
	lines := []domain.LineInput{line(s.oil, "1.5"), line(s.bulgur, "1.750")}
	cash := price(t, domain.CartInput{Lines: lines}, s.ctx)
	credit := price(t, onCredit(c, "", "", "", lines...), s.ctx)
	if cash.TotalMinor != credit.TotalMinor || cash.RoundingMinor != credit.RoundingMinor || credit.RoundingMinor == 0 || credit.DebtMinor != credit.TotalMinor {
		t.Fatalf("cash %d (%d) vs credit %d (%d), debt %d", cash.TotalMinor, cash.RoundingMinor, credit.TotalMinor, credit.RoundingMinor, credit.DebtMinor)
	}
}

func TestTheTokenCoversThePaymentAndTheCustomer(t *testing.T) {
	s, c := creditShop(t)
	cash := price(t, domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}}, s.ctx)
	credit := price(t, onCredit(c, "", "", "", line(s.jar, "1")), s.ctx)
	other := domain.Customer{ID: newID(t), Name: "خالد", Active: true}
	s.ctx.Customer = other
	otherQ := price(t, onCredit(other, "", "", "", line(s.jar, "1")), s.ctx)
	s.ctx.Balances = map[string]int64{"SYP": 99_000} // a repayment elsewhere moves the balance, not what is charged
	balanceMoved := price(t, onCredit(other, "", "", "", line(s.jar, "1")), s.ctx)
	if cash.Token == credit.Token || credit.Token == otherQ.Token || otherQ.Token != balanceMoved.Token {
		t.Fatal("the token does not cover the payment and customer, or covers the balance")
	}
}

// TestTheSalesVerifierKnowsCreditSales plants one discrepancy per L5 §9.2 row.
func TestTheSalesVerifierKnowsCreditSales(t *testing.T) {
	s, c := creditShop(t)
	day := func() ([]domain.Sale, []domain.StockMovement, []domain.Charge) {
		cash := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2")}, TenderCurrency: "USD", Tendered: "20"}, 1)
		credit := assembled(t, s, onCredit(c, "USD", "SYP", "50000", line(s.oil, "1")), 2)
		voided := assembled(t, s, onCredit(c, "SYP", "SYP", "20000", line(s.jar, "1")), 3)
		voided, _ = voided.Void(time.Now(), "2026-09-14", "أعاده")
		sales := []domain.Sale{cash, credit, voided}
		var moves []domain.StockMovement
		for _, sale := range sales {
			moves = append(moves, movementsOf(sale)...)
		}
		charges := []domain.Charge{
			{SaleID: credit.ID, CustomerID: c.ID, Currency: "USD", AmountMinor: 317},
			{SaleID: voided.ID, CustomerID: c.ID, Currency: "SYP", AmountMinor: 25_000, Reversed: true},
		}
		return sales, moves, charges
	}
	sales, moves, charges := day()
	if f := domain.Verify(sales, moves, charges, syp, usd); len(f) != 0 {
		t.Fatalf("a consistent day with credit: %+v", f)
	}
	for name, plant := range map[string]struct {
		change func([]domain.Sale, []domain.Charge) ([]domain.Sale, []domain.Charge)
		code   string
	}{
		"a credit sale with no charge": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			return ss, ch[1:]
		}, domain.FindingCreditWithoutCharge},
		"a credit sale with two charges": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			return ss, append(ch, ch[0])
		}, domain.FindingChargeWrong},
		"a charge of the wrong amount": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			ch[0].AmountMinor = 316
			return ss, ch
		}, domain.FindingChargeWrong},
		"a charge in the other currency": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			ch[0].Currency = "SYP"
			return ss, ch
		}, domain.FindingChargeWrong},
		"a voided credit sale whose charge stands": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			ch[1].Reversed = false
			return ss, ch
		}, domain.FindingChargeWrong},
		"a charge on a cash sale": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			return ss, append(ch, domain.Charge{SaleID: ss[0].ID, CustomerID: c.ID, Currency: "SYP", AmountMinor: 1})
		}, domain.FindingChargeWithoutCredit},
		"a charge naming no sale": {func(ss []domain.Sale, ch []domain.Charge) ([]domain.Sale, []domain.Charge) {
			return ss, append(ch, domain.Charge{SaleID: newID(t), CustomerID: c.ID, Currency: "SYP", AmountMinor: 1})
		}, domain.FindingChargeWithoutCredit},
	} {
		t.Run(name, func(t *testing.T) {
			ss, m, ch := day()
			ss, ch = plant.change(ss, ch)
			found := domain.Verify(ss, m, ch, syp, usd)
			if len(found) == 0 {
				t.Fatalf("nothing found; want %s", plant.code)
			}
			for _, f := range found {
				if f.Code != plant.code {
					t.Fatalf("found %+v; want only %s", found, plant.code)
				}
			}
		})
	}
}

// TestAVoidReturnsWhatTheReceiptSaysWasPaid is L6 §7.2 and Q-L6.5: a cash sale hands back its total, a credit sale what was
// paid now, both in the currency the receipt was charged in.
func TestAVoidReturnsWhatTheReceiptSaysWasPaid(t *testing.T) {
	cash := domain.Sale{Payment: domain.PaymentCash, SettlementCurrency: "SYP", TotalMinor: 73_000, TenderedCurrency: "USD", TenderedMinor: 500}
	if cur, minor := cash.VoidReturn(); cur != "SYP" || minor != 73_000 {
		t.Fatalf("cash = %s %d", cur, minor)
	}
	credit := domain.Sale{Payment: domain.PaymentCredit, SettlementCurrency: "SYP", TotalMinor: 76_000, TenderedMinor: 20_000,
		Credit: domain.Credit{Currency: "SYP", AmountMinor: 56_000}}
	if cur, minor := credit.VoidReturn(); cur != "SYP" || minor != 20_000 {
		t.Fatalf("credit = %s %d", cur, minor)
	}
	nothingPaid := domain.Sale{Payment: domain.PaymentCredit, SettlementCurrency: "USD", TotalMinor: 1_625, Credit: domain.Credit{AmountMinor: 1_625}}
	if cur, minor := nothingPaid.VoidReturn(); cur != "USD" || minor != 0 {
		t.Fatalf("credit with nothing paid = %s %d", cur, minor)
	}
}
