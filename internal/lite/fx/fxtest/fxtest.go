// Package fxtest is the in-memory rate store, fake settings, a scripted rate source, a recording owner gate, and the
// contract every store must meet — run against this fake AND SQLite (L0 D-L0.3).
package fxtest

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

// Fake is an in-memory fx.Store enforcing the schema's uniqueness rules.
type Fake struct {
	mu             sync.Mutex
	rates          []domain.Rate
	fetches        []domain.Fetch
	failAppendRate bool
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{} }

// FailAppendRate makes every AppendRate fail — after a fetch was appended, in Refresh.
func (f *Fake) FailAppendRate() { f.mu.Lock(); f.failAppendRate = true; f.mu.Unlock() }

// ErrInjected is returned once a failure has been armed.
var ErrInjected = errors.New("fxtest: injected failure")

func (f *Fake) InForce(_ context.Context, local string) (domain.Rate, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.Rate
	found := false
	for _, r := range f.rates {
		if r.LocalCurrency == local && (!found || r.Seq > best.Seq) {
			best, found = r, true
		}
	}
	return best, found, nil
}

func (f *Fake) Rates(_ context.Context, local string, limit int) ([]domain.Rate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Rate
	for _, r := range f.rates {
		if r.LocalCurrency == local {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *Fake) AppendRate(_ context.Context, r domain.Rate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAppendRate {
		return ErrInjected
	}
	if r.LocalCurrency == domain.USD || r.Nano <= 0 || r.Seq < 1 || (r.Source == domain.SourceFetched) != (r.FetchID != "") {
		return errs.Validation("database.constraint_violation", "check constraint")
	}
	for _, other := range f.rates {
		if other.ID == r.ID || (other.LocalCurrency == r.LocalCurrency && other.Seq == r.Seq) || (r.FetchID != "" && other.FetchID == r.FetchID) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	if r.FetchID != "" && !f.hasFetch(r.FetchID) {
		return errs.Validation("database.constraint_violation", "foreign key")
	}
	f.rates = append(f.rates, r)
	return nil
}

func (f *Fake) hasFetch(fetchID id.ID) bool {
	for _, x := range f.fetches {
		if x.ID == fetchID {
			return true
		}
	}
	return false
}

func (f *Fake) LastFetch(_ context.Context, local string) (domain.Fetch, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.Fetch
	found := false
	for _, x := range f.fetches {
		if x.LocalCurrency == local && (!found || x.Seq > best.Seq) {
			best, found = x, true
		}
	}
	return best, found, nil
}

func (f *Fake) Fetch(_ context.Context, fetchID id.ID) (domain.Fetch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.fetches {
		if x.ID == fetchID {
			return x, nil
		}
	}
	return domain.Fetch{}, errs.NotFound(domain.CodeFetchNotFound, "no such fetch")
}

func (f *Fake) AppendFetch(_ context.Context, x domain.Fetch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	failed := x.Outcome == domain.OutcomeFailed
	if x.Seq < 1 || failed != (x.Provider == "") || failed != (x.Nano == 0) || failed != (x.ErrorCode != "") || x.Nano < 0 {
		return errs.Validation("database.constraint_violation", "check constraint")
	}
	for _, other := range f.fetches {
		if other.ID == x.ID || (other.LocalCurrency == x.LocalCurrency && other.Seq == x.Seq) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	f.fetches = append(f.fetches, x)
	return nil
}

// Fetches returns every attempt in the order appended.
func (f *Fake) Fetches() []domain.Fetch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Fetch(nil), f.fetches...)
}

// Settings is fake settings: a mode and the local currency.
type Settings struct {
	mu    sync.Mutex
	Mode  domain.Mode
	Local string
	// AdjustPercentMicro is the shop's margin on the internet's rate, at 10⁻⁶ of a percentage point (2026-09-17).
	AdjustPercentMicro int64
}

// NewSettings returns manual mode for SYP — the application's default (settings.Defaults).
func NewSettings() *Settings { return &Settings{Mode: domain.ModeManual, Local: "SYP"} }

func (s *Settings) RateAdjustPercentMicro(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AdjustPercentMicro, nil
}

func (s *Settings) RateMode(context.Context) (domain.Mode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Mode, nil
}

func (s *Settings) SetRateMode(_ context.Context, m domain.Mode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mode = m
	return nil
}

func (s *Settings) LocalCurrency(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Local, nil
}

// Source is a scripted rate source: it answers Quote, or Err, and counts calls.
type Source struct {
	mu    sync.Mutex
	Quote domain.Quote
	Err   error
	Calls int
}

// Fetch implements fx.Source.
func (s *Source) Fetch(context.Context, string) (domain.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls++
	return s.Quote, s.Err
}

// Answer sets what the source answers next.
func (s *Source) Answer(provider string, nano int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Quote, s.Err = domain.Quote{Provider: provider, Nano: nano}, nil
}

// Fail makes the source fail with err.
func (s *Source) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Quote, s.Err = domain.Quote{}, err
}

// CodeOwnerRequired mirrors the owner module's refusal; the fake cannot import that module.
const CodeOwnerRequired = "lite.owner.required"

// Gate is a recording owner gate. With Elevated false it refuses as the owner module would.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []fx.GuardedAct
	Asked    int
}

// Require implements fx.OwnerGate.
func (g *Gate) Require(_ context.Context, act fx.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Asked++
	if !g.Elevated {
		return errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}
