package httpsource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/httpsource"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// publishedProviders are the real providers pointed at a local test server, as the other tests here do.
func publishedProviders(url string) []httpsource.Provider {
	providers := httpsource.Providers()
	for i := range providers {
		providers[i].URL = url
	}
	return providers
}

// TestTheShopsOwnEndpointIsReadAndTriedFirst is the owner's request of 2026-09-17: a shop that knows a local market rate
// points Mizan at it, and the published providers stay behind it as the fallback.
func TestTheShopsOwnEndpointIsReadAndTriedFirst(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"usd": {"sell": 15300, "buy": 15200}}`))
	}))
	defer local.Close()
	published := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"usd": {"syp": 13007.5355}}`))
	}))
	defer published.Close()

	client := httpsource.New("t", publishedProviders(published.URL), nil)
	client.TryFirst(func(context.Context) (httpsource.Provider, bool) {
		return httpsource.LocalProvider(httpsource.LocalConfig{URL: local.URL, Field: "usd.sell"}), true
	})

	q, err := client.Fetch(context.Background(), "SYP")
	if err != nil {
		t.Fatal(err)
	}
	if q.Provider != httpsource.LocalMarket || q.Nano != 15_300*1_000_000_000 {
		t.Fatalf("the shop's own endpoint = %+v", q)
	}
}

// The whole point of the fallback: a small local endpoint will fail, and the shop must still be priced.
func TestTheStandardProvidersCatchAFailingLocalEndpoint(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	published := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"usd": {"syp": 13007.5355}}`))
	}))
	defer published.Close()

	client := httpsource.New("t", publishedProviders(published.URL), nil)
	client.TryFirst(func(context.Context) (httpsource.Provider, bool) {
		return httpsource.LocalProvider(httpsource.LocalConfig{URL: broken.URL, Field: "usd.sell"}), true
	})

	q, err := client.Fetch(context.Background(), "SYP")
	if err != nil {
		t.Fatalf("a broken local endpoint left the shop with no rate: %v", err)
	}
	if q.Provider != httpsource.CurrencyAPIJsDelivr {
		t.Fatalf("the fallback did not answer: %+v", q)
	}
}

// A shop that has not chosen the local source is fetched exactly as it was before any of this existed.
func TestNoLocalEndpointLeavesThePublishedProvidersAlone(t *testing.T) {
	published := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"usd": {"syp": 13007.5355}}`))
	}))
	defer published.Close()
	client := httpsource.New("t", publishedProviders(published.URL), nil)
	client.TryFirst(func(context.Context) (httpsource.Provider, bool) { return httpsource.Provider{}, false })

	q, err := client.Fetch(context.Background(), "SYP")
	if err != nil || q.Provider != httpsource.CurrencyAPIJsDelivr {
		t.Fatalf("fetch = %+v, %v", q, err)
	}
}

func TestALocalEndpointIsCheckedWhenItIsSaved(t *testing.T) {
	for name, raw := range map[string]string{
		"not a url": "nonsense",
		"no scheme": "rates.example.sy/api",
		"not http":  "ftp://rates.example.sy/api",
		"no host":   "https://",
	} {
		if _, err := httpsource.ParseLocalURL(raw); errs.CodeOf(err) != httpsource.CodeLocalRateURLInvalid {
			t.Errorf("%s was accepted as an endpoint: %v", name, err)
		}
	}
	// Empty is allowed: it means the shop has not set one up.
	if got, err := httpsource.ParseLocalURL("  "); err != nil || got != "" {
		t.Fatalf("an empty endpoint = %q, %v", got, err)
	}
	if got, err := httpsource.ParseLocalURL("https://rates.example.sy/api "); err != nil || got != "https://rates.example.sy/api" {
		t.Fatalf("a real endpoint = %q, %v", got, err)
	}
	// And the setting that chooses it.
	if _, err := settingsdomain.ParseRateSource("nonsense"); errs.CodeOf(err) != settingsdomain.CodeInvalidRateSource {
		t.Fatal("an unknown rate source was accepted")
	}
}
