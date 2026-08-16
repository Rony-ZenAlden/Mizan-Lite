package bootstrap

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
)

// Dashboard is several modules' answers on one screen.
//
// # Why this lives in the composition root and is not a module
//
// Phase 8's analysis called the dashboard an ASSEMBLY rather than a computation: every tile is
// one module's own answer, and nothing here combines two modules' data into a third number. A
// tile that needed two modules multiplied together would be a REPORT, and would belong to
// whichever module owns the multiplication.
//
// That distinction is what keeps this file from becoming the reporting module the analysis
// refused. The test for it is mechanical: if any line below does arithmetic on figures from two
// different services, the dashboard has started making decisions and the work belongs elsewhere.
type Dashboard struct {
	CompanyID id.ID
	// From and To are the range every periodic tile covers, so a reader is never left guessing
	// whether "sales" means today or this month.
	From string
	To   string

	Tiles []Tile
}

// Tile is one figure, with the module that answered and the range it covers.
//
// # Why every tile names its source
//
// DoD criterion 7. A dashboard is where somebody first notices a number is wrong, and "revenue
// 4,500" with no indication of what it counts or who counted it sends them looking through five
// screens. `Source` is where to go next.
type Tile struct {
	// Key is the stable identifier a screen renders against, e.g. "sales.revenue". Never a
	// translated label: the label is the frontend's, from the i18n catalogue.
	Key string
	// Source names the module that answered.
	Source string

	// AmountMinor carries money tiles. Count carries countable ones. A tile sets one.
	AmountMinor int64
	Count       int

	// Kind tells a screen which field to render and how: "money", "count".
	Kind string

	// Periodic marks a tile whose figure covers From..To rather than being a position as at
	// today. Mixing the two silently is how "stock value 40,000" gets read as this month's
	// purchases.
	Periodic bool

	// Failed marks a tile whose module could not answer.
	//
	// A dashboard that renders nothing because one query broke is a home screen that fails
	// entirely for a reason nobody can see. The same choice `search.Response.Failed` makes, for
	// the same reason.
	Failed bool
}

// dashboardSources is what the dashboard is allowed to ask.
//
// Narrowed to interfaces rather than taking *App, so that adding a tile means adding a method
// here — visible, and reviewable against the "no cross-module arithmetic" rule above.
type dashboardSources struct {
	sales      *sales.Service
	purchasing *purchasing.Service
	inventory  *inventory.Service
}

// Dashboard assembles the home screen.
//
// The range is the caller's. There is no default of "this month" hidden in here: a screen that
// shows figures for a period the user did not choose is a screen whose numbers cannot be
// checked, and 8.1's D5 made the same call for statements.
func (a *App) Dashboard(
	ctx context.Context, companyID id.ID, from, to string,
) (Dashboard, error) {
	sources := dashboardSources{
		sales: a.Sales, purchasing: a.Purchasing, inventory: a.Inventory,
	}
	board := Dashboard{CompanyID: companyID, From: from, To: to, Tiles: make([]Tile, 0, 6)}

	// Sales over the range.
	if analysis, err := sources.sales.SalesByPeriod(
		ctx, companyID, from, to, sales.ByDay,
	); err != nil {
		board.Tiles = append(board.Tiles,
			Tile{Key: "sales.revenue", Source: "sales", Kind: "money", Periodic: true,
				Failed: true},
			Tile{Key: "sales.gross_margin", Source: "sales", Kind: "money", Periodic: true,
				Failed: true})
	} else {
		board.Tiles = append(board.Tiles,
			Tile{Key: "sales.revenue", Source: "sales", Kind: "money", Periodic: true,
				AmountMinor: analysis.Total.RevenueMinor},
			Tile{Key: "sales.gross_margin", Source: "sales", Kind: "money", Periodic: true,
				AmountMinor: analysis.Total.GrossMarginMinor},
			Tile{Key: "sales.documents", Source: "sales", Kind: "count", Periodic: true,
				Count: documentsIn(analysis)})
	}

	// Spend over the same range.
	if analysis, err := sources.purchasing.SpendByPeriod(
		ctx, companyID, from, to, purchasing.ByDay,
	); err != nil {
		board.Tiles = append(board.Tiles,
			Tile{Key: "purchasing.spend", Source: "purchasing", Kind: "money", Periodic: true,
				Failed: true})
	} else {
		board.Tiles = append(board.Tiles,
			Tile{Key: "purchasing.spend", Source: "purchasing", Kind: "money", Periodic: true,
				AmountMinor: analysis.Total.NetMinor})
	}

	// Stock value AS AT NOW, not over the range — which is why `Periodic` exists.
	if valuation, err := sources.inventory.Valuation(ctx, companyID); err != nil {
		board.Tiles = append(board.Tiles,
			Tile{Key: "inventory.stock_value", Source: "inventory", Kind: "money", Failed: true})
	} else {
		board.Tiles = append(board.Tiles,
			Tile{Key: "inventory.stock_value", Source: "inventory", Kind: "money",
				AmountMinor: valuation.TotalMinor},
			Tile{Key: "inventory.lines_held", Source: "inventory", Kind: "count",
				Count: len(valuation.Lines)})
	}

	return board, nil
}

// documentsIn counts the distinct documents behind a period analysis.
//
// Summing each bucket's own count is correct BECAUSE the buckets are days and a document has one
// date: no document can appear in two. Written here rather than as a field on the analysis
// because it is only true for the daily grouping, and a field would carry that condition
// invisibly.
func documentsIn(analysis sales.Analysis) int {
	var total int
	for _, row := range analysis.Rows {
		total += row.Documents
	}
	return total
}

// Today is the ISO date the application considers today, from the injected clock.
//
// Exposed so a caller with no date can ask for one rather than inventing it — and so a test can
// make "today" deterministic, which `time.Now()` scattered through a screen cannot.
func (a *App) Today() string {
	return a.opts.Clock.Now().Format(time.DateOnly)
}

// MonthToDate is the range a home screen most often wants, computed once here.
func (a *App) MonthToDate() (from, to string) {
	now := a.opts.Clock.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).
			Format(time.DateOnly),
		now.Format(time.DateOnly)
}
