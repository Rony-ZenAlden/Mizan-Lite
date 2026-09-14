package httpsource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/httpsource"
)

// local rewrites the real providers to a local test server, keeping their names, scales and readers: nothing in this
// file reaches the internet except TestTheRealProvidersAnswer, which runs only when asked.
func local(t *testing.T, handler http.HandlerFunc) ([]httpsource.Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	providers := httpsource.Providers()
	for i := range providers {
		providers[i].URL = srv.URL + "/" + providers[i].Name
	}
	return providers, srv
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// refuseNetwork is a transport that fails the test if anything tries to leave the machine.
type refuseNetwork struct{ t *testing.T }

func (r refuseNetwork) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasPrefix(req.URL.Host, "127.0.0.1") {
		r.t.Fatalf("a test reached for the network: %s", req.URL)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestTheFirstProviderAnswersInOldPounds(t *testing.T) {
	var agent string
	providers, _ := local(t, func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		_, _ = w.Write(fixture(t, "currency-api.json"))
	})
	q, err := httpsource.New("1.2.3", providers, refuseNetwork{t}).Fetch(context.Background(), "SYP")
	if err != nil || q.Provider != httpsource.CurrencyAPIJsDelivr || q.Nano != 13_007_535_500_000 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	if agent != "MizanLite/1.2.3" {
		t.Fatalf("user agent = %q", agent)
	}
}

func TestTheNewPoundIsScaledToTheOldOne(t *testing.T) {
	providers, _ := local(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, httpsource.ExchangeRateAPIOpen) {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(fixture(t, "exchangerate-api-open.json"))
	})
	q, err := httpsource.New("v", providers, refuseNetwork{t}).Fetch(context.Background(), "SYP")
	// 121.957862 × 100 = 12,195.7862.
	if err != nil || q.Provider != httpsource.ExchangeRateAPIOpen || q.Nano != 12_195_786_200_000 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
}

func TestProvidersAreTriedInOrder(t *testing.T) {
	var order []string
	providers, _ := local(t, func(w http.ResponseWriter, r *http.Request) {
		order = append(order, strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasSuffix(r.URL.Path, httpsource.CurrencyAPIPages) {
			_, _ = w.Write(fixture(t, "currency-api.json"))
			return
		}
		_, _ = w.Write([]byte(`{"date": "2026-09-14", "usd": {"eur": 0.86}}`)) // SYP missing
	})
	q, err := httpsource.New("v", providers, refuseNetwork{t}).Fetch(context.Background(), "SYP")
	if err != nil || q.Provider != httpsource.CurrencyAPIPages || strings.Join(order, ",") != "currency-api-jsdelivr,currency-api-pages" {
		t.Fatalf("quote = %+v, %v, order %v", q, err, order)
	}
}

func TestABadAnswerIsATypedFailure(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         "<html>301 Moved</html>",
		"negative rate":    `{"usd": {"syp": -13007.5}}`,
		"zero rate":        `{"usd": {"syp": 0}}`,
		"words for a rate": `{"usd": {"syp": "thirteen thousand"}}`,
		"missing":          `{"usd": {}}`,
		"too small":        `{"usd": {"syp": 0.00001}}`,
	} {
		t.Run(name, func(t *testing.T) {
			providers, _ := local(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := httpsource.New("v", providers[:1], refuseNetwork{t}).Fetch(context.Background(), "SYP")
			if errs.CodeOf(err) != domain.CodeFetchFailed {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("an error status", func(t *testing.T) {
		providers, _ := local(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "limit", http.StatusTooManyRequests) })
		_, err := httpsource.New("v", providers, refuseNetwork{t}).Fetch(context.Background(), "SYP")
		if typed, _ := errs.AsError(err); errs.CodeOf(err) != domain.CodeFetchFailed || !strings.Contains(typed.Params["status"], "429") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("an unsuccessful table", func(t *testing.T) {
		providers, _ := local(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result": "error", "base_code": "USD", "rates": {"SYP": 121.9}}`))
		})
		_, err := httpsource.New("v", providers[2:], refuseNetwork{t}).Fetch(context.Background(), "SYP")
		if errs.CodeOf(err) != domain.CodeFetchFailed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("an answer too large", func(t *testing.T) {
		providers, _ := local(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"usd": {"syp": 13007.5, "pad": "` + strings.Repeat("x", 2<<20) + `"}}`))
		})
		_, err := httpsource.New("v", providers[:1], refuseNetwork{t}).Fetch(context.Background(), "SYP")
		if typed, _ := errs.AsError(err); errs.CodeOf(err) != domain.CodeFetchFailed || typed.Params["provider"] == "" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNoConnectionIsOffline(t *testing.T) {
	providers, srv := local(t, func(http.ResponseWriter, *http.Request) {})
	srv.Close() // nothing listens any more: the connection is refused
	_, err := httpsource.New("v", providers, refuseNetwork{t}).Fetch(context.Background(), "SYP")
	if errs.CodeOf(err) != domain.CodeFetchOffline {
		t.Fatalf("err = %v", err)
	}
}

func TestACancelledContextStopsTrying(t *testing.T) {
	var calls atomic.Int32
	providers, _ := local(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := httpsource.New("v", providers, refuseNetwork{t}).Fetch(ctx, "SYP"); errs.CodeOf(err) != domain.CodeFetchOffline {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d providers were tried after the context ended", calls.Load())
	}
}

// TestTheRealProvidersAnswer calls the real internet. It runs only with LITE_FX_LIVE=1 and is otherwise NOT RUN — never
// counted as passed.
func TestTheRealProvidersAnswer(t *testing.T) {
	if os.Getenv("LITE_FX_LIVE") != "1" {
		t.Skip("NOT RUN: set LITE_FX_LIVE=1 to call the real rate providers")
	}
	for _, p := range httpsource.Providers() {
		t.Run(p.Name, func(t *testing.T) {
			q, err := httpsource.New("live-test", []httpsource.Provider{p}, nil).Fetch(context.Background(), "SYP")
			if err != nil {
				t.Fatalf("%s: %v", p.Name, err)
			}
			t.Logf("%s: %s SYP per USD", p.Name, domain.FormatRate(q.Nano))
			// Every provider, scaled, is in old-pound thousands (Q-L3.4).
			if q.Nano < 1_000_000_000_000 || q.Nano > 100_000_000_000_000 {
				t.Errorf("%s answered %s, outside 1,000–100,000: its scale has changed", p.Name, domain.FormatRate(q.Nano))
			}
		})
	}
}
