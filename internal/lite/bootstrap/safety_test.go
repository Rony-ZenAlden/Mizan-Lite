package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/backups"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

type failingVouchers struct{}

func (failingVouchers) AssignVoucher(context.Context, id.ID) (int64, error) {
	return 0, errs.Internal("lite.test.voucher", "numbering failed")
}

// TestAVoucherFailureRollsThePaymentBack: the voucher number is written in the payment's transaction, so a payment is never
// recorded without its number (Q-L7.3) — and numbers run in order.
func TestAVoucherFailureRollsThePaymentBack(t *testing.T) {
	s := openShop(t, 71)
	c := s.people[0]
	s.owner()
	if _, err := s.app.Customers.Opening(s.ctx, customers.AmountInput{CustomerID: c.ID, Currency: "USD", Amount: "20", Note: "دفتر"}); err != nil {
		t.Fatal(err)
	}
	pay := func() (customersdomain.Entry, error) {
		in := customersdomain.CashInput{Currency: "USD", Amount: "2"}
		q, err := s.app.Customers.QuotePayment(s.ctx, c.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		return s.app.Customers.RecordPayment(s.ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token})
	}
	for want := int64(1); want <= 2; want++ {
		e, err := pay()
		if err != nil {
			t.Fatal(err)
		}
		if n, found, _ := s.app.Printing.Voucher(s.ctx, e.ID); !found || n != want {
			t.Fatalf("voucher %d, want %d", n, want)
		}
	}
	s.app.Customers.SetVouchers(failingVouchers{})
	if _, err := pay(); errs.CodeOf(err) != "lite.test.voucher" {
		t.Fatalf("a payment whose number failed: %v", err)
	}
	if b, _ := s.app.Customers.Balance(s.ctx, c.ID, "USD"); b != 1_600 {
		t.Fatalf("the unnumbered payment was kept: balance %d", b)
	}
}

// TestTheScheduledBackupIsCopiedOutside: the daily job's backup lands, verified, in the owner's folder (D-L7.14).
func TestTheScheduledBackupIsCopiedOutside(t *testing.T) {
	ctx := context.Background()
	app := start(t, dataDir(t), clock.System())
	usb := t.TempDir()
	if _, err := app.Settings.Update(ctx, settingsdomain.Update{Printing: settingsdomain.PrintingUpdate{BackupFolder: &usb}}); err != nil {
		t.Fatal(err)
	}
	if err := app.Scheduler.RunNow(ctx, bootstrap.KeyScheduledBackup); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(usb, "Mizan Lite backups", "scheduled-*.db"))
	if len(copies) != 1 {
		t.Fatalf("outside copies %v", copies)
	}
	st, err := app.Safety.Status(ctx)
	if err != nil || st.LastOutside == nil || st.OutsideStale || st.OutsideFailed != "" {
		t.Fatalf("status %+v %v", st, err)
	}
}

// TestARestoreRoundTripsThroughARestart: back up, trade, see the loss counted, restore with the PIN, start again — the
// backup's data, the restore in the owner's history, and the snapshot of what was replaced ready to undo it.
func TestARestoreRoundTripsThroughARestart(t *testing.T) {
	ctx := context.Background()
	p := dataDir(t)
	clk := clock.NewFixed(time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC))
	open := func() *bootstrap.App {
		t.Helper()
		app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher(), Clock: clk, Location: damascus})
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	app := open()
	if _, err := app.Setup.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "15000"}); err != nil {
		t.Fatal(err)
	}
	oil, err := app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "زيت", UnitCode: "l", PriceCurrency: "USD", Price: "3"})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	kept, err := app.Safety.TakeNow(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Trade after the backup: a product, stock, a sale.
	clk.Advance(time.Hour)
	if _, err = app.Catalog.Create(ctx, catalogdomain.Draft{NameAR: "سكر", UnitCode: "kg", PriceCurrency: "SYP", Price: "14000"}); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Stock.Opening(ctx, stock.ReceiveInput{ProductID: oil.ID, Quantity: "5", Cost: stockdomain.CostInput{Amount: "10", Currency: "USD"}}); err != nil {
		t.Fatal(err)
	}
	cart := salesdomain.CartInput{Lines: []salesdomain.LineInput{{ProductID: oil.ID, Quantity: "1"}}}
	q, err := app.Sales.Quote(ctx, cart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cart, Token: q.Token}); err != nil {
		t.Fatal(err)
	}

	preview, err := app.Safety.LossPreview(ctx, kept.Name)
	if err != nil || preview.Loss.Sales != 1 || preview.Loss.StockMovements != 2 || !preview.Backup.TakenAt.Equal(kept.TakenAt) {
		t.Fatalf("preview %+v %v", preview, err)
	}
	clk.Advance(time.Minute)
	intent, err := app.Safety.Restore(ctx, kept.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	clk.Advance(time.Minute)
	app = open()
	t.Cleanup(func() { _ = app.Shutdown(ctx) })
	if app.Restored == nil || app.Restored.From != kept.Name || app.Restored.SafetyBackup != intent.SafetyBackup {
		t.Fatalf("restored %+v", app.Restored)
	}
	products, _ := app.Catalog.Search(ctx, "", true)
	if len(products) != 1 || products[0].NameAR != "زيت" {
		t.Fatalf("products after the restore: %+v", products)
	}
	if day, _ := app.Sales.Day(ctx, "2026-09-15"); len(day.Sales) != 0 {
		t.Fatal("the sale after the backup survived the restore")
	}
	events, _ := app.Owner.Events(ctx, 50)
	recorded := false
	for _, e := range events {
		if e.Kind == ownerdomain.EventGuardedAct && e.Action == backups.ActRestore && e.After == kept.Name {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("the restore is not in the restored database's owner history")
	}
	list, _ := app.Safety.List(ctx)
	if len(list) == 0 || list[0].Reason != backup.BeforeRestore || list[0].Name != intent.SafetyBackup {
		t.Fatalf("the safety snapshot is not first: %+v", list)
	}
	if _, err = os.Stat(p.DBFile + ".replaced"); err != nil {
		t.Fatal("the replaced database was not kept aside")
	}

	// Undo: restoring the safety snapshot brings the trade back.
	if _, err = app.Owner.Elevate(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Safety.Restore(ctx, intent.SafetyBackup); err != nil {
		t.Fatal(err)
	}
	if err = app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	app = open()
	t.Cleanup(func() { _ = app.Shutdown(ctx) })
	if products, _ = app.Catalog.Search(ctx, "", true); len(products) != 2 {
		t.Fatalf("undo: %d products", len(products))
	}
}
