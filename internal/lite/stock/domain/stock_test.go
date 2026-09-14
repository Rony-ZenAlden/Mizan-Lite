package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

const (
	u   = int64(1_000_000) // one unit, or one dollar of unit cost
	day = "2026-09-14"
)

var currencies = []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}

func newID(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func stamp(t *testing.T) domain.Stamp {
	return domain.Stamp{ID: newID(t), BusinessDate: day, OccurredAt: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)}
}

func usd(unitCostMicro int64) domain.Cost {
	return domain.Cost{UnitCostMicro: unitCostMicro, Entered: domain.Entered{Currency: "USD", UnitCostMicro: unitCostMicro}}
}

func code(err error) string { return errs.CodeOf(err) }

// stocked is a product that received quantity at cost as its opening stock.
func stocked(t *testing.T, quantity, cost int64) domain.Level {
	t.Helper()
	_, l, err := domain.Opening(domain.Level{ProductID: newID(t)}, stamp(t), quantity, usd(cost), "")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// checkChain asserts m follows before and leads to after.
func checkChain(t *testing.T, before domain.Level, m domain.Movement, after domain.Level) {
	t.Helper()
	if m.OnHandBeforeMicro != before.OnHandMicro || m.AvgCostBeforeMicro != before.AvgCostMicro {
		t.Errorf("before = %d @ %d, level was %d @ %d", m.OnHandBeforeMicro, m.AvgCostBeforeMicro, before.OnHandMicro, before.AvgCostMicro)
	}
	if m.OnHandAfterMicro != m.OnHandBeforeMicro+m.QuantityMicro {
		t.Errorf("after %d ≠ before %d + quantity %d", m.OnHandAfterMicro, m.OnHandBeforeMicro, m.QuantityMicro)
	}
	if after.OnHandMicro != m.OnHandAfterMicro || after.AvgCostMicro != m.AvgCostAfterMicro ||
		after.LastMovementID != m.ID || after.LastSeq != before.LastSeq+1 || m.Seq != after.LastSeq {
		t.Errorf("level after = %+v, movement = %+v", after, m)
	}
	if after.RowVersion != before.RowVersion || after.ProductID != before.ProductID || m.ProductID != before.ProductID {
		t.Errorf("the act changed what it must not: %+v → %+v", before, after)
	}
}

// §3.2, row by row.

func TestOpeningSetsQuantityAndCost(t *testing.T) {
	before := domain.Level{ProductID: newID(t)}
	m, after, err := domain.Opening(before, stamp(t), 12*u, usd(17*u), "  على الرف  ")
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, m, after)
	if m.Kind != domain.KindOpening || m.QuantityMicro != 12*u || m.UnitCostMicro != 17*u || after.AvgCostMicro != 17*u {
		t.Fatalf("m = %+v", m)
	}
	if m.Note != "على الرف" || m.Seq != 1 || m.BusinessDate != day {
		t.Fatalf("note %q, seq %d, date %s", m.Note, m.Seq, m.BusinessDate)
	}
}

func TestOpeningOnlyAsTheFirstMovement(t *testing.T) {
	l := stocked(t, 12*u, 17*u)
	if _, _, err := domain.Opening(l, stamp(t), u, usd(u), ""); code(err) != domain.CodeNotFirstMovement {
		t.Fatalf("a second opening: %v", err)
	}
}

func TestAReceiptAveragesIn(t *testing.T) {
	before := stocked(t, 10*u, 100*u)
	m, after, err := domain.Receive(before, stamp(t), 30*u, usd(120*u), "من أبو خليل")
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, m, after)
	// (10 × 100 + 30 × 120) ÷ 40 = 115
	if after.AvgCostMicro != 115*u || after.OnHandMicro != 40*u || m.UnitCostMicro != 120*u || m.Kind != domain.KindReceipt {
		t.Fatalf("after = %+v", after)
	}
}

func TestAReceiptAverageRoundsOnceHalfUp(t *testing.T) {
	before := stocked(t, 3*u, 1*u)
	// (3 × 1 + 1 × 2) ÷ 4 = 1.25 exactly; (2 × 1 + 1 × 2) ÷ 3 = 1.333333…3 → 1.333333
	_, after, _ := domain.Receive(before, stamp(t), u, usd(2*u), "")
	if after.AvgCostMicro != 1_250_000 {
		t.Fatalf("avg = %d", after.AvgCostMicro)
	}
	_, after, _ = domain.Receive(stocked(t, 2*u, u), stamp(t), u, usd(2*u), "")
	if after.AvgCostMicro != 1_333_333 {
		t.Fatalf("avg = %d", after.AvgCostMicro)
	}
	// (1 × 1 + 2 × 2) ÷ 3 = 1.6666667 → 1.666667 (half up)
	_, after, _ = domain.Receive(stocked(t, u, u), stamp(t), 2*u, usd(2*u), "")
	if after.AvgCostMicro != 1_666_667 {
		t.Fatalf("avg = %d", after.AvgCostMicro)
	}
}

// TestReceiptIntoNegativeStockTakesTheReceiptCost is Mizan's −759 case: 10 received at 120 into −5 carrying a
// phantom average of 999. The till in L4 can take stock below zero; the rule must already be right.
func TestReceiptIntoNegativeStockTakesTheReceiptCost(t *testing.T) {
	before := domain.Level{ProductID: newID(t), OnHandMicro: -5 * u, AvgCostMicro: 999 * u, LastMovementID: newID(t), LastSeq: 7}
	m, after, err := domain.Receive(before, stamp(t), 10*u, usd(120*u), "")
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, m, after)
	if after.AvgCostMicro != 120*u || after.OnHandMicro != 5*u {
		t.Fatalf("after = %+v — averaging against negative stock", after)
	}
	_, after, _ = domain.Receive(domain.Level{ProductID: newID(t), AvgCostMicro: 50 * u, LastSeq: 1, LastMovementID: newID(t)}, stamp(t), u, usd(7*u), "")
	if after.AvgCostMicro != 7*u {
		t.Fatalf("a receipt into zero stock kept the old average: %+v", after)
	}
}

func TestReceiveRefusesNothing(t *testing.T) {
	l := stocked(t, u, u)
	for _, q := range []int64{0, -u} {
		if _, got, err := domain.Receive(l, stamp(t), q, usd(u), ""); code(err) != domain.CodeQuantityRequired || got != l {
			t.Errorf("quantity %d: %v, level %+v", q, err, got)
		}
	}
}

func TestACountNeverMovesTheAverage(t *testing.T) {
	before := stocked(t, 12*u, 17*u)
	for _, counted := range []int64{10 * u, 12 * u, 15 * u, 0} {
		m, after, err := domain.Count(before, stamp(t), counted, "")
		if err != nil {
			t.Fatalf("counted %d: %v", counted, err)
		}
		checkChain(t, before, m, after)
		if after.OnHandMicro != counted || after.AvgCostMicro != 17*u || m.QuantityMicro != counted-12*u {
			t.Fatalf("counted %d: %+v", counted, m)
		}
		if m.Reason != domain.ReasonCount || m.UnitCostMicro != 17*u || m.Kind != domain.KindCount {
			t.Fatalf("m = %+v", m)
		}
		if m.Lowers() != (counted < 12*u) {
			t.Fatalf("counted %d: Lowers = %v", counted, m.Lowers())
		}
	}
}

func TestAnAdjustmentNeverMovesTheAverage(t *testing.T) {
	before := stocked(t, 12*u, 17*u)
	for _, tc := range []struct {
		q      int64
		reason domain.Reason
		note   string
	}{{-3 * u, domain.ReasonDamaged, ""}, {-u, domain.ReasonExpired, ""}, {-u, domain.ReasonOwnUse, ""},
		{-u, domain.ReasonGift, "عينة للزبون"}, {2 * u, domain.ReasonOther, "وجدت في المخزن"}} {
		m, after, err := domain.Adjust(before, stamp(t), tc.q, tc.reason, tc.note)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		checkChain(t, before, m, after)
		if after.AvgCostMicro != 17*u || after.OnHandMicro != 12*u+tc.q || m.Reason != tc.reason || m.UnitCostMicro != 17*u {
			t.Fatalf("%+v: %+v", tc, m)
		}
	}
}

func TestAnAdjustmentNeedsAKnownReasonAndOtherNeedsANote(t *testing.T) {
	l := stocked(t, 12*u, 17*u)
	for _, reason := range []domain.Reason{"", domain.ReasonCount, "stolen"} {
		if _, _, err := domain.Adjust(l, stamp(t), -u, reason, "x"); code(err) != domain.CodeUnknownReason {
			t.Errorf("reason %q: %v", reason, err)
		}
	}
	if _, _, err := domain.Adjust(l, stamp(t), -u, domain.ReasonOther, "   "); code(err) != domain.CodeNoteRequired {
		t.Errorf("other without a note: %v", err)
	}
	if _, _, err := domain.Adjust(l, stamp(t), 0, domain.ReasonDamaged, ""); code(err) != domain.CodeQuantityRequired {
		t.Errorf("a zero adjustment: %v", err)
	}
	long := ""
	for range domain.MaxNoteRunes + 1 {
		long += "ز"
	}
	if _, _, err := domain.Adjust(l, stamp(t), -u, domain.ReasonDamaged, long); code(err) != domain.CodeNoteTooLong {
		t.Errorf("a long note: %v", err)
	}
}

func TestStockWithNoCostIsRefused(t *testing.T) {
	never := domain.Level{ProductID: newID(t)}
	if _, _, err := domain.Count(never, stamp(t), 5*u, ""); code(err) != domain.CodeNoCost {
		t.Errorf("a count on a product never received: %v", err)
	}
	if _, _, err := domain.Count(never, stamp(t), 0, ""); code(err) != domain.CodeNoCost {
		t.Errorf("a zero count before any opening would block the opening: %v", err)
	}
	if _, _, err := domain.Adjust(never, stamp(t), 5*u, domain.ReasonOther, "found"); code(err) != domain.CodeNoCost {
		t.Errorf("an adjustment on a product never received: %v", err)
	}
	// A receipt reversed back to nothing leaves no cost either.
	receipt, l, _ := domain.Receive(never, stamp(t), 4*u, usd(3*u), "")
	_, l, err := domain.ReverseReceipt(l, receipt, stamp(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.Count(l, stamp(t), 2*u, ""); code(err) != domain.CodeNoCost {
		t.Errorf("found stock entered at no cost after a reversal: %v", err)
	}
	// Sold out, with an average: found stock enters at that average.
	soldOut := domain.Level{ProductID: newID(t), AvgCostMicro: 17 * u, LastMovementID: newID(t), LastSeq: 3}
	if _, after, err := domain.Count(soldOut, stamp(t), 2*u, ""); err != nil || after.AvgCostMicro != 17*u {
		t.Errorf("found stock on a sold-out product: %v %+v", err, after)
	}
}

func TestNothingGoesBelowZeroOutsideTheTill(t *testing.T) {
	l := stocked(t, 3*u, 5*u)
	if _, got, err := domain.Adjust(l, stamp(t), -3*u-1, domain.ReasonDamaged, ""); code(err) != domain.CodeBelowZero || got != l {
		t.Errorf("an adjustment below zero: %v", err)
	}
	if _, _, err := domain.Adjust(l, stamp(t), -3*u, domain.ReasonDamaged, ""); err != nil {
		t.Errorf("an adjustment to exactly zero: %v", err)
	}
	if _, _, err := domain.Count(l, stamp(t), -u, ""); code(err) != domain.CodeQuantityRequired {
		t.Errorf("a negative count: %v", err)
	}
	pkg := stocked(t, u, 95*u)
	link := domain.Package{ContentProductID: newID(t), ContentQuantityMicro: 16 * u}
	if _, err := domain.OpenPackage(pkg, domain.Level{ProductID: link.ContentProductID}, link, 2*u, stamp(t), stamp(t), newID(t)); code(err) != domain.CodeBelowZero {
		t.Errorf("opening more packages than on hand: %v", err)
	}
}

func TestReversalRestoresTheAverageExactly(t *testing.T) {
	before := stocked(t, 3*u, 1*u)
	receipt, received, _ := domain.Receive(before, stamp(t), 3*u, usd(2*u+333_333), "")
	m, after, err := domain.ReverseReceipt(received, receipt, stamp(t), "خطأ في الكمية")
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, received, m, after)
	if after.OnHandMicro != before.OnHandMicro || after.AvgCostMicro != before.AvgCostMicro {
		t.Fatalf("after reversal %+v, before receipt %+v", after, before)
	}
	if m.ReversesID != receipt.ID || m.QuantityMicro != -3*u || m.UnitCostMicro != receipt.UnitCostMicro || m.Kind != domain.KindReceiptReversal {
		t.Fatalf("m = %+v", m)
	}
}

func TestOnlyTheNewestReceiptCanBeReversed(t *testing.T) {
	l := stocked(t, 10*u, u)
	receipt, l, _ := domain.Receive(l, stamp(t), 5*u, usd(2*u), "")
	_, later, _ := domain.Adjust(l, stamp(t), -u, domain.ReasonDamaged, "")
	if _, _, err := domain.ReverseReceipt(later, receipt, stamp(t), ""); code(err) != domain.CodeNotNewest {
		t.Errorf("a receipt with a later movement was reversed: %v", err)
	}
	opening := stocked(t, u, u)
	openingRow := domain.Movement{ID: opening.LastMovementID, ProductID: opening.ProductID, Kind: domain.KindOpening}
	if _, _, err := domain.ReverseReceipt(opening, openingRow, stamp(t), ""); code(err) != domain.CodeNotReversible {
		t.Errorf("an opening was reversed: %v", err)
	}
	other := stocked(t, 10*u, u)
	if _, _, err := domain.ReverseReceipt(other, receipt, stamp(t), ""); code(err) != domain.CodeNotReversible {
		t.Errorf("another product's receipt was reversed: %v", err)
	}
}

func TestAReversalRefusedWhenStockAlreadyLeft(t *testing.T) {
	// Only reachable once the till can take stock out without a movement of its own kind (L4), but the domain
	// refuses it now rather than trusting that it cannot happen.
	l := stocked(t, 0+u, u)
	receipt, l, _ := domain.Receive(l, stamp(t), 5*u, usd(u), "")
	l.OnHandMicro = 2 * u
	if _, _, err := domain.ReverseReceipt(l, receipt, stamp(t), ""); code(err) != domain.CodeBelowZero {
		t.Fatalf("err = %v", err)
	}
}

// TestACostCorrectionMovesValueNotQuantity writes the zero-quantity row through the domain, as Mizan's revaluation
// never was until the table had to be rebuilt (H3). The store test writes this same row to SQLite.
func TestACostCorrectionMovesValueNotQuantity(t *testing.T) {
	before := stocked(t, 20*u, 9_500_000)
	m, after, err := domain.CorrectCost(before, stamp(t), 95*u, "كتبت 9.50 بدل 95.00")
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, m, after)
	if m.QuantityMicro != 0 || m.Kind != domain.KindCostCorrection || after.OnHandMicro != 20*u ||
		m.AvgCostBeforeMicro != 9_500_000 || after.AvgCostMicro != 95*u || m.UnitCostMicro != 95*u {
		t.Fatalf("m = %+v", m)
	}
	if _, _, err := domain.CorrectCost(before, stamp(t), 95*u, ""); code(err) != domain.CodeNoteRequired {
		t.Errorf("a correction without a note: %v", err)
	}
	if _, _, err := domain.CorrectCost(before, stamp(t), 9_500_000, "same"); code(err) != domain.CodeCostUnchanged {
		t.Errorf("a correction to the same cost: %v", err)
	}
	if _, _, err := domain.CorrectCost(domain.Level{ProductID: newID(t)}, stamp(t), u, "x"); code(err) != domain.CodeNoCost {
		t.Errorf("a correction on a product never received: %v", err)
	}
}

func TestOpeningAPackageCarriesItsCost(t *testing.T) {
	pkg := stocked(t, 3*u, 95*u)
	contentBefore := stocked(t, 4*u, 6*u)
	link := domain.Package{ContentProductID: contentBefore.ProductID, ContentQuantityMicro: 16 * u}
	pair := newID(t)
	opened, err := domain.OpenPackage(pkg, contentBefore, link, u, stamp(t), stamp(t), pair)
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, pkg, opened.Out, opened.PackageLevel)
	checkChain(t, contentBefore, opened.In, opened.ContentLv)
	if opened.Out.Kind != domain.KindPackageOut || opened.Out.QuantityMicro != -u || opened.Out.UnitCostMicro != 95*u ||
		opened.PackageLevel.AvgCostMicro != 95*u || opened.PackageLevel.OnHandMicro != 2*u {
		t.Fatalf("out = %+v", opened.Out)
	}
	// 95 ÷ 16 = 5.9375 a litre, averaged with 4 L at 6: (4 × 6 + 16 × 5.9375) ÷ 20 = 5.95
	if opened.In.Kind != domain.KindContentIn || opened.In.QuantityMicro != 16*u || opened.In.UnitCostMicro != 5_937_500 ||
		opened.ContentLv.AvgCostMicro != 5_950_000 || opened.ContentLv.OnHandMicro != 20*u {
		t.Fatalf("in = %+v, level %+v", opened.In, opened.ContentLv)
	}
	if opened.Out.PairID != pair || opened.In.PairID != pair || opened.In.Entered != (domain.Entered{}) {
		t.Fatalf("pair or entered wrong: %+v %+v", opened.Out, opened.In)
	}
}

// TestOpeningAPackageConservesValueWithinTheBound checks §3.5's stated bound on real levels: at most one cent
// between the value that left and the value that arrived.
func TestOpeningAPackageConservesValueWithinTheBound(t *testing.T) {
	for _, tc := range []struct{ avg, content, packages int64 }{
		{95 * u, 16 * u, u}, {95 * u, 15 * u, u}, {95 * u, 15 * u, 3 * u}, {1, 7 * u, u},
		{123_456_789, 19_999 * u, u}, {99_999_999, 1_500, 4 * u}, {17 * u, 3 * u, 12 * u},
	} {
		pkg := stocked(t, tc.packages, tc.avg)
		content := domain.Level{ProductID: newID(t)}
		link := domain.Package{ContentProductID: content.ProductID, ContentQuantityMicro: tc.content}
		opened, err := domain.OpenPackage(pkg, content, link, tc.packages, stamp(t), stamp(t), newID(t))
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		out, _ := domain.Level{AvgCostMicro: opened.Out.UnitCostMicro, OnHandMicro: tc.packages}.Value()
		in, _ := domain.Level{AvgCostMicro: opened.In.UnitCostMicro, OnHandMicro: opened.In.QuantityMicro}.Value()
		if d := out - in; d > 1 || d < -1 {
			t.Errorf("%+v: value out %d, in %d cents", tc, out, in)
		}
	}
}

func TestOpeningPackagesIsWholePackagesOnly(t *testing.T) {
	pkg := stocked(t, 3*u, 95*u)
	link := domain.Package{ContentProductID: newID(t), ContentQuantityMicro: 16 * u}
	content := domain.Level{ProductID: link.ContentProductID}
	for q, want := range map[int64]string{u / 2: domain.CodeWholePackagesOnly, 0: domain.CodeQuantityRequired, -u: domain.CodeQuantityRequired} {
		if _, err := domain.OpenPackage(pkg, content, link, q, stamp(t), stamp(t), newID(t)); code(err) != want {
			t.Errorf("%d packages: %v", q, err)
		}
	}
	if _, err := domain.OpenPackage(pkg, content, domain.Package{ContentProductID: link.ContentProductID}, u, stamp(t), stamp(t), newID(t)); code(err) != domain.CodeNotAPackage {
		t.Errorf("a link with no content: %v", err)
	}
}

// Value, at 0, 2 and 3 decimals (D-L2.5) — the USD figure is what L2 stores; the formatter is tested at every scale.

func TestAValueIsInCents(t *testing.T) {
	for _, tc := range []struct {
		onHand, avg, want int64
	}{{12 * u, 17 * u, 20_400}, {40 * u, 1_262_000, 5_048}, {3 * u, 5_937_500, 1_781}, {1_500, 5_937_500, 1}, {0, 17 * u, 0}} {
		got, err := domain.Level{OnHandMicro: tc.onHand, AvgCostMicro: tc.avg}.Value()
		if err != nil || got != tc.want {
			t.Errorf("%d × %d = %d cents, want %d (%v)", tc.onHand, tc.avg, got, tc.want, err)
		}
	}
}

func TestFormatting(t *testing.T) {
	for _, tc := range []struct {
		v        int64
		decimals int
		want     string
	}{
		{12_500_000, 3, "12.500"}, {3 * u, 0, "3"}, {5_937_500, 2, "5.9375"}, {-2_500_000, 3, "-2.500"},
		{1_500, 3, "0.0015"}, {0, 0, "0"}, {0, 2, "0.00"}, {-1, 0, "-0.000001"},
		{-9_223_372_036_854_775_808, 0, "-9223372036854.775808"},
	} {
		if got := domain.FormatScaled(tc.v, tc.decimals); got != tc.want {
			t.Errorf("FormatScaled(%d, %d) = %s, want %s", tc.v, tc.decimals, got, tc.want)
		}
	}
	for minor, want := range map[int64]string{114_000: "1140.00", 5: "0.05", -1_781: "-17.81", 0: "0.00"} {
		if got := domain.FormatMinor(minor); got != want {
			t.Errorf("FormatMinor(%d) = %s, want %s", minor, got, want)
		}
	}
}

// Typed input.

func TestQuantitiesRespectTheirUnit(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		decimals int
		want     int64
		code     string
	}{
		{"12.5", 3, 12_500_000, ""}, {"١٢٫٥", 3, 12_500_000, ""}, {"3", 0, 3 * u, ""},
		{"1.5", 0, 0, domain.CodeQuantityDecimals}, {"0.0005", 3, 0, domain.CodeQuantityDecimals},
		{"1,500", 3, 0, "lite.number.grouping"}, {"", 3, 0, "lite.number.invalid"}, {"-1", 3, 0, "lite.number.invalid"},
		{"99999999999999", 0, 0, domain.CodeQuantityTooLarge},
	} {
		got, err := domain.ParseQuantity(tc.raw, tc.decimals, domain.FieldQuantity)
		if code(err) != tc.code || got != tc.want {
			t.Errorf("ParseQuantity(%q, %d) = %d, %v", tc.raw, tc.decimals, got, err)
		}
		if tc.code != "" {
			typed, _ := errs.AsError(err)
			if typed == nil || len(typed.Fields) == 0 || typed.Fields[len(typed.Fields)-1].Field != domain.FieldQuantity {
				t.Errorf("%q: the refusal names no field: %v", tc.raw, err)
			}
		}
	}
}

func TestACostTypedAsATotalIsNeverRoundedToTheCurrencyFirst(t *testing.T) {
	// A sack of 50 kg for $63.10 is $1.262 a kilo: more decimals than the currency has.
	cost, err := domain.ResolveCost(domain.CostInput{Mode: domain.CostTotal, Amount: "63.10", Currency: "USD"}, 50*u, currencies)
	if err != nil || cost.UnitCostMicro != 1_262_000 {
		t.Fatalf("cost = %+v, %v", cost, err)
	}
	if cost.Entered != (domain.Entered{Currency: "USD", UnitCostMicro: 1_262_000}) {
		t.Fatalf("entered = %+v", cost.Entered)
	}
	// 3 jars for $10.00 is 3.333333 a jar.
	cost, _ = domain.ResolveCost(domain.CostInput{Amount: "10", Currency: "USD"}, 3*u, currencies)
	if cost.UnitCostMicro != 3_333_333 {
		t.Fatalf("an empty mode is not the total: %+v", cost)
	}
}

func TestAPoundReceiptIsConvertedAtTheRateTypedOnIt(t *testing.T) {
	// 25 kg for 450,000 SYP at 15,000 a dollar: 18,000 a kilo, $1.20 a kilo.
	cost, err := domain.ResolveCost(domain.CostInput{Mode: domain.CostTotal, Amount: "450000", Currency: "SYP", Rate: "15000"}, 25*u, currencies)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Cost{UnitCostMicro: 1_200_000, Entered: domain.Entered{Currency: "SYP", UnitCostMicro: 18_000 * u, LocalPerUSDNano: 15_000_000_000_000}}
	if cost != want {
		t.Fatalf("cost = %+v\nwant %+v", cost, want)
	}
	// Per unit, and the one rounding: 10,000 at 15,000 = 0.6666666… → 0.666667.
	cost, _ = domain.ResolveCost(domain.CostInput{Mode: domain.CostUnit, Amount: "10000", Currency: "SYP", Rate: "١٥٠٠٠"}, 7*u, currencies)
	if cost.UnitCostMicro != 666_667 || cost.Entered.UnitCostMicro != 10_000*u {
		t.Fatalf("cost = %+v", cost)
	}
	for _, rate := range []string{"", "0", "abc", "-1"} {
		_, err := domain.ResolveCost(domain.CostInput{Amount: "450000", Currency: "SYP", Rate: rate}, 25*u, currencies)
		if code(err) != domain.CodeRateRequired && code(err) != "lite.number.invalid" {
			t.Errorf("rate %q: %v", rate, err)
		}
	}
	// A dollar receipt ignores a rate left in the form.
	cost, _ = domain.ResolveCost(domain.CostInput{Amount: "10", Currency: "USD", Rate: "15000"}, u, currencies)
	if cost.Entered.LocalPerUSDNano != 0 {
		t.Fatalf("a USD receipt kept a rate: %+v", cost)
	}
}

func TestACostIsRefusedWithTooManyDecimalsOrNoCurrency(t *testing.T) {
	for _, tc := range []struct {
		in   domain.CostInput
		code string
	}{
		{domain.CostInput{Amount: "10.005", Currency: "USD"}, domain.CodeCostDecimals},
		{domain.CostInput{Amount: "450000.5", Currency: "SYP", Rate: "15000"}, domain.CodeCostDecimals},
		{domain.CostInput{Mode: domain.CostUnit, Amount: "1.1234567", Currency: "USD"}, domain.CodeCostDecimals},
		{domain.CostInput{Amount: "10", Currency: "EUR"}, domain.CodeUnknownCurrency},
		{domain.CostInput{Amount: "ten", Currency: "USD"}, "lite.number.invalid"},
		{domain.CostInput{Mode: "guess", Amount: "10", Currency: "USD"}, "lite.number.invalid"},
	} {
		if _, err := domain.ResolveCost(tc.in, u, currencies); code(err) != tc.code {
			t.Errorf("%+v: %v, want %s", tc.in, err, tc.code)
		}
	}
	if _, err := domain.ResolveCost(domain.CostInput{Amount: "10", Currency: "USD"}, 0, currencies); code(err) != domain.CodeQuantityRequired {
		t.Errorf("a cost for no quantity: %v", err)
	}
}

func TestAUSDCostCorrectionFigure(t *testing.T) {
	for raw, want := range map[string]int64{"95": 95 * u, "5.9375": 5_937_500, "٠٫٥": 500_000} {
		if got, err := domain.ParseUSDCost(raw); err != nil || got != want {
			t.Errorf("%q = %d, %v", raw, got, err)
		}
	}
	if _, err := domain.ParseUSDCost("1.0000001"); code(err) != domain.CodeCostDecimals {
		t.Errorf("seven decimals: %v", err)
	}
}

func TestEveryKindAndReasonMatchesTheSchema(t *testing.T) {
	// The schema's lists, as 0003_stock.sql spells them; the store test proves the database accepts each.
	kinds := "opening receipt receipt_reversal count adjustment package_out content_in cost_correction sale sale_void"
	got := ""
	for i, k := range domain.Kinds {
		if i > 0 {
			got += " "
		}
		got += string(k)
	}
	if got != kinds {
		t.Fatalf("kinds = %s", got)
	}
	if len(domain.AdjustmentReasons) != 5 || strconv.Quote(string(domain.AdjustmentReasons[3])) != `"gift"` {
		t.Fatalf("reasons = %v", domain.AdjustmentReasons)
	}
}

// TestQuantitiesFollowTheSharedFixture is the Go half of the quantities contract; the frontend's quantityProblem reads
// the same cases.
func TestQuantitiesFollowTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "numinput", "testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Quantities []struct {
			Input    string `json:"input"`
			Decimals int    `json:"decimals"`
			Error    string `json:"error"`
		} `json:"quantities"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Quantities) < 8 {
		t.Fatalf("only %d quantity cases", len(fixture.Quantities))
	}
	for _, c := range fixture.Quantities {
		_, err := domain.ParseQuantity(c.Input, c.Decimals, domain.FieldQuantity)
		if code(err) != c.Error {
			t.Errorf("ParseQuantity(%q, %d) = %v, want %q", c.Input, c.Decimals, err, c.Error)
		}
	}
}

// L4: the till's movements.

func TestASaleMayTakeStockBelowZero(t *testing.T) {
	before := stocked(t, 2*u, 17*u)
	saleID, lineID := newID(t), newID(t)
	m, after, err := domain.Sale(before, stamp(t), 5*u, saleID, lineID)
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, m, after)
	if m.Kind != domain.KindSale || m.QuantityMicro != -5*u || after.OnHandMicro != -3*u || m.SaleID != saleID || m.SaleLineID != lineID {
		t.Fatalf("m = %+v", m)
	}
	if _, _, err := domain.Sale(before, stamp(t), 0, saleID, lineID); code(err) != domain.CodeQuantityRequired {
		t.Fatalf("a sale of nothing: %v", err)
	}
}

func TestASaleNeverMovesTheAverage(t *testing.T) {
	before := stocked(t, 10*u, 17*u)
	m, after, _ := domain.Sale(before, stamp(t), 3*u, newID(t), newID(t))
	if after.AvgCostMicro != 17*u || m.UnitCostMicro != 17*u || m.AvgCostAfterMicro != 17*u {
		t.Fatalf("m = %+v", m)
	}
}

func TestANeverReceivedProductSellsWithItsCostUnknown(t *testing.T) {
	never := domain.Level{ProductID: newID(t)}
	if never.CostKnown() {
		t.Fatal("a product that never moved has a known cost")
	}
	m, after, err := domain.Sale(never, stamp(t), u, newID(t), newID(t))
	if err != nil || m.UnitCostMicro != 0 || after.OnHandMicro != -u || after.AvgCostMicro != 0 {
		t.Fatalf("m = %+v, %v", m, err)
	}
	if !stocked(t, u, 0).CostKnown() {
		t.Fatal("stock received at no cost has an unknown cost; it is known to be zero")
	}
	soldOut := domain.Level{ProductID: newID(t), AvgCostMicro: 17 * u, LastSeq: 2, LastMovementID: newID(t)}
	if !soldOut.CostKnown() {
		t.Fatal("a sold-out product has lost its average")
	}
}

func TestAVoidAveragesInAtTheCostTheGoodsLeftAt(t *testing.T) {
	l := stocked(t, 10*u, 10*u)
	sale, l, _ := domain.Sale(l, stamp(t), 4*u, newID(t), newID(t))
	// A delivery at a higher cost arrives before the void: 6 at 10 and 6 at 16 average 13.
	_, l, _ = domain.Receive(l, stamp(t), 6*u, usd(16*u), "")
	if l.AvgCostMicro != 13*u {
		t.Fatalf("avg = %d", l.AvgCostMicro)
	}
	before := l
	void, after, err := domain.SaleVoid(l, sale, stamp(t))
	if err != nil {
		t.Fatal(err)
	}
	checkChain(t, before, void, after)
	// 12 at 13 and 4 back at 10: (156 + 40) ÷ 16 = 12.25 — not today's 13, not the sale-day 10.
	if void.Kind != domain.KindSaleVoid || void.QuantityMicro != 4*u || void.UnitCostMicro != 10*u || after.AvgCostMicro != 12_250_000 ||
		void.ReversesID != sale.ID || void.SaleLineID != sale.SaleLineID || void.SaleID != sale.SaleID {
		t.Fatalf("void = %+v, after %+v", void, after)
	}
	// Into negative stock the goods take the cost they left at.
	neg := stocked(t, u, 10*u)
	sale, neg, _ = domain.Sale(neg, stamp(t), 3*u, newID(t), newID(t))
	neg.AvgCostMicro = 99 * u // a phantom average, as in Mizan's −759 case
	_, back, _ := domain.SaleVoid(neg, sale, stamp(t))
	if back.AvgCostMicro != 10*u || back.OnHandMicro != u {
		t.Fatalf("void into negative stock = %+v", back)
	}
	if _, _, err := domain.SaleVoid(neg, domain.Movement{Kind: domain.KindReceipt, ProductID: neg.ProductID}, stamp(t)); code(err) != domain.CodeNotReversible {
		t.Fatalf("voiding a receipt: %v", err)
	}
}
