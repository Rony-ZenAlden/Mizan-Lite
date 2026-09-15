package cashbook_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/cashbooktest"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

var ctx = context.Background()

var damascus = time.FixedZone("Damascus", 3*3600)

type fixture struct {
	svc      *cashbook.Service
	store    *cashbooktest.Fake
	rates    *cashbooktest.Rates
	gate     *cashbooktest.Gate
	expected *cashbooktest.Expected
	clk      *clock.Fixed
}

func newFixture() fixture {
	f := fixture{store: cashbooktest.NewFake(), rates: cashbooktest.NewRates(), gate: &cashbooktest.Gate{},
		expected: &cashbooktest.Expected{Cash: map[string]int64{"SYP": 250_000, "USD": 1_500}},
		clk:      clock.NewFixed(time.Date(2026, 9, 14, 22, 30, 0, 0, time.UTC))} // 01:30 on the 15th in Damascus
	f.svc = cashbook.NewService(litetest.Immediate{}, f.store, f.rates, cashbooktest.Currencies{}, f.gate, f.expected, f.clk, damascus)
	return f
}

func code(err error) string { return errs.CodeOf(err) }

func TestMoneyEntriesNeedTheOwner(t *testing.T) {
	f := newFixture()
	for _, in := range []cashbook.RecordInput{
		{Kind: domain.KindExpense, Currency: "SYP", Amount: "250000", Category: "electricity", FromDrawer: true},
		{Kind: domain.KindWithdrawal, Currency: "USD", Amount: "20"},
		{Kind: domain.KindDeposit, Currency: "SYP", Amount: "٥٠٠٠٠"},
	} {
		if _, err := f.svc.Record(ctx, in); code(err) != cashbook.CodeOwnerRequired {
			t.Fatalf("%s outside owner mode: %v", in.Kind, err)
		}
	}
	if n, _ := f.store.NextSeq(ctx); n != 1 {
		t.Fatal("a refused entry was written")
	}
	f.gate.Elevated = true
	var acts []string
	for _, in := range []cashbook.RecordInput{
		{Kind: domain.KindExpense, Currency: "SYP", Amount: "250000", Category: "electricity", FromDrawer: true, Note: "فاتورة أيلول"},
		{Kind: domain.KindWithdrawal, Currency: "USD", Amount: "20"},
		{Kind: domain.KindDeposit, Currency: "SYP", Amount: "٥٠٠٠٠"},
	} {
		e, err := f.svc.Record(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if e.BusinessDate != "2026-09-15" || e.Seq == 0 || e.ID == "" {
			t.Fatalf("stamped %+v", e)
		}
	}
	for _, a := range f.gate.Acts {
		acts = append(acts, a.Action)
	}
	if len(acts) != 3 || acts[0] != cashbook.ActExpense || acts[1] != cashbook.ActWithdrawal || acts[2] != cashbook.ActDeposit {
		t.Fatalf("acts = %v", acts)
	}
	entries, _ := f.svc.Between(ctx, "2026-09-15", "2026-09-15")
	if len(entries) != 3 || entries[0].Entry.AmountMinor != 250_000 || !entries[0].Entry.FromDrawer || entries[1].Entry.AmountMinor != 2_000 ||
		entries[2].Entry.AmountMinor != 50_000 {
		t.Fatalf("entries = %+v", entries)
	}
	for _, bad := range []struct {
		in   cashbook.RecordInput
		want string
	}{
		{cashbook.RecordInput{Kind: domain.KindExpense, Currency: "SYP", Amount: "100", Category: "bribes"}, domain.CodeUnknownCategory},
		{cashbook.RecordInput{Kind: domain.KindCount, Currency: "SYP", Amount: "100"}, domain.CodeUnknownKind},
		{cashbook.RecordInput{Kind: domain.KindWithdrawal, Currency: "EUR", Amount: "100"}, domain.CodeUnknownCurrency},
		{cashbook.RecordInput{Kind: domain.KindWithdrawal, Currency: "USD", Amount: "0"}, domain.CodeAmountRequired},
		{cashbook.RecordInput{Kind: domain.KindWithdrawal, Currency: "SYP", Amount: "1.5"}, domain.CodeAmountDecimals},
	} {
		if _, err := f.svc.Record(ctx, bad.in); code(err) != bad.want {
			t.Errorf("%+v: %v, want %s", bad.in, err, bad.want)
		}
	}
}

func TestAnExpenseSnapshotsItsRate(t *testing.T) {
	f := newFixture()
	f.gate.Elevated = true
	e, err := f.svc.Record(ctx, cashbook.RecordInput{Kind: domain.KindExpense, Currency: "USD", Amount: "100", Category: "rent"})
	if err != nil {
		t.Fatal(err)
	}
	f.rates.Nano = 16_000_000_000_000
	if e.RateID != f.rates.RateID || e.RateNano != 15_000_000_000_000 {
		t.Fatalf("snapshot = %s %d", e.RateID, e.RateNano)
	}
	if got, _ := f.svc.Entry(ctx, e.ID); got.RateNano != 15_000_000_000_000 {
		t.Fatal("the stored rate moved with today's")
	}
	f.rates.Set = false
	if _, err = f.svc.Record(ctx, cashbook.RecordInput{Kind: domain.KindWithdrawal, Currency: "USD", Amount: "1"}); code(err) != domain.CodeNoRate {
		t.Fatalf("no rate: %v", err)
	}
}

func TestACountNeedsNoPIN(t *testing.T) {
	f := newFixture()
	c, err := f.svc.Count(ctx, cashbook.CountInput{Currency: "SYP", Counted: "240000", Note: "آخر النهار"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.gate.Acts) != 0 {
		t.Fatal("a count went through the owner gate")
	}
	if c.ExpectedMinor != 250_000 || c.Difference() != -10_000 || c.BusinessDate != "2026-09-15" || c.RateID != "" {
		t.Fatalf("count = %+v", c)
	}
	if len(f.expected.Asked) != 1 || f.expected.Asked[0] != "2026-09-15 SYP" {
		t.Fatalf("expected asked for %v — the business date, not the machine's", f.expected.Asked)
	}
	if _, err = f.svc.Count(ctx, cashbook.CountInput{Currency: "USD", Counted: "0"}); err != nil {
		t.Fatalf("an empty dollar drawer: %v", err)
	}
	if _, err = f.svc.Count(ctx, cashbook.CountInput{Currency: "USD", Counted: ""}); code(err) != domain.CodeAmountRequired {
		t.Fatalf("a blank count: %v", err)
	}
}

func TestAnEntryIsReversedOnceWithAReason(t *testing.T) {
	f := newFixture()
	count, err := f.svc.Count(ctx, cashbook.CountInput{Currency: "SYP", Counted: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Reverse(ctx, count.ID, "عدّ خطأ"); code(err) != cashbook.CodeOwnerRequired {
		t.Fatalf("a reversal outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	if _, err = f.svc.Reverse(ctx, count.ID, "  "); code(err) != domain.CodeReasonRequired {
		t.Fatalf("no reason: %v", err)
	}
	r, err := f.svc.Reverse(ctx, count.ID, "عدّ خطأ")
	if err != nil {
		t.Fatal(err)
	}
	if r.ReversesID != count.ID || r.AmountMinor != 0 || r.Currency != "SYP" || f.gate.Acts[0].Action != cashbook.ActReverse {
		t.Fatalf("reversal = %+v, acts %+v", r, f.gate.Acts)
	}
	if _, err = f.svc.Reverse(ctx, count.ID, "مرة ثانية"); code(err) != domain.CodeAlreadyReversed {
		t.Fatalf("twice: %v", err)
	}
	if _, err = f.svc.Reverse(ctx, r.ID, "عكس العكس"); code(err) != domain.CodeNotReversible {
		t.Fatalf("a reversal reversed: %v", err)
	}
	facts, _ := f.svc.Between(ctx, "2026-09-15", "2026-09-15")
	if len(facts) != 2 || !facts[0].Reversed || facts[1].Reverses.ID != count.ID {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestTheLastCountBeforeADateSkipsReversedCounts(t *testing.T) {
	f := newFixture()
	f.gate.Elevated = true
	f.clk.Current = time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	first, _ := f.svc.Count(ctx, cashbook.CountInput{Currency: "SYP", Counted: "100000"})
	second, _ := f.svc.Count(ctx, cashbook.CountInput{Currency: "SYP", Counted: "120000"})
	_, _ = f.svc.Count(ctx, cashbook.CountInput{Currency: "USD", Counted: "5"})
	f.clk.Current = time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	today, _ := f.svc.Count(ctx, cashbook.CountInput{Currency: "SYP", Counted: "1"})
	got, found, err := f.svc.LastCountBefore(ctx, "2026-09-14", "SYP")
	if err != nil || !found || got.ID != second.ID {
		t.Fatalf("last count = %+v %v %v", got, found, err)
	}
	if _, err = f.svc.Reverse(ctx, second.ID, "خطأ"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = f.svc.LastCountBefore(ctx, "2026-09-14", "SYP"); got.ID != first.ID {
		t.Fatalf("after reversing the newest, last count = %+v", got)
	}
	if got, _, _ = f.svc.LastCountBefore(ctx, "2026-09-15", "SYP"); got.ID != today.ID {
		t.Fatal("today's count does not close today")
	}
	if _, found, _ = f.svc.LastCountBefore(ctx, "2026-09-13", "SYP"); found {
		t.Fatal("a count before any was made")
	}
	if _, err = f.svc.Reverse(ctx, first.ID+"x", "x"); code(err) != domain.CodeEntryNotFound {
		t.Fatalf("an unknown entry: %v", err)
	}
}
