package httpsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// The local market source, pre-configured (the owner's request, 2026-09-17).
//
// # What this is, and what it is not
//
// sp-today.com publishes the Syrian pound's market rate city by city. It has no API: the figures arrive inside the page's
// own streaming payload, as escaped JSON. This reads that payload.
//
// That makes it a SCRAPER, and a scraper is a promise someone else can break without telling you. It will stop working
// the day the site is rebuilt. Three things make that survivable rather than dangerous:
//
//   - The published providers sit behind it (Client.TryFirst). When this returns nothing the shop is still priced, by the
//     official rate, and the fetch log says which one answered.
//   - The parse is pinned by a golden fixture of the real page (testdata/sp-today.html), so a change in shape fails a test
//     here rather than a shop's prices there.
//   - Every figure still passes the 20% guard: a scraper that starts reading gold prices instead of dollars is refused,
//     not applied.
//
// The city is Damascus and the figure is the SELL price — what the market charges for a dollar, which is what a shop pays
// when it buys the stock it prices in dollars.
const (
	// SPToday is the provider name recorded on a fetch and shown beside the rate.
	SPToday = "sp-today-damascus"
	// SPTodayURL is the page the figures live in.
	SPTodayURL = "https://sp-today.com/"
	// SPTodayCity is the city whose rate is read.
	SPTodayCity = "damascus"
)

// spTodayUSD marks the dollar's entry in the payload. The payload is escaped JSON inside a JavaScript string, so the
// quotes are \" rather than ".
var spTodayUSD = []byte(`\"symbol\":\"USD\"`)

// spTodayCityRates pulls the city's buy and sell out of the window that follows.
var spTodayCityRates = regexp.MustCompile(`\\"` + SPTodayCity + `\\":\{\\"buy\\":(-?[0-9.]+),\\"sell\\":(-?[0-9.]+)`)

// spTodayWindow is how far after the dollar's symbol its own city rates are looked for. Far enough to cover the entry,
// short enough that the NEXT currency's rates can never be read as the dollar's.
const spTodayWindow = 600

var errSPTodayShape = errors.New("the market page no longer carries a Damascus rate for this currency in the shape this reads")

// SPTodayProvider is the local market source, ready to use with no address from the shop.
func SPTodayProvider() Provider {
	return Provider{
		Name:  SPToday,
		URL:   SPTodayURL,
		Scale: 1, // the page quotes the pounds a counter already uses
		read:  readSPToday,
	}
}

// readSPToday reads the market's selling price for one dollar, in the local currency's own units.
//
// It only ever reads USD: the page prices many currencies against the pound, and this application quotes exactly one pair
// (DESIGN: USD and the local currency). A shop whose local currency is not the pound gets nothing from here, and the
// published providers answer instead.
func readSPToday(body []byte, localCurrency string) (string, error) {
	if !strings.EqualFold(localCurrency, "SYP") {
		return "", errors.New("the market page prices the Syrian pound only")
	}
	at := bytes.Index(body, spTodayUSD)
	if at < 0 {
		return "", errSPTodayShape
	}
	window := body[at:min(at+spTodayWindow, len(body))]
	rates := spTodayCityRates.FindSubmatch(window)
	if rates == nil {
		return "", errSPTodayShape
	}
	// json.Number rather than a float: the exact text is what the rate is parsed from, as with every other provider.
	var number json.Number
	if err := json.Unmarshal(rates[2], &number); err != nil {
		return "", errSPTodayShape
	}
	return number.String(), nil
}
