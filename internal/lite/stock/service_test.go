package stock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock/stocktest"
)

const u = int64(1_000_000)

var damascus = time.FixedZone("Damascus", 3*3600)

type fixture struct {
	svc       *stock.Service
	store     *stocktest.Fake
	catalogue *stocktest.Catalogue
	gate      *stocktest.Gate
	clk       *clock.Fixed
}

func newFixture() fixture {
	f := fixture{
		store: stocktest.NewFake(), catalogue: stocktest.NewCatalogue(), gate: &stocktest.Gate{},
		// 01:00 on 14 September in Damascus.
		clk: clock.NewFixed(time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC)),
	}
	f.svc = stock.NewService(litetest.Immediate{}, f.store, f.catalogue, f.gate, f.clk, damascus)
	return f
}

var ctx = context.Background()

func dollars(amount string) domain.CostInput {
	return domain.CostInput{Mode: domain.CostTotal, Amount: amount, Currency: "USD"}
}

func (f fixture) level(t *testing.T, productID id.ID) domain.Level {
	t.Helper()
	l, err := f.store.Level(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// opened is a product in kg with 12 kg of opening stock at $17.00 a kg.
func (f fixture) opened(t *testing.T) id.ID {
	t.Helper()
	p := f.catalogue.Add(t, 3, true)
	if _, err := f.svc.Opening(ctx, stock.ReceiveInput{ProductID: p, Quantity: "12", Cost: dollars("204")}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAMovementCarriesTheShopsBusinessDate(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	history, err := f.svc.History(ctx, p, 10)
	if err != nil || len(history.Movements) != 1 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	m := history.Movements[0]
	if m.BusinessDate != "2026-09-14" || !m.OccurredAt.Equal(f.clk.Now()) {
		t.Fatalf("date %s at %s — a movement at 01:00 in Damascus belongs to the shop's today", m.BusinessDate, m.OccurredAt)
	}
}

func TestReceivingByTotalAndInPounds(t *testing.T) {
	f := newFixture()
	p := f.catalogue.Add(t, 3, true)
	m, err := f.svc.Receive(ctx, stock.ReceiveInput{ProductID: p, Quantity: "25",
		Cost: domain.CostInput{Mode: domain.CostTotal, Amount: "450000", Currency: "SYP", Rate: "15000"}, Note: "أبو خليل"})
	if err != nil {
		t.Fatal(err)
	}
	if m.UnitCostMicro != 1_200_000 || m.Entered.LocalPerUSDNano != 15_000*1_000_000_000 || m.Kind != domain.KindReceipt {
		t.Fatalf("m = %+v", m)
	}
	if l := f.level(t, p); l.OnHandMicro != 25*u || l.AvgCostMicro != 1_200_000 || l.RowVersion != 1 {
		t.Fatalf("level = %+v", l)
	}
	if _, err := f.svc.Receive(ctx, stock.ReceiveInput{ProductID: p, Quantity: "1.2345", Cost: dollars("1")}); errs.CodeOf(err) != domain.CodeQuantityDecimals {
		t.Fatalf("a fourth decimal of a kilo: %v", err)
	}
	if len(f.store.Snapshot()) != 1 {
		t.Fatal("a refused receipt wrote a movement")
	}
}

func TestOpeningOnlyAsTheFirstMovement(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	if _, err := f.svc.Opening(ctx, stock.ReceiveInput{ProductID: p, Quantity: "1", Cost: dollars("1")}); errs.CodeOf(err) != domain.CodeNotFirstMovement {
		t.Fatalf("a second opening: %v", err)
	}
}

func TestReceivingIntoAnInactiveProductIsRefused(t *testing.T) {
	f := newFixture()
	p := f.catalogue.Add(t, 0, false)
	for name, act := range map[string]func(context.Context, stock.ReceiveInput) (domain.Movement, error){
		"receive": f.svc.Receive, "opening": f.svc.Opening,
	} {
		if _, err := act(ctx, stock.ReceiveInput{ProductID: p, Quantity: "1", Cost: dollars("1")}); errs.CodeOf(err) != domain.CodeInactiveProduct {
			t.Errorf("%s into an inactive product: %v", name, err)
		}
	}
	missing, _ := id.New()
	if _, err := f.svc.Receive(ctx, stock.ReceiveInput{ProductID: missing, Quantity: "1", Cost: dollars("1")}); errs.CodeOf(err) != stocktest.CodeProductNotFound {
		t.Errorf("a product that does not exist: %v", err)
	}
}

func TestLoweringStockNeedsTheOwnerAndRaisingDoesNot(t *testing.T) {
	f := newFixture()
	p := f.opened(t)

	if _, err := f.svc.Count(ctx, stock.CountInput{ProductID: p, Counted: "14"}); err != nil {
		t.Fatalf("a count that raises: %v", err)
	}
	if _, err := f.svc.Adjust(ctx, stock.AdjustInput{ProductID: p, Direction: stock.DirectionIn, Quantity: "1", Reason: domain.ReasonOther, Note: "وجدت"}); err != nil {
		t.Fatalf("an adjustment that raises: %v", err)
	}
	if _, err := f.svc.Count(ctx, stock.CountInput{ProductID: p, Counted: "15"}); err != nil {
		t.Fatalf("a count that matches: %v", err)
	}
	if f.gate.Asked != 0 {
		t.Fatalf("raising stock asked for the owner %d times", f.gate.Asked)
	}

	before := f.level(t, p)
	if _, err := f.svc.Count(ctx, stock.CountInput{ProductID: p, Counted: "13"}); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("a lowering count outside owner mode: %v", err)
	}
	if _, err := f.svc.Adjust(ctx, stock.AdjustInput{ProductID: p, Direction: stock.DirectionOut, Quantity: "3", Reason: domain.ReasonDamaged}); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("a write-off outside owner mode: %v", err)
	}
	if f.level(t, p) != before {
		t.Fatal("a refused act changed the level")
	}

	f.gate.Elevated = true
	if _, err := f.svc.Count(ctx, stock.CountInput{ProductID: p, Counted: "13"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Adjust(ctx, stock.AdjustInput{ProductID: p, Direction: stock.DirectionOut, Quantity: "3", Reason: domain.ReasonDamaged}); err != nil {
		t.Fatal(err)
	}
	if len(f.gate.Acts) != 2 || f.gate.Acts[0].Action != stock.ActCountLower || f.gate.Acts[0].Before != "15.000" ||
		f.gate.Acts[0].After != "13.000" || f.gate.Acts[1].Action != stock.ActAdjustLower || f.gate.Acts[1].SubjectID != p {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}
	if l := f.level(t, p); l.OnHandMicro != 10*u || l.AvgCostMicro != 17*u {
		t.Fatalf("level = %+v", l)
	}
}

func TestARefusalIsDecidedBeforeTheOwnerIsAsked(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	// Below zero is refused whatever the owner would say: a PIN typed for an act that cannot happen is typed for nothing.
	if _, err := f.svc.Adjust(ctx, stock.AdjustInput{ProductID: p, Direction: stock.DirectionOut, Quantity: "13", Reason: domain.ReasonDamaged}); errs.CodeOf(err) != domain.CodeBelowZero {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.svc.Adjust(ctx, stock.AdjustInput{ProductID: p, Quantity: "1", Reason: domain.ReasonDamaged}); errs.CodeOf(err) != domain.CodeQuantityRequired {
		t.Fatalf("an adjustment with no direction: %v", err)
	}
	if f.gate.Asked != 0 {
		t.Fatalf("the owner was asked %d times", f.gate.Asked)
	}
}

func TestOnlyTheNewestReceiptCanBeReversedAndOnlyOnce(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	receipt, err := f.svc.Receive(ctx, stock.ReceiveInput{ProductID: p, Quantity: "10", Cost: dollars("95")})
	if err != nil {
		t.Fatal(err)
	}
	history, _ := f.svc.History(ctx, p, 10)
	if history.ReversibleID != receipt.ID {
		t.Fatalf("the newest receipt is not offered for reversal: %+v", history.ReversibleID)
	}

	if _, err = f.svc.ReverseReceipt(ctx, receipt.ID, ""); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("a reversal outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	m, err := f.svc.ReverseReceipt(ctx, receipt.ID, "الكمية خطأ")
	if err != nil {
		t.Fatal(err)
	}
	if l := f.level(t, p); l.OnHandMicro != 12*u || l.AvgCostMicro != 17*u || l.LastMovementID != m.ID {
		t.Fatalf("level after reversal = %+v", l)
	}
	if _, err = f.svc.ReverseReceipt(ctx, receipt.ID, ""); errs.CodeOf(err) != domain.CodeNotNewest {
		t.Fatalf("a receipt reversed twice: %v", err)
	}
	if history, _ := f.svc.History(ctx, p, 10); history.ReversibleID != "" {
		t.Fatal("a reversal is offered after the receipt was reversed")
	}

	second, _ := f.svc.Receive(ctx, stock.ReceiveInput{ProductID: p, Quantity: "1", Cost: dollars("17")})
	if _, err = f.svc.Count(ctx, stock.CountInput{ProductID: p, Counted: "13"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.ReverseReceipt(ctx, second.ID, ""); errs.CodeOf(err) != domain.CodeNotNewest {
		t.Fatalf("a receipt with a later movement was reversed: %v", err)
	}
	missing, _ := id.New()
	if _, err = f.svc.ReverseReceipt(ctx, missing, ""); errs.CodeOf(err) != domain.CodeMovementNotFound {
		t.Fatalf("a movement that does not exist: %v", err)
	}
	if len(f.gate.Acts) != 1 || f.gate.Acts[0].Action != stock.ActReverseReceipt {
		t.Fatalf("acts = %+v", f.gate.Acts)
	}
}

func TestACostCorrectionIsTheOwnersAndMovesNoStock(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	in := stock.CorrectCostInput{ProductID: p, AverageCost: "15.50", Note: "كتبت السعر خطأ"}
	if _, err := f.svc.CorrectCost(ctx, in); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	m, err := f.svc.CorrectCost(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.QuantityMicro != 0 || f.level(t, p).AvgCostMicro != 15_500_000 || f.level(t, p).OnHandMicro != 12*u {
		t.Fatalf("m = %+v", m)
	}
	if f.gate.Acts[0] != (stock.GuardedAct{Action: stock.ActCorrectCost, SubjectID: p, Before: "17.00", After: "15.50"}) {
		t.Fatalf("act = %+v", f.gate.Acts[0])
	}
}

func TestOpeningAPackageWritesBothRowsOrNeither(t *testing.T) {
	f := newFixture()
	tin := f.catalogue.Add(t, 0, true)
	loose := f.catalogue.Add(t, 3, true)
	f.catalogue.Link(tin, loose, 16*u)
	if _, err := f.svc.Opening(ctx, stock.ReceiveInput{ProductID: tin, Quantity: "3", Cost: dollars("285")}); err != nil {
		t.Fatal(err)
	}

	opened, err := f.svc.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: tin, Packages: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Out.PairID == "" || opened.Out.PairID != opened.In.PairID || opened.In.QuantityMicro != 16*u || opened.In.UnitCostMicro != 5_937_500 {
		t.Fatalf("opened = %+v", opened)
	}
	if f.level(t, tin).OnHandMicro != 2*u || f.level(t, loose).OnHandMicro != 16*u {
		t.Fatal("the levels did not follow")
	}
	if findings, _ := f.svc.VerifyUnguarded(ctx); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}

	// The content's level fails to write after both rows were appended: through a real transaction neither remains
	// (TestTheLevelAndItsMovementCommitTogether proves the rollback); here the service must at least return the failure.
	f.store.FailSaveLevel(2)
	if _, err := f.svc.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: tin, Packages: "1"}); !errors.Is(err, stocktest.ErrInjected) {
		t.Fatalf("err = %v", err)
	}

	for packages, want := range map[string]string{"1.5": domain.CodeQuantityDecimals, "3": domain.CodeBelowZero, "0": domain.CodeQuantityRequired} {
		if _, err := f.svc.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: loose, Packages: packages}); errs.CodeOf(err) != domain.CodeNotAPackage {
			t.Errorf("loose oil is no package: %v", err)
		}
		fresh := newFixture()
		ftin, floose := fresh.catalogue.Add(t, 0, true), fresh.catalogue.Add(t, 3, true)
		fresh.catalogue.Link(ftin, floose, 16*u)
		if _, err := fresh.svc.Opening(ctx, stock.ReceiveInput{ProductID: ftin, Quantity: "2", Cost: dollars("190")}); err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.svc.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: ftin, Packages: packages}); errs.CodeOf(err) != want {
			t.Errorf("%s packages: %v, want %s", packages, err, want)
		}
		fresh.catalogue.SetActive(floose, false)
		if _, err := fresh.svc.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: ftin, Packages: "1"}); errs.CodeOf(err) != domain.CodeInactiveContent {
			t.Errorf("opening into an inactive product: %v", err)
		}
	}
}

func TestCostsAreOnlyForTheOwner(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	if _, err := f.svc.Valuation(ctx); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("valuation outside owner mode: %v", err)
	}
	if _, err := f.svc.Verify(ctx); errs.CodeOf(err) != stock.CodeOwnerRequired {
		t.Fatalf("verify outside owner mode: %v", err)
	}
	history, err := f.svc.History(ctx, p, 10)
	if err != nil || history.CostsVisible {
		t.Fatalf("history = %+v, %v", history, err)
	}
	m := history.Movements[0]
	if m.UnitCostMicro != 0 || m.AvgCostAfterMicro != 0 || m.Entered != (domain.Entered{}) || m.QuantityMicro != 12*u {
		t.Fatalf("costs leaked outside owner mode: %+v", m)
	}
	levels, err := f.svc.Levels(ctx)
	if err != nil || len(levels) != 1 || levels[0].OnHandMicro != 12*u || levels[0].UnitDecimals != 3 {
		t.Fatalf("levels = %+v, %v", levels, err)
	}

	f.gate.Elevated = true
	valuation, err := f.svc.Valuation(ctx)
	if err != nil || len(valuation.Lines) != 1 || valuation.Lines[0].ValueMinor != 20_400 || valuation.TotalMinor != 20_400 ||
		valuation.Lines[0].AvgCostMicro != 17*u {
		t.Fatalf("valuation = %+v, %v", valuation, err)
	}
	history, _ = f.svc.History(ctx, p, 10)
	if !history.CostsVisible || history.Movements[0].UnitCostMicro != 17*u {
		t.Fatalf("history in owner mode = %+v", history)
	}
	if f.gate.Asked != 0 {
		t.Fatal("a guarded read was recorded as an act")
	}
}

func TestVerifyFindsWhatTheStoreHolds(t *testing.T) {
	f := newFixture()
	p := f.opened(t)
	f.gate.Elevated = true
	if findings, err := f.svc.Verify(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	l := f.level(t, p)
	l.OnHandMicro += u
	f.store.Plant(l)
	findings, err := f.svc.Verify(ctx)
	if err != nil || len(findings) != 1 || findings[0].Code != domain.FindingLevelMismatch || findings[0].ProductID != p {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	if f.level(t, p) != l {
		t.Fatal("the verifier repaired what it found")
	}
}
