package httpsource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/fx/infra/httpsource"
)

// TestTheMarketPageIsReadForDamascusDollars pins the parse against a real page saved on 2026-09-17. When sp-today is
// rebuilt this test fails here, which is the point: a shop finds out from the fallback, not from a wrong price.
func TestTheMarketPageIsReadForDamascusDollars(t *testing.T) {
	page, err := os.ReadFile("testdata/sp-today.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(page)
	}))
	defer server.Close()

	provider := httpsource.SPTodayProvider()
	provider.URL = server.URL
	client := httpsource.New("t", []httpsource.Provider{provider}, nil)

	q, err := client.Fetch(context.Background(), "SYP")
	if err != nil {
		t.Fatal(err)
	}
	// The saved page quotes Damascus at 13,600 buy / 13,675 sell. A shop pays the sell price for its dollars.
	if q.Nano != 13_675*1_000_000_000 {
		t.Fatalf("the Damascus sell price = %d, want 13,675", q.Nano)
	}
	if q.Provider != httpsource.SPToday {
		t.Fatalf("provider = %q", q.Provider)
	}
}

// The page prices many currencies against the pound; only the dollar is this application's pair.
func TestTheMarketPageIsNotReadForACurrencyItDoesNotPrice(t *testing.T) {
	page, _ := os.ReadFile("testdata/sp-today.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(page)
	}))
	defer server.Close()
	provider := httpsource.SPTodayProvider()
	provider.URL = server.URL
	client := httpsource.New("t", []httpsource.Provider{provider}, nil)

	if _, err := client.Fetch(context.Background(), "TRY"); err == nil {
		t.Fatal("a currency the page does not price was read anyway")
	}
}

// A page that has been rebuilt fails here rather than silently returning a wrong number.
func TestARebuiltMarketPageFailsRatherThanGuessing(t *testing.T) {
	for name, body := range map[string]string{
		"an empty page":        "",
		"a page without rates": "<html><body>الصفحة قيد الصيانة</body></html>",
		"the shape changed":    `<script>self.__next_f.push([1,"{\"symbol\":\"USD\",\"cities\":{\"aleppo\":{\"buy\":1,\"sell\":2}}}"])</script>`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		provider := httpsource.SPTodayProvider()
		provider.URL = server.URL
		client := httpsource.New("t", []httpsource.Provider{provider}, nil)
		if _, err := client.Fetch(context.Background(), "SYP"); err == nil {
			t.Errorf("%s was read as a rate", name)
		}
		server.Close()
	}
}

// TestTheMarketPageStillAnswers reaches the real site. Opt-in, like the live provider check (L3 D-L3.26): CI never leaves
// the machine, and this is how a person finds out the scraper has gone stale before a shop does.
//
//	LITE_FX_LIVE=1 go test ./internal/lite/fx/infra/httpsource/ -run TestTheMarketPageStillAnswers -v
func TestTheMarketPageStillAnswers(t *testing.T) {
	if os.Getenv("LITE_FX_LIVE") != "1" {
		t.Skip("set LITE_FX_LIVE=1 to reach the real market page")
	}
	client := httpsource.New("live", []httpsource.Provider{httpsource.SPTodayProvider()}, nil)
	q, err := client.Fetch(context.Background(), "SYP")
	if err != nil {
		t.Fatalf("the market page no longer answers in the shape Mizan reads: %v", err)
	}
	t.Logf("Damascus sells a dollar at %d nano", q.Nano)
	if q.Nano <= 0 {
		t.Fatal("a rate of nothing")
	}
}
