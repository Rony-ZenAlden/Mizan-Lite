package fx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

const n = int64(1_000_000_000)

var (
	ctx      = context.Background()
	damascus = time.FixedZone("Damascus", 3*3600)
)

type fixture struct {
	svc      *fx.Service
	store    *fxtest.Fake
	settings *fxtest.Settings
	gate     *fxtest.Gate
	source   *fxtest.Source
	clk      *clock.Fixed
}

func newFixture() fixture {
	f := fixture{
		store: fxtest.NewFake(), settings: fxtest.NewSettings(), gate: &fxtest.Gate{}, source: &fxtest.Source{},
		// 09:00 on 14 September in Damascus.
		clk: clock.NewFixed(time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)),
	}
	// Automatic, so the fetch rules are exercised; the default is manual (TestTheDefaultModeIsManual).
	f.settings.Mode = domain.ModeAutomatic
	f.svc = fx.NewService(litetest.Immediate{}, f.store, f.settings, f.gate, f.source, f.clk, damascus)
	return f
}

// withRate records a rate as the owner would, then leaves owner mode off.
func (f fixture) withRate(t *testing.T, rate string) domain.Rate {
	t.Helper()
	f.gate.Elevated = true
	r, err := f.svc.SetRate(ctx, fx.SetRateInput{Rate: rate, ConfirmLargeChange: true})
	if err != nil {
		t.Fatal(err)
	}
	f.gate.Elevated = false
	return r
}

func (f fixture) current(t *testing.T) fx.Current {
	t.Helper()
	c, err := f.svc.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f fixture) nextDay() { f.clk.Advance(24 * time.Hour) }

func TestTheDefaultModeIsManual(t *testing.T) {
	svc := fx.NewService(litetest.Immediate{}, fxtest.NewFake(), fxtest.NewSettings(), &fxtest.Gate{}, nil, clock.System(), time.UTC)
	c, err := svc.Current(ctx)
	if err != nil || c.Mode != domain.ModeManual {
		t.Fatalf("default mode = %s, %v", c.Mode, err)
	}
}

func TestNoRateIsAStateNotANumber(t *testing.T) {
	f := newFixture()
	c := f.current(t)
	if c.Found || c.Rate.Nano != 0 || c.Stale || c.Local != "SYP" || c.Mode != domain.ModeAutomatic || c.Fetched {
		t.Fatalf("current = %+v", c)
	}
}

func TestSettingTheRateNeedsTheOwner(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.SetRate(ctx, fx.SetRateInput{Rate: "15000"}); errs.CodeOf(err) != fxtest.CodeOwnerRequired {
		t.Fatalf("outside owner mode: %v", err)
	}
	if c := f.current(t); c.Found {
		t.Fatal("a refused rate was recorded")
	}
	f.gate.Elevated = true
	r, err := f.svc.SetRate(ctx, fx.SetRateInput{Rate: "١٥٠٠٠", Note: " السوق "})
	if err != nil || r.Nano != 15_000*n || r.Source != domain.SourceManual || r.Seq != 1 || r.Note != "السوق" || r.BusinessDate != "2026-09-14" {
		t.Fatalf("r = %+v, %v", r, err)
	}
	if f.gate.Acts[0] != (fx.GuardedAct{Action: fx.ActSetRate, Before: "—", After: "15000"}) {
		t.Fatalf("act = %+v", f.gate.Acts[0])
	}
}

func TestALargeTypedChangeIsRefusedUntilConfirmedAndBeforeThePIN(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	asked := f.gate.Asked
	_, err := f.svc.SetRate(ctx, fx.SetRateInput{Rate: "15.000"})
	typed, _ := errs.AsError(err)
	if errs.CodeOf(err) != domain.CodeLargeChange || typed.Params["typed"] != "15" || typed.Params["percent"] != "-99.9" {
		t.Fatalf("err = %v", err)
	}
	if f.gate.Asked != asked {
		t.Fatal("the owner was asked for a rate that is refused anyway")
	}
	f.gate.Elevated = true
	if _, err := f.svc.SetRate(ctx, fx.SetRateInput{Rate: "15.000", ConfirmLargeChange: true}); err != nil {
		t.Fatalf("a confirmed large change: %v", err)
	}
	if c := f.current(t); c.Rate.Nano != 15*n || c.Rate.Seq != 2 {
		t.Fatalf("current = %+v", c.Rate)
	}
}

func TestASameDayCorrectionWins(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15200")
	f.clk.Advance(-2 * time.Hour) // and the clock is set back before the correction
	f.withRate(t, "15000")
	c := f.current(t)
	if c.Rate.Nano != 15_000*n || c.Rate.Seq != 2 || c.Age != 0 {
		t.Fatalf("current = %+v, age %s", c.Rate, c.Age)
	}
	history, err := f.svc.History(ctx, 10)
	if err != nil || len(history) != 2 || history[0].Rate.Nano != 15_000*n || history[0].Change != "-1.3" || history[1].Change != "" {
		t.Fatalf("history = %+v, %v", history, err)
	}
}

func TestEveryReadCarriesItsAge(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	f.clk.Advance(3 * time.Hour)
	if c := f.current(t); c.Age != 3*time.Hour || c.Stale {
		t.Fatalf("today: %+v", c)
	}
	f.nextDay()
	if c := f.current(t); c.Age != 27*time.Hour || !c.Stale {
		t.Fatalf("tomorrow: %+v", c)
	}
	// 22:30 UTC on the 14th is 01:30 on the 15th in Damascus: a rate from 09:00 on the 14th is from yesterday.
	g := newFixture()
	g.withRate(t, "15000")
	g.clk.Current = time.Date(2026, 9, 14, 22, 30, 0, 0, time.UTC)
	if !g.current(t).Stale {
		t.Fatal("a rate from the shop's yesterday is not stale at 01:30")
	}
}

func TestRefreshAppliesWhatTheDecisionAllows(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	f.source.Answer("currency-api-jsdelivr", 13_007_535_500_000)

	// The owner's rate from today holds (D-L3.18).
	got, err := f.svc.Refresh(ctx)
	if err != nil || got.Outcome != domain.OutcomeHeldToday || got.Nano != 13_007_535_500_000 || got.Provider != "currency-api-jsdelivr" {
		t.Fatalf("refresh = %+v, %v", got, err)
	}
	c := f.current(t)
	if c.Rate.Nano != 15_000*n || !c.Fetched || c.FetchChange != "-13.3" || c.Acceptable {
		t.Fatalf("current = %+v", c)
	}

	// The next day it applies, and names its fetch.
	f.nextDay()
	got, _ = f.svc.Refresh(ctx)
	c = f.current(t)
	if got.Outcome != domain.OutcomeApplied || c.Rate.Source != domain.SourceFetched || c.Rate.FetchID != got.ID || c.Rate.Nano != 13_007_535_500_000 || c.Stale {
		t.Fatalf("next day: %+v, current %+v", got, c.Rate)
	}

	// An hour later the same figure is unchanged, and records no rate.
	f.clk.Advance(time.Hour)
	got, _ = f.svc.Refresh(ctx)
	if got.Outcome != domain.OutcomeUnchanged || f.current(t).Rate.Seq != c.Rate.Seq {
		t.Fatalf("unchanged: %+v", got)
	}
	if f.gate.Asked != 1 {
		t.Fatalf("refreshing asked the owner: %d", f.gate.Asked)
	}
}

func TestRefreshInManualModeOnlyShowsTheInternetRate(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	f.settings.Mode = domain.ModeManual
	f.nextDay()
	f.source.Answer("p", 13_000*n)
	got, err := f.svc.Refresh(ctx)
	if err != nil || got.Outcome != domain.OutcomeHeldMode || f.current(t).Rate.Nano != 15_000*n {
		t.Fatalf("manual mode: %+v, %v", got, err)
	}
}

func TestTheFirstFetchedRateIsOnlyAProposal(t *testing.T) {
	f := newFixture()
	f.source.Answer("p", 13_000*n)
	got, err := f.svc.Refresh(ctx)
	if err != nil || got.Outcome != domain.OutcomeProposed || got.AgainstRateID != "" {
		t.Fatalf("refresh = %+v, %v", got, err)
	}
	c := f.current(t)
	if c.Found || !c.Acceptable {
		t.Fatalf("current = %+v", c)
	}
	if _, err = f.svc.AcceptProposal(ctx, got.ID); errs.CodeOf(err) != fxtest.CodeOwnerRequired {
		t.Fatalf("accepting without the owner: %v", err)
	}
	f.gate.Elevated = true
	r, err := f.svc.AcceptProposal(ctx, got.ID)
	if err != nil || r.Source != domain.SourceFetched || r.FetchID != got.ID || r.Nano != 13_000*n || r.Seq != 1 {
		t.Fatalf("accepted = %+v, %v", r, err)
	}
	if _, err := f.svc.AcceptProposal(ctx, got.ID); errs.CodeOf(err) != domain.CodeProposalStale {
		t.Fatalf("accepted twice: %v", err)
	}
	if f.gate.Acts[0].Action != fx.ActAcceptProposal || f.gate.Acts[0].Before != "—" || f.gate.Acts[0].After != "13000" {
		t.Fatalf("act = %+v", f.gate.Acts[0])
	}
}

func TestALargeFetchedChangeIsAProposalThatGoesStale(t *testing.T) {
	f := newFixture()
	f.withRate(t, "13000")
	f.nextDay()
	f.source.Answer("exchangerate-api-open", 121_957_900_000) // the new pound, unscaled: 99% lower (L3 §14.3 F1)
	proposal, _ := f.svc.Refresh(ctx)
	if proposal.Outcome != domain.OutcomeProposed || f.current(t).Rate.Nano != 13_000*n {
		t.Fatalf("a 99%% change was applied: %+v", proposal)
	}
	// The owner sets a rate meanwhile: the proposal no longer compares with the rate in force.
	f.withRate(t, "13100")
	f.gate.Elevated = true
	if _, err := f.svc.AcceptProposal(ctx, proposal.ID); errs.CodeOf(err) != domain.CodeProposalStale {
		t.Fatalf("a stale proposal was accepted: %v", err)
	}
	if c := f.current(t); c.Acceptable {
		t.Fatal("a stale proposal is offered")
	}
	// Not a proposal at all.
	f.clk.Advance(2 * time.Minute)
	f.source.Answer("p", 13_100*n)
	unchanged, _ := f.svc.Refresh(ctx)
	if _, err := f.svc.AcceptProposal(ctx, unchanged.ID); errs.CodeOf(err) != domain.CodeNotAProposal {
		t.Fatalf("accepting a held fetch: %v", err)
	}
}

func TestAFailedFetchIsLoggedWithACodeAndChangesNothing(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	f.source.Fail(errs.Conflict(domain.CodeFetchOffline, "dial tcp: no route to host"))
	got, err := f.svc.Refresh(ctx)
	if err != nil || got.Outcome != domain.OutcomeFailed || got.ErrorCode != domain.CodeFetchOffline || got.Provider != "" || got.Nano != 0 {
		t.Fatalf("refresh = %+v, %v", got, err)
	}
	f.clk.Advance(2 * time.Minute)
	f.source.Fail(errors.New("provider prose, never stored"))
	got, _ = f.svc.Refresh(ctx)
	if got.ErrorCode != domain.CodeFetchFailed {
		t.Fatalf("an untyped failure stored %q", got.ErrorCode)
	}
	if c := f.current(t); c.Rate.Seq != 1 {
		t.Fatal("a failed fetch changed the rate")
	}
}

func TestRefreshIsThrottledToOneAttemptAMinute(t *testing.T) {
	f := newFixture()
	f.source.Answer("p", 13_000*n)
	first, _ := f.svc.Refresh(ctx)
	f.clk.Advance(30 * time.Second)
	again, err := f.svc.Refresh(ctx)
	if err != nil || again.ID != first.ID || f.source.Calls != 1 {
		t.Fatalf("a second press reached the network: %d calls", f.source.Calls)
	}
	f.clk.Advance(31 * time.Second)
	if later, _ := f.svc.Refresh(ctx); later.ID == first.ID || f.source.Calls != 2 {
		t.Fatal("a press a minute later did not fetch")
	}
	// A clock set back does not stop refreshing for ever.
	f.clk.Advance(-time.Hour)
	if _, err := f.svc.Refresh(ctx); err != nil || f.source.Calls != 3 {
		t.Fatalf("after the clock went back: %d calls, %v", f.source.Calls, err)
	}
}

func TestWithNoProvidersNothingIsFetched(t *testing.T) {
	store := fxtest.NewFake()
	svc := fx.NewService(litetest.Immediate{}, store, fxtest.NewSettings(), &fxtest.Gate{}, nil, clock.System(), time.UTC)
	if svc.CanFetch() {
		t.Fatal("CanFetch with no source")
	}
	if _, err := svc.Refresh(ctx); errs.CodeOf(err) != domain.CodeFetchUnavailable {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := svc.Quote(ctx); errs.CodeOf(err) != domain.CodeFetchUnavailable {
		t.Fatalf("quote: %v", err)
	}
	if len(store.Fetches()) != 0 {
		t.Fatal("an attempt with no provider was logged")
	}
}

func TestAQuoteRecordsNothing(t *testing.T) {
	f := newFixture()
	f.source.Answer("p", 13_000*n)
	q, err := f.svc.Quote(ctx)
	if err != nil || q.Nano != 13_000*n || len(f.store.Fetches()) != 0 || f.current(t).Found {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	f.source.Fail(errs.Conflict(domain.CodeFetchOffline, "offline"))
	if _, err := f.svc.Quote(ctx); errs.CodeOf(err) != domain.CodeFetchOffline {
		t.Fatalf("offline quote: %v", err)
	}
}

func TestTheFirstRunRateIsRecordedOnceWithoutTheOwner(t *testing.T) {
	f := newFixture()
	r, err := f.svc.RecordFirstRun(ctx, "14800")
	if err != nil || r.Source != domain.SourceFirstRun || r.Seq != 1 || f.gate.Asked != 0 {
		t.Fatalf("first run = %+v, %v", r, err)
	}
	if _, err := f.svc.RecordFirstRun(ctx, "15000"); errs.CodeOf(err) != domain.CodeAlreadyHasRate {
		t.Fatalf("a second first-run rate: %v", err)
	}
	if _, err := newFixture().svc.RecordFirstRun(ctx, "0.0000667"); errs.CodeOf(err) != domain.CodeRateDecimals {
		t.Fatalf("an inverse rate at first run: %v", err)
	}
}

func TestSwitchingModeNeedsTheOwner(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.SetMode(ctx, "manual"); errs.CodeOf(err) != fxtest.CodeOwnerRequired || f.settings.Mode != domain.ModeAutomatic {
		t.Fatalf("outside owner mode: %v", err)
	}
	f.gate.Elevated = true
	if m, err := f.svc.SetMode(ctx, "manual"); err != nil || m != domain.ModeManual || f.settings.Mode != domain.ModeManual {
		t.Fatalf("SetMode = %v, %v", m, err)
	}
	if f.gate.Acts[0] != (fx.GuardedAct{Action: fx.ActSetMode, Before: "automatic", After: "manual"}) {
		t.Fatalf("act = %+v", f.gate.Acts[0])
	}
	if _, err := f.svc.SetMode(ctx, "manual"); err != nil || len(f.gate.Acts) != 1 {
		t.Fatal("setting the same mode was recorded")
	}
	if _, err := f.svc.SetMode(ctx, "sometimes"); errs.CodeOf(err) != domain.CodeInvalidMode {
		t.Fatal(err)
	}
}

func TestTheSameRateAgainIsRecordedAndClearsStale(t *testing.T) {
	f := newFixture()
	f.withRate(t, "15000")
	f.nextDay()
	if !f.current(t).Stale {
		t.Fatal("not stale the next day")
	}
	f.withRate(t, "15000")
	if c := f.current(t); c.Stale || c.Rate.Seq != 2 {
		t.Fatalf("re-confirmed = %+v", c)
	}
}
