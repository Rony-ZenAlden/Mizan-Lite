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
// and then the stock verifier and the sales verifier, both of which must find nothing (L4 §11).
package demoseed

import (
	"context"
	"embed"
	"encoding/json"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// CodeStockInconsistent fails a run whose stock the verifier finds anything wrong with: a demo that cannot pass the
// application's own check is a demo of a defect.
const CodeStockInconsistent = "lite.demoseed.stock_inconsistent"

// CodeSalesInconsistent fails a run whose sales the sales verifier finds anything wrong with.
const CodeSalesInconsistent = "lite.demoseed.sales_inconsistent"

// CodeAlreadySetUp refuses an installation that has completed first run. Re-seeding means deleting the data
// directory — a decision for whoever runs this, not something a seeder should do on their behalf.
const CodeAlreadySetUp = "lite.demoseed.already_set_up"

//go:embed data/catalogue.json data/stock.json data/rates.json data/sales.json
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
	// Sales and Voids are the day at the till; Takings is what was charged less what was refunded, per currency, as Go
	// formats it.
	Sales   int
	Voids   int
	Takings map[string]string
}

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

	res := Result{ShopName: cat.ShopName}
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
	// The count and the write-off lower stock, so they are the owner's too (Q-L2.3) — seeded in the same owner mode.
	if err := seedStock(ctx, app, stk, rates, byName, &res); err != nil {
		return Result{}, err
	}
	if _, err := app.Owner.EndElevation(ctx); err != nil {
		return Result{}, err
	}
	if err := seedSales(ctx, app, day, opts.PIN, byName, &res); err != nil {
		return Result{}, err
	}
	if err := check(ctx, app, opts.PIN, &res); err != nil {
		return Result{}, err
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
		sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cart, Token: q.Token, Payment: salesdomain.PaymentCash})
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
			res.Takings[c.Code] = fxdomain.FormatMinor(t.ChargedMinor-t.RefundedMinor, c.Decimals)
		}
	}
	_, err = app.Owner.EndElevation(ctx)
	return err
}

// seedStock runs L2's part of the demo in the order a shop adopting Lite would: what opens into what, the stock on
// the shelves, the week's deliveries, a count, a write-off, and a tin opened for loose sale. Then it checks.
func seedStock(ctx context.Context, app *bootstrap.App, stk stockData, rates ratesData, byName map[string]domain.Product, res *Result) error {
	product := func(name string) (domain.Product, error) {
		p, ok := byName[name]
		if !ok {
			return domain.Product{}, errs.Internal(CodeStockInconsistent, "the demo stock names a product the catalogue lacks").WithParam("product", name)
		}
		return p, nil
	}
	tin, err := product(stk.Package.Package)
	if err != nil {
		return err
	}
	oil, err := product(stk.Package.Content)
	if err != nil {
		return err
	}
	if _, err = app.Catalog.SetPackage(ctx, catalog.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: stk.Package.ContentQuantity}); err != nil {
		return err
	}

	receive := func(line costLine, act func(context.Context, stock.ReceiveInput) (stockdomain.Movement, error)) error {
		p, lookupErr := product(line.Product)
		if lookupErr != nil {
			return lookupErr
		}
		_, actErr := act(ctx, stock.ReceiveInput{
			ProductID: p.ID, Quantity: line.Quantity, Note: line.Note,
			Cost: stockdomain.CostInput{Mode: stockdomain.CostTotal, Amount: line.Cost, Currency: line.Currency, Rate: line.Rate},
		})
		if actErr != nil {
			return errs.Wrap(actErr, errs.CategoryInternal, errs.CodeOf(actErr), "seeding stock of "+line.Product)
		}
		return nil
	}
	for _, line := range stk.Openings {
		if err = receive(line, app.Stock.Opening); err != nil {
			return err
		}
		res.Openings++
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
