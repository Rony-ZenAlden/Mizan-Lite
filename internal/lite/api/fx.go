package api

import (
	"context"
	"math"
	"time"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// FX is the exchange rate: fetched from the internet in automatic mode, the owner's manual rate as the fallback and
// override (L3 §14). Rates cross as decimal strings formatted by Go; an age crosses as whole seconds.
type FX struct{ core *core }

// FetchDTO is one attempt to fetch the rate.
type FetchDTO struct {
	ID          string `json:"id"`
	AttemptedAt string `json:"attemptedAt"`
	// AgeSeconds is how long ago the attempt was, measured by Go, so the webview's clock is never compared with it.
	AgeSeconds int64  `json:"ageSeconds"`
	Outcome    string `json:"outcome"`
	Provider   string `json:"provider"`
	Rate       string `json:"rate"`
	ErrorCode  string `json:"errorCode"`
	// Change is the fetched rate's change from the rate in force, in percent ("-13.3"), or "".
	Change string `json:"change"`
	// EffectiveRate is what the shop's margin makes of Rate — equal to Rate when there is no margin (2026-09-17).
	EffectiveRate string `json:"effectiveRate"`
	// Acceptable is true when the owner may still accept this proposal.
	Acceptable bool `json:"acceptable"`
}

// RateDTO is the exchange rate in force.
type RateDTO struct {
	// Set is false when no rate has been recorded — a state, never a rate of 1 (L3 §4.4).
	Set           bool   `json:"set"`
	LocalCurrency string `json:"localCurrency"`
	Rate          string `json:"rate"`
	Source        string `json:"source"`
	RecordedAt    string `json:"recordedAt"`
	AgeSeconds    int64  `json:"ageSeconds"`
	Stale         bool   `json:"stale"`
	Mode          string `json:"mode"`
	// AdjustPercent is the margin the shop puts on the internet's rate, as typed ("5", "-2.5"), or "0".
	AdjustPercent string `json:"adjustPercent"`
	// CanFetch is false when the application has no rate provider.
	CanFetch  bool     `json:"canFetch"`
	HasFetch  bool     `json:"hasFetch"`
	LastFetch FetchDTO `json:"lastFetch"`
}

// RateHistoryDTO is one recorded rate.
type RateHistoryDTO struct {
	ID         string `json:"id"`
	Rate       string `json:"rate"`
	Source     string `json:"source"`
	Provider   string `json:"provider"`
	RecordedAt string `json:"recordedAt"`
	Change     string `json:"change"`
	Note       string `json:"note"`
}

// SetRateInput is a rate the owner typed.
type SetRateInput struct {
	Rate               string `json:"rate"`
	Note               string `json:"note"`
	ConfirmLargeChange bool   `json:"confirmLargeChange"`
}

// QuoteDTO is a rate fetched and not recorded.
type QuoteDTO struct {
	Provider string `json:"provider"`
	Rate     string `json:"rate"`
}

func ageSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Floor(d.Seconds()))
}

func toFetchDTO(f fxdomain.Fetch, shop moneyfmt.Shop) FetchDTO {
	dto := FetchDTO{ID: f.ID.String(), AttemptedAt: clock.Format(f.AttemptedAt), Outcome: string(f.Outcome), Provider: f.Provider, ErrorCode: f.ErrorCode}
	if f.Outcome != fxdomain.OutcomeFailed {
		dto.Rate = rateText(shop, f.Nano)
	}
	return dto
}

// toRateDTO reads the rate as the shop reads its own currency (L10): a rate is local money per dollar.
func toRateDTO(c fx.Current, canFetch bool, shop moneyfmt.Shop) RateDTO {
	dto := RateDTO{Set: c.Found, LocalCurrency: c.Local, Mode: string(c.Mode), CanFetch: canFetch, HasFetch: c.Fetched,
		AdjustPercent: settingsdomain.FormatRateAdjustPercent(c.AdjustPercentMicro)}
	if c.Found {
		dto.Rate = rateText(shop, c.Rate.Nano)
		dto.Source = string(c.Rate.Source)
		dto.RecordedAt = clock.Format(c.Rate.RecordedAt)
		dto.AgeSeconds = ageSeconds(c.Age)
		dto.Stale = c.Stale
	}
	if c.Fetched {
		dto.LastFetch = toFetchDTO(c.Fetch, shop)
		dto.LastFetch.Change = c.FetchChange
		dto.LastFetch.AgeSeconds = ageSeconds(c.FetchAge)
		dto.LastFetch.Acceptable = c.Acceptable
		dto.LastFetch.EffectiveRate = rateText(shop, c.Fetch.EffectiveNano)
	}
	return dto
}

// current reads the rate in force as a DTO.
func (f *FX) current(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
	c, err := app.FX.Current(ctx)
	if err != nil {
		return RateDTO{}, err
	}
	shop, err := moneyShop(ctx, app)
	return toRateDTO(c, app.FX.CanFetch(), shop), err
}

// Current is the rate in force, its age, the mode and the last fetch. Everyone may read it.
func (f *FX) Current() envelope.Result[RateDTO] {
	return call(f.core, "FX.Current", f.current)
}

// ActRateAdjust names the margin change in the owner's history.
const ActRateAdjust = "fx.adjust.set"

// SetAdjustPercent sets the margin the shop puts on the internet's rate (the owner's request, 2026-09-17): a shop that
// sells dollars above the published figure sets it once here instead of retyping a rate every morning.
//
// It changes nothing about a rate typed by hand, and nothing in manual mode — where no fetched rate is applied at all.
func (f *FX) SetAdjustPercent(percent string) envelope.Result[RateDTO] {
	return call(f.core, "FX.SetAdjustPercent", func(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
		value, err := settingsdomain.ParseRateAdjustPercent(percent)
		if err != nil {
			return RateDTO{}, err
		}
		text := settingsdomain.FormatRateAdjustPercent(value)
		err = app.DB.Do(ctx, func(ctx context.Context) error {
			if _, updateErr := app.Settings.Update(ctx, settingsdomain.Update{RateAdjustPercent: &text}); updateErr != nil {
				return updateErr
			}
			return requireOwner(ctx, app, ActRateAdjust, text)
		})
		if err != nil {
			return RateDTO{}, err
		}
		return f.current(ctx, app)
	})
}

// History is the recorded rates, newest first, each with its change from the one before.
func (f *FX) History(limit int) envelope.Result[[]RateHistoryDTO] {
	return call(f.core, "FX.History", func(ctx context.Context, app *bootstrap.App) ([]RateHistoryDTO, error) {
		rows, err := app.FX.History(ctx, limit)
		if err != nil {
			return nil, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return nil, err
		}
		out := make([]RateHistoryDTO, 0, len(rows))
		for _, r := range rows {
			dto := RateHistoryDTO{
				ID: r.Rate.ID.String(), Rate: rateText(shop, r.Rate.Nano), Source: string(r.Rate.Source),
				RecordedAt: clock.Format(r.Rate.RecordedAt), Change: r.Change, Note: r.Rate.Note,
			}
			if r.Rate.FetchID != "" {
				fetch, err := app.FX.FetchOf(ctx, r.Rate.FetchID)
				if err != nil {
					return nil, err
				}
				dto.Provider = fetch.Provider
			}
			out = append(out, dto)
		}
		return out, nil
	})
}

// SetRate records the owner's rate. Outside owner mode it returns lite.owner.required; a change beyond 20% returns
// lite.fx.large_change until resent with confirmLargeChange.
func (f *FX) SetRate(in SetRateInput) envelope.Result[RateDTO] {
	return call(f.core, "FX.SetRate", func(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return RateDTO{}, err
		}
		// A rate is local currency per dollar, so a shop reading new pounds types 150 and the books record 15,000 (L10).
		if _, err := app.FX.SetRate(ctx, fx.SetRateInput{Rate: shop.Base(in.Rate, shop.Local), Note: in.Note, ConfirmLargeChange: in.ConfirmLargeChange}); err != nil {
			return RateDTO{}, err
		}
		return f.current(ctx, app)
	})
}

// Refresh fetches the rate now and applies what the rules allow. Everyone may ask.
func (f *FX) Refresh() envelope.Result[RateDTO] {
	return call(f.core, "FX.Refresh", func(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
		if _, err := app.FX.Refresh(ctx); err != nil {
			return RateDTO{}, err
		}
		return f.current(ctx, app)
	})
}

// AcceptProposal records a proposed fetched rate. Owner only.
func (f *FX) AcceptProposal(fetchID string) envelope.Result[RateDTO] {
	return call(f.core, "FX.AcceptProposal", func(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
		parsed, err := id.Parse(fetchID)
		if err != nil {
			return RateDTO{}, notAFetch()
		}
		if _, err := app.FX.AcceptProposal(ctx, parsed); err != nil {
			return RateDTO{}, err
		}
		return f.current(ctx, app)
	})
}

// SetMode switches between "automatic" and "manual". Owner only.
func (f *FX) SetMode(mode string) envelope.Result[RateDTO] {
	return call(f.core, "FX.SetMode", func(ctx context.Context, app *bootstrap.App) (RateDTO, error) {
		if _, err := app.FX.SetMode(ctx, mode); err != nil {
			return RateDTO{}, err
		}
		return f.current(ctx, app)
	})
}

// FetchQuote fetches a rate and records nothing — for first run's "Fetch from the internet".
func (f *FX) FetchQuote() envelope.Result[QuoteDTO] {
	return call(f.core, "FX.FetchQuote", func(ctx context.Context, app *bootstrap.App) (QuoteDTO, error) {
		q, err := app.FX.Quote(ctx)
		shop, shopErr := moneyShop(ctx, app)
		if shopErr != nil {
			return QuoteDTO{}, shopErr
		}
		return QuoteDTO{Provider: q.Provider, Rate: rateText(shop, q.Nano)}, err
	})
}

func notAFetch() error {
	return errs.NotFound(fxdomain.CodeFetchNotFound, "no such fetch")
}
