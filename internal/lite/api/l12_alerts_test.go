package api_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
)

// TestTheBellAnswersBehindThePIN is the notification engine through the real module graph (2026-09-23), in the one
// configuration its fakes cannot vouch for: PIN protection on and nobody in owner mode. The engine reads seven modules;
// had any of those reads been owner-gated, the bell would fail for every cashier of every shop that turned the PIN on.
// It answers, holds back what is the owner's, and shows the rest.
func TestTheBellAnswersBehindThePIN(t *testing.T) {
	set, _ := tillShop(t) // the rate 15,000
	elevate(t, set)
	// Molasses priced in pounds at the rate of the day, two jars on the shelf at $1.00 each, telling the shop below five.
	jar := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "دبس رمان", UnitCode: "jar", PriceCurrency: "SYP", Price: "15000"})
	if !jar.OK {
		t.Fatal(jar.Error)
	}
	if r := set.Stock.Opening(api.ReceiveInput{ProductID: jar.Data.ID, Quantity: "2", CostMode: "total", Cost: "2", Currency: "USD"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := set.Catalog.SetReorder(api.SetReorderInput{ID: jar.Data.ID, RowVersion: jar.Data.RowVersion, Level: "5"}); !r.OK {
		t.Fatal(r.Error)
	}
	// The pound falls a tenth: the jar still sells at 15,000, below the 16,500 a dollar's jar now costs to replace.
	if r := set.FX.SetRate(api.SetRateInput{Rate: "16500"}); !r.OK {
		t.Fatal(r.Error)
	}

	setPIN(t, set, true)
	if r := set.Owner.EndElevation(); !r.OK {
		t.Fatal(r.Error)
	}
	hidden := set.Alerts.Current()
	if !hidden.OK {
		t.Fatalf("the bell failed behind the PIN: %+v", hidden.Error)
	}
	if len(hidden.Data.LowStock) != 1 || len(hidden.Data.Stale) != 1 {
		t.Fatalf("low %+v, stale %+v — the shop's own facts were withheld with the owner's", hidden.Data.LowStock, hidden.Data.Stale)
	}
	if s := hidden.Data.Stale[0]; s.Proposed != "16500" || s.Replacement != "" || s.Loss || hidden.Data.LossCount != 0 {
		t.Fatalf("stale = %+v, losses %d — a replacement cost was shown outside owner mode", s, hidden.Data.LossCount)
	}
	if !hidden.Data.OwnerHidden {
		t.Fatal("the owner's figures were held back without saying so")
	}

	elevate(t, set)
	shown := set.Alerts.Current()
	if !shown.OK {
		t.Fatal(shown.Error)
	}
	if s := shown.Data.Stale[0]; s.Replacement != "16500" || !s.Loss || shown.Data.LossCount != 1 || shown.Data.OwnerHidden {
		t.Fatalf("in owner mode: stale = %+v, losses %d, hidden %v", s, shown.Data.LossCount, shown.Data.OwnerHidden)
	}
	// The same notifications in both views: what is at a loss is not part of what the bell has read (D-099.12).
	if len(hidden.Data.Notifications) != len(shown.Data.Notifications) {
		t.Fatalf("hidden %d notifications, shown %d", len(hidden.Data.Notifications), len(shown.Data.Notifications))
	}
	for i := range hidden.Data.Notifications {
		if h, s := hidden.Data.Notifications[i], shown.Data.Notifications[i]; h.Key != s.Key || h.Fingerprint != s.Fingerprint {
			t.Fatalf("notification %d reads %s/%s hidden and %s/%s shown", i, h.Key, h.Fingerprint, s.Key, s.Fingerprint)
		}
	}
}
