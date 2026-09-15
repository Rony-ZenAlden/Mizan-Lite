package stocktest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

const unit = int64(1_000_000)

// Subject is a fresh, empty store and a way to make products it may hold stock of — the SQLite store's rows
// reference real products.
type Subject struct {
	Store      stock.Store
	NewProduct func(t *testing.T) id.ID
	// NewSaleLine returns a sale and a line of it that stock movements may name — real rows on SQLite (L4).
	NewSaleLine func(t *testing.T, productID id.ID) (saleID, saleLineID id.ID)
}

// StoreContract is the behaviour every stock.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 22, 0, 0, 123_000_000, time.UTC)

	stampFor := func(t *testing.T) domain.Stamp {
		t.Helper()
		movementID, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Second)
		return domain.Stamp{ID: movementID, BusinessDate: "2026-09-14", OccurredAt: at}
	}
	// write appends m and saves after, as the service does.
	write := func(t *testing.T, s stock.Store) func(domain.Movement, domain.Level) domain.Level {
		return func(m domain.Movement, after domain.Level) domain.Level {
			t.Helper()
			if err := s.Append(ctx, m); err != nil {
				t.Fatalf("Append(%s): %v", m.Kind, err)
			}
			saved, err := s.SaveLevel(ctx, after)
			if err != nil {
				t.Fatalf("SaveLevel after %s: %v", m.Kind, err)
			}
			return saved
		}
	}
	pound := domain.Cost{UnitCostMicro: 1_200_000, Entered: domain.Entered{Currency: "SYP", UnitCostMicro: 18_000 * unit, LocalPerUSDNano: 15_000_000_000_000}}
	dollar := domain.Cost{UnitCostMicro: 17 * unit, Entered: domain.Entered{Currency: "USD", UnitCostMicro: 17 * unit}}

	t.Run("a product that never moved has a zero level", func(t *testing.T) {
		sub := newSubject(t)
		productID := sub.NewProduct(t)
		l, err := sub.Store.Level(ctx, productID)
		if err != nil || l != (domain.Level{ProductID: productID}) {
			t.Fatalf("Level = %+v, %v", l, err)
		}
		if levels, err := sub.Store.Levels(ctx); err != nil || len(levels) != 0 {
			t.Fatalf("Levels = %+v, %v", levels, err)
		}
	})

	t.Run("every kind of movement reads back exactly, and so does its level", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		tin, loose := sub.NewProduct(t), sub.NewProduct(t)
		var written []domain.Movement
		apply := func(m domain.Movement, after domain.Level, err error) domain.Level {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			written = append(written, m)
			return write(t, s)(m, after)
		}

		tinLevel := apply(domain.Opening(domain.Level{ProductID: tin}, stampFor(t), 3*unit, dollar, "على الرف"))
		receipt, received, err := domain.Receive(tinLevel, stampFor(t), 2*unit, pound, "من أبو خليل")
		tinLevel = apply(receipt, received, err)
		tinLevel = apply(domain.ReverseReceipt(tinLevel, receipt, stampFor(t), ""))
		tinLevel = apply(domain.Count(tinLevel, stampFor(t), 2*unit, "جرد"))
		tinLevel = apply(domain.Adjust(tinLevel, stampFor(t), -unit, domain.ReasonOther, "كسر"))
		tinLevel = apply(domain.Adjust(tinLevel, stampFor(t), unit, domain.ReasonGift, ""))
		// The zero-quantity row that Mizan's schema could not hold (H3), through the domain and into this store.
		tinLevel = apply(domain.CorrectCost(tinLevel, stampFor(t), 95*unit, "السعر الصحيح"))

		looseLevel := domain.Level{ProductID: loose}
		pairID, _ := id.New()
		opened, err := domain.OpenPackage(tinLevel, looseLevel, domain.Package{ContentProductID: loose, ContentQuantityMicro: 16 * unit}, unit, stampFor(t), stampFor(t), pairID)
		if err != nil {
			t.Fatal(err)
		}
		tinLevel = apply(opened.Out, opened.PackageLevel, nil)
		looseLevel = apply(opened.In, opened.ContentLv, nil)

		for _, m := range written {
			got, readErr := s.Movement(ctx, m.ID)
			if readErr != nil || !sameMovement(got, m) {
				t.Errorf("%s reads back as\n%+v, %v\nwant\n%+v", m.Kind, got, readErr, m)
			}
		}
		for _, want := range []domain.Level{tinLevel, looseLevel} {
			got, readErr := s.Level(ctx, want.ProductID)
			if readErr != nil || got != want {
				t.Errorf("level = %+v, %v\nwant %+v", got, readErr, want)
			}
		}
		levels, err := s.Levels(ctx)
		if err != nil || len(levels) != 2 {
			t.Fatalf("Levels = %+v, %v", levels, err)
		}
	})

	t.Run("history is newest first, limited, and the walk is by product then place", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		a, b := sub.NewProduct(t), sub.NewProduct(t)
		levelA := write(t, s)(must(t)(domain.Opening(domain.Level{ProductID: a}, stampFor(t), 10*unit, dollar, "")))
		levelB := write(t, s)(must(t)(domain.Opening(domain.Level{ProductID: b}, stampFor(t), 10*unit, dollar, "")))
		for range 3 {
			levelA = write(t, s)(must(t)(domain.Receive(levelA, stampFor(t), unit, dollar, "")))
			levelB = write(t, s)(must(t)(domain.Adjust(levelB, stampFor(t), -unit, domain.ReasonDamaged, "")))
		}
		history, err := s.Movements(ctx, a, 2)
		if err != nil || len(history) != 2 || history[0].Seq != 4 || history[1].Seq != 3 || history[0].ProductID != a {
			t.Fatalf("Movements = %+v, %v", history, err)
		}
		var walked []domain.Movement
		if err := s.EachMovement(ctx, func(m domain.Movement) error { walked = append(walked, m); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(walked) != 8 {
			t.Fatalf("walked %d movements", len(walked))
		}
		for i := 1; i < len(walked); i++ {
			prev, cur := walked[i-1], walked[i]
			if cur.ProductID < prev.ProductID || (cur.ProductID == prev.ProductID && cur.Seq != prev.Seq+1) {
				t.Fatalf("the walk is out of order at %d: %s/%d after %s/%d", i, cur.ProductID, cur.Seq, prev.ProductID, prev.Seq)
			}
		}
	})

	t.Run("the walk follows places, not the clock", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		p := sub.NewProduct(t)
		l := write(t, s)(must(t)(domain.Opening(domain.Level{ProductID: p}, stampFor(t), 10*unit, dollar, "")))
		// The shop's clock is set back an hour between two movements.
		st := stampFor(t)
		st.OccurredAt = st.OccurredAt.Add(-time.Hour)
		write(t, s)(must(t)(domain.Receive(l, st, unit, dollar, "")))
		history, _ := s.Movements(ctx, p, 10)
		if len(history) != 2 || history[0].Seq != 2 {
			t.Fatalf("history ordered by the clock: %+v", history)
		}
	})

	t.Run("sale movements read back with their sale, one sale and one void per line", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		p := sub.NewProduct(t)
		l := write(t, s)(must(t)(domain.Opening(domain.Level{ProductID: p}, stampFor(t), 2*unit, dollar, "")))
		saleID, lineID := sub.NewSaleLine(t, p)
		sale, sold := must(t)(domain.Sale(l, stampFor(t), 5*unit, saleID, lineID)) // below zero: allowed
		l = write(t, s)(sale, sold)
		if l.OnHandMicro != -3*unit {
			t.Fatalf("on hand = %d", l.OnHandMicro)
		}
		got, found, err := s.SaleMovement(ctx, lineID, domain.KindSale)
		if err != nil || !found || !sameMovement(got, sale) {
			t.Fatalf("SaleMovement = %+v %v %v\nwant %+v", got, found, err, sale)
		}
		if _, found, _ := s.SaleMovement(ctx, lineID, domain.KindSaleVoid); found {
			t.Fatal("a void found before one was written")
		}
		again, _, _ := domain.Sale(l, stampFor(t), unit, saleID, lineID)
		if err := s.Append(ctx, again); err == nil {
			t.Error("a second sale movement for one line was stored")
		}
		void, returned := must(t)(domain.SaleVoid(l, sale, stampFor(t)))
		write(t, s)(void, returned)
		if got, found, _ := s.SaleMovement(ctx, lineID, domain.KindSaleVoid); !found || !sameMovement(got, void) {
			t.Fatalf("void = %+v", got)
		}
		var walked []domain.Movement
		if err := s.EachSaleMovement(ctx, func(m domain.Movement) error { walked = append(walked, m); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(walked) != 2 || walked[0].Kind != domain.KindSale || walked[1].Kind != domain.KindSaleVoid {
			t.Fatalf("EachSaleMovement = %+v", walked)
		}
		noSale, _, _ := domain.Receive(returned, stampFor(t), unit, dollar, "")
		noSale.SaleID = saleID
		if err := s.Append(ctx, noSale); err == nil {
			t.Error("a receipt naming a sale was stored")
		}
	})

	t.Run("a range holds its business dates, and the last place on or before a date is by place, not clock", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		a, b := sub.NewProduct(t), sub.NewProduct(t)
		levels := map[id.ID]domain.Level{a: {ProductID: a}, b: {ProductID: b}}
		receive := func(p id.ID, date string, qty int64) {
			t.Helper()
			st := stampFor(t)
			st.BusinessDate = date
			m, after, err := domain.Receive(levels[p], st, qty*unit, dollar, "")
			if err != nil {
				t.Fatal(err)
			}
			levels[p] = write(t, s)(m, after)
		}
		receive(a, "2026-09-12", 1)
		receive(a, "2026-09-13", 2)
		receive(b, "2026-09-13", 5)
		receive(a, "2026-09-15", 4)
		in, err := s.Between(ctx, "2026-09-13", "2026-09-14")
		if err != nil || len(in) != 2 {
			t.Fatalf("Between 13–14 = %+v, %v", in, err)
		}
		last, err := s.LastOnOrBefore(ctx, "2026-09-14")
		if err != nil || len(last) != 2 {
			t.Fatalf("LastOnOrBefore = %+v, %v", last, err)
		}
		for _, m := range last {
			if m.ProductID == a && (m.Seq != 2 || m.OnHandAfterMicro != 3*unit) || m.ProductID == b && m.OnHandAfterMicro != 5*unit {
				t.Fatalf("last on or before the 14th = %+v", m)
			}
		}
		if before, _ := s.LastOnOrBefore(ctx, "2026-09-11"); len(before) != 0 {
			t.Fatalf("before anything moved = %+v", before)
		}
	})

	t.Run("a missing movement is NotFound", func(t *testing.T) {
		missing, _ := id.New()
		if _, err := newSubject(t).Store.Movement(ctx, missing); errs.CodeOf(err) != domain.CodeMovementNotFound {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a level is versioned", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		p := sub.NewProduct(t)
		m, after, _ := domain.Opening(domain.Level{ProductID: p}, stampFor(t), 10*unit, dollar, "")
		saved := write(t, s)(m, after)
		if saved.RowVersion != 1 {
			t.Fatalf("an inserted level is at version %d", saved.RowVersion)
		}
		m2, after2, _ := domain.Receive(saved, stampFor(t), unit, dollar, "")
		if err := s.Append(ctx, m2); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveLevel(ctx, after); errs.CodeOf(err) == "" {
			t.Error("a second insert of the same level was accepted")
		}
		updated, err := s.SaveLevel(ctx, after2)
		if err != nil || updated.RowVersion != 2 {
			t.Fatalf("update = %+v, %v", updated, err)
		}
		if _, err := s.SaveLevel(ctx, after2); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("a stale level was written: %v", err)
		}
	})

	t.Run("one place per product, and a receipt reversed once", func(t *testing.T) {
		sub := newSubject(t)
		s := sub.Store
		p := sub.NewProduct(t)
		l := write(t, s)(must(t)(domain.Opening(domain.Level{ProductID: p}, stampFor(t), 10*unit, dollar, "")))
		receipt, received, _ := domain.Receive(l, stampFor(t), unit, dollar, "")
		l = write(t, s)(receipt, received)

		samePlace, _, _ := domain.Adjust(l, stampFor(t), -unit, domain.ReasonDamaged, "")
		samePlace.Seq = receipt.Seq
		if err := s.Append(ctx, samePlace); err == nil {
			t.Error("two movements took one place")
		}
		reversal, reversed, _ := domain.ReverseReceipt(l, receipt, stampFor(t), "")
		write(t, s)(reversal, reversed)
		again, _, _ := domain.ReverseReceipt(l, receipt, stampFor(t), "")
		again.Seq = reversed.LastSeq + 1
		if err := s.Append(ctx, again); err == nil {
			t.Error("a receipt was reversed twice")
		}
	})
}

// sameMovement compares movements with their instants compared as instants.
func sameMovement(a, b domain.Movement) bool {
	if !a.OccurredAt.Equal(b.OccurredAt) {
		return false
	}
	a.OccurredAt, b.OccurredAt = time.Time{}, time.Time{}
	return a == b
}

// must returns the act's results, failing t if the act refused.
func must(t *testing.T) func(domain.Movement, domain.Level, error) (domain.Movement, domain.Level) {
	return func(m domain.Movement, after domain.Level, err error) (domain.Movement, domain.Level) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s refused: %v", m.Kind, err)
		}
		return m, after
	}
}
