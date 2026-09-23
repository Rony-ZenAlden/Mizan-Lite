package api

import (
	"context"
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/alerts"
	alertsdomain "github.com/mizan-erp/mizan/internal/lite/alerts/domain"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Alerts is the notification engine as the screen reads it (the owner's request, 2026-09-23): one call answers the bell,
// the toasts and the notification centre, so the three cannot disagree.
type Alerts struct{ core *core }

// NotificationDTO is one entry the bell counts. The screen marks Key+Fingerprint read, so a notification that gets
// worse — a new rate, a deeper capital shift — is unread again, and one that is merely still true is not repeated.
type NotificationDTO struct {
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	Severity    string `json:"severity"`
	// ProductID is set for a notification about one product; NameAR/NameEN name it for a toast.
	ProductID string `json:"productId"`
	NameAR    string `json:"nameAr"`
	NameEN    string `json:"nameEn"`
	// Count is how many items a summary notification stands for (stale prices).
	Count int `json:"count"`
}

// LowStockDTO is one product at or below the level the shop set.
type LowStockDTO struct {
	ProductID string `json:"productId"`
	NameAR    string `json:"nameAr"`
	NameEN    string `json:"nameEn"`
	UnitCode  string `json:"unitCode"`
	OnHand    string `json:"onHand"`
	Level     string `json:"level"`
}

// StaleDTO is one price the exchange rate has left behind, with the price proposed for it. Replacement and Loss are
// the owner's: they are empty and false outside owner mode when the shop has PIN protection on.
type StaleDTO struct {
	ProductID   string `json:"productId"`
	NameAR      string `json:"nameAr"`
	NameEN      string `json:"nameEn"`
	Currency    string `json:"currency"`
	Price       string `json:"price"`
	Shift       string `json:"shift"`
	Proposed    string `json:"proposed"`
	Replacement string `json:"replacement"`
	Loss        bool   `json:"loss"`
}

// CapitalDTO is what the shop is worth in dollars now and about a month ago, and what the pounds it held lost to the
// rate over the same days.
type CapitalDTO struct {
	ThenDate     string `json:"thenDate"`
	NowDate      string `json:"nowDate"`
	ThenUSD      string `json:"thenUsd"`
	NowUSD       string `json:"nowUsd"`
	Change       string `json:"change"`
	ThenRate     string `json:"thenRate"`
	NowRate      string `json:"nowRate"`
	RateShift    string `json:"rateShift"`
	Depreciation string `json:"depreciation"`
	// Rising is true when the shop is worth more in dollars than it was.
	Rising bool `json:"rising"`
}

// AlertsDTO is everything the engine found.
type AlertsDTO struct {
	Notifications []NotificationDTO `json:"notifications"`
	LowStock      []LowStockDTO     `json:"lowStock"`
	Stale         []StaleDTO        `json:"stale"`
	LossCount     int               `json:"lossCount"`
	// Capital is set only when HasCapital: there is a week of history and the viewer may see it. A value with a flag,
	// never a null — a null crossing the boundary once crashed a screen (L8's lesson).
	Capital    CapitalDTO `json:"capital"`
	HasCapital bool       `json:"hasCapital"`
	// HistoryDays is how far back the daily snapshots reach, and HistoryNeeded how far they must before there is a
	// comparison to show: "collecting history, 3 of 7 days".
	HistoryDays   int `json:"historyDays"`
	HistoryNeeded int `json:"historyNeeded"`
	// Backup is "" when the shop's own backups are fine, else "none" or "old".
	Backup string `json:"backup"`
	// OwnerHidden is true when an owner's figure was held back: PIN protection is on and nobody is in owner mode.
	OwnerHidden   bool   `json:"ownerHidden"`
	LocalCurrency string `json:"localCurrency"`
}

// Current is what deserves the shop's attention right now.
func (a *Alerts) Current() envelope.Result[AlertsDTO] {
	return call(a.core, "Alerts.Current", func(ctx context.Context, app *bootstrap.App) (AlertsDTO, error) {
		r, err := app.Alerts.Current(ctx)
		if err != nil {
			return AlertsDTO{}, err
		}
		v, err := newTillView(ctx, app)
		if err != nil {
			return AlertsDTO{}, err
		}
		return v.alerts(r), nil
	})
}

func (v tillView) alerts(r alerts.Report) AlertsDTO {
	out := AlertsDTO{Notifications: []NotificationDTO{}, LowStock: []LowStockDTO{}, Stale: []StaleDTO{},
		HistoryDays: r.HistoryDays, HistoryNeeded: alertsdomain.MinHistoryDays, OwnerHidden: r.OwnerHidden,
		LocalCurrency: r.LocalCurrency}

	names := map[string][2]string{}
	for _, l := range r.Facts.Low {
		p := l.Product
		names[p.ID.String()] = [2]string{p.NameAR, p.NameEN}
		out.LowStock = append(out.LowStock, LowStockDTO{ProductID: p.ID.String(), NameAR: p.NameAR, NameEN: p.NameEN,
			UnitCode: p.UnitCode, OnHand: numinput.FormatFixed(l.OnHandMicro, 6, p.UnitDecimals),
			Level: numinput.FormatFixed(p.ReorderMicro, 6, p.UnitDecimals)})
	}
	for _, s := range r.Facts.Stale {
		d := v.ref.Currencies[s.Currency].Decimals
		item := StaleDTO{ProductID: s.ProductID.String(), NameAR: s.NameAR, NameEN: s.NameEN, Currency: s.Currency,
			Price:    v.shop.Display(numinput.FormatFixed(s.PriceMicro, 6, d), s.Currency),
			Proposed: v.shop.Display(numinput.FormatFixed(s.ProposedMicro, 6, d), s.Currency),
			Shift:    signedPercent(s.ShiftMicro), Loss: s.Loss}
		if s.CostKnown {
			item.Replacement = v.shop.Display(numinput.FormatFixed(s.ReplacementMicro, 6, d), s.Currency)
		}
		if s.Loss {
			out.LossCount++
		}
		out.Stale = append(out.Stale, item)
	}
	if c := r.Facts.Capital; c != nil {
		out.HasCapital = true
		out.Capital = CapitalDTO{ThenDate: c.Then.BusinessDate, NowDate: c.Now.BusinessDate,
			ThenUSD: v.money(c.ThenUSD, "USD"), NowUSD: v.money(c.NowUSD, "USD"), Change: signedPercent(c.ChangeMicro),
			ThenRate: rateText(v.shop, c.Then.RateNano), NowRate: rateText(v.shop, c.Now.RateNano),
			RateShift: signedPercent(c.RateShiftMicro), Depreciation: v.money(c.DepreciationUSD, "USD"),
			Rising: c.ChangeMicro > 0}
	}
	for _, n := range r.Notifications {
		dto := NotificationDTO{Key: n.Key, Fingerprint: n.Fingerprint, Kind: string(n.Kind), Severity: string(n.Severity)}
		switch n.Kind {
		case alertsdomain.KindLowStock:
			dto.ProductID = n.ProductID.String()
			dto.NameAR, dto.NameEN = names[dto.ProductID][0], names[dto.ProductID][1]
		case alertsdomain.KindStalePrices:
			dto.Count = len(out.Stale)
		case alertsdomain.KindBackupNone:
			out.Backup = "none"
		case alertsdomain.KindBackupOld:
			out.Backup = "old"
		}
		out.Notifications = append(out.Notifications, dto)
	}
	return out
}

// signedPercent is a percentage at 10⁻⁶ of a point as a person reads it, one decimal, with its sign: "+10.0", "-4.5".
func signedPercent(micro int64) string {
	sign := "+"
	if micro < 0 {
		sign, micro = "-", -micro
	}
	// Round half up to one decimal: 10⁵ micro is a tenth of a point.
	tenths := (micro + 50_000) / 100_000
	return sign + strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10)
}
