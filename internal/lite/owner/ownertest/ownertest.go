// Package ownertest is the in-memory owner store, a fast hasher for tests, and the contract every store
// must meet — run against this fake and against SQLite.
package ownertest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	"github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
)

// FastParams makes Argon2id cheap enough to hash hundreds of times in a test run. The application uses
// crypto.DefaultParams; nothing about the owner's logic depends on the cost.
var FastParams = crypto.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

// Hasher returns a hasher with FastParams.
func Hasher() crypto.Hasher { return crypto.NewArgon2id(FastParams) }

// Fake is an in-memory owner.Store.
type Fake struct {
	mu         sync.Mutex
	creds      *owner.Credentials
	events     []owner.Event
	failUpdate bool
}

// ErrInjected is returned once a failure has been armed.
var ErrInjected = errors.New("ownertest: injected failure")

// FailUpdates makes every UpdateCredentials fail.
func (f *Fake) FailUpdates() { f.mu.Lock(); f.failUpdate = true; f.mu.Unlock() }

// NewFake returns a store before first run.
func NewFake() *Fake { return &Fake{} }

// Policy is the shop's master PIN switch for a test: off unless the test turns it on.
type Policy struct {
	mu       sync.Mutex
	required bool
	err      error
}

// NewPolicy is a switch in the position every shop starts in — off.
func NewPolicy() *Policy { return &Policy{} }

// Set turns the switch on or off, as the settings screen would.
func (p *Policy) Set(required bool) { p.mu.Lock(); p.required = required; p.mu.Unlock() }

// Fail makes the switch unreadable, so a test can prove what the guard does when it cannot be read.
func (p *Policy) Fail(err error) { p.mu.Lock(); p.err = err; p.mu.Unlock() }

func (p *Policy) PINRequired(context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.required, p.err
}

func (f *Fake) Credentials(context.Context) (owner.Credentials, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creds == nil {
		return owner.Credentials{}, false, nil
	}
	return *f.creds, true, nil
}

func (f *Fake) CreateCredentials(_ context.Context, c owner.Credentials, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creds != nil {
		return errs.Conflict("database.duplicate", "singleton")
	}
	c.RowVersion = 1
	f.creds = &c
	return nil
}

func (f *Fake) UpdateCredentials(_ context.Context, c owner.Credentials, _ time.Time) (owner.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpdate {
		return owner.Credentials{}, ErrInjected
	}
	if f.creds == nil || f.creds.RowVersion != c.RowVersion {
		return owner.Credentials{}, errs.Conflict("database.concurrent_modification", "stale")
	}
	c.RowVersion++
	f.creds = &c
	return c, nil
}

func (f *Fake) AppendEvent(_ context.Context, e owner.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if (e.Kind == domain.EventGuardedAct) != (e.Action != "") {
		return errs.Validation("database.constraint_violation", "a guarded act needs an action, and only it")
	}
	f.events = append(f.events, e)
	return nil
}

func (f *Fake) Events(_ context.Context, limit int) ([]owner.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]owner.Event(nil), f.events...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// StoreContract is the behaviour every owner.Store must have. newStore returns a fresh store before first run.
func StoreContract(t *testing.T, newStore func(t *testing.T) owner.Store) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)

	t.Run("no credentials before first run", func(t *testing.T) {
		if _, found, err := newStore(t).Credentials(ctx); err != nil || found {
			t.Fatalf("found=%v err=%v", found, err)
		}
	})

	t.Run("created credentials read back at version 1", func(t *testing.T) {
		s := newStore(t)
		if err := s.CreateCredentials(ctx, owner.Credentials{PINHash: "p", RecoveryHash: "r"}, at); err != nil {
			t.Fatal(err)
		}
		got, found, err := s.Credentials(ctx)
		if err != nil || !found || got.PINHash != "p" || got.RecoveryHash != "r" || got.RowVersion != 1 ||
			got.FailedAttempts != 0 || !got.LockedUntil.IsZero() {
			t.Fatalf("got %+v found=%v err=%v", got, found, err)
		}
	})

	t.Run("there is only ever one owner", func(t *testing.T) {
		s := newStore(t)
		_ = s.CreateCredentials(ctx, owner.Credentials{PINHash: "p", RecoveryHash: "r"}, at)
		if err := s.CreateCredentials(ctx, owner.Credentials{PINHash: "x", RecoveryHash: "y"}, at); err == nil {
			t.Fatal("a second owner credential was stored")
		}
	})

	t.Run("an update round-trips the lockout, to the second, and refuses a stale version", func(t *testing.T) {
		s := newStore(t)
		_ = s.CreateCredentials(ctx, owner.Credentials{PINHash: "p", RecoveryHash: "r"}, at)
		current, _, _ := s.Credentials(ctx)
		current.FailedAttempts = 6
		current.LockedUntil = at.Add(90 * time.Second)
		updated, err := s.UpdateCredentials(ctx, current, at)
		if err != nil || updated.RowVersion != 2 {
			t.Fatalf("update = %+v, %v", updated, err)
		}
		got, _, _ := s.Credentials(ctx)
		if got.FailedAttempts != 6 || !got.LockedUntil.Equal(at.Add(90*time.Second)) {
			t.Fatalf("stored = %+v", got)
		}
		if _, err := s.UpdateCredentials(ctx, current, at); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("a stale credential write was accepted: %v", err)
		}
		got.LockedUntil = time.Time{}
		got.FailedAttempts = 0
		if _, err := s.UpdateCredentials(ctx, got, at); err != nil {
			t.Fatal(err)
		}
		cleared, _, _ := s.Credentials(ctx)
		if !cleared.LockedUntil.IsZero() {
			t.Fatalf("a cleared lock did not read back as zero: %v", cleared.LockedUntil)
		}
	})

	t.Run("events read back newest first with every field", func(t *testing.T) {
		s := newStore(t)
		subject, _ := id.New()
		for i, kind := range []domain.EventKind{domain.EventPINSet, domain.EventElevated, domain.EventGuardedAct} {
			eventID, _ := id.New()
			e := owner.Event{ID: eventID, OccurredAt: at.Add(time.Duration(i) * time.Minute), Kind: kind}
			if kind == domain.EventGuardedAct {
				e.Action, e.SubjectID, e.Before, e.After = "catalog.price.change", subject, "USD 3.25", "USD 3.50"
			}
			if err := s.AppendEvent(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Events(ctx, 10)
		if err != nil || len(got) != 3 {
			t.Fatalf("events = %+v, %v", got, err)
		}
		first := got[0]
		if first.Kind != domain.EventGuardedAct || first.Action != "catalog.price.change" || first.SubjectID != subject ||
			first.Before != "USD 3.25" || first.After != "USD 3.50" || !first.OccurredAt.Equal(at.Add(2*time.Minute)) {
			t.Fatalf("newest event = %+v", first)
		}
		if got[2].Kind != domain.EventPINSet || !got[2].SubjectID.IsZero() {
			t.Fatalf("oldest event = %+v", got[2])
		}
		if limited, _ := s.Events(ctx, 2); len(limited) != 2 {
			t.Fatalf("the limit was not applied: %d", len(limited))
		}
	})

	t.Run("a guarded act without an action is refused, and so is an action on any other event", func(t *testing.T) {
		s := newStore(t)
		eventID, _ := id.New()
		if err := s.AppendEvent(ctx, owner.Event{ID: eventID, OccurredAt: at, Kind: domain.EventGuardedAct}); err == nil {
			t.Error("a guarded act with no action was stored")
		}
		eventID, _ = id.New()
		if err := s.AppendEvent(ctx, owner.Event{ID: eventID, OccurredAt: at, Kind: domain.EventElevated, Action: "x"}); err == nil {
			t.Error("an action on a non-act event was stored")
		}
	})
}
