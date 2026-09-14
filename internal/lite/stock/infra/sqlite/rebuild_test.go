package sqlite_test

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/stock/stocktest"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// upTo migrates db through the migrations numbered at most last.
func upTo(t *testing.T, db *database.Store, path string, last int) {
	t.Helper()
	subset := fstest.MapFS{}
	err := fs.WalkDir(migrations.SQLite(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		var version int
		if _, scanErr := fmt.Sscanf(name, "%04d_", &version); scanErr != nil || version > last {
			return nil //nolint:nilerr // a file that is not a numbered migration, or one beyond the target
		}
		body, err := fs.ReadFile(migrations.SQLite(), name)
		subset[name] = &fstest.MapFile{Data: body}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrate.New(db, migrate.Options{FS: subset, DBPath: path, SkipBackup: true, Logger: litetest.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("migrating through %04d: %v", last, err)
	}
}

// oldLedgerColumns are the ledger's columns before 0005: what an L3 installation holds.
const oldLedgerColumns = `id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
	on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro, entered_currency,
	entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id, created_at`

func dump(t *testing.T, db *database.Store, q string) string {
	t.Helper()
	rows, err := db.Reader(context.Background()).QueryContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var b strings.Builder
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, values...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestTheLedgerRebuildKeepsEveryRow runs 0005 through the real runner on an L3 database holding every L2 movement
// kind — a receipt reversal (the ledger's self-reference) and a package pair included — and compares every row and
// every level (L4 §6.2, A-L4.1). L2's plan as first written failed on exactly this data.
func TestTheLedgerRebuildKeepsEveryRow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "l3.db")
	db, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	upTo(t, db, path, 4)

	// Every L2 act, through the L2 domain, written with the L3 columns.
	tin, loose, ghee := insertProduct(t, db), insertProduct(t, db), insertProduct(t, db)
	at := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	st := func() domain.Stamp {
		at = at.Add(time.Second)
		movementID, _ := id.New()
		return domain.Stamp{ID: movementID, BusinessDate: "2026-09-14", OccurredAt: at}
	}
	dollars := domain.Cost{UnitCostMicro: 95_000_000, Entered: domain.Entered{Currency: "USD", UnitCostMicro: 95_000_000}}
	pounds := domain.Cost{UnitCostMicro: 1_200_000, Entered: domain.Entered{Currency: "SYP", UnitCostMicro: 18_000_000_000, LocalPerUSDNano: 15_000_000_000_000}}
	levels := map[id.ID]domain.Level{}
	var movements []domain.Movement
	apply := func(m domain.Movement, after domain.Level, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		movements = append(movements, m)
		levels[after.ProductID] = after
	}
	apply(domain.Opening(domain.Level{ProductID: tin}, st(), 4_000_000, dollars, "على الرف"))
	apply(domain.Opening(domain.Level{ProductID: ghee}, st(), 10_000_000, pounds, ""))
	receipt, received, err := domain.Receive(levels[ghee], st(), 5_000_000, pounds, "من أبو خليل")
	apply(receipt, received, err)
	apply(domain.ReverseReceipt(levels[ghee], receipt, st(), "خطأ"))
	apply(domain.Count(levels[ghee], st(), 9_000_000, "جرد"))
	apply(domain.Adjust(levels[ghee], st(), -1_000_000, domain.ReasonOther, "كسر"))
	apply(domain.CorrectCost(levels[ghee], st(), 1_300_000, "تصحيح"))
	pairID, _ := id.New()
	opened, err := domain.OpenPackage(levels[tin], domain.Level{ProductID: loose}, domain.Package{ContentProductID: loose, ContentQuantityMicro: 16_000_000}, 1_000_000, st(), st(), pairID)
	apply(opened.Out, opened.PackageLevel, err)
	apply(opened.In, opened.ContentLv, nil)

	w := db.Writer(ctx)
	for _, m := range movements {
		var currency, enteredCost, rate, reason, note, reverses, pair any
		if m.Entered.Currency != "" {
			currency, enteredCost = m.Entered.Currency, m.Entered.UnitCostMicro
			if m.Entered.LocalPerUSDNano != 0 {
				rate = m.Entered.LocalPerUSDNano
			}
		}
		if m.Reason != "" {
			reason = string(m.Reason)
		}
		if m.Note != "" {
			note = m.Note
		}
		if m.ReversesID != "" {
			reverses = m.ReversesID.String()
		}
		if m.PairID != "" {
			pair = m.PairID.String()
		}
		if _, err := w.ExecContext(ctx, `INSERT INTO stock_ledger (`+oldLedgerColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID.String(), m.ProductID.String(), m.Seq, m.BusinessDate, clock.Format(m.OccurredAt), string(m.Kind), m.QuantityMicro,
			m.UnitCostMicro, m.OnHandBeforeMicro, m.AvgCostBeforeMicro, m.OnHandAfterMicro, m.AvgCostAfterMicro,
			currency, enteredCost, rate, reason, note, reverses, pair, clock.Format(m.OccurredAt)); err != nil {
			t.Fatalf("writing %s at L3: %v", m.Kind, err)
		}
	}
	for _, l := range levels {
		if _, err := w.ExecContext(ctx, `INSERT INTO stock_levels (product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at) VALUES (?,?,?,?,1,?)`,
			l.ProductID.String(), l.OnHandMicro, l.AvgCostMicro, l.LastMovementID.String(), clock.Format(at)); err != nil {
			t.Fatal(err)
		}
	}
	ledgerBefore := dump(t, db, `SELECT `+oldLedgerColumns+` FROM stock_ledger ORDER BY id`)
	levelsBefore := dump(t, db, `SELECT * FROM stock_levels ORDER BY product_id`)
	if strings.Count(ledgerBefore, "\n") != 9 || !strings.Contains(ledgerBefore, "receipt_reversal") || !strings.Contains(ledgerBefore, "content_in") {
		t.Fatalf("the L3 ledger is not the one this test means to rebuild:\n%s", ledgerBefore)
	}

	upTo(t, db, path, 5)

	if got := dump(t, db, `SELECT `+oldLedgerColumns+` FROM stock_ledger ORDER BY id`); got != ledgerBefore {
		t.Fatalf("the ledger changed in the rebuild:\nbefore\n%s\nafter\n%s", ledgerBefore, got)
	}
	if got := dump(t, db, `SELECT * FROM stock_levels ORDER BY product_id`); got != levelsBefore {
		t.Fatalf("the levels changed:\nbefore\n%s\nafter\n%s", levelsBefore, got)
	}
	if n := dump(t, db, `SELECT COUNT(*) FROM stock_ledger WHERE sale_id IS NOT NULL OR sale_line_id IS NOT NULL`); n != "0\n" {
		t.Fatalf("old rows gained sale links: %s", n)
	}
	if fk := dump(t, db, `PRAGMA foreign_key_check`); fk != "" {
		t.Fatalf("foreign key violations after the rebuild:\n%s", fk)
	}
	if leftovers := dump(t, db, `SELECT name FROM sqlite_master WHERE name LIKE '%_copy' OR name LIKE '%_new'`); leftovers != "" {
		t.Fatalf("the rebuild left tables behind: %s", leftovers)
	}
	for _, table := range []string{"stock_levels", "stock_ledger"} {
		if fks := dump(t, db, `SELECT "table", "to" FROM pragma_foreign_key_list('`+table+`') ORDER BY "table", "to"`); !strings.Contains(fks, "stock_ledger id") && table == "stock_levels" {
			t.Fatalf("stock_levels lost its reference to the ledger: %s", fks)
		} else if table == "stock_ledger" && !strings.Contains(fks, "stock_ledger id") {
			t.Fatalf("the ledger's self-reference was not rewritten to its own name: %s", fks)
		}
	}

	// The rebuilt ledger is the ledger the application uses: the verifier walks it clean, and a new act appends to it.
	store := sqlite.NewStore(db, clock.System())
	svc := stock.NewService(db, store, &productsInDB{decimals: 3}, &stocktest.Gate{}, clock.System(), time.UTC)
	if findings, err := svc.VerifyUnguarded(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("findings after the rebuild = %+v, %v", findings, err)
	}
	if _, err := svc.Receive(ctx, stock.ReceiveInput{ProductID: ghee, Quantity: "1", Cost: domain.CostInput{Amount: "2", Currency: "USD"}}); err != nil {
		t.Fatalf("an act after the rebuild: %v", err)
	}
}
