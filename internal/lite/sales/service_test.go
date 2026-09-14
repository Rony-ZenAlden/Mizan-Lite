package sales_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales/salestest"
)

const u = int64(1_000_000)

var (
	ctx      = context.Background()
	damascus = time.FixedZone("Damascus", 3*3600)
)

type fixture struct {
	svc       *sales.Service
	store     *salestest.Fake
	catalogue *salestest.Catalogue
	stock     *salestest.Stock
	debts     *salestest.Debts
	rates     *salestest.Rates
	settings  *salestest.Settings
	gate      *salestest.Gate
	clk       *clock.Fixed
	oil, jar  domain.Product
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{
		store: salestest.NewFake(), catalogue: salestest.NewCatalogue(), stock: salestest.NewStock(), debts: salestest.NewDebts(), rates: salestest.NewRates(),
		settings: salestest.NewSettings(), gate: &salestest.Gate{},
		clk: clock.NewFixed(time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC)), // 10:00 in Damascus
	}
	f.oil = f.catalogue.Add(domain.Product{NameAR: "زيت زيتون", NameEN: "Olive oil", UnitCode: "l", UnitDecimals: 3, PriceCurrency: "USD", PriceMicro: 6_500_000, Active: true}, "6290001000011")
	f.jar = f.catalogue.Add(domain.Product{NameAR: "دبس رمان", UnitCode: "jar", PriceCurrency: "SYP", PriceMicro: 45_000 * u, Active: true}, "6290001000028")
	f.stock.Set(f.oil.ID, domain.Stocked{OnHandMicro: 12 * u, CostKnown: true, AvgCostMicro: 4_680_000})
	f.stock.Set(f.jar.ID, domain.Stocked{OnHandMicro: 3 * u, CostKnown: true, AvgCostMicro: 2 * u})
	f.svc = sales.NewService(litetest.Immediate{}, f.store, f.catalogue, f.stock, f.debts, f.rates, f.settings, f.gate, f.clk, damascus)
	return f
}

func cart(lines ...domain.LineInput) domain.CartInput { return domain.CartInput{Lines: lines} }

func line(p domain.Product, qty string) domain.LineInput {
	return domain.LineInput{ProductID: p.ID, Quantity: qty}
}

// sell quotes and checks out a cart as the till does.
func (f fixture) sell(t *testing.T, in domain.CartInput) domain.Sale {
	t.Helper()
	q, err := f.svc.Quote(ctx, in)
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	sale, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	return sale
}

func TestACheckoutRecordsTheSaleAndMovesItsStock(t *testing.T) {
	f := newFixture(t)
	in := cart(line(f.oil, "2"), line(f.jar, "1"))
	in.TenderCurrency, in.Tendered = "USD", "20"
	sale := f.sell(t, in)
	// 2 L at $6.50 is 195,000 pounds, one jar 45,000: 240,000; $20 at 15,000 is 300,000; change 60,000 pounds.
	if sale.ReceiptNo != 1 || sale.TotalMinor != 240_000 || sale.ChangeCurrency != "SYP" || sale.ChangeMinor != 60_000 ||
		sale.BusinessDate != "2026-09-14" || sale.ShopName != "بقالية المونة" || sale.RateNano != 15_000_000_000_000 {
		t.Fatalf("sale = %+v", sale)
	}
	stored, err := f.svc.Receipt(ctx, sale.ID)
	if err != nil || stored.ID != sale.ID || len(stored.Lines) != 2 {
		t.Fatalf("receipt = %+v, %v", stored, err)
	}
	sold := f.stock.Sold()
	if len(sold) != 2 || sold[0].SaleLineID != sale.Lines[0].ID || sold[0].QuantityMicro != 2*u || sold[1].SaleID != sale.ID {
		t.Fatalf("stock = %+v", sold)
	}
	if f.gate.Asked != 0 {
		t.Fatal("a sale without a discount asked for the owner")
	}
}

func TestCheckoutRefusesAStaleQuoteAndWritesNothing(t *testing.T) {
	for name, change := range map[string]func(f fixture){
		"the rate":  func(f fixture) { f.rates.Change(15_200_000_000_000) },
		"a price":   func(f fixture) { p := f.oil; p.PriceMicro = 7 * u; f.catalogue.Update(p) },
		"a version": func(f fixture) { f.catalogue.Update(f.oil) },
		"the note":  func(f fixture) { f.settings.Note = 1000 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			in := cart(line(f.oil, "2"))
			q, err := f.svc.Quote(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			change(f)
			if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); errs.CodeOf(err) != domain.CodeQuoteStale {
				t.Fatalf("err = %v", err)
			}
			if n, _ := f.store.NextReceiptNo(ctx); n != 1 || len(f.stock.Sold()) != 0 {
				t.Fatal("a stale checkout wrote something")
			}
			// Re-quoted, it sells.
			f.sell(t, in)
		})
	}
}

func TestNoRateNoSale(t *testing.T) {
	f := newFixture(t)
	f.rates.Set = false
	if _, err := f.svc.Quote(ctx, cart(line(f.jar, "1"))); errs.CodeOf(err) != domain.CodeNoRate {
		t.Fatalf("quote: %v", err)
	}
	if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: cart(line(f.jar, "1")), Token: "x"}); errs.CodeOf(err) != domain.CodeNoRate {
		t.Fatalf("checkout: %v", err)
	}
}

func TestReceiptNumbersAreContinuous(t *testing.T) {
	f := newFixture(t)
	for want := int64(1); want <= 3; want++ {
		if sale := f.sell(t, cart(line(f.jar, "1"))); sale.ReceiptNo != want {
			t.Fatalf("receipt %d, want %d", sale.ReceiptNo, want)
		}
	}
}

// TestAnInactiveProductInTheCartIsRefused: deactivated after the quote, refused at checkout.
func TestAnInactiveProductInTheCartIsRefused(t *testing.T) {
	f := newFixture(t)
	in := cart(line(f.jar, "1"))
	q, _ := f.svc.Quote(ctx, in)
	p := f.jar
	p.Active = false
	f.catalogue.Update(p)
	if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); errs.CodeOf(err) != domain.CodeInactiveProduct {
		t.Fatalf("err = %v", err)
	}
}

func TestADiscountNeedsTheOwner(t *testing.T) {
	f := newFixture(t)
	in := domain.CartInput{Lines: []domain.LineInput{{ProductID: f.jar.ID, Quantity: "2", DiscountPercent: "10"}}, SaleDiscount: "1000"}
	q, err := f.svc.Quote(ctx, in)
	if err != nil || !q.Discounted {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	if _, err = f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); errs.CodeOf(err) != sales.CodeOwnerRequired {
		t.Fatalf("a discount outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	sale, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	// 90,000 − 10% = 81,000; − 1,000 = 80,000.
	if err != nil || sale.TotalMinor != 80_000 || sale.DiscountLocalMinor != 1_000 || sale.Lines[0].DiscountLocalMinor != 9_000 {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	if len(f.gate.Acts) != 1 || f.gate.Acts[0] != (sales.GuardedAct{Action: sales.ActDiscount, Before: "90000 SYP", After: "80000 SYP"}) {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}
}

// credit is a cart put on credit.
func credit(customer domain.Customer, settle, tenderCur, tendered string, lines ...domain.LineInput) domain.CartInput {
	return domain.CartInput{Lines: lines, Settlement: settle, TenderCurrency: tenderCur, Tendered: tendered, Payment: domain.PaymentCredit, CustomerID: customer.ID}
}

func TestACreditSaleNeedsACustomer(t *testing.T) {
	f := newFixture(t)
	in := credit(domain.Customer{}, "", "", "", line(f.jar, "1"))
	q, err := f.svc.Quote(ctx, in)
	if err != nil || !q.NeedsCustomer {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); errs.CodeOf(err) != domain.CodeCustomerRequired {
		t.Fatalf("a credit sale with no customer: %v", err)
	}
	if n, _ := f.store.NextReceiptNo(ctx); n != 1 || len(f.stock.Sold()) != 0 || len(f.debts.Charges()) != 0 {
		t.Fatal("a refused credit sale wrote")
	}
}

// TestACreditSaleChargesItsCustomerInTheSameCheckout: a pounds total of 90,000 with 20,000 paid now, charged to سمير.
func TestACreditSaleChargesItsCustomerInTheSameCheckout(t *testing.T) {
	f := newFixture(t)
	samir := f.debts.Add(domain.Customer{Name: "سمير", Active: true}, map[string]int64{"SYP": 10_000})
	in := credit(samir, "SYP", "SYP", "20000", line(f.jar, "2"))
	q, err := f.svc.Quote(ctx, in)
	if err != nil || q.DebtMinor != 70_000 || q.BalanceBeforeMinor != 10_000 || q.BalanceAfterMinor != 80_000 || q.Customer.Name != "سمير" {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	sale, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.Payment != domain.PaymentCredit || sale.TenderedMinor != 20_000 || sale.ChangeMinor != 0 ||
		sale.Credit != (domain.Credit{CustomerID: samir.ID, CustomerName: "سمير", Currency: "SYP", AmountMinor: 70_000, BalanceAfterMinor: 80_000}) {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	if ch := f.debts.Charges(); len(ch) != 1 || ch[0].SaleID != sale.ID || ch[0].AmountMinor != 70_000 {
		t.Fatalf("charges = %+v", ch)
	}
	if receipt, _ := f.svc.Receipt(ctx, sale.ID); receipt.Credit.AmountMinor != 70_000 {
		t.Fatalf("the receipt lost its credit: %+v", receipt.Credit)
	}
	day, _ := f.svc.Day(ctx, "")
	if syp := day.Totals["SYP"]; syp.OnCreditMinor != 70_000 || syp.CashInMinor != 20_000 || syp.ChargedMinor != 90_000 {
		t.Fatalf("the day = %+v", *syp)
	}
	f.gate.Elevated = true
	if findings, err := f.svc.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}

func TestAFailedChargeFailsTheCheckout(t *testing.T) {
	f := newFixture(t)
	samir := f.debts.Add(domain.Customer{Name: "سمير", Active: true}, nil)
	in := credit(samir, "USD", "", "", line(f.oil, "1"))
	q, _ := f.svc.Quote(ctx, in)
	f.debts.FailCharges()
	if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); !errors.Is(err, salestest.ErrInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestVoidingACreditSaleReversesItsCharge(t *testing.T) {
	f := newFixture(t)
	samir := f.debts.Add(domain.Customer{Name: "سمير", Active: true}, nil)
	sale := f.sell(t, credit(samir, "USD", "", "", line(f.oil, "1")))
	f.gate.Elevated = true
	voided, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "أعاده"})
	if err != nil || !voided.Credit.Reversed {
		t.Fatalf("voided = %+v, %v", voided, err)
	}
	if b, _ := f.debts.Balances(ctx, samir.ID); b["USD"] != 0 {
		t.Fatalf("balance after the void = %v", b)
	}
	if findings, _ := f.svc.Verify(ctx); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestVoidNeedsTheOwnerAndReturnsTheStock(t *testing.T) {
	f := newFixture(t)
	sale := f.sell(t, cart(line(f.oil, "2"), line(f.jar, "1")))
	if _, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "خطأ"}); errs.CodeOf(err) != sales.CodeOwnerRequired {
		t.Fatalf("a void outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	if _, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: " "}); errs.CodeOf(err) != domain.CodeVoidReasonRequired {
		t.Fatalf("a void without a reason: %v", err)
	}
	f.clk.Advance(24 * time.Hour) // voided the next day
	voided, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "الكمية خطأ"})
	if err != nil || voided.Status != domain.StatusVoided || voided.VoidBusinessDate != "2026-09-15" || voided.BusinessDate != "2026-09-14" {
		t.Fatalf("voided = %+v, %v", voided, err)
	}
	if v := f.stock.Voided(); len(v) != 2 || v[0] != sale.Lines[0].ID {
		t.Fatalf("stock returned = %v", v)
	}
	if _, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "again"}); errs.CodeOf(err) != domain.CodeAlreadyVoided {
		t.Fatalf("voided twice: %v", err)
	}
	if f.gate.Acts[0].Action != sales.ActVoid || f.gate.Acts[0].Before != "No. 1 · 240000 SYP" || f.gate.Acts[0].After != "الكمية خطأ" {
		t.Fatalf("act = %+v", f.gate.Acts[0])
	}
}

func TestTheDaysTotalsPerCurrency(t *testing.T) {
	f := newFixture(t)
	pounds := cart(line(f.jar, "1"))
	pounds.TenderCurrency, pounds.Tendered = "SYP", "50000"
	dollars := cart(line(f.oil, "1"))
	dollars.Settlement, dollars.TenderCurrency, dollars.Tendered = "USD", "USD", "10"
	mixed := cart(line(f.jar, "2"))
	mixed.TenderCurrency, mixed.Tendered = "USD", "10"
	f.sell(t, pounds)         // 45,000; in 50,000; change 5,000
	f.sell(t, dollars)        // $6.50; in $10; change $3.50
	third := f.sell(t, mixed) // 90,000; in $10; change 60,000 pounds
	f.gate.Elevated = true
	if _, err := f.svc.Void(ctx, sales.VoidInput{SaleID: third.ID, Reason: "خطأ"}); err != nil {
		t.Fatal(err)
	}
	day, err := f.svc.Day(ctx, "")
	if err != nil || day.BusinessDate != "2026-09-14" || len(day.Sales) != 3 {
		t.Fatalf("day = %+v, %v", day, err)
	}
	syp, usd := day.Totals["SYP"], day.Totals["USD"]
	// The voided sale was rung up today and voided today: charged and refunded both.
	if syp.Sales != 2 || syp.ChargedMinor != 135_000 || syp.CashInMinor != 50_000 || syp.ChangeOutMinor != 65_000 || syp.Voids != 1 || syp.RefundedMinor != 90_000 {
		t.Fatalf("SYP = %+v", *syp)
	}
	if usd.Sales != 1 || usd.ChargedMinor != 650 || usd.CashInMinor != 2_000 || usd.ChangeOutMinor != 350 || usd.Voids != 0 {
		t.Fatalf("USD = %+v", *usd)
	}
	if _, err := f.svc.Day(ctx, "14/09/2026"); errs.CodeOf(err) != sales.CodeInvalidDate {
		t.Fatalf("a bad date: %v", err)
	}
}

// TestAVoidBelongsToTheDayItWasMade: a sale voided the next day leaves its own day's totals as they were, and is counted
// on the day of the void (§8.1, D-L4.14) — a closed day never changes.
func TestAVoidBelongsToTheDayItWasMade(t *testing.T) {
	f := newFixture(t)
	sale := f.sell(t, cart(line(f.jar, "1"))) // 45,000 on the 14th
	before, err := f.svc.Day(ctx, "2026-09-14")
	if err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(24 * time.Hour)
	f.gate.Elevated = true
	if _, err = f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "أعاده الزبون"}); err != nil {
		t.Fatal(err)
	}

	sold, err := f.svc.Day(ctx, "2026-09-14")
	if err != nil || *sold.Totals["SYP"] != *before.Totals["SYP"] || sold.Totals["SYP"].Voids != 0 {
		t.Fatalf("the day of the sale changed: %+v, was %+v, %v", sold.Totals["SYP"], before.Totals["SYP"], err)
	}
	voided, err := f.svc.Day(ctx, "")
	if err != nil || voided.BusinessDate != "2026-09-15" || len(voided.Sales) != 1 || voided.Sales[0].ID != sale.ID {
		t.Fatalf("the day of the void = %+v, %v", voided, err)
	}
	if v := voided.Totals["SYP"]; v.Sales != 0 || v.ChargedMinor != 0 || v.CashInMinor != 0 || v.Voids != 1 || v.RefundedMinor != 45_000 {
		t.Fatalf("the void's day = %+v", *v)
	}
}

func TestScanFindsAProductWhateverTheKeyboardLayout(t *testing.T) {
	f := newFixture(t)
	got, found, err := f.svc.Scan(ctx, "٦٢٩٠٠٠١٠٠٠٠١١")
	if err != nil || !found || got.Product.ID != f.oil.ID || got.Stocked.OnHandMicro != 12*u {
		t.Fatalf("scan = %+v %v %v", got, found, err)
	}
	if _, found, _ := f.svc.Scan(ctx, "000"); found {
		t.Fatal("an unknown barcode was found")
	}
}

func TestTheCashNoteIsTheOwners(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SetCashNote(ctx, "1000"); errs.CodeOf(err) != sales.CodeOwnerRequired {
		t.Fatalf("outside owner mode: %v", err)
	}
	if v, err := f.svc.SetCashNote(ctx, "٥٠٠"); err != nil || v != 500 || f.gate.Asked != 1 {
		t.Fatalf("setting the same note asked the owner: %d, %v, asked %d", v, err, f.gate.Asked)
	}
	f.gate.Elevated = true
	if v, err := f.svc.SetCashNote(ctx, "1000"); err != nil || v != 1000 || f.gate.Acts[0] != (sales.GuardedAct{Action: sales.ActCashNote, Before: "500", After: "1000"}) {
		t.Fatalf("SetCashNote = %d, %v, %+v", v, err, f.gate.Acts)
	}
}

func TestVerifyIsTheOwnersAndFindsNothingInAConsistentDay(t *testing.T) {
	f := newFixture(t)
	f.sell(t, cart(line(f.oil, "2")))
	sale := f.sell(t, cart(line(f.jar, "1")))
	if _, err := f.svc.Verify(ctx); errs.CodeOf(err) != sales.CodeOwnerRequired {
		t.Fatalf("outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	if _, err := f.svc.Void(ctx, sales.VoidInput{SaleID: sale.ID, Reason: "خطأ"}); err != nil {
		t.Fatal(err)
	}
	if findings, err := f.svc.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}

func TestAFailedStockMovementFailsTheCheckout(t *testing.T) {
	f := newFixture(t)
	in := cart(line(f.jar, "1"))
	q, _ := f.svc.Quote(ctx, in)
	f.stock.FailSales()
	if _, err := f.svc.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); !errors.Is(err, salestest.ErrInjected) {
		t.Fatalf("err = %v — the real transaction's rollback is proven on SQLite", err)
	}
}
