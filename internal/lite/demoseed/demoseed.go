// Package demoseed fills an empty Mizan Lite installation with a pantry shop's worth of believable data.
//
// # Why it drives the services and never writes rows
//
// The reasoning of Mizan's own demo seeder: rows written by hand satisfy none of the rules the application
// enforces, and later phases check data against itself. So this goes through the same services the bindings
// call — first run, the catalogue, and the owner's guard — and a demo database is a database a shop could
// have produced. It is also an end-to-end run of everything built so far, in the order a shop does it.
//
// It grows with every phase (DESIGN §9.4). L1: first run and the catalogue.
package demoseed

import (
	"context"
	"embed"
	"encoding/json"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
)

// CodeAlreadySetUp refuses an installation that has completed first run. Re-seeding means deleting the data
// directory — a decision for whoever runs this, not something a seeder should do on their behalf.
const CodeAlreadySetUp = "lite.demoseed.already_set_up"

//go:embed data/catalogue.json
var data embed.FS

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
	raw, err := data.ReadFile("data/catalogue.json")
	if err != nil {
		return Result{}, err
	}
	if err = json.Unmarshal(raw, &cat); err != nil {
		return Result{}, err
	}

	res := Result{ShopName: cat.ShopName}
	if res.RecoveryCode, err = app.Setup.Run(ctx, setup.Input{ShopName: cat.ShopName, Locale: opts.Locale, PIN: opts.PIN}); err != nil {
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
	if _, err := app.Owner.EndElevation(ctx); err != nil {
		return Result{}, err
	}
	return res, nil
}
