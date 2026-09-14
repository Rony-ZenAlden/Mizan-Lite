package sqlite_test

import (
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/stock/stocktest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestLedgerIsInsertOnly reads the store's source: the only statement it may run against stock_ledger is INSERT.
func TestLedgerIsInsertOnly(t *testing.T) {
	raw, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ToUpper(string(raw))
	if !strings.Contains(src, "INSERT INTO STOCK_LEDGER") {
		t.Fatal("the scan found no ledger insert; it is not reading the store")
	}
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`UPDATE\s+STOCK_LEDGER`), regexp.MustCompile(`DELETE\s+FROM\s+STOCK_LEDGER`),
		regexp.MustCompile(`REPLACE\s+INTO\s+STOCK_LEDGER`), regexp.MustCompile(`ON\s+CONFLICT`),
	} {
		if forbidden.MatchString(src) {
			t.Errorf("the store rewrites the ledger: %s", forbidden)
		}
	}
}

// row is a stock_ledger row as raw columns; nil is SQL NULL.
type row map[string]any

func validRow(t *testing.T, productID id.ID) row {
	t.Helper()
	movementID, _ := id.New()
	return row{
		"id": movementID.String(), "product_id": productID.String(), "seq": 1, "business_date": "2026-09-14",
		"occurred_at": "2026-09-13T22:00:00.000Z", "kind": "receipt", "quantity_micro": 5_000_000,
		"unit_cost_usd_micro": 1_200_000, "on_hand_before_micro": 0, "avg_cost_before_usd_micro": 0,
		"on_hand_after_micro": 5_000_000, "avg_cost_after_usd_micro": 1_200_000,
		"entered_currency": "SYP", "entered_unit_cost_micro": 18_000_000_000, "local_per_usd_nano": 15_000_000_000_000,
		"reason_code": nil, "note": nil, "reverses_id": nil, "pair_id": nil, "created_at": "2026-09-13T22:00:00.000Z",
	}
}

func (r row) with(changes row) row {
	out := row{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func insertRow(db *database.Store, r row) error {
	columns := make([]string, 0, len(r))
	for c := range r {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	args := make([]any, len(columns))
	for i, c := range columns {
		args[i] = r[c]
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	_, err := db.Writer(context.Background()).ExecContext(context.Background(),
		`INSERT INTO stock_ledger (`+strings.Join(columns, ", ")+`) VALUES (`+placeholders+`)`, args...)
	return err
}

// TestACheckRefusesAnImpossibleRow inserts, for each constraint in 0003_stock.sql, a row only that constraint
// forbids — including every NULL case, because a CHECK that evaluates to NULL passes (L2 §4 notes).
func TestACheckRefusesAnImpossibleRow(t *testing.T) {
	db := litetest.OpenMigrated(t)
	productID := insertProduct(t, db)
	base := validRow(t, productID)
	fresh := func(r row) row {
		movementID, _ := id.New()
		return r.with(row{"id": movementID.String()})
	}

	accepted := map[string]row{
		"a pound receipt with its rate":  base,
		"a dollar receipt":               base.with(row{"entered_currency": "USD", "entered_unit_cost_micro": 1_200_000, "local_per_usd_nano": nil}),
		"a receipt with nothing entered": base.with(row{"entered_currency": nil, "entered_unit_cost_micro": nil, "local_per_usd_nano": nil}),
		"a count that confirms the shelf": base.with(row{"kind": "count", "quantity_micro": 0, "on_hand_after_micro": 0, "reason_code": "count",
			"entered_currency": nil, "entered_unit_cost_micro": nil, "local_per_usd_nano": nil}),
		// H3: the row Mizan's schema could not hold.
		"a cost correction of quantity zero": base.with(row{"kind": "cost_correction", "quantity_micro": 0, "on_hand_after_micro": 0,
			"entered_currency": nil, "entered_unit_cost_micro": nil, "local_per_usd_nano": nil}),
		"a gift write-off": base.with(row{"kind": "adjustment", "quantity_micro": -1, "on_hand_after_micro": -1, "reason_code": "gift",
			"entered_currency": nil, "entered_unit_cost_micro": nil, "local_per_usd_nano": nil}),
	}
	seq := 1
	for name, r := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			seq++
			if err := insertRow(db, fresh(r).with(row{"seq": seq})); err != nil {
				t.Fatalf("refused: %v", err)
			}
		})
	}

	noEntered := row{"entered_currency": nil, "entered_unit_cost_micro": nil, "local_per_usd_nano": nil}
	// A receipt that exists, for the rows that must point at one — so a reversal is refused by the constraint under
	// test and not by its foreign key.
	receipt := fresh(base.with(row{"seq": 50}))
	if err := insertRow(db, receipt); err != nil {
		t.Fatal(err)
	}
	reversal := noEntered.with(row{"kind": "receipt_reversal", "quantity_micro": -5_000_000, "on_hand_before_micro": 5_000_000,
		"on_hand_after_micro": 0, "reverses_id": receipt["id"]})
	adjustment := noEntered.with(row{"kind": "adjustment", "quantity_micro": -1_000_000, "on_hand_after_micro": 4_000_000,
		"on_hand_before_micro": 5_000_000, "reason_code": "damaged"})

	// Each case names the constraint that must be the one refusing it.
	refused := []struct {
		name       string
		changes    row
		constraint string
	}{
		{"an unknown kind", row{"kind": "transfer"}, "kind IN"},
		{"a receipt of nothing", row{"quantity_micro": 0, "on_hand_after_micro": 0}, "ck_ledger_quantity_by_kind"},
		{"a reversal that adds", reversal.with(row{"quantity_micro": 5_000_000, "on_hand_after_micro": 10_000_000}), "ck_ledger_quantity_by_kind"},
		{"an adjustment of nothing", adjustment.with(row{"quantity_micro": 0, "on_hand_after_micro": 5_000_000}), "ck_ledger_quantity_by_kind"},
		{"a cost correction that moves stock", noEntered.with(row{"kind": "cost_correction"}), "ck_ledger_quantity_by_kind"},
		{"after that does not follow before", row{"on_hand_after_micro": 4_000_000}, "ck_ledger_after_follows_before"},
		{"a negative cost", row{"unit_cost_usd_micro": -1}, "unit_cost_usd_micro >= 0"},
		{"a negative average before", row{"avg_cost_before_usd_micro": -1}, "avg_cost_before_usd_micro >= 0"},
		{"a negative average after", row{"avg_cost_after_usd_micro": -1}, "avg_cost_after_usd_micro >= 0"},
		{"a count with no reason", adjustment.with(row{"kind": "count", "reason_code": nil}), "ck_ledger_reason_where_needed"},
		{"an adjustment with no reason (NULL)", adjustment.with(row{"reason_code": nil}), "ck_ledger_reason_where_needed"},
		{"a receipt with a reason", row{"reason_code": "damaged"}, "ck_ledger_reason_where_needed"},
		{"an unknown reason", adjustment.with(row{"reason_code": "stolen"}), "ck_ledger_reason_known"},
		{"other without a note (NULL)", adjustment.with(row{"reason_code": "other", "note": nil}), "ck_ledger_other_needs_note"},
		{"a reversal pointing at nothing (NULL)", reversal.with(row{"reverses_id": nil}), "ck_ledger_reversal_links"},
		{"a receipt pointing at another", row{"reverses_id": receipt["id"]}, "ck_ledger_reversal_links"},
		{"a reversal of a row that does not exist", reversal.with(row{"reverses_id": base["id"]}), "FOREIGN KEY"},
		{"a package out with no pair (NULL)", noEntered.with(row{"kind": "package_out", "quantity_micro": -5_000_000, "on_hand_after_micro": -5_000_000}), "ck_ledger_package_pairs"},
		{"a receipt with a pair", row{"pair_id": receipt["id"]}, "ck_ledger_package_pairs"},
		{"a pound receipt with no rate (NULL)", row{"local_per_usd_nano": nil}, "ck_ledger_entered_cost"},
		{"a pound receipt at a zero rate", row{"local_per_usd_nano": 0}, "ck_ledger_entered_cost"},
		{"a cost with no currency (NULL)", row{"entered_currency": nil}, "ck_ledger_entered_cost"},
		{"a dollar receipt with a rate", row{"entered_currency": "USD"}, "ck_ledger_entered_cost"},
		{"an entered currency with no cost (NULL)", row{"entered_unit_cost_micro": nil}, "ck_ledger_entered_cost"},
		{"a negative entered cost", row{"entered_unit_cost_micro": -1}, "ck_ledger_entered_cost"},
		{"an entered cost on an adjustment", adjustment.with(row{"entered_currency": "USD", "entered_unit_cost_micro": 1}), "ck_ledger_entered_cost"},
		{"a currency that does not exist", row{"entered_currency": "EUR"}, "FOREIGN KEY"},
		{"place zero", row{"seq": 0}, "seq >= 1"},
		{"a product that does not exist", row{"product_id": base["id"]}, "FOREIGN KEY"},
	}
	for _, tc := range refused {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			seq++
			err := insertRow(db, fresh(base.with(row{"seq": seq})).with(tc.changes))
			if err == nil {
				t.Fatalf("an impossible row was stored: %v", tc.changes)
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("refused, but not by %s: %v", tc.constraint, err)
			}
		})
	}

	t.Run("refuses two movements in one place, and a receipt reversed twice", func(t *testing.T) {
		first := fresh(base.with(row{"seq": 100}))
		if err := insertRow(db, first); err != nil {
			t.Fatal(err)
		}
		if err := insertRow(db, fresh(base.with(row{"seq": 100}))); err == nil {
			t.Error("two movements took place 100")
		}
		reversal := fresh(base).with(noEntered).with(row{"seq": 101, "kind": "receipt_reversal", "quantity_micro": -5_000_000,
			"on_hand_before_micro": 5_000_000, "on_hand_after_micro": 0, "reverses_id": first["id"]})
		if err := insertRow(db, reversal); err != nil {
			t.Fatalf("a valid reversal was refused: %v", err)
		}
		if err := insertRow(db, fresh(reversal).with(row{"seq": 102})); err == nil {
			t.Error("a receipt was reversed twice")
		}
	})

	t.Run("a package cannot open into itself, nor into nothing", func(t *testing.T) {
		other := insertProduct(t, db)
		insertPackage := func(pkg, content id.ID, qty int64) error {
			_, err := db.Writer(context.Background()).ExecContext(context.Background(),
				`INSERT INTO product_packages (package_product_id, content_product_id, content_quantity_micro, updated_at) VALUES (?, ?, ?, ?)`,
				pkg.String(), content.String(), qty, "2026-09-13T22:00:00.000Z")
			return err
		}
		if err := insertPackage(productID, productID, 16_000_000); err == nil {
			t.Error("a package opens into itself")
		}
		if err := insertPackage(productID, other, 0); err == nil {
			t.Error("a package opens into nothing")
		}
		if err := insertPackage(productID, other, 16_000_000); err != nil {
			t.Errorf("a valid package link was refused: %v", err)
		}
	})
}

// failingLevels fails SaveLevel after the movement was appended.
type failingLevels struct{ *sqlite.Store }

var errAfterAppend = errors.New("the level write failed")

func (failingLevels) SaveLevel(context.Context, domain.Level) (domain.Level, error) {
	return domain.Level{}, errAfterAppend
}

// TestTheLevelAndItsMovementCommitTogether runs an act against the real database with the level write failing after
// the ledger insert: neither may remain.
func TestTheLevelAndItsMovementCommitTogether(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	productID := insertProduct(t, db)
	catalogue := &productsInDB{decimals: 3}
	store := sqlite.NewStore(db, clock.System())
	svc := stock.NewService(db, failingLevels{store}, catalogue, &stocktest.Gate{}, clock.System(), time.UTC)

	_, err := svc.Opening(ctx, stock.ReceiveInput{ProductID: productID, Quantity: "5",
		Cost: domain.CostInput{Amount: "10", Currency: "USD"}})
	if !errors.Is(err, errAfterAppend) {
		t.Fatalf("err = %v", err)
	}
	var movements int
	if err := db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_ledger`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 0 {
		t.Fatalf("%d movements survived a failed act", movements)
	}

	// And the same act, with nothing failing, writes both.
	ok := stock.NewService(db, store, catalogue, &stocktest.Gate{}, clock.System(), time.UTC)
	if _, err := ok.Opening(ctx, stock.ReceiveInput{ProductID: productID, Quantity: "5",
		Cost: domain.CostInput{Amount: "10", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	level, _ := store.Level(ctx, productID)
	if level.OnHandMicro != 5_000_000 || level.AvgCostMicro != 2_000_000 {
		t.Fatalf("level = %+v", level)
	}
}

// productsInDB answers the catalogue port for any product id, active, in a unit of the given decimals.
type productsInDB struct{ decimals int }

func (p *productsInDB) Product(_ context.Context, productID id.ID) (domain.Product, error) {
	return domain.Product{ID: productID, UnitDecimals: p.decimals, Active: true}, nil
}

func (p *productsInDB) Products(context.Context) ([]domain.Product, error) { return nil, nil }

func (p *productsInDB) Package(context.Context, id.ID) (domain.Package, bool, error) {
	return domain.Package{}, false, nil
}

func (p *productsInDB) Currencies(context.Context) ([]domain.Currency, error) {
	return stocktest.Currencies, nil
}

// failSecondLevel fails the second SaveLevel: in an opening, the content's level, after both movements and the
// package's level were written.
type failSecondLevel struct {
	*sqlite.Store
	saves int
}

func (f *failSecondLevel) SaveLevel(ctx context.Context, l domain.Level) (domain.Level, error) {
	f.saves++
	if f.saves == 2 {
		return domain.Level{}, errAfterAppend
	}
	return f.Store.SaveLevel(ctx, l)
}

// linkedCatalogue answers for a tin that opens into 16 litres of loose oil.
type linkedCatalogue struct {
	productsInDB
	tin, loose id.ID
}

func (c *linkedCatalogue) Product(_ context.Context, productID id.ID) (domain.Product, error) {
	decimals := 3
	if productID == c.tin {
		decimals = 0
	}
	return domain.Product{ID: productID, UnitDecimals: decimals, Active: true}, nil
}

func (c *linkedCatalogue) Package(_ context.Context, productID id.ID) (domain.Package, bool, error) {
	if productID != c.tin {
		return domain.Package{}, false, nil
	}
	return domain.Package{ContentProductID: c.loose, ContentQuantityMicro: 16_000_000}, true, nil
}

// TestOpeningAPackageWritesBothRowsOrNeither: with the content's level failing after the package's rows were written,
// the real transaction leaves the ledger and both levels exactly as they were.
func TestOpeningAPackageWritesBothRowsOrNeither(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	catalogue := &linkedCatalogue{tin: insertProduct(t, db), loose: insertProduct(t, db)}
	store := sqlite.NewStore(db, clock.System())
	ok := stock.NewService(db, store, catalogue, &stocktest.Gate{}, clock.System(), time.UTC)
	if _, err := ok.Opening(ctx, stock.ReceiveInput{ProductID: catalogue.tin, Quantity: "3", Cost: domain.CostInput{Amount: "285", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	tinBefore, _ := store.Level(ctx, catalogue.tin)

	failing := stock.NewService(db, &failSecondLevel{Store: store}, catalogue, &stocktest.Gate{}, clock.System(), time.UTC)
	if _, err := failing.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: catalogue.tin, Packages: "1"}); !errors.Is(err, errAfterAppend) {
		t.Fatalf("err = %v", err)
	}
	var movements int
	if err := db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_ledger`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 1 {
		t.Fatalf("%d movements after a failed opening; only the opening stock may remain", movements)
	}
	if tinAfter, _ := store.Level(ctx, catalogue.tin); tinAfter != tinBefore {
		t.Fatalf("the tin's level moved: %+v → %+v", tinBefore, tinAfter)
	}
	if loose, _ := store.Level(ctx, catalogue.loose); loose.Moved() {
		t.Fatalf("the loose oil has a level: %+v", loose)
	}

	if _, err := ok.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: catalogue.tin, Packages: "1"}); err != nil {
		t.Fatal(err)
	}
	if findings, err := ok.VerifyUnguarded(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}
