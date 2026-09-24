package api

import (
	"context"
	"image"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// words is what a printout or export is written in: the interface's language and direction, the shop's time zone, the
// settings of the moment. Every label comes from the catalogs the screens use; every figure is isolated (D-L7.3).
type words struct {
	app      *bootstrap.App
	loc      locale.Locale
	dir      documents.Direction
	tz       *time.Location
	ts       *typeset.Typesetter
	settings settingsdomain.Settings
	// logo is the shop's logo, nil when it has none: every document's header draws it (0.10.0).
	logo image.Image
}

func newWords(ctx context.Context, app *bootstrap.App) (words, error) {
	current, err := app.Settings.Get(ctx)
	if err != nil {
		return words{}, err
	}
	ts, err := typeset.Default()
	if err != nil {
		return words{}, err
	}
	w := words{app: app, loc: locale.Locale(current.Locale), dir: documents.RTL, tz: app.Location(), ts: ts, settings: current}
	if current.Locale == settingsdomain.English {
		w.dir = documents.LTR
	}
	stored, found, err := app.Settings.Logo(ctx)
	if err != nil {
		return words{}, err
	}
	if found {
		// A logo that cannot be decoded prints the name in type instead: a damaged picture must not stop a receipt.
		if img, decodeErr := settingsdomain.DecodeLogo(stored); decodeErr == nil {
			w.logo = img
		}
	}
	return w, nil
}

// usdOnly reports whether the shop has gone over to dollars only (0.10.0): its documents then carry no pound figure — no
// rate line, no pound column, no pound reading of a dollar amount (0.10.1).
func (w words) usdOnly() bool { return moneyfmt.Parse(w.settings.MoneyDisplay) == moneyfmt.USD }

// t translates a key; params are key–value pairs.
func (w words) t(key string, params ...string) string {
	return w.app.Messages.T(w.loc, key, i18n.Param(params...))
}

func (w words) fig(s string) string { return typeset.Isolate(s) }

// user is text the shop typed — a name, a note, a reason — isolated in its own first strong direction, so its digits and
// punctuation cannot reorder the line around it (U+2068 … U+2069).
func (w words) user(s string) string {
	if s == "" {
		return s
	}
	return "\u2068" + s + "\u2069"
}

func (w words) short(code string) string { return w.t("currency.short." + code) }

// money is an amount Go formatted with its currency's short name.
func (w words) money(value, code string) string {
	if value == "" {
		return ""
	}
	return documents.Money(value, w.short(code))
}

// when is a stored timestamp in the shop's time, isolated.
func (w words) when(stamp string) string {
	at, ok := clock.ParseTimestamp(stamp)
	if !ok {
		return w.fig(stamp)
	}
	return w.fig(at.In(w.tz).Format(timeLayout))
}

func (w words) now() string { return w.fig(w.app.Now().In(w.tz).Format(timeLayout)) }

// timeLayout is how the shop reads a moment, on paper as on screen: day/month/year and a 24-hour time, digits only (L8 Q-L8.6).
const timeLayout = "02/01/2006 15:04"

// date is a business date ("2026-09-15") as the shop reads it ("15/09/2026"), isolated; a month ("2026-09") as "09/2026".
func (w words) date(d string) string { return w.fig(shopDate(d)) }

// shopDate reorders a business date or month without isolating it — for workbook cells, which carry no bidi marks.
func shopDate(d string) string {
	if len(d) == 10 && d[4] == '-' && d[7] == '-' {
		return d[8:10] + "/" + d[5:7] + "/" + d[0:4]
	}
	if len(d) == 7 && d[4] == '-' {
		return d[5:7] + "/" + d[0:4]
	}
	return d
}

// amount is a figure grouped and isolated with no currency name, for a column whose heading names the currency.
func (w words) amount(value string) string {
	if value == "" {
		return ""
	}
	return w.fig(documents.Group(value))
}

func (w words) unit(code string) string { return w.t("uom." + code) }

// name is a product's name in the interface's language when it has one.
func (w words) name(ar, en string) string {
	if w.loc == locale.Locale(settingsdomain.English) && en != "" {
		return w.user(en)
	}
	return w.user(ar)
}

func (w words) english() bool { return w.loc == locale.Locale(settingsdomain.English) }
