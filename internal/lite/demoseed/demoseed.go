// Package demoseed fills an empty Mizan Lite installation with a pantry shop's worth of believable data.
//
// # Why it drives the services and never writes rows
//
// The reasoning of Mizan's own demo seeder: rows written by hand satisfy none of the rules the application
// enforces, and later phases check data against itself. So this goes through the same services the bindings
// call — first run, the catalogue, and the owner's guard — and a demo database is a database a shop could
// have produced. It is also an end-to-end run of everything built so far, in the order a shop does it.
//
// It grows with every phase (DESIGN §9.4). L1: first run and the catalogue. L2: a package link, opening stock
// in both currencies, deliveries entered as invoice totals, a count and a write-off through the owner's PIN, one
// tin opened — and then the stock verifier, which must find nothing (L2 §10). L3: an opening exchange rate at first run,
// a later rate and a same-day correction, both through the owner's guard (L3 §11.2). The seeder registers no rate
// provider: it never reaches the internet. L4: a day at the till — scanned barcodes and quick products, weighed goods,
// a pounds total paid in dollars, a dollar sale, a sale beyond the shelf, two discounts and a void through the owner's PIN —
// and then the stock verifier and the sales verifier, both of which must find nothing (L4 §11). L5: eight customers from
// the paper book, credit sales in both currencies with part paid now, repayments both ways — pay all of a dollar debt in
// pounds — a voided repaid credit sale refunded, and a write-off; then the debt book's verifier too (L5 §12). L6: with Days,
// that many days of history before today under a clock the seeder steps — drifting rates, weekly deliveries, sales and voids,
// losses, expenses and closing counts (history.go) — and then the reports' own reconciliations, which must hold (L6 §11).
package demoseed

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/printing"
	reportsdomain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// CodeStockInconsistent fails a run whose stock the verifier finds anything wrong with: a demo that cannot pass the
// application's own check is a demo of a defect.
const CodeStockInconsistent = "lite.demoseed.stock_inconsistent"

// CodeSalesInconsistent fails a run whose sales the sales verifier finds anything wrong with.
const CodeSalesInconsistent = "lite.demoseed.sales_inconsistent"

// CodeDebtsInconsistent fails a run whose debt book the verifier finds anything wrong with.
const CodeDebtsInconsistent = "lite.demoseed.debts_inconsistent"

// CodeReportsInconsistent fails a run whose reports do not reconcile: a month that is not the sum of its days, stock
// movements that do not add up to the closing value, or a count recorded against another figure than the drawer's.
const CodeReportsInconsistent = "lite.demoseed.reports_inconsistent"

// CodeAlreadySetUp refuses an installation that has completed first run. Re-seeding means deleting the data
// directory — a decision for whoever runs this, not something a seeder should do on their behalf.
const CodeAlreadySetUp = "lite.demoseed.already_set_up"

//go:embed data/catalogue.json data/stock.json data/rates.json data/sales.json data/customers.json
var data embed.FS

type rateLine struct {
	Rate string `json:"rate"`
	Note string `json:"note"`
}

type ratesData struct {
	FirstRun   string   `json:"firstRun"`
	Later      rateLine `json:"later"`
	Correction rateLine `json:"correction"`
}

type costLine struct {
	Product  string `json:"product"`
	Quantity string `json:"quantity"`
	Cost     string `json:"cost"`
	Currency string `json:"currency"`
	Rate     string `json:"rate"`
	Note     string `json:"note"`
}

type stockData struct {
	Package struct {
		Package         string `json:"package"`
		Content         string `json:"content"`
		ContentQuantity string `json:"contentQuantity"`
	} `json:"package"`
	Openings []costLine `json:"openings"`
	Receipts []costLine `json:"receipts"`
	Count    struct {
		Product string `json:"product"`
		Counted string `json:"counted"`
		Note    string `json:"note"`
	} `json:"count"`
	WriteOff struct {
		Product  string `json:"product"`
		Quantity string `json:"quantity"`
		Reason   string `json:"reason"`
		Note     string `json:"note"`
	} `json:"writeOff"`
	OpenPackages string `json:"openPackages"`
}

type saleLine struct {
	Product         string `json:"product"`
	Barcode         string `json:"barcode"`
	Quantity        string `json:"quantity"`
	DiscountPercent string `json:"discountPercent"`
}

type salesData struct {
	Sales []struct {
		Lines          []saleLine `json:"lines"`
		Settlement     string     `json:"settlement"`
		SaleDiscount   string     `json:"saleDiscount"`
		TenderCurrency string     `json:"tenderCurrency"`
		Tendered       string     `json:"tendered"`
		// Owner is true for a sale with a discount: the owner enters the PIN at the till for it, and leaves.
		Owner bool `json:"owner"`
	} `json:"sales"`
	Void struct {
		Sale   int    `json:"sale"`
		Reason string `json:"reason"`
	} `json:"void"`
}

type customersData struct {
	Customers []struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Note     string `json:"note"`
		Openings []struct {
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
			Note     string `json:"note"`
		} `json:"openings"`
	} `json:"customers"`
	CreditSales []struct {
		Customer       string     `json:"customer"`
		Settlement     string     `json:"settlement"`
		Lines          []saleLine `json:"lines"`
		TenderCurrency string     `json:"tenderCurrency"`
		Tendered       string     `json:"tendered"`
	} `json:"creditSales"`
	Payments []struct {
		Customer       string `json:"customer"`
		Currency       string `json:"currency"`
		TenderCurrency string `json:"tenderCurrency"`
		Amount         string `json:"amount"`
		All            bool   `json:"all"`
	} `json:"payments"`
	WriteOff struct {
		Customer string `json:"customer"`
		Currency string `json:"currency"`
		Reason   string `json:"reason"`
	} `json:"writeOff"`
	VoidCredit struct {
		Customer string `json:"customer"`
		Reason   string `json:"reason"`
	} `json:"voidCredit"`
	Refund struct {
		Customer string `json:"customer"`
		Currency string `json:"currency"`
		Reason   string `json:"reason"`
	} `json:"refund"`
}

type catalogue struct {
	ShopName    string `json:"shopName"`
	PriceChange struct {
		Product  string `json:"product"`
		Currency string `json:"currency"`
		Price    string `json:"price"`
	} `json:"priceChange"`
	Products []struct {
		NameAR    string `json:"nameAr"`
		NameEN    string `json:"nameEn"`
		Unit      string `json:"unit"`
		Currency  string `json:"currency"`
		Price     string `json:"price"`
		Barcode   string `json:"barcode"`
		QuickSlot int    `json:"quickSlot"`
	} `json:"products"`
}

// Options configures a run.
type Options struct {
	// PIN is the owner PIN the seeded installation gets.
	PIN string
	// Locale is the interface language; Arabic by default.
	Locale string
	// Days is how many days of history come before today (L6 §11); 0 seeds today only. History needs Clock — the clock the
	// graph was started with, which the seeder steps — and Now, today's instant.
	Days  int
	Clock *clock.Fixed
	Now   time.Time
}

// Result is what a run produced, for the command to print.
type Result struct {
	ShopName     string
	RecoveryCode string
	Products     int
	QuickSlots   int
	Openings     int
	Receipts     int
	// StockValue is the valuation's total in USD, as the owner's screen shows it.
	StockValue string
	// Rate is the exchange rate in force after seeding.
	Rate string
	// Sales and Voids are the day at the till; Takings is what was charged less what voids took back, per currency, as Go
	// formats it.
	Sales   int
	Voids   int
	Takings map[string]string
	// Customers, DebtOpenings, CreditSales and Payments are the debt book's part (L5).
	Customers    int
	DebtOpenings int
	CreditSales  int
	Payments     int
	// HistoryDays and the rest are L6's month before today.
	HistoryDays     int
	HistorySales    int
	HistoryVoids    int
	HistoryReceipts int
	Expenses        int
	Counts          int
	// HistoryProfitUSD and HistoryProfitLocal are the history's net profit in each reading, as Go formats it — dollars at
	// cost, pounds at each sale's rate — which diverge as the pound moves (DESIGN §9.4).
	HistoryProfitUSD   string
	HistoryProfitLocal string
	// PrintJobs, Vouchers and Backups are L7's part (L8 "the seeder complete"): receipts and a voucher printed (and one that
	// failed), the payments' voucher numbers, and a backup copied to an outside folder inside the data directory.
	PrintJobs int
	Vouchers  int
	Backups   int
}

// NewClock is a clock fixed on now, for the graph the seeder steps through its history: the command wires Lite and imports
// nothing else (lite-cmd-entry).
func NewClock() *clock.Fixed { return clock.NewFixed(clock.System().Now()) }

// Run seeds app. It refuses an installation that has completed first run.
func Run(ctx context.Context, app *bootstrap.App, opts Options) (Result, error) {
	if opts.Locale == "" {
		opts.Locale = "ar"
	}
	done, err := app.Setup.Complete(ctx)
	if err != nil {
		return Result{}, err
	}
	if done {
		return Result{}, errs.Conflict(CodeAlreadySetUp, "this installation is already set up")
	}

	var cat catalogue
	if err = readData("data/catalogue.json", &cat); err != nil {
		return Result{}, err
	}
	var stk stockData
	if err = readData("data/stock.json", &stk); err != nil {
		return Result{}, err
	}
	var rates ratesData
	if err = readData("data/rates.json", &rates); err != nil {
		return Result{}, err
	}
	var day salesData
	if err = readData("data/sales.json", &day); err != nil {
		return Result{}, err
	}
	var book customersData
	if err = readData("data/customers.json", &book); err != nil {
		return Result{}, err
	}

	if opts.Days > 0 && opts.Clock == nil {
		return Result{}, errs.Internal(CodeReportsInconsistent, "history needs the clock the graph was started with")
	}
	res := Result{ShopName: cat.ShopName}
	start := opts.Now
	if opts.Days > 0 {
		now := opts.Now.In(time.Local)
		start = time.Date(now.Year(), now.Month(), now.Day()-opts.Days, 0, 0, 0, 0, time.Local)
		opts.Clock.Current = start.Add(7 * time.Hour)
	}
	if res.RecoveryCode, err = app.Setup.Run(ctx, setup.Input{ShopName: cat.ShopName, Locale: opts.Locale, PIN: opts.PIN, Rate: rates.FirstRun}); err != nil {
		return Result{}, err
	}

	byName := map[string]domain.Product{}
	for _, item := range cat.Products {
		p, err := app.Catalog.Create(ctx, domain.Draft{
			NameAR: item.NameAR, NameEN: item.NameEN, Barcode: item.Barcode,
			UnitCode: item.Unit, PriceCurrency: item.Currency, Price: item.Price,
		})
		if err != nil {
			return Result{}, errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "seeding "+item.NameEN)
		}
		if item.QuickSlot > 0 {
			if p, err = app.Catalog.SetQuickSlot(ctx, p.ID, item.QuickSlot); err != nil {
				return Result{}, err
			}
			res.QuickSlots++
		}
		byName[item.NameAR] = p
		res.Products++
	}

	if opts.Days > 0 {
		if err := seedHistory(ctx, app, opts, cat, stk, byName, start, &res); err != nil {
			return Result{}, err
		}
	}

	// One price change through the owner's guard, entered with the PIN exactly as a person would — so the
	// seeder meets the same rule every caller meets, and the owner's history is not empty.
	if _, err := app.Owner.Elevate(ctx, opts.PIN); err != nil {
		return Result{}, err
	}
	target := byName[cat.PriceChange.Product]
	if _, err := app.Catalog.SetPrice(ctx, catalog.SetPriceInput{
		ID: target.ID, RowVersion: target.RowVersion, Currency: cat.PriceChange.Currency, Price: cat.PriceChange.Price,
	}); err != nil {
		return Result{}, err
	}
	// The count and the write-off lower stock, so they are the owner's too (Q-L2.3) — seeded in the same owner mode. With
	// history the openings were entered on its first day.
	if opts.Days == 0 {
		if err := seedOpenings(ctx, app, stk, byName, &res); err != nil {
			return Result{}, err
		}
	}
	if err := seedStock(ctx, app, stk, rates, byName, &res); err != nil {
		return Result{}, err
	}
	if _, err := app.Owner.EndElevation(ctx); err != nil {
		return Result{}, err
	}
	if err := seedSales(ctx, app, day, opts.PIN, byName, &res); err != nil {
		return Result{}, err
	}
	if err := seedCustomers(ctx, app, book, opts.PIN, byName, &res); err != nil {
		return Result{}, err
	}
	if err := seedPrinting(ctx, app, opts.PIN, &res); err != nil {
		return Result{}, err
	}
	if err := check(ctx, app, opts.PIN, &res); err != nil {
		return Result{}, err
	}
	if opts.Days > 0 {
		if err := checkReports(ctx, app, opts.PIN, start.Format("2006-01-02"), &res); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// seedSales rings up a day at the till the way a cashier does: each cart quoted, then paid with that quote's token. A
// discount is the owner's, so the owner comes to the till for it and leaves (Q-L4.6); so is the void (Q-L4.4).
func seedSales(ctx context.Context, app *bootstrap.App, day salesData, pin string, byName map[string]domain.Product, res *Result) error {
	receipts := map[int]salesdomain.Sale{}
	for i, s := range day.Sales {
		cart := salesdomain.CartInput{Settlement: s.Settlement, SaleDiscount: s.SaleDiscount, TenderCurrency: s.TenderCurrency, Tendered: s.Tendered}
		for _, l := range s.Lines {
			productID := byName[l.Product].ID
			if l.Barcode != "" {
				scanned, found, err := app.Sales.Scan(ctx, l.Barcode)
				if err != nil {
					return err
				}
				if !found {
					return errs.Internal(CodeSalesInconsistent, "a demo barcode scans nothing").WithParam("barcode", l.Barcode)
				}
				productID = scanned.Product.ID
			}
			if productID == "" {
				return errs.Internal(CodeSalesInconsistent, "a demo sale names a product the catalogue lacks").WithParam("product", l.Product)
			}
			cart.Lines = append(cart.Lines, salesdomain.LineInput{ProductID: productID, Quantity: l.Quantity, DiscountPercent: l.DiscountPercent})
		}
		q, err := app.Sales.Quote(ctx, cart)
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "quoting demo sale "+strconv.Itoa(i+1))
		}
		if s.Owner {
			if _, err = app.Owner.Elevate(ctx, pin); err != nil {
				return err
			}
		}
		sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cart, Token: q.Token})
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "ringing up demo sale "+strconv.Itoa(i+1))
		}
		if s.Owner {
			if _, err = app.Owner.EndElevation(ctx); err != nil {
				return err
			}
		}
		receipts[i+1] = sale
		res.Sales++
	}

	voided, ok := receipts[day.Void.Sale]
	if !ok {
		return errs.Internal(CodeSalesInconsistent, "the demo void names no demo sale").WithParam("sale", strconv.Itoa(day.Void.Sale))
	}
	if _, err := app.Owner.Elevate(ctx, pin); err != nil {
		return err
	}
	if _, err := app.Sales.Void(ctx, sales.VoidInput{SaleID: voided.ID, Reason: day.Void.Reason}); err != nil {
		return err
	}
	res.Voids++
	_, err := app.Owner.EndElevation(ctx)
	return err
}

// seedCustomers adopts a paper debt book and runs a day of credit (L5 §12): the owner enters the openings with the PIN,
// the counter sells on credit and takes repayments without it, and the owner voids, refunds and writes off with it.
func seedCustomers(ctx context.Context, app *bootstrap.App, book customersData, pin string, byName map[string]domain.Product, res *Result) error {
	people := map[string]customersdomain.Customer{}
	who := func(name string) (customersdomain.Customer, error) {
		c, ok := people[name]
		if !ok {
			return customersdomain.Customer{}, errs.Internal(CodeDebtsInconsistent, "the demo names a customer it did not create").WithParam("name", name)
		}
		return c, nil
	}
	elevated := func(fn func() error) error {
		if _, err := app.Owner.Elevate(ctx, pin); err != nil {
			return err
		}
		if err := fn(); err != nil {
			return err
		}
		_, err := app.Owner.EndElevation(ctx)
		return err
	}

	for _, c := range book.Customers {
		created, err := app.Customers.Create(ctx, customersdomain.Draft{Name: c.Name, Phone: c.Phone, Note: c.Note})
		if err != nil {
			return err
		}
		people[c.Name] = created
		res.Customers++
	}
	if err := elevated(func() error {
		for _, c := range book.Customers {
			for _, o := range c.Openings {
				if _, err := app.Customers.Opening(ctx, customers.AmountInput{CustomerID: people[c.Name].ID, Currency: o.Currency, Amount: o.Amount, Note: o.Note}); err != nil {
					return err
				}
				res.DebtOpenings++
			}
		}
		return nil
	}); err != nil {
		return err
	}

	sold := map[string]salesdomain.Sale{}
	for i, cs := range book.CreditSales {
		c, err := who(cs.Customer)
		if err != nil {
			return err
		}
		settlement := cs.Settlement
		if settlement == "" {
			current, getErr := app.Settings.Get(ctx)
			if getErr != nil {
				return getErr
			}
			settlement = current.DebtCurrency // the till's default on credit (Q-L5.1)
		}
		cart := salesdomain.CartInput{Settlement: settlement, TenderCurrency: cs.TenderCurrency, Tendered: cs.Tendered,
			Payment: salesdomain.PaymentCredit, CustomerID: c.ID}
		for _, l := range cs.Lines {
			productID := byName[l.Product].ID
			if l.Barcode != "" {
				scanned, found, scanErr := app.Sales.Scan(ctx, l.Barcode)
				if scanErr != nil || !found {
					return errs.Internal(CodeDebtsInconsistent, "a demo barcode scans nothing").WithParam("barcode", l.Barcode)
				}
				productID = scanned.Product.ID
			}
			cart.Lines = append(cart.Lines, salesdomain.LineInput{ProductID: productID, Quantity: l.Quantity})
		}
		q, err := app.Sales.Quote(ctx, cart)
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "quoting demo credit sale "+strconv.Itoa(i+1))
		}
		sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cart, Token: q.Token})
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "ringing up demo credit sale "+strconv.Itoa(i+1))
		}
		sold[cs.Customer] = sale
		res.CreditSales++
	}

	for i, p := range book.Payments {
		c, err := who(p.Customer)
		if err != nil {
			return err
		}
		in := customersdomain.CashInput{Currency: p.Currency, TenderCurrency: p.TenderCurrency, Amount: p.Amount, All: p.All}
		q, err := app.Customers.QuotePayment(ctx, c.ID, in)
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "quoting demo payment "+strconv.Itoa(i+1))
		}
		if _, err = app.Customers.RecordPayment(ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token}); err != nil {
			return err
		}
		res.Payments++
	}

	return elevated(func() error {
		off, err := who(book.WriteOff.Customer)
		if err != nil {
			return err
		}
		if _, err = app.Customers.WriteOff(ctx, customers.AmountInput{CustomerID: off.ID, Currency: book.WriteOff.Currency, All: true, Note: book.WriteOff.Reason}); err != nil {
			return err
		}
		voided, ok := sold[book.VoidCredit.Customer]
		if !ok {
			return errs.Internal(CodeDebtsInconsistent, "the demo voids a credit sale it did not make")
		}
		if _, err = app.Sales.Void(ctx, sales.VoidInput{SaleID: voided.ID, Reason: book.VoidCredit.Reason}); err != nil {
			return err
		}
		res.Voids++
		refunded, err := who(book.Refund.Customer)
		if err != nil {
			return err
		}
		_, err = app.Customers.Refund(ctx, customers.RefundInput{CustomerID: refunded.ID, Reason: book.Refund.Reason,
			Cash: customersdomain.CashInput{Currency: book.Refund.Currency, All: true}})
		return err
	})
}

// check runs both verifiers over everything seeded, in owner mode, and records the figures the command prints. A demo
// that fails the application's own checks is a demo of a defect.
func check(ctx context.Context, app *bootstrap.App, pin string, res *Result) error {
	if _, err := app.Owner.Elevate(ctx, pin); err != nil {
		return err
	}
	stockFindings, err := app.Stock.Verify(ctx)
	if err != nil {
		return err
	}
	if len(stockFindings) > 0 {
		return errs.Internal(CodeStockInconsistent, "the stock verifier found problems in the seeded data").
			WithParam("first", stockFindings[0].Code).WithParam("count", strconv.Itoa(len(stockFindings)))
	}
	salesFindings, err := app.Sales.Verify(ctx)
	if err != nil {
		return err
	}
	if len(salesFindings) > 0 {
		return errs.Internal(CodeSalesInconsistent, "the sales verifier found problems in the seeded data").
			WithParam("first", salesFindings[0].Code).WithParam("count", strconv.Itoa(len(salesFindings)))
	}
	debtFindings, err := app.Customers.Verify(ctx)
	if err != nil {
		return err
	}
	if len(debtFindings) > 0 {
		return errs.Internal(CodeDebtsInconsistent, "the debt book's verifier found problems in the seeded data").
			WithParam("first", debtFindings[0].Code).WithParam("count", strconv.Itoa(len(debtFindings)))
	}
	valuation, err := app.Stock.Valuation(ctx)
	if err != nil {
		return err
	}
	res.StockValue = stockdomain.FormatMinor(valuation.TotalMinor)
	current, err := app.FX.Current(ctx)
	if err != nil {
		return err
	}
	res.Rate = fxdomain.FormatRate(current.Rate.Nano)

	day, err := app.Sales.Day(ctx, "")
	if err != nil {
		return err
	}
	currencies, err := app.Catalog.Currencies(ctx)
	if err != nil {
		return err
	}
	res.Takings = map[string]string{}
	for _, c := range currencies {
		if t, ok := day.Totals[c.Code]; ok {
			res.Takings[c.Code] = fxdomain.FormatMinor(t.ChargedMinor-t.VoidedMinor, c.Decimals)
		}
	}
	_, err = app.Owner.EndElevation(ctx)
	return err
}

// seedStock runs L2's part of the demo in the order a shop adopting Lite would: what opens into what, the stock on
// the shelves, the week's deliveries, a count, a write-off, and a tin opened for loose sale. Then it checks.
func stockProduct(byName map[string]domain.Product, name string) (domain.Product, error) {
	p, ok := byName[name]
	if !ok {
		return domain.Product{}, errs.Internal(CodeStockInconsistent, "the demo stock names a product the catalogue lacks").WithParam("product", name)
	}
	return p, nil
}

func receiveLine(ctx context.Context, byName map[string]domain.Product, line costLine, act func(context.Context, stock.ReceiveInput) (stockdomain.Movement, error)) error {
	p, err := stockProduct(byName, line.Product)
	if err != nil {
		return err
	}
	_, err = act(ctx, stock.ReceiveInput{
		ProductID: p.ID, Quantity: line.Quantity, Note: line.Note,
		Cost: stockdomain.CostInput{Mode: stockdomain.CostTotal, Amount: line.Cost, Currency: line.Currency, Rate: line.Rate},
	})
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "seeding stock of "+line.Product)
	}
	return nil
}

// seedOpenings links the tin to its oil and enters the stock on the shelves — the first thing a shop adopting Lite does.
func seedOpenings(ctx context.Context, app *bootstrap.App, stk stockData, byName map[string]domain.Product, res *Result) error {
	tin, err := stockProduct(byName, stk.Package.Package)
	if err != nil {
		return err
	}
	oil, err := stockProduct(byName, stk.Package.Content)
	if err != nil {
		return err
	}
	if _, err = app.Catalog.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: stk.Package.ContentQuantity}); err != nil {
		return err
	}
	for _, line := range stk.Openings {
		if err = receiveLine(ctx, byName, line, app.Stock.Opening); err != nil {
			return err
		}
		res.Openings++
	}
	return nil
}

func seedStock(ctx context.Context, app *bootstrap.App, stk stockData, rates ratesData, byName map[string]domain.Product, res *Result) error {
	product := func(name string) (domain.Product, error) { return stockProduct(byName, name) }
	receive := func(line costLine, act func(context.Context, stock.ReceiveInput) (stockdomain.Movement, error)) error {
		return receiveLine(ctx, byName, line, act)
	}
	tin, err := product(stk.Package.Package)
	if err != nil {
		return err
	}
	// The day's rate, then its correction: the history shows both, and the correction is in force (L3 §11.2).
	for _, r := range []rateLine{rates.Later, rates.Correction} {
		if _, err = app.FX.SetRate(ctx, fx.SetRateInput{Rate: r.Rate, Note: r.Note}); err != nil {
			return err
		}
	}
	for _, line := range stk.Receipts {
		if err = receive(line, app.Stock.Receive); err != nil {
			return err
		}
		res.Receipts++
	}

	counted, err := product(stk.Count.Product)
	if err != nil {
		return err
	}
	if _, err = app.Stock.Count(ctx, stock.CountInput{ProductID: counted.ID, Counted: stk.Count.Counted, Note: stk.Count.Note}); err != nil {
		return err
	}
	spoiled, err := product(stk.WriteOff.Product)
	if err != nil {
		return err
	}
	if _, err = app.Stock.Adjust(ctx, stock.AdjustInput{
		ProductID: spoiled.ID, Direction: stock.DirectionOut, Quantity: stk.WriteOff.Quantity,
		Reason: stockdomain.Reason(stk.WriteOff.Reason), Note: stk.WriteOff.Note,
	}); err != nil {
		return err
	}
	if _, err = app.Stock.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: tin.ID, Packages: stk.OpenPackages}); err != nil {
		return err
	}
	return nil
}

func readData(name string, into any) error {
	raw, err := data.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// seedHistory enters the openings on the first day of history, then runs the days before today (history.go), and leaves the
// clock on today.
func seedHistory(ctx context.Context, app *bootstrap.App, opts Options, cat catalogue, stk stockData, byName map[string]domain.Product,
	start time.Time, res *Result) error {
	if _, err := app.Owner.Elevate(ctx, opts.PIN); err != nil {
		return err
	}
	if err := seedOpenings(ctx, app, stk, byName, res); err != nil {
		return err
	}
	ref, err := app.Catalog.Reference(ctx)
	if err != nil {
		return err
	}
	h := &history{ctx: ctx, app: app, pin: opts.PIN, clk: opts.Clock, rng: rand.New(rand.NewSource(6)), byName: byName, units: map[string]int{}, res: res} //nolint:gosec // a demo, reproducible
	for _, item := range cat.Products {
		h.names = append(h.names, item.NameAR)
	}
	for code, u := range ref.Units {
		h.units[code] = u.InputDecimals
	}
	if err = h.days(start, opts.Days); err != nil {
		return err
	}
	opts.Clock.Current = opts.Now
	for _, c := range h.counts {
		d, drawerErr := app.Reports.Drawer(ctx, c.BusinessDate)
		if drawerErr != nil {
			return drawerErr
		}
		for _, terms := range d.Currencies {
			if terms.Currency == c.Currency && (terms.Count == nil || terms.Count.ExpectedMinor != terms.Expected) {
				return errs.Internal(CodeReportsInconsistent, "a closing count was recorded against another figure than the drawer's").
					WithParam("date", c.BusinessDate).WithParam("currency", c.Currency)
			}
		}
	}
	_, err = app.Owner.EndElevation(ctx)
	return err
}

// checkReports holds the seeded history to the reports' reconciliations (L6 §12.2): each month is the sum of its days, the
// stock's movements add up from the first day's value to today's within the rounding they carry, and today's value is the
// stock screen's.
func checkReports(ctx context.Context, app *bootstrap.App, pin, from string, res *Result) error {
	if _, err := app.Owner.Elevate(ctx, pin); err != nil {
		return err
	}
	today := app.Reports.Today()
	var all []reportsdomain.Day
	for month := from[:7]; month <= today[:7]; {
		m, err := app.Reports.Month(ctx, month)
		if err != nil {
			return err
		}
		var days []reportsdomain.Day
		for _, date := range reportsdomain.Dates(m.From, m.To) {
			d, err := app.Reports.Day(ctx, date)
			if err != nil {
				return err
			}
			days = append(days, d)
			if date >= from && date <= today {
				all = append(all, d)
			}
		}
		if fmt.Sprintf("%+v", reportsdomain.Total(m.From, days)) != fmt.Sprintf("%+v", m.Total) {
			return errs.Internal(CodeReportsInconsistent, "a month is not the sum of its days").WithParam("month", month)
		}
		next, _ := time.Parse("2006-01", month)
		month = next.AddDate(0, 1, 0).Format("2006-01")
	}
	stockReport, err := app.Reports.Stock(ctx, from, today)
	if err != nil {
		return err
	}
	c := stockReport.Reconciliation
	sum := c.Opening + c.Received + c.Sold + c.Losses + c.Gains + c.Revaluation + c.Packages + c.NegativeStock + c.Rounding
	if sum != c.Closing || 2*absolute(c.Rounding) > int64(c.Rounded) {
		return errs.Internal(CodeReportsInconsistent, "the stock movements do not reconcile").WithParam("rounding", strconv.FormatInt(c.Rounding, 10))
	}
	valuation, err := app.Stock.Valuation(ctx)
	if err != nil {
		return err
	}
	var negative int64
	for _, l := range valuation.Lines {
		if l.OnHandMicro < 0 {
			negative += l.ValueMinor
		}
	}
	if valuation.TotalMinor-negative != c.Closing {
		return errs.Internal(CodeReportsInconsistent, "today's stock value is not the stock screen's").
			WithParam("report", strconv.FormatInt(c.Closing, 10)).WithParam("screen", strconv.FormatInt(valuation.TotalMinor, 10))
	}
	total := reportsdomain.Total(from, all)
	pair, err := app.Reports.Pair(ctx)
	if err != nil {
		return err
	}
	res.HistoryProfitUSD = fxdomain.FormatMinor(total.NetUSD(), pair.USD.Decimals)
	res.HistoryProfitLocal = fxdomain.FormatMinor(total.NetLocal(), pair.Local.Decimals)
	_, err = app.Owner.EndElevation(ctx)
	return err
}

func absolute(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// DemoPrinter is the receipt printer a seeded shop is set up with: a name no computer has, so a print reports "not installed"
// honestly until the owner chooses a real one in Printer settings.
const DemoPrinter = "Demo XP-80 (طابعة تجريبية)"

// seedPrinting exercises L7 the way a shop does (L8 §2.1): the printer settings and the receipt's lines, a day's last receipt
// printed and reprinted as a copy, a payment's voucher printed, a print that failed, and a backup copied to an outside folder —
// a folder inside the data directory, so the seeder writes nowhere else.
func seedPrinting(ctx context.Context, app *bootstrap.App, pin string, res *Result) error {
	if _, err := app.Owner.Elevate(ctx, pin); err != nil {
		return err
	}
	defer func() { _, _ = app.Owner.EndElevation(ctx) }()
	printer, paper, path, auto, drawer := DemoPrinter, "80", "driver", "credit", "false"
	phone, address, footer := "011 612 3456", "دمشق — المزة، شارع الفيلات", "شكراً لزيارتكم"
	outside := filepath.Join(app.Paths.Data, "outside-folder-demo")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStockInconsistent, "creating the demo outside folder")
	}
	if _, err := app.Settings.Update(ctx, settingsdomain.Update{Printing: settingsdomain.PrintingUpdate{Printer: &printer, PaperMM: &paper,
		Path: &path, AutoPrint: &auto, Drawer: &drawer, Phone: &phone, Address: &address, Footer: &footer, BackupFolder: &outside}}); err != nil {
		return err
	}
	day, err := app.Sales.Day(ctx, "")
	if err != nil {
		return err
	}
	if len(day.Sales) > 0 {
		last := day.Sales[len(day.Sales)-1]
		for copyNo := 1; copyNo <= 2; copyNo++ {
			if _, err = app.Printing.Record(ctx, printing.Job{Kind: printing.KindSale, SubjectID: last.ID, CopyNo: copyNo, Printer: printer, Path: path, Sent: true}); err != nil {
				return err
			}
			res.PrintJobs++
		}
	}
	entries, err := app.Customers.EntriesBetween(ctx, "0001-01-01", "9999-12-31")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Entry.Kind != customersdomain.KindPayment {
			continue
		}
		if _, numbered, voucherErr := app.Printing.Voucher(ctx, e.Entry.ID); voucherErr != nil {
			return voucherErr
		} else if numbered {
			res.Vouchers++
		}
		if res.PrintJobs < 3 {
			if _, err = app.Printing.Record(ctx, printing.Job{Kind: printing.KindPayment, SubjectID: e.Entry.ID, CopyNo: 1, Printer: printer, Path: path, Sent: true}); err != nil {
				return err
			}
			if _, err = app.Printing.Record(ctx, printing.Job{Kind: printing.KindTest, CopyNo: 1, Printer: printer, Path: "raw", ErrorCode: "lite.printers.not_found"}); err != nil {
				return err
			}
			res.PrintJobs += 2
		}
	}
	if _, err = app.Safety.TakeNow(ctx); err != nil {
		return err
	}
	status, err := app.Safety.Status(ctx)
	if err != nil {
		return err
	}
	if status.LastOutside == nil || status.OutsideFailed != "" {
		return errs.Internal(CodeStockInconsistent, "the seeded backup was not copied to the outside folder")
	}
	list, err := app.Safety.List(ctx)
	res.Backups = len(list)
	return err
}
