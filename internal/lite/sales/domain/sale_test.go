package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

func assembled(t *testing.T, s shop, in domain.CartInput, receiptNo int64) domain.Sale {
	t.Helper()
	q := price(t, in, s.ctx)
	ids := make([]id.ID, len(q.Lines))
	for i := range ids {
		ids[i] = newID(t)
	}
	return domain.Assemble(q, s.ctx.Local, domain.Stamp{
		SaleID: newID(t), LineIDs: ids, ReceiptNo: receiptNo, BusinessDate: "2026-09-14",
		SoldAt: time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC), ShopName: "بقالية المونة", Payment: domain.PaymentCash,
	})
}

// movementsOf is the stock that moves with a sale: one sale movement per line, and one void per line when voided.
func movementsOf(sale domain.Sale) []domain.StockMovement {
	var out []domain.StockMovement
	for _, l := range sale.Lines {
		out = append(out, domain.StockMovement{Kind: "sale", ProductID: l.ProductID, QuantityMicro: -l.QuantityMicro, SaleID: sale.ID, SaleLineID: l.ID})
		if sale.Status == domain.StatusVoided {
			out = append(out, domain.StockMovement{Kind: "sale_void", ProductID: l.ProductID, QuantityMicro: l.QuantityMicro, SaleID: sale.ID, SaleLineID: l.ID})
		}
	}
	return out
}

func TestAssembleSnapshotsTheReceipt(t *testing.T) {
	s := newShop(t)
	sale := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2"), line(s.bulgur, "1.750")}, TenderCurrency: "USD", Tendered: "20"}, 7)
	if sale.ReceiptNo != 7 || sale.Status != domain.StatusPosted || sale.RateNano != 15_000*n || sale.LinesLocalMinor != 226_500 ||
		sale.TotalMinor != 226_500 || sale.TenderedCurrency != "USD" || sale.ChangeCurrency != "SYP" || sale.ChangeMinor != 73_500 ||
		sale.ShopName != "بقالية المونة" || len(sale.Lines) != 2 || sale.Lines[0].NameAR != "زيت زيتون" || sale.Lines[1].LineNo != 2 {
		t.Fatalf("sale = %+v", sale)
	}
}

func TestAVoidNeedsAReasonAndHappensOnce(t *testing.T) {
	s := newShop(t)
	sale := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{line(s.jar, "1")}}, 1)
	at := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	if _, err := sale.Void(at, "2026-09-15", "   "); code(err) != domain.CodeVoidReasonRequired {
		t.Fatalf("no reason: %v", err)
	}
	if _, err := sale.Void(at, "2026-09-15", strings.Repeat("س", domain.MaxReasonRunes+1)); code(err) != domain.CodeVoidReasonTooLong {
		t.Fatalf("a long reason: %v", err)
	}
	voided, err := sale.Void(at, "2026-09-15", " خطأ في الكمية ")
	if err != nil || voided.Status != domain.StatusVoided || voided.VoidReason != "خطأ في الكمية" || voided.VoidBusinessDate != "2026-09-15" || !voided.VoidedAt.Equal(at) {
		t.Fatalf("voided = %+v, %v", voided, err)
	}
	if _, err := voided.Void(at, "2026-09-15", "again"); code(err) != domain.CodeAlreadyVoided {
		t.Fatalf("voided twice: %v", err)
	}
}

func TestTheSalesVerifier(t *testing.T) {
	s := newShop(t)
	day := func() ([]domain.Sale, []domain.StockMovement) {
		one := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{line(s.oil, "2"), line(s.bulgur, "1.750")}, TenderCurrency: "USD", Tendered: "20"}, 1)
		two := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{{ProductID: s.jar, Quantity: "1", DiscountPercent: "10"}}, Settlement: "USD", Tendered: "5", TenderCurrency: "USD"}, 2)
		three := assembled(t, s, domain.CartInput{Lines: []domain.LineInput{line(s.uncosted, "3")}, SaleDiscount: "1000"}, 3)
		three, _ = three.Void(time.Now(), "2026-09-14", "خطأ")
		sales := []domain.Sale{three, one, two}
		var moves []domain.StockMovement
		for _, sale := range sales {
			moves = append(moves, movementsOf(sale)...)
		}
		return sales, moves
	}
	sales, moves := day()
	if f := domain.Verify(sales, moves, syp, usd); len(f) != 0 {
		t.Fatalf("a consistent day: %+v", f)
	}
	for name, plant := range map[string]struct {
		change func([]domain.Sale, []domain.StockMovement) ([]domain.Sale, []domain.StockMovement)
		code   string
	}{
		"lines that do not add": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			ss[1].Lines[0].GrossLocalMinor++ // the line changed; the totals did not
			return ss, m
		}, domain.FindingLinesDoNotAdd},
		"a total not rounded to the note": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			// The exact-money sale: total and tender move together, so only the rounding is wrong.
			ss[0].RoundingMinor += 500
			ss[0].TotalMinor += 500
			ss[0].TenderedMinor += 500
			return ss, m
		}, domain.FindingRoundingWrong},
		"change short by a note": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			ss[1].ChangeMinor -= 500
			return ss, m
		}, domain.FindingChangeWrong},
		"a sale with no stock movement": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			return ss, m[1:]
		}, domain.FindingStockMissing},
		"a voided sale whose stock never returned": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			var kept []domain.StockMovement
			for _, x := range m {
				if x.Kind != "sale_void" {
					kept = append(kept, x)
				}
			}
			return ss, kept
		}, domain.FindingStockMissing},
		"stock that moved for a sale that does not exist": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			return ss, append(m, domain.StockMovement{Kind: "sale", ProductID: s.oil, QuantityMicro: -u, SaleID: newID(t), SaleLineID: newID(t)})
		}, domain.FindingStockWithoutSale},
		"a receipt number missing": {func(ss []domain.Sale, m []domain.StockMovement) ([]domain.Sale, []domain.StockMovement) {
			ss[0].ReceiptNo = 4
			return ss, m
		}, domain.FindingReceiptNumberGap},
	} {
		t.Run(name, func(t *testing.T) {
			ss, m := day()
			ss, m = plant.change(ss, m)
			found := domain.Verify(ss, m, syp, usd)
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
