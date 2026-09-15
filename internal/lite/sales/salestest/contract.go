package salestest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// Subject is a fresh, empty store, and what a sale it holds must reference: a product and a rate.
type Subject struct {
	Store      sales.Store
	NewProduct func(t *testing.T) id.ID
	NewRate    func(t *testing.T) id.ID
}

// ASale is a valid two-line sale, as a checkout would record it: a pounds total paid with dollars, change in pounds,
// one line discounted and one with unknown cost.
func ASale(t *testing.T, receiptNo int64, rateID, oil, jar id.ID) domain.Sale {
	t.Helper()
	ids := make([]id.ID, 3)
	for i := range ids {
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = v
	}
	at := time.Date(2026, 9, 14, 7, 30, 15, 123_000_000, time.UTC)
	return domain.Sale{
		ID: ids[0], ReceiptNo: receiptNo, BusinessDate: "2026-09-14", SoldAt: at, Status: domain.StatusPosted, Payment: domain.PaymentCash,
		LocalCurrency: "SYP", RateID: rateID, RateNano: 15_000_000_000_000, RateRecordedAt: at.Add(-time.Hour), SettlementCurrency: "SYP",
		LinesLocalMinor: 220_500, LinesUSDMinor: 1_470, DiscountLocalMinor: 500, DiscountUSDMinor: 3, CashNoteMinor: 500,
		RoundingMinor: 0, TotalMinor: 220_000, TenderedCurrency: "USD", TenderedMinor: 2_000, ChangeCurrency: "SYP", ChangeMinor: 80_000,
		CostUSDMinor: 936, ShopName: "بقالية المونة", RowVersion: 1,
		Lines: []domain.Line{
			{ID: ids[1], LineNo: 1, ProductID: oil, NameAR: "زيت زيتون", NameEN: "Olive oil", UnitCode: "l", QuantityMicro: 2_000_000,
				PriceCurrency: "USD", UnitPriceMicro: 6_500_000, GrossLocalMinor: 195_000, GrossUSDMinor: 1_300, DiscountPercentMicro: 100_000,
				DiscountLocalMinor: 19_500, DiscountUSDMinor: 130, UnitCostMicro: 4_680_000, CostKnown: true, CostUSDMinor: 936, CostLocalMinor: 140_400},
			{ID: ids[2], LineNo: 2, ProductID: jar, NameAR: "دبس رمان", UnitCode: "jar", QuantityMicro: 1_000_000,
				PriceCurrency: "SYP", UnitPriceMicro: 45_000_000_000, GrossLocalMinor: 45_000, GrossUSDMinor: 300},
		},
	}
}

// StoreContract is the behaviour every sales.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()

	t.Run("a sale reads back exactly, with its lines", func(t *testing.T) {
		sub := newSubject(t)
		rate := sub.NewRate(t)
		want := ASale(t, 1, rate, sub.NewProduct(t), sub.NewProduct(t))
		if n, err := sub.Store.NextReceiptNo(ctx); err != nil || n != 1 {
			t.Fatalf("the first receipt number = %d, %v", n, err)
		}
		if err := sub.Store.Insert(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, err := sub.Store.Get(ctx, want.ID)
		if err != nil || !sameSale(got, want) {
			t.Fatalf("Get = %+v, %v\nwant %+v", got, err, want)
		}
		if n, _ := sub.Store.NextReceiptNo(ctx); n != 2 {
			t.Fatalf("the next receipt number = %d", n)
		}
		missing, _ := id.New()
		if _, err := sub.Store.Get(ctx, missing); errs.CodeOf(err) != domain.CodeSaleNotFound {
			t.Fatalf("a missing sale: %v", err)
		}
	})

	t.Run("a receipt number is used once", func(t *testing.T) {
		sub := newSubject(t)
		rate, oil, jar := sub.NewRate(t), sub.NewProduct(t), sub.NewProduct(t)
		if err := sub.Store.Insert(ctx, ASale(t, 1, rate, oil, jar)); err != nil {
			t.Fatal(err)
		}
		if err := sub.Store.Insert(ctx, ASale(t, 1, rate, oil, jar)); err == nil {
			t.Fatal("receipt number 1 was used twice")
		}
	})

	t.Run("a void is written once, from posted, at its version", func(t *testing.T) {
		sub := newSubject(t)
		sale := ASale(t, 1, sub.NewRate(t), sub.NewProduct(t), sub.NewProduct(t))
		if err := sub.Store.Insert(ctx, sale); err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
		voided, err := sale.Void(at, "2026-09-15", "خطأ")
		if err != nil {
			t.Fatal(err)
		}
		got, err := sub.Store.Void(ctx, voided)
		if err != nil || got.Status != domain.StatusVoided || got.RowVersion != 2 || got.VoidReason != "خطأ" {
			t.Fatalf("Void = %+v, %v", got, err)
		}
		if stored, _ := sub.Store.Get(ctx, sale.ID); stored.Status != domain.StatusVoided || !stored.VoidedAt.Equal(at) ||
			stored.VoidBusinessDate != "2026-09-15" || len(stored.Lines) != 2 {
			t.Fatalf("stored = %+v", stored)
		}
		if _, err := sub.Store.Void(ctx, voided); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("voided twice: %v", err)
		}
	})

	t.Run("a day holds its sales and its voids, by receipt number; every sale walks in order", func(t *testing.T) {
		sub := newSubject(t)
		rate, oil, jar := sub.NewRate(t), sub.NewProduct(t), sub.NewProduct(t)
		yesterday := ASale(t, 1, rate, oil, jar)
		yesterday.BusinessDate = "2026-09-13"
		today := ASale(t, 2, rate, oil, jar)
		tomorrow := ASale(t, 3, rate, oil, jar)
		tomorrow.BusinessDate = "2026-09-15"
		for _, s := range []domain.Sale{tomorrow, yesterday, today} {
			if err := sub.Store.Insert(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		voided, _ := yesterday.Void(time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC), "2026-09-14", "خطأ")
		if _, err := sub.Store.Void(ctx, voided); err != nil {
			t.Fatal(err)
		}
		day, err := sub.Store.Day(ctx, "2026-09-14")
		if err != nil || len(day) != 2 || day[0].ReceiptNo != 1 || day[1].ReceiptNo != 2 || len(day[1].Lines) != 2 {
			t.Fatalf("Day = %+v, %v", day, err)
		}
		var order []int64
		if err := sub.Store.Each(ctx, func(s domain.Sale) error {
			order = append(order, s.ReceiptNo)
			if len(s.Lines) != 2 {
				t.Errorf("sale %d walked without its lines", s.ReceiptNo)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(order) != 3 || order[0] != 1 || order[2] != 3 {
			t.Fatalf("Each order = %v", order)
		}
		// A range holds the sales sold in it and those voided in it — yesterday's sale voided today is in today's range.
		if r, err := sub.Store.Range(ctx, "2026-09-14", "2026-09-15"); err != nil || len(r) != 3 || len(r[2].Lines) != 2 {
			t.Fatalf("Range 14–15 = %d sales, %v", len(r), err)
		}
		if r, _ := sub.Store.Range(ctx, "2026-09-15", "2026-09-15"); len(r) != 1 || r[0].ReceiptNo != 3 {
			t.Fatalf("Range 15 = %+v", r)
		}
		if r, _ := sub.Store.Range(ctx, "2026-09-10", "2026-09-12"); len(r) != 0 {
			t.Fatalf("an empty range = %+v", r)
		}
	})
}

func sameSale(a, b domain.Sale) bool {
	if !a.SoldAt.Equal(b.SoldAt) || !a.RateRecordedAt.Equal(b.RateRecordedAt) || !a.VoidedAt.Equal(b.VoidedAt) || len(a.Lines) != len(b.Lines) {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	a.SoldAt, b.SoldAt, a.RateRecordedAt, b.RateRecordedAt, a.VoidedAt, b.VoidedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}
	a.Lines, b.Lines = nil, nil
	return a.ID == b.ID && a.ReceiptNo == b.ReceiptNo && a.BusinessDate == b.BusinessDate && a.Status == b.Status &&
		a.Payment == b.Payment && a.LocalCurrency == b.LocalCurrency && a.RateID == b.RateID && a.RateNano == b.RateNano &&
		a.SettlementCurrency == b.SettlementCurrency && a.LinesLocalMinor == b.LinesLocalMinor && a.LinesUSDMinor == b.LinesUSDMinor &&
		a.DiscountLocalMinor == b.DiscountLocalMinor && a.DiscountUSDMinor == b.DiscountUSDMinor && a.CashNoteMinor == b.CashNoteMinor &&
		a.RoundingMinor == b.RoundingMinor && a.TotalMinor == b.TotalMinor && a.TenderedCurrency == b.TenderedCurrency &&
		a.TenderedMinor == b.TenderedMinor && a.ChangeCurrency == b.ChangeCurrency && a.ChangeMinor == b.ChangeMinor &&
		a.CostUSDMinor == b.CostUSDMinor && a.ShopName == b.ShopName && a.VoidBusinessDate == b.VoidBusinessDate &&
		a.VoidReason == b.VoidReason && a.RowVersion == b.RowVersion
}
