package suppliers_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/supplierstest"
)

// shop fakes every port: a stock book that records receipts, a catalogue of cups and plates, SYP as the local currency,
// and an owner gate that records every act.
type shop struct {
	receipts []suppliers.Receipt
	reversed []id.ID
	refuse   map[id.ID]bool // receipts the stock book will not reverse (a later movement)
	products map[id.ID]domain.Product
	acts     []string
	cups     id.ID
	plates   id.ID
	pinOn    bool // the PIN switch on and nobody in owner mode
}

func (s *shop) Receive(_ context.Context, r suppliers.Receipt) (id.ID, error) {
	s.receipts = append(s.receipts, r)
	return id.New()
}

func (s *shop) ReverseReceipt(_ context.Context, ledgerID id.ID, _ string) error {
	if s.refuse[ledgerID] {
		return errs.Conflict("lite.stock.not_newest", "only the newest movement can be reversed")
	}
	s.reversed = append(s.reversed, ledgerID)
	return nil
}

func (s *shop) Products(_ context.Context, ids []id.ID) (map[id.ID]domain.Product, error) {
	out := map[id.ID]domain.Product{}
	for _, i := range ids {
		if p, ok := s.products[i]; ok {
			out[i] = p
		}
	}
	return out, nil
}

func (s *shop) Currencies(context.Context) ([]domain.Currency, error) {
	return []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}, nil
}

func (s *shop) LocalCurrency(context.Context) (string, error) { return "SYP", nil }

func (s *shop) Require(_ context.Context, act suppliers.GuardedAct) error {
	s.acts = append(s.acts, act.Action)
	return nil
}

func (s *shop) Allowed(context.Context) bool { return !s.pinOn }

func newBook(t *testing.T) (*shop, *suppliers.Service, *supplierstest.Fake) {
	t.Helper()
	cups, _ := id.New()
	plates, _ := id.New()
	s := &shop{cups: cups, plates: plates, refuse: map[id.ID]bool{}, products: map[id.ID]domain.Product{
		cups:   {ID: cups, NameAR: "فنجان قهوة", UnitCode: "jar", Active: true},
		plates: {ID: plates, NameAR: "صحون", UnitCode: "piece", Active: true},
	}}
	store := supplierstest.NewFake()
	svc := suppliers.NewService(supplierstest.Immediate{}, store,
		suppliers.Ports{Stock: s, Catalogue: s, Money: s, Gate: s},
		clock.NewFixed(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)), time.UTC)
	return s, svc, store
}

func aSupplier(t *testing.T, svc *suppliers.Service) domain.Supplier {
	t.Helper()
	sup, err := svc.Create(context.Background(), domain.Draft{Name: "المروى", Phone: "0988 703 785", City: "حلب"})
	if err != nil {
		t.Fatal(err)
	}
	return sup
}

func balance(t *testing.T, svc *suppliers.Service, supplierID id.ID, currency string) int64 {
	t.Helper()
	st, err := svc.StatementOf(context.Background(), supplierID, currency)
	if err != nil {
		t.Fatal(err)
	}
	return st.BalanceMinor
}

func TestSuppliersAreUniqueByNameAsAReaderReadsIt(t *testing.T) {
	_, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	if _, err := svc.Create(ctx, domain.Draft{Name: "المروي"}); errs.CodeOf(err) != domain.CodeDuplicateName {
		t.Fatalf("a second supplier whose name reads the same: %v", err)
	}
	edited, err := svc.Update(ctx, suppliers.UpdateInput{ID: sup.ID, RowVersion: sup.RowVersion, Draft: domain.Draft{Name: "المروى B&Z", City: "حلب"}})
	if err != nil || edited.Name != "المروى B&Z" || edited.RowVersion != 2 {
		t.Fatalf("Update = %+v, %v", edited, err)
	}
	if _, err := svc.Update(ctx, suppliers.UpdateInput{ID: sup.ID, RowVersion: 1, Draft: domain.Draft{Name: "x"}}); errs.CodeOf(err) != "database.concurrent_modification" {
		t.Fatalf("a stale edit: %v", err)
	}
}

// TestAPurchaseReceivesTheGoodUnitsAtWhatTheyCost: ten cups, two damaged, 10% off — eight received at 72 dollars; the
// book owes 72, less the 50 paid from the drawer on the spot.
func TestAPurchaseReceivesTheGoodUnitsAtWhatTheyCost(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	in := domain.Input{SupplierID: sup.ID, Currency: "USD", SupplierRef: "2970", PaidNow: "50", PaidFrom: "drawer", Lines: []domain.LineInput{
		{ProductID: s.cups, Quantity: "10", Damaged: "2", UnitCost: "10", DiscountPercent: "10"},
	}}
	quote, err := svc.QuotePurchase(ctx, in)
	if err != nil || quote.Purchase.DueMinor() != 7_200 || quote.BalanceBeforeMinor != 0 || quote.BalanceAfterMinor != 2_200 {
		t.Fatalf("QuotePurchase = %+v, %v", quote, err)
	}
	if len(s.receipts) != 0 {
		t.Fatal("a quote received stock")
	}
	p, err := svc.RecordPurchase(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if p.PurchaseNo != 1 || p.BusinessDate != "2026-09-24" || p.Lines[0].StockLedgerID == "" {
		t.Fatalf("RecordPurchase = %+v", p)
	}
	if len(s.receipts) != 1 || s.receipts[0].Quantity != "8" || s.receipts[0].Total != "72.00" || s.receipts[0].Currency != "USD" {
		t.Fatalf("the stock book received %+v — want 8 at 72.00 dollars", s.receipts)
	}
	if got := balance(t, svc, sup.ID, "USD"); got != 2_200 {
		t.Fatalf("the shop owes %d, want 22.00", got)
	}
	st, _ := svc.StatementOf(ctx, sup.ID, "USD")
	if len(st.Entries) != 2 || st.Entries[0].Kind != domain.KindPurchase || st.Entries[1].Kind != domain.KindPayment ||
		st.Entries[1].Source != domain.SourceDrawer || st.Entries[1].PurchaseID != p.ID {
		t.Fatalf("the book = %+v", st.Entries)
	}
	if len(s.acts) != 1 || s.acts[0] != suppliers.ActPurchase {
		t.Fatalf("owner's acts = %v", s.acts)
	}
	// The drawer paid 50 dollars out today.
	if drawer, _ := svc.DrawerOn(ctx, "2026-09-24"); drawer["USD"] != -5_000 {
		t.Fatalf("the drawer moved %+v", drawer)
	}
}

// TestGoodsThatAllArriveDamagedCostNothing: nothing is received and nothing is owed — but the purchase is on file.
func TestGoodsThatAllArriveDamagedCostNothing(t *testing.T) {
	s, svc, store := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	p, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD", Lines: []domain.LineInput{
		{ProductID: s.plates, Quantity: "3", Damaged: "3", UnitCost: "5"},
	}})
	if err != nil || p.DueMinor() != 0 || p.Lines[0].StockLedgerID != "" || len(s.receipts) != 0 {
		t.Fatalf("RecordPurchase = %+v, %v; receipts %+v", p, err, s.receipts)
	}
	if st, _ := svc.StatementOf(ctx, sup.ID, "USD"); len(st.Entries) != 0 {
		t.Fatalf("a purchase that cost nothing wrote %+v", st.Entries)
	}
	if on, _ := store.Purchase(ctx, p.ID); on.Lines[0].DamagedMicro != 3_000_000 {
		t.Fatal("the damage is not on file")
	}
}

// TestPaidGoodsThatArriveDamagedAreOwedBack: the owner's rule — the supplier does not charge for damaged goods, so money
// paid for them is the supplier owing the shop, and a refund settles it.
func TestPaidGoodsThatArriveDamagedAreOwedBack(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	// Six cups at 18 paid in full, 108 dollars — but one arrived broken: the shop owes 90 and paid 108.
	if _, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD", PaidNow: "108", PaidFrom: "owner",
		Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "6", Damaged: "1", UnitCost: "18"}}}); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, svc, sup.ID, "USD"); got != -1_800 {
		t.Fatalf("the supplier owes the shop %d, want 18.00", -got)
	}
	if drawer, _ := svc.DrawerOn(ctx, "2026-09-24"); drawer["USD"] != 0 {
		t.Fatalf("the owner's own money moved the drawer: %+v", drawer)
	}
	acts := len(s.acts)
	if _, err := svc.Refund(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "18", Source: "drawer"}); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, svc, sup.ID, "USD"); got != 0 || len(s.acts) != acts {
		t.Fatalf("after the refund: %d owed, acts %v — money coming in is nobody's guarded act", got, s.acts)
	}
	if drawer, _ := svc.DrawerOn(ctx, "2026-09-24"); drawer["USD"] != 1_800 {
		t.Fatalf("the refund into the drawer: %+v", drawer)
	}
}

// TestAVoidTakesTheGoodsBackOutAndTheCostOffTheBook — and money paid stays paid, which the supplier then owes back.
func TestAVoidTakesTheGoodsBackOutAndTheCostOffTheBook(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	p, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD", PaidNow: "20", PaidFrom: "drawer",
		Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "6", UnitCost: "18"}, {ProductID: s.plates, Quantity: "2", UnitCost: "4"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.VoidPurchase(ctx, p.ID, " "); errs.CodeOf(err) != domain.CodeReasonRequired {
		t.Fatalf("a void without a reason: %v", err)
	}
	voided, err := svc.VoidPurchase(ctx, p.ID, "entered twice")
	if err != nil || voided.Status != domain.StatusVoided || len(s.reversed) != 2 {
		t.Fatalf("VoidPurchase = %+v, %v; reversed %v", voided, err, s.reversed)
	}
	if got := balance(t, svc, sup.ID, "USD"); got != -2_000 {
		t.Fatalf("after the void the book is %d — the 20 paid is owed back", got)
	}
	if _, err := svc.VoidPurchase(ctx, p.ID, "again"); errs.CodeOf(err) != domain.CodePurchaseVoided {
		t.Fatalf("a second void: %v", err)
	}
	if s.acts[len(s.acts)-1] != suppliers.ActVoidPurchase {
		t.Fatalf("the void was not the owner's act: %v", s.acts)
	}
}

// TestAVoidThatStockCannotUndoNamesTheLine: once goods from the delivery have moved, the void is refused whole and says
// which line — nothing half-undone.
func TestAVoidThatStockCannotUndoNamesTheLine(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	p, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD",
		Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "6", UnitCost: "18"}, {ProductID: s.plates, Quantity: "2", UnitCost: "4"}}})
	if err != nil {
		t.Fatal(err)
	}
	s.refuse[p.Lines[1].StockLedgerID] = true
	_, err = svc.VoidPurchase(ctx, p.ID, "wrong supplier")
	typed, _ := errs.AsError(err)
	if errs.CodeOf(err) != domain.CodeLineNotReversible || typed.Params["line"] != "2" || typed.Params["name"] != "صحون" {
		t.Fatalf("the refusal: %v", err)
	}
}

// TestPaymentsOpeningsAndReversalsAreTheOwnersAndMoveTheDrawerOnlyFromIt.
func TestPaymentsOpeningsAndReversalsAreTheOwnersAndMoveTheDrawerOnlyFromIt(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	if _, err := svc.Opening(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "SYP", Amount: "900000"}); err != nil {
		t.Fatal(err)
	}
	pay, err := svc.Pay(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "SYP", Amount: "400000", Source: "drawer", Note: "نقداً"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Pay(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "SYP", Amount: "1", Source: "bank"}); errs.CodeOf(err) != domain.CodeCashSourceInvalid {
		t.Fatalf("money from nowhere the shop keeps: %v", err)
	}
	if drawer, _ := svc.DrawerOn(ctx, "2026-09-24"); drawer["SYP"] != -400_000 {
		t.Fatalf("the drawer: %+v", drawer)
	}
	if _, err = svc.Reverse(ctx, pay.ID, "typed on the wrong supplier"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Reverse(ctx, pay.ID, "again"); errs.CodeOf(err) != domain.CodeAlreadyReversed {
		t.Fatalf("a second reversal: %v", err)
	}
	if drawer, _ := svc.DrawerOn(ctx, "2026-09-24"); drawer["SYP"] != 0 {
		t.Fatalf("a reversed payment left the drawer short: %+v", drawer)
	}
	if got := balance(t, svc, sup.ID, "SYP"); got != 900_000 {
		t.Fatalf("balance %d", got)
	}
	favour, err := svc.Opening(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "30", InShopsFavour: true})
	if err != nil || favour.AmountMinor != -3_000 {
		t.Fatalf("an opening in the shop's favour: %+v, %v", favour, err)
	}
	want := []string{suppliers.ActOpening, suppliers.ActPayment, suppliers.ActReverse, suppliers.ActOpening}
	if len(s.acts) != len(want) {
		t.Fatalf("owner's acts = %v, want %v", s.acts, want)
	}
	for i := range want {
		if s.acts[i] != want[i] {
			t.Fatalf("owner's acts = %v, want %v", s.acts, want)
		}
	}
	payables, _ := svc.Payables(ctx)
	if payables["SYP"] != 900_000 || payables["USD"] != -3_000 {
		t.Fatalf("payables = %+v", payables)
	}
}

func TestAnInactiveSupplierIsNotBoughtFrom(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	if _, err := svc.SetActive(ctx, sup.ID, sup.RowVersion, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD",
		Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "1", UnitCost: "1"}}}); errs.CodeOf(err) != domain.CodeInactive {
		t.Fatalf("a purchase from an inactive supplier: %v", err)
	}
	if found, _ := svc.Search(ctx, "", false); len(found) != 0 {
		t.Fatalf("an inactive supplier was listed: %+v", found)
	}
}

// TestTheDrawerSeesEachReversalAgainstWhatItUndoes: a reversed payment gives the drawer its money back as a payment below
// nought, and a reversed refund takes it out again as a refund below nought — so the drawer's lines read true.
func TestTheDrawerSeesEachReversalAgainstWhatItUndoes(t *testing.T) {
	_, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	pay, err := svc.Pay(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "40", Source: "drawer"})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.Refund(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "15", Source: "drawer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Pay(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "5", Source: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range []domain.Entry{refund, pay} {
		if _, err = svc.Reverse(ctx, e.ID, "wrong supplier"); err != nil {
			t.Fatal(err)
		}
	}
	moves, err := svc.DrawerBetween(ctx, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	want := []suppliers.DrawerMove{
		{BusinessDate: "2026-09-24", Currency: "USD", PaidOutMinor: 4_000},
		{BusinessDate: "2026-09-24", Currency: "USD", RefundInMinor: 1_500},
		{BusinessDate: "2026-09-24", Currency: "USD", RefundInMinor: -1_500},
		{BusinessDate: "2026-09-24", Currency: "USD", PaidOutMinor: -4_000},
	}
	if len(moves) != len(want) {
		t.Fatalf("moves = %+v", moves)
	}
	for i := range want {
		if moves[i] != want[i] {
			t.Fatalf("move %d = %+v, want %+v (all: %+v)", i, moves[i], want[i], moves)
		}
	}
}

// TestWithThePINOnTheBookIsTheOwners: what the goods cost and what the shop owes are not the counter's to read — but the
// drawer still sees the money, and the notification engine still sees the debt.
func TestWithThePINOnTheBookIsTheOwners(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	if _, err := svc.Pay(ctx, suppliers.MoneyInput{SupplierID: sup.ID, Currency: "USD", Amount: "10", Source: "drawer"}); err != nil {
		t.Fatal(err)
	}
	s.pinOn = true
	for name, err := range map[string]error{
		"search":    func() error { _, err := svc.Search(ctx, "", true); return err }(),
		"get":       func() error { _, err := svc.Get(ctx, sup.ID); return err }(),
		"statement": func() error { _, err := svc.StatementOf(ctx, sup.ID, "USD"); return err }(),
		"purchases": func() error { _, err := svc.Purchases(ctx, suppliers.PurchaseQuery{}); return err }(),
		"quote": func() error {
			_, err := svc.QuotePurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD",
				Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "1", UnitCost: "1"}}})
			return err
		}(),
		"create": func() error { _, err := svc.Create(ctx, domain.Draft{Name: "آخر"}); return err }(),
	} {
		if errs.CodeOf(err) != suppliers.CodeOwnerRequired {
			t.Errorf("%s outside owner mode: %v", name, err)
		}
	}
	if moves, err := svc.DrawerBetween(ctx, "2026-09-24", "2026-09-24"); err != nil || len(moves) != 1 {
		t.Fatalf("the drawer lost sight of the payment: %+v, %v", moves, err)
	}
	if owed, err := svc.Payables(ctx); err != nil || owed["USD"] != -1_000 {
		t.Fatalf("the notification engine lost sight of the book: %+v, %v", owed, err)
	}
}

// TestGoodsThatArrivedDamagedAreListedAtWhatTheyWouldHaveCost (0.10.0): the loss report's section on goods the supplier
// did not charge for — voided purchases left out.
func TestGoodsThatArrivedDamagedAreListedAtWhatTheyWouldHaveCost(t *testing.T) {
	s, svc, _ := newBook(t)
	ctx := context.Background()
	sup := aSupplier(t, svc)
	if _, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD",
		Lines: []domain.LineInput{{ProductID: s.cups, Quantity: "10", Damaged: "3", UnitCost: "2.50"}, {ProductID: s.plates, Quantity: "4", UnitCost: "1"}}}); err != nil {
		t.Fatal(err)
	}
	voided, err := svc.RecordPurchase(ctx, domain.Input{SupplierID: sup.ID, Currency: "USD",
		Lines: []domain.LineInput{{ProductID: s.plates, Quantity: "2", Damaged: "1", UnitCost: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.VoidPurchase(ctx, voided.ID, "entered twice"); err != nil {
		t.Fatal(err)
	}
	damaged, err := svc.DamagedOnArrival(ctx, "2026-09-01", "2026-09-30")
	if err != nil || len(damaged) != 1 {
		t.Fatalf("DamagedOnArrival = %+v, %v", damaged, err)
	}
	if d := damaged[0]; d.NameAR != "فنجان قهوة" || d.DamagedMicro != 3_000_000 || d.ValueMinor != 750 || d.SupplierName != "المروى" || d.PurchaseNo != 1 {
		t.Fatalf("the damaged cups %+v — three at $2.50 is $7.50", d)
	}
	if none, _ := svc.DamagedOnArrival(ctx, "2026-10-01", "2026-10-31"); len(none) != 0 {
		t.Fatalf("damage outside the period: %+v", none)
	}
}
