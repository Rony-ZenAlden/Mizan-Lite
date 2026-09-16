// Package fx is Mizan Lite's exchange rate: fetched from the internet in automatic mode, set by the owner in manual mode
// or to override, append-only, with its age (L3, and §14's dual-mode requirement).
package fx

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

// The acts only the owner may do (Q7). Their codes are what the owner's history records.
const (
	ActSetRate        = "fx.rate.set"
	ActAcceptProposal = "fx.rate.accept"
	ActSetMode        = "fx.mode.set"
)

// RefreshInterval is how soon a second Refresh reaches the network again; sooner, it answers from the last attempt.
const RefreshInterval = time.Minute

// MaxHistory bounds a history read.
const MaxHistory = 500

// Store persists rates and fetch attempts. infra/sqlite implements it; fxtest.Fake implements it in memory, and one
// contract suite runs against both.
type Store interface {
	// InForce returns the rate with the highest place for the currency, or found=false.
	InForce(ctx context.Context, localCurrency string) (domain.Rate, bool, error)
	// Rates returns the newest rates first.
	Rates(ctx context.Context, localCurrency string, limit int) ([]domain.Rate, error)
	AppendRate(ctx context.Context, r domain.Rate) error
	// LastFetch returns the attempt with the highest place, or found=false.
	LastFetch(ctx context.Context, localCurrency string) (domain.Fetch, bool, error)
	// Fetch returns one attempt, or domain CodeFetchNotFound.
	Fetch(ctx context.Context, fetchID id.ID) (domain.Fetch, error)
	AppendFetch(ctx context.Context, f domain.Fetch) error
}

// Settings is what fx needs from the settings module: the mode, and the currency the rate prices.
type Settings interface {
	RateMode(ctx context.Context) (domain.Mode, error)
	SetRateMode(ctx context.Context, mode domain.Mode) error
	LocalCurrency(ctx context.Context) (string, error)
	// RateAdjustPercentMicro is the margin the shop puts on the internet's rate, at 10⁻⁶ of a percentage point, signed.
	// Zero leaves a fetched figure exactly as it came (the owner's request, 2026-09-17).
	RateAdjustPercentMicro(ctx context.Context) (int64, error)
}

// Source fetches a quote from the internet. httpsource implements it; tests pass fakes.
type Source interface {
	Fetch(ctx context.Context, localCurrency string) (domain.Quote, error)
}

// OwnerGate is what fx needs from the owner, declared here and satisfied in the composition root.
type OwnerGate interface {
	// Require permits an owner-only act, recording it in the caller's transaction, or refuses.
	Require(ctx context.Context, act GuardedAct) error
}

// GuardedAct describes an owner-only act, for the owner's history.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Transactor runs fn atomically.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the exchange-rate use cases.
type Service struct {
	tx       Transactor
	store    Store
	settings Settings
	gate     OwnerGate
	source   Source // nil: no providers registered — the application offline by construction (D-L3.26)
	clk      clock.Clock
	loc      *time.Location
	newID    func() (id.ID, error)
}

// NewService builds the service. source may be nil.
func NewService(tx Transactor, store Store, settings Settings, gate OwnerGate, source Source, clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, settings: settings, gate: gate, source: source, clk: clk, loc: loc, newID: id.New}
}

// CanFetch reports whether any provider is registered.
func (s *Service) CanFetch() bool { return s.source != nil }

// Current is the rate in force, its age, the mode, and the last fetch.
type Current struct {
	Local   string
	Mode    domain.Mode
	Rate    domain.Rate
	Found   bool
	Age     time.Duration
	Stale   bool
	Fetch   domain.Fetch
	Fetched bool
	// Acceptable is true when the last fetch is a proposal that may still be accepted (L3 §14.5).
	Acceptable bool
	// FetchAge is how long ago the last fetch was attempted — never negative.
	FetchAge time.Duration
	// FetchChange is the last fetch's change from the rate in force it was compared with, in percent, or "".
	FetchChange string
	// AdjustPercentMicro is the shop's margin on the internet's rate, and EffectiveFetchNano what that margin makes of the
	// last published figure — what the shop would charge at if the next fetch applied (2026-09-17).
	AdjustPercentMicro int64
	EffectiveFetchNano int64
}

// Current reads the rate in force with its age — on every path, including a rate that is years old (H5).
func (s *Service) Current(ctx context.Context) (Current, error) {
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return Current{}, err
	}
	mode, err := s.settings.RateMode(ctx)
	if err != nil {
		return Current{}, err
	}
	adjust, err := s.settings.RateAdjustPercentMicro(ctx)
	if err != nil {
		return Current{}, err
	}
	out := Current{Local: local, Mode: mode, AdjustPercentMicro: adjust}
	if out.Rate, out.Found, err = s.store.InForce(ctx, local); err != nil {
		return Current{}, err
	}
	now := s.clk.Now()
	if out.Found {
		out.Age = out.Rate.Age(now)
		out.Stale = out.Rate.Stale(bizdate.Date(now, s.loc))
	}
	if out.Fetch, out.Fetched, err = s.store.LastFetch(ctx, local); err != nil {
		return Current{}, err
	}
	if out.Fetched {
		// Filled in from the margin in force, not stored on the row: the log keeps the published figure (2026-09-17).
		out.Fetch.AdjustPercentMicro = adjust
		out.Fetch.EffectiveNano = domain.Adjust(out.Fetch.Nano, adjust)
		out.EffectiveFetchNano = out.Fetch.EffectiveNano
		if d := now.Sub(out.Fetch.AttemptedAt); d > 0 {
			out.FetchAge = d
		}
		out.Acceptable = out.Fetch.Outcome == domain.OutcomeProposed && out.Fetch.AgainstRateID == inForceID(out.Rate, out.Found)
		if out.Found && out.Fetch.Outcome != domain.OutcomeFailed {
			out.FetchChange = domain.ChangePercent(out.Rate.Nano, out.Fetch.Nano)
		}
	}
	return out, nil
}

// HistoryRow is a rate and its change from the one before it.
type HistoryRow struct {
	Rate   domain.Rate
	Change string // "" for the first rate
}

// History returns the newest rates first, each with its change from the rate before it.
// AllRates returns every recorded rate in place order, oldest first, with the local currency — what the reports read to
// find the rate of a business day (L6 §4.3). A shop records a handful of rates a day; a year is a few thousand rows.
func (s *Service) AllRates(ctx context.Context) ([]domain.Rate, string, error) {
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return nil, "", err
	}
	rates, err := s.store.Rates(ctx, local, allRates)
	for i, j := 0, len(rates)-1; i < j; i, j = i+1, j-1 {
		rates[i], rates[j] = rates[j], rates[i]
	}
	return rates, local, err
}

// allRates is a limit no shop reaches.
const allRates = 1 << 30

func (s *Service) History(ctx context.Context, limit int) ([]HistoryRow, error) {
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxHistory {
		limit = MaxHistory
	}
	// One more than asked, so the oldest row shown still has something to compare with.
	rates, err := s.store.Rates(ctx, local, limit+1)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryRow, 0, min(len(rates), limit))
	for i := 0; i < len(rates) && i < limit; i++ {
		row := HistoryRow{Rate: rates[i]}
		if i+1 < len(rates) {
			row.Change = domain.ChangePercent(rates[i+1].Nano, rates[i].Nano)
		}
		out = append(out, row)
	}
	return out, nil
}

// FetchOf returns one fetch attempt — the provider behind a fetched rate in the history.
func (s *Service) FetchOf(ctx context.Context, fetchID id.ID) (domain.Fetch, error) {
	return s.store.Fetch(ctx, fetchID)
}

// SetRateInput is a rate the owner typed.
type SetRateInput struct {
	Rate string
	Note string
	// ConfirmLargeChange resends a rate refused as a change beyond 20% (L3 §4.2).
	ConfirmLargeChange bool
}

// SetRate records the owner's rate (L3 §7). Owner only; a change beyond 20% needs confirmation. In automatic mode it
// holds for the rest of its business day (D-L3.18).
func (s *Service) SetRate(ctx context.Context, in SetRateInput) (domain.Rate, error) {
	var out domain.Rate
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		value, err := domain.ParseRate(in.Rate)
		if err != nil {
			return err
		}
		note, err := domain.CleanNote(in.Note)
		if err != nil {
			return err
		}
		local, err := s.settings.LocalCurrency(ctx)
		if err != nil {
			return err
		}
		inForce, found, err := s.store.InForce(ctx, local)
		if err != nil {
			return err
		}
		// Refused before the owner is asked: a PIN typed for a rate that will be refused is typed for nothing.
		if found && domain.ChangeExceedsLimit(inForce.Nano, value) && !in.ConfirmLargeChange {
			return domain.LargeChange(inForce.Nano, value)
		}
		before := "—"
		if found {
			before = domain.FormatRate(inForce.Nano)
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActSetRate, Before: before, After: domain.FormatRate(value)}); err != nil {
			return err
		}
		if out, err = s.appendRate(ctx, local, inForce, found, value, domain.SourceManual, "", note); err != nil {
			return err
		}
		// Typing a rate puts the shop in manual mode and leaves it there (the owner's request, 2026-09-17). Before this, a
		// typed rate held only for the rest of its own business day and the hourly fetch resumed the next morning — which
		// is exactly the overwriting the owner asked to be rid of. Switching back is one tap on the Rates screen.
		mode, err := s.settings.RateMode(ctx)
		if err != nil {
			return err
		}
		if mode == domain.ModeAutomatic {
			return s.settings.SetRateMode(ctx, domain.ModeManual)
		}
		return nil
	})
	return out, err
}

// RecordFirstRun records the opening rate inside first run's transaction (L3 §7). No owner mode: first run is where
// the PIN is created, in the same transaction. Refused if any rate exists.
func (s *Service) RecordFirstRun(ctx context.Context, raw string) (domain.Rate, error) {
	value, err := domain.ParseRate(raw)
	if err != nil {
		return domain.Rate{}, err
	}
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return domain.Rate{}, err
	}
	inForce, found, err := s.store.InForce(ctx, local)
	if err != nil {
		return domain.Rate{}, err
	}
	if found {
		return domain.Rate{}, errs.Conflict(domain.CodeAlreadyHasRate, "a rate is already recorded")
	}
	return s.appendRate(ctx, local, inForce, found, value, domain.SourceFirstRun, "", "")
}

// Refresh fetches a rate and decides what it does (L3 §14.5). Anyone may ask; nothing beyond 20% is applied without the
// owner. The network is called OUTSIDE any transaction (D-L3.21), and a second call within RefreshInterval answers from
// the last attempt without calling out.
func (s *Service) Refresh(ctx context.Context) (domain.Fetch, error) {
	if s.source == nil {
		return domain.Fetch{}, errs.Conflict(domain.CodeFetchUnavailable, "no rate provider is configured")
	}
	local, last, recent, err := s.lastAttempt(ctx)
	if err != nil || recent {
		return last, err
	}
	quote, fetchErr := s.source.Fetch(ctx, local)
	return s.record(ctx, local, quote, fetchErr)
}

// lastAttempt reads the local currency and the last fetch, and whether it was recent enough to answer from.
func (s *Service) lastAttempt(ctx context.Context) (string, domain.Fetch, bool, error) {
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return "", domain.Fetch{}, false, err
	}
	last, fetched, err := s.store.LastFetch(ctx, local)
	if err != nil || !fetched {
		return local, domain.Fetch{}, false, err
	}
	since := s.clk.Now().Sub(last.AttemptedAt)
	return local, last, since >= 0 && since < RefreshInterval, nil
}

// record decides what a fetched quote does and writes the attempt, and the rate it applies, in one transaction — after
// the network call has returned.
func (s *Service) record(ctx context.Context, local string, quote domain.Quote, fetchErr error) (domain.Fetch, error) {
	var out domain.Fetch
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		mode, err := s.settings.RateMode(ctx)
		if err != nil {
			return err
		}
		inForce, found, err := s.store.InForce(ctx, local)
		if err != nil {
			return err
		}
		last, fetched, err := s.store.LastFetch(ctx, local)
		if err != nil {
			return err
		}
		fetchID, err := s.newID()
		if err != nil {
			return err
		}
		at := s.clk.Now().UTC().Truncate(time.Millisecond)
		f := domain.Fetch{
			ID: fetchID, LocalCurrency: local, Seq: nextSeq(last.Seq, fetched), AttemptedAt: at,
			BusinessDate: bizdate.Date(at, s.loc), AgainstRateID: inForceID(inForce, found),
		}
		if fetchErr != nil {
			f.Outcome, f.ErrorCode = domain.OutcomeFailed, fetchCode(fetchErr)
			out = f
			return s.store.AppendFetch(ctx, f)
		}
		// What the internet said is recorded as the internet said it. The shop's margin is applied to the rate that comes
		// OUT of the fetch, not to the record of the fetch, so the log always shows the published figure (2026-09-17).
		adjustMicro, err := s.settings.RateAdjustPercentMicro(ctx)
		if err != nil {
			return err
		}
		f.Provider, f.Nano = quote.Provider, quote.Nano
		f.AdjustPercentMicro = adjustMicro
		effective := domain.Quote{Provider: quote.Provider, Nano: domain.Adjust(quote.Nano, adjustMicro)}
		f.EffectiveNano = effective.Nano
		var current *domain.Rate
		if found {
			current = &inForce
		}
		// The decision compares the rate the shop would actually charge at, margin included: a 5% margin on a figure that
		// barely moved must not read as a large change, and one that took it past the guard must.
		f.Outcome = domain.Decide(mode, current, f.BusinessDate, effective)
		if err = s.store.AppendFetch(ctx, f); err != nil {
			return err
		}
		out = f
		if f.Outcome != domain.OutcomeApplied {
			return nil
		}
		_, err = s.appendRate(ctx, local, inForce, found, effective.Nano, domain.SourceFetched, f.ID, "")
		return err
	})
	return out, err
}

// AcceptProposal records a fetched rate the owner looked at and accepted (L3 §14.5). Owner only; only while it is the
// newest fetch and the rate in force is still the one it was compared with.
func (s *Service) AcceptProposal(ctx context.Context, fetchID id.ID) (domain.Rate, error) {
	var out domain.Rate
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		f, err := s.store.Fetch(ctx, fetchID)
		if err != nil {
			return err
		}
		if f.Outcome != domain.OutcomeProposed {
			return errs.Conflict(domain.CodeNotAProposal, "only a proposed rate can be accepted")
		}
		last, _, err := s.store.LastFetch(ctx, f.LocalCurrency)
		if err != nil {
			return err
		}
		inForce, found, err := s.store.InForce(ctx, f.LocalCurrency)
		if err != nil {
			return err
		}
		if last.ID != f.ID || f.AgainstRateID != inForceID(inForce, found) {
			return errs.Conflict(domain.CodeProposalStale, "the rate or a newer fetch has replaced this proposal")
		}
		before := "—"
		if found {
			before = domain.FormatRate(inForce.Nano)
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActAcceptProposal, SubjectID: f.ID, Before: before, After: domain.FormatRate(f.Nano)}); err != nil {
			return err
		}
		out, err = s.appendRate(ctx, f.LocalCurrency, inForce, found, f.Nano, domain.SourceFetched, f.ID, "")
		return err
	})
	return out, err
}

// SetMode switches between automatic and manual (L3 §14.4). Owner only.
func (s *Service) SetMode(ctx context.Context, raw string) (domain.Mode, error) {
	var out domain.Mode
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		mode, err := domain.ParseMode(raw)
		if err != nil {
			return err
		}
		current, err := s.settings.RateMode(ctx)
		if err != nil {
			return err
		}
		out = mode
		if mode == current {
			return nil
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActSetMode, Before: string(current), After: string(mode)}); err != nil {
			return err
		}
		return s.settings.SetRateMode(ctx, mode)
	})
	return out, err
}

// Quote fetches a rate and records nothing — for first run, where the owner confirms it by completing setup (D-L3.25).
func (s *Service) Quote(ctx context.Context) (domain.Quote, error) {
	if s.source == nil {
		return domain.Quote{}, errs.Conflict(domain.CodeFetchUnavailable, "no rate provider is configured")
	}
	local, err := s.settings.LocalCurrency(ctx)
	if err != nil {
		return domain.Quote{}, err
	}
	q, err := s.source.Fetch(ctx, local)
	if err != nil {
		return domain.Quote{}, errs.Wrap(err, errs.CategoryConflict, fetchCode(err), "the rate could not be fetched")
	}
	return q, nil
}

func (s *Service) appendRate(ctx context.Context, local string, inForce domain.Rate, found bool, value int64, source domain.Source, fetchID id.ID, note string) (domain.Rate, error) {
	rateID, err := s.newID()
	if err != nil {
		return domain.Rate{}, err
	}
	at := s.clk.Now().UTC().Truncate(time.Millisecond)
	r := domain.Rate{
		ID: rateID, LocalCurrency: local, Seq: nextSeq(inForce.Seq, found), Nano: value, Source: source,
		FetchID: fetchID, BusinessDate: bizdate.Date(at, s.loc), RecordedAt: at, Note: note,
	}
	return r, s.store.AppendRate(ctx, r)
}

func nextSeq(last int64, found bool) int64 {
	if !found {
		return 1
	}
	return last + 1
}

func inForceID(r domain.Rate, found bool) id.ID {
	if !found {
		return ""
	}
	return r.ID
}

// fetchCode is the stored code for a failed fetch: the source's own typed code when it is one of fx's, else a failure.
func fetchCode(err error) string {
	switch code := errs.CodeOf(err); code {
	case domain.CodeFetchOffline, domain.CodeFetchFailed:
		return code
	default:
		return domain.CodeFetchFailed
	}
}
