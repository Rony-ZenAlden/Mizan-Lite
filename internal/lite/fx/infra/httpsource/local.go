package httpsource

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// LocalMarket is the name a locally-sourced rate is recorded under.
const LocalMarket = "local-market"

// The local market source (the owner's request, 2026-09-17).
//
// # Why there is no built-in endpoint
//
// A pantry shop in Syria prices at the market rate, not the published one — PROGRESS O10 found the free providers quote an
// official-style figure 13% below the market, which is why manual mode became the default. A local-market endpoint would
// fix that, but there is no agreed one to build against: O11 is still open, and inventing a URL here would ship a provider
// that fails silently on the first day.
//
// So the shop supplies the endpoint. This reads JSON only: a black-market page scraped out of HTML breaks the first time
// the page is restyled, and a shop would find out through a wrong price rather than an error.
//
// The contract is a JSON document with a number somewhere in it, named by a dotted path:
//
//	{"usd": {"sell": 15300}}   with path "usd.sell"
//	{"rate": "15300"}          with path "rate"      (a numeric string is read too)
type LocalConfig struct {
	// URL is the endpoint; empty means the shop has not set one up and the local source is not tried.
	URL string
	// Field is the dotted path to the number. Empty means "rate".
	Field string
}

// CodeLocalRateURLInvalid is a local-market URL that is not an absolute http(s) address.
const CodeLocalRateURLInvalid = "lite.fx.local_url_invalid"

// ParseLocalURL checks the endpoint a shop typed. Empty is allowed: it means the local source is not configured.
func ParseLocalURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errs.Validation(CodeLocalRateURLInvalid, "a local market endpoint is a full http or https address").
			WithField("localRateUrl", CodeLocalRateURLInvalid, "not a URL").WithParam("value", raw)
	}
	return trimmed, nil
}

// LocalProvider is the shop's own endpoint as a provider, so it goes through the same size guards, timeout and body limit
// as every other one.
func LocalProvider(cfg LocalConfig) Provider {
	field := strings.TrimSpace(cfg.Field)
	if field == "" {
		field = "rate"
	}
	return Provider{
		Name:  LocalMarket,
		URL:   cfg.URL,
		Scale: 1, // a local market quotes the pounds the counter already uses
		read: func(body []byte, _ string) (string, error) {
			return readJSONPath(body, field)
		},
	}
}

// readJSONPath walks a dotted path to a number and returns it as decimal text.
//
// The walk is over json.RawMessage, never through `any`: decoding a JSON number into an interface gives a float64, and a
// rate of 13007.53553648 does not survive that intact. The leaf is read as json.Number — the exact text the endpoint
// sent — which is what every other provider here does, and what the no-float rule is there to enforce.
//
// A numeric string is read too: endpoints quote rates both ways, and refusing one would refuse half of them.
func readJSONPath(body []byte, field string) (string, error) {
	current := json.RawMessage(body)
	for _, key := range strings.Split(field, ".") {
		var object map[string]json.RawMessage
		if err := decode(current, &object); err != nil {
			return "", fmt.Errorf("%q is not an object at %q", field, key)
		}
		next, ok := object[key]
		if !ok {
			return "", fmt.Errorf("the endpoint has no %q", field)
		}
		current = next
	}
	var number json.Number
	if err := decode(current, &number); err == nil && number.String() != "" {
		return number.String(), nil
	}
	var text string
	if err := decode(current, &text); err != nil {
		return "", fmt.Errorf("%q is not a number", field)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%q is empty", field)
	}
	return strings.TrimSpace(text), nil
}
