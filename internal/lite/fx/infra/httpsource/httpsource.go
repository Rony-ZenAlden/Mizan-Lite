// Package httpsource fetches the exchange rate from the internet (L3 §14.6). It is the only Lite package allowed to
// import net/http (lite-network-only-in-httpsource).
//
// # What the providers actually report
//
// Checked on 2026-09-14 for USD → SYP: the @fawazahmed0 currency API reports 13,007.54 — the old pound, an official-style
// figure — while open.er-api.com reports 121.96, the redenominated pound under the same code. So the providers are
// ordered and scaled explicitly, and the fx module's 20% guard keeps any scale mistake — ours or a provider's future
// change — from being applied without the owner. Neither is the shop's market rate (L3 §14.3 F2).
package httpsource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

const (
	// Timeout bounds one provider's answer. The fetch runs outside any transaction (D-L3.21), so it holds nothing.
	Timeout = 10 * time.Second
	// maxBody bounds a response: the providers' full answers are well under 100 KB.
	maxBody = 1 << 20
)

// Provider is one place a rate can come from.
type Provider struct {
	// Name is stored on the fetch and shown with the rate; the screen words it from the catalog.
	Name string
	URL  string
	// Scale multiplies the provider's figure into the local currency's units the shop quotes in (Q-L3.4).
	Scale int64
	// read extracts the rate's decimal text for a currency code from a response body.
	read func(body []byte, localCurrency string) (string, error)
}

// The provider names.
const (
	CurrencyAPIJsDelivr = "currency-api-jsdelivr"
	CurrencyAPIPages    = "currency-api-pages"
	ExchangeRateAPIOpen = "exchangerate-api-open"
)

// Providers are the real providers, in the order they are tried.
func Providers() []Provider {
	return []Provider{
		{Name: CurrencyAPIJsDelivr, URL: "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json", Scale: 1, read: readCurrencyAPI},
		{Name: CurrencyAPIPages, URL: "https://latest.currency-api.pages.dev/v1/currencies/usd.json", Scale: 1, read: readCurrencyAPI},
		// It reports the redenominated pound: ×100 to the pounds the counter quotes (L3 §14.3 F1).
		{Name: ExchangeRateAPIOpen, URL: "https://open.er-api.com/v6/latest/USD", Scale: 100, read: readExchangeRateAPI},
	}
}

// Client tries each provider in order until one answers.
type Client struct {
	http      *http.Client
	providers []Provider
	userAgent string
}

// New builds a client. transport nil uses a clone of the default transport, which honours the operating system's proxy
// settings and verifies certificates against the system store on Windows and macOS.
func New(version string, providers []Provider, transport http.RoundTripper) *Client {
	if transport == nil {
		base, _ := http.DefaultTransport.(*http.Transport)
		clone := base.Clone()
		clone.Proxy = http.ProxyFromEnvironment
		transport = clone
	}
	return &Client{
		http:      &http.Client{Timeout: Timeout, Transport: transport},
		providers: providers,
		userAgent: "MizanLite/" + version,
	}
}

// Fetch implements fx.Source: the first provider that answers wins; if none does, the last failure is returned, typed as
// offline or failed.
func (c *Client) Fetch(ctx context.Context, localCurrency string) (domain.Quote, error) {
	var last error = errs.Conflict(domain.CodeFetchFailed, "no provider is configured")
	for _, p := range c.providers {
		q, err := c.fetchOne(ctx, p, localCurrency)
		if err == nil {
			return q, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return domain.Quote{}, last
}

func (c *Client) fetchOne(ctx context.Context, p Provider, localCurrency string) (domain.Quote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return domain.Quote{}, failed(p, "request", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if isOffline(err) {
			return domain.Quote{}, errs.Wrap(err, errs.CategoryConflict, domain.CodeFetchOffline, "the rate provider could not be reached").
				WithParam("provider", p.Name)
		}
		return domain.Quote{}, failed(p, "transport", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return domain.Quote{}, errs.Conflict(domain.CodeFetchFailed, "the rate provider answered with an error").
			WithParam("provider", p.Name).WithParam("status", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return domain.Quote{}, failed(p, "read", err)
	}
	if len(body) > maxBody {
		return domain.Quote{}, errs.Conflict(domain.CodeFetchFailed, "the rate provider's answer is too large").WithParam("provider", p.Name)
	}
	text, err := p.read(body, localCurrency)
	if err != nil {
		return domain.Quote{}, failed(p, "parse", err)
	}
	value, ok := domain.QuoteFromDecimal(text, p.Scale)
	if !ok {
		return domain.Quote{}, errs.Conflict(domain.CodeFetchFailed, "the rate provider's figure is not a positive rate").
			WithParam("provider", p.Name).WithParam("value", text)
	}
	return domain.Quote{Provider: p.Name, Nano: value}, nil
}

var errMissing = errors.New("the currency is missing from the answer")

// readCurrencyAPI reads {"date": "…", "usd": {"syp": 13007.53553648, …}}.
func readCurrencyAPI(body []byte, localCurrency string) (string, error) {
	var doc struct {
		USD map[string]json.Number `json:"usd"`
	}
	if err := decode(body, &doc); err != nil {
		return "", err
	}
	v, ok := doc.USD[strings.ToLower(localCurrency)]
	if !ok {
		return "", errMissing
	}
	return v.String(), nil
}

// readExchangeRateAPI reads {"result": "success", "base_code": "USD", "rates": {"SYP": 121.957862, …}}.
func readExchangeRateAPI(body []byte, localCurrency string) (string, error) {
	var doc struct {
		Result   string                 `json:"result"`
		BaseCode string                 `json:"base_code"`
		Rates    map[string]json.Number `json:"rates"`
	}
	if err := decode(body, &doc); err != nil {
		return "", err
	}
	if doc.Result != "success" || doc.BaseCode != domain.USD {
		return "", errors.New("the answer is not a successful USD table")
	}
	v, ok := doc.Rates[strings.ToUpper(localCurrency)]
	if !ok {
		return "", errMissing
	}
	return v.String(), nil
}

// decode reads JSON with numbers kept as their text, so no float ever holds a rate.
func decode(body []byte, into any) error {
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.UseNumber()
	return d.Decode(into)
}

func failed(p Provider, stage string, err error) error {
	return errs.Wrap(err, errs.CategoryConflict, domain.CodeFetchFailed, "the rate could not be fetched").
		WithParam("provider", p.Name).WithParam("stage", stage)
}

// isOffline reports a failure to reach the provider at all: no network, no DNS, a refused connection, a timeout.
func isOffline(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	var opErr *net.OpError
	return errors.As(err, &dnsErr) || errors.As(err, &opErr) || errors.Is(err, context.DeadlineExceeded)
}
