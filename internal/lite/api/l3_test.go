package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/paths"
)

// attachedWithSource is attached with a scripted rate source registered.
func attachedWithSource(t *testing.T, source *fxtest.Source) *api.Set {
	t.Helper()
	p := paths.Layout(filepath.Join(t.TempDir(), "data"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{Paths: p, Logger: litetest.Logger(), PINHasher: ownertest.Hasher(), RateSource: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	set := api.New("t", litetest.Logger())
	set.Attach(app)
	return set
}

func TestTheRateThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)

	current := set.FX.Current()
	if !current.OK || !current.Data.Set || current.Data.Rate != "15000" || current.Data.Source != "first_run" ||
		current.Data.LocalCurrency != "SYP" || current.Data.Mode != "manual" || current.Data.CanFetch || current.Data.HasFetch {
		t.Fatalf("Current = %+v", current)
	}
	if r := set.FX.SetRate(api.SetRateInput{Rate: "15200"}); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("a rate went through without the owner")
	}
	if r := set.FX.Refresh(); codeOf(t, r) != fxdomain.CodeFetchUnavailable {
		t.Fatal("a graph with no provider fetched")
	}
	if r := set.FX.FetchQuote(); codeOf(t, r) != fxdomain.CodeFetchUnavailable {
		t.Fatal("a quote with no provider")
	}

	elevate(t, set)
	large := set.FX.SetRate(api.SetRateInput{Rate: "15.000"})
	if codeOf(t, large) != fxdomain.CodeLargeChange || large.Error.Params["typed"] != "15" || large.Error.Params["inForce"] != "15000" {
		t.Fatalf("a 1000× mistake = %+v", large)
	}
	set1 := set.FX.SetRate(api.SetRateInput{Rate: "15200", Note: "السوق"})
	if !set1.OK || set1.Data.Rate != "15200" || set1.Data.Source != "manual" || set1.Data.Stale {
		t.Fatalf("SetRate = %+v", set1)
	}
	corrected := set.FX.SetRate(api.SetRateInput{Rate: "15000", Note: "تصحيح"})
	if !corrected.OK || corrected.Data.Rate != "15000" {
		t.Fatalf("correction = %+v", corrected)
	}
	history := set.FX.History(10)
	if !history.OK || len(history.Data) != 3 || history.Data[0].Rate != "15000" || history.Data[0].Change != "-1.3" ||
		history.Data[0].Note != "تصحيح" || history.Data[2].Source != "first_run" || history.Data[2].Change != "" {
		t.Fatalf("History = %+v", history)
	}
	if m := set.FX.SetMode("automatic"); !m.OK || m.Data.Mode != "automatic" {
		t.Fatalf("SetMode = %+v", m)
	}
	if m := set.FX.SetMode("sometimes"); codeOf(t, m) != fxdomain.CodeInvalidMode {
		t.Fatal("an unknown mode")
	}
	if r := set.FX.AcceptProposal("nonsense"); codeOf(t, r) != fxdomain.CodeFetchNotFound {
		t.Fatal("an unparsable fetch id")
	}

	// Conversions on the catalogue, at 15,000: $6.50 is 97,500 pounds; 18,000 pounds is $1.20.
	oil := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "زيت", UnitCode: "l", PriceCurrency: "USD", Price: "6.50"})
	bulgur := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "برغل", UnitCode: "kg", PriceCurrency: "SYP", Price: "18000"})
	if oil.Data.ConvertedPrice != "97500" || oil.Data.ConvertedCurrency != "SYP" || bulgur.Data.ConvertedPrice != "1.20" || bulgur.Data.ConvertedCurrency != "USD" {
		t.Fatalf("converted = %+v / %+v", oil.Data, bulgur.Data)
	}
}

// TestStockValueInPoundsIsSummedPerLineNotConverted: two lines of half a litre at $3.33, at 13,000. Each line is 21,645
// pounds exactly, 43,290 together; converting the rounded dollar total ($3.34) would give 43,420 (L3 §5.4).
func TestStockValueInPoundsIsSummedPerLineNotConverted(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	elevate(t, set)
	if r := set.FX.SetRate(api.SetRateInput{Rate: "13000"}); !r.OK {
		t.Fatal(r.Error)
	}
	for _, name := range []string{"زيت أ", "زيت ب"} {
		p := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: name, UnitCode: "l", PriceCurrency: "USD", Price: "5"})
		if r := set.Stock.Opening(api.ReceiveInput{ProductID: p.Data.ID, Quantity: "0.5", CostMode: "unit", Cost: "3.33", Currency: "USD"}); !r.OK {
			t.Fatal(r.Error)
		}
	}
	v := set.Stock.Valuation()
	if !v.OK || v.Data.Total != "3.34" || v.Data.TotalLocal != "43290" || v.Data.Rate != "13000" || v.Data.LocalCurrency != "SYP" {
		t.Fatalf("valuation = %+v", v)
	}
	for _, line := range v.Data.Lines {
		if line.ValueLocal != "21645" || line.Value != "1.67" {
			t.Fatalf("line = %+v", line)
		}
	}
}

func TestNoRateMeansNoConvertedFigure(t *testing.T) {
	set, app := attached(t, litetest.Logger())
	// An installation upgraded from L2 has a shop and no rate: first run is not repeated.
	if _, err := app.Owner.SetUp(context.Background(), testPIN); err != nil {
		t.Fatal(err)
	}
	current := set.FX.Current()
	if !current.OK || current.Data.Set || current.Data.Rate != "" || current.Data.AgeSeconds != 0 {
		t.Fatalf("Current with no rate = %+v", current)
	}
	p := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "زيت", UnitCode: "l", PriceCurrency: "USD", Price: "6.50"})
	if !p.OK || p.Data.ConvertedPrice != "" || p.Data.ConvertedCurrency != "" {
		t.Fatalf("a price converted with no rate: %+v", p)
	}
	elevate(t, set)
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: p.Data.ID, Quantity: "2", Cost: "10", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	v := set.Stock.Valuation()
	if !v.OK || v.Data.TotalLocal != "" || v.Data.LocalCurrency != "" || v.Data.Lines[0].ValueLocal != "" || v.Data.Total != "10.00" {
		t.Fatalf("valuation with no rate = %+v", v)
	}
}

func TestAFetchedProposalThroughTheBindings(t *testing.T) {
	source := &fxtest.Source{}
	set := attachedWithSource(t, source)
	source.Answer("currency-api-jsdelivr", 13_007_535_500_000)
	quote := set.FX.FetchQuote()
	if !quote.OK || quote.Data.Rate != "13007.5355" || quote.Data.Provider != "currency-api-jsdelivr" {
		t.Fatalf("FetchQuote = %+v", quote)
	}
	// First run with the quote as typed; then the internet answers the redenominated pound, unscaled.
	done := set.App.CompleteFirstRun(api.FirstRunInput{ShopName: "المونة", Locale: "ar", PIN: testPIN, Rate: quote.Data.Rate})
	if !done.OK {
		t.Fatal(done.Error)
	}
	// Manual by default: the owner switches to automatic, then leaves owner mode.
	elevate(t, set)
	if m := set.FX.SetMode("automatic"); !m.OK {
		t.Fatal(m.Error)
	}
	if r := set.Owner.EndElevation(); !r.OK {
		t.Fatal(r.Error)
	}
	source.Answer("exchangerate-api-open", 121_957_900_000)
	refreshed := set.FX.Refresh()
	if !refreshed.OK || refreshed.Data.Rate != "13007.5355" || !refreshed.Data.HasFetch || refreshed.Data.LastFetch.Outcome != "proposed" ||
		!refreshed.Data.LastFetch.Acceptable || refreshed.Data.LastFetch.Change != "-99.1" || !refreshed.Data.CanFetch {
		t.Fatalf("Refresh = %+v", refreshed)
	}
	if r := set.FX.AcceptProposal(refreshed.Data.LastFetch.ID); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("a proposal accepted without the owner")
	}
	elevate(t, set)
	accepted := set.FX.AcceptProposal(refreshed.Data.LastFetch.ID)
	if !accepted.OK || accepted.Data.Rate != "121.9579" || accepted.Data.Source != "fetched" || accepted.Data.LastFetch.Acceptable {
		t.Fatalf("AcceptProposal = %+v", accepted)
	}
	if h := set.FX.History(1); !h.OK || h.Data[0].Provider != "exchangerate-api-open" {
		t.Fatalf("History = %+v", h)
	}
}
