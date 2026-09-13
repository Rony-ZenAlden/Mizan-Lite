// Package owner is the shop owner's PIN: setting it, verifying it with lockout, owner mode, recovery, and
// the guard owner-only acts pass through (L1 §7).
package owner

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
)

// Credentials is the stored owner credential row.
type Credentials struct {
	PINHash        string
	RecoveryHash   string
	FailedAttempts int
	// LockedUntil is the zero time when no wait is in force.
	LockedUntil time.Time
	RowVersion  int64
}

// Event is one entry in the owner's history. It never holds a PIN, a hash, or a recovery code.
type Event struct {
	ID         id.ID
	OccurredAt time.Time
	Kind       domain.EventKind
	Action     string
	SubjectID  id.ID // zero when the event is about no particular thing
	Before     string
	After      string
}

// Store persists the credential and the history. infra/sqlite implements it; ownertest.Fake implements it
// in memory, and one contract suite runs against both.
type Store interface {
	// Credentials returns the row, or found=false before first run.
	Credentials(ctx context.Context) (Credentials, bool, error)
	CreateCredentials(ctx context.Context, c Credentials, at time.Time) error
	// UpdateCredentials writes c if the row is still at c.RowVersion, and increments the version.
	UpdateCredentials(ctx context.Context, c Credentials, at time.Time) (Credentials, error)
	AppendEvent(ctx context.Context, e Event) error
	// Events returns the newest first, at most limit.
	Events(ctx context.Context, limit int) ([]Event, error)
}

// Transactor runs fn atomically. platform/database.Store satisfies it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Act describes an owner-only act for the history.
type Act struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Status is what the header and the owner screen show.
type Status struct {
	SetUp bool
	// LockedFor is how long until another attempt is allowed; zero when not locked.
	LockedFor time.Duration
	// ElevatedFor is how long owner mode has left; zero when not in owner mode.
	ElevatedFor time.Duration
}

// Service is the owner use cases.
type Service struct {
	tx     Transactor
	store  Store
	hasher crypto.Hasher
	clk    clock.Clock
	random io.Reader
	log    *slog.Logger
	newID  func() (id.ID, error)

	// elevatedUntil is owner mode. In memory ONLY (D-L1.8): never a token in JavaScript, never a row in the
	// database. A restart ends it.
	mu            sync.Mutex
	elevatedUntil time.Time
}

// NewService builds the service. random is crypto/rand.Reader in the application.
func NewService(tx Transactor, store Store, hasher crypto.Hasher, clk clock.Clock, random io.Reader, log *slog.Logger) *Service {
	return &Service{tx: tx, store: store, hasher: hasher, clk: clk, random: random, log: log, newID: id.New}
}

// IsSetUp reports whether first run has created the credential.
func (s *Service) IsSetUp(ctx context.Context) (bool, error) {
	_, found, err := s.store.Credentials(ctx)
	return found, err
}

// SetUp creates the owner's PIN and returns a new recovery code — the only time the code exists in a form
// anyone can read. It joins the caller's transaction, so first run is all or nothing (D-L1.11).
func (s *Service) SetUp(ctx context.Context, rawPIN string) (string, error) {
	pin, pinErr := domain.NormalisePIN(rawPIN)
	if pinErr != nil {
		return "", pinErr
	}
	var recovery string
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if _, found, err := s.store.Credentials(ctx); err != nil {
			return err
		} else if found {
			return errs.Conflict(domain.CodeAlreadySetUp, "the owner is already set up")
		}
		pinHash, recoveryHash, code, err := s.hashNew(pin)
		if err != nil {
			return err
		}
		now := s.clk.Now()
		if err := s.store.CreateCredentials(ctx, Credentials{PINHash: pinHash, RecoveryHash: recoveryHash, RowVersion: 1}, now); err != nil {
			return err
		}
		recovery = code
		return s.event(ctx, domain.EventPINSet, Act{})
	})
	if err != nil {
		return "", err
	}
	return recovery, nil
}

// Status reports set-up, lockout and owner mode.
func (s *Service) Status(ctx context.Context) (Status, error) {
	creds, found, err := s.store.Credentials(ctx)
	if err != nil {
		return Status{}, err
	}
	now := s.clk.Now()
	out := Status{SetUp: found, ElevatedFor: s.elevatedFor(now)}
	if found && now.Before(creds.LockedUntil) {
		out.LockedFor = creds.LockedUntil.Sub(now)
	}
	return out, nil
}

// Elevate enters owner mode for domain.ElevationWindow if the PIN is right.
func (s *Service) Elevate(ctx context.Context, rawPIN string) (Status, error) {
	err := s.attempt(ctx, rawPIN, secretPIN, func(ctx context.Context, _ *Credentials) error {
		return s.event(ctx, domain.EventElevated, Act{})
	})
	if err != nil {
		return Status{}, err
	}
	// Only after the attempt COMMITTED. Set inside the transaction, a credential write that then failed
	// would roll back the record of the attempt and leave the application in owner mode anyway.
	s.mu.Lock()
	s.elevatedUntil = s.clk.Now().Add(domain.ElevationWindow)
	s.mu.Unlock()
	return s.Status(ctx)
}

// EndElevation leaves owner mode now — the header's Lock button.
func (s *Service) EndElevation(ctx context.Context) (Status, error) {
	s.mu.Lock()
	wasElevated := s.clk.Now().Before(s.elevatedUntil)
	s.elevatedUntil = time.Time{}
	s.mu.Unlock()
	if wasElevated {
		if err := s.tx.Do(ctx, func(ctx context.Context) error { return s.event(ctx, domain.EventElevationEnded, Act{}) }); err != nil {
			return Status{}, err
		}
	}
	return s.Status(ctx)
}

// ChangePIN replaces the PIN. It needs the current one, through the same lockout.
func (s *Service) ChangePIN(ctx context.Context, currentPIN, newPIN string) error {
	// The new PIN is validated before an attempt is spent: a weak choice is a typing matter, not a guess.
	next, err := domain.NormalisePIN(newPIN)
	if err != nil {
		return err
	}
	return s.attempt(ctx, currentPIN, secretPIN, func(ctx context.Context, creds *Credentials) error {
		hash, err := s.hasher.Hash(next)
		if err != nil {
			return err
		}
		creds.PINHash = hash
		return s.event(ctx, domain.EventPINChanged, Act{})
	})
}

// Recover sets a new PIN with the recovery code and returns a NEW code; the old one is spent. The code goes
// through the same lockout as the PIN — otherwise it would be a second, unthrottled door (L1 §7.5).
func (s *Service) Recover(ctx context.Context, recoveryCode, newPIN string) (string, error) {
	next, pinErr := domain.NormalisePIN(newPIN)
	if pinErr != nil {
		return "", pinErr
	}
	var code string
	err := s.attempt(ctx, domain.CanonicalRecoveryCode(recoveryCode), secretRecovery, func(ctx context.Context, creds *Credentials) error {
		pinHash, recoveryHash, fresh, err := s.hashNew(next)
		if err != nil {
			return err
		}
		creds.PINHash, creds.RecoveryHash, code = pinHash, recoveryHash, fresh
		return s.event(ctx, domain.EventRecovered, Act{})
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// Events returns the owner's history, newest first.
func (s *Service) Events(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.Events(ctx, limit)
}

// Require permits an owner-only act in owner mode and records it in the CALLER's transaction — so an act
// that rolls back leaves no record that it happened. Outside owner mode it refuses with CodeRequired.
func (s *Service) Require(ctx context.Context, act Act) error {
	if s.elevatedFor(s.clk.Now()) <= 0 {
		return errs.Permission(domain.CodeRequired, "the owner's PIN is required").WithParam("action", act.Action)
	}
	return s.event(ctx, domain.EventGuardedAct, act)
}

type secretKind int

const (
	secretPIN secretKind = iota
	secretRecovery
)

// attempt verifies a secret under lockout and, on success, lets onSuccess change the credential.
//
// # The failure path COMMITS
//
// A wrong PIN must be recorded even though the call fails. If the failure were returned from inside the
// transaction, the Unit of Work would roll back the incremented counter along with it — and lockout would
// never engage, however many guesses were made. So the transaction ends with nil, carrying the refusal out in
// `outcome`, and the refusal is returned only after the commit. TestAWrongPINIsCountedEvenThoughItFails pins it.
func (s *Service) attempt(ctx context.Context, rawSecret string, kind secretKind, onSuccess func(context.Context, *Credentials) error) error {
	var outcome error
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		creds, found, err := s.store.Credentials(ctx)
		if err != nil {
			return err
		}
		if !found {
			outcome = errs.Conflict(domain.CodeNotSetUp, "the owner is not set up")
			return nil
		}
		now := s.clk.Now()
		if now.Before(creds.LockedUntil) {
			outcome = locked(creds.LockedUntil.Sub(now))
			return nil
		}

		stored := creds.PINHash
		if kind == secretRecovery {
			stored = creds.RecoveryHash
		}
		match, needsRehash, err := s.hasher.Verify(stored, rawSecretFor(kind, rawSecret))
		if err != nil {
			return err
		}

		if !match {
			creds.FailedAttempts++
			delay := domain.LockDelay(creds.FailedAttempts)
			if delay > 0 {
				creds.LockedUntil = now.Add(delay)
			}
			if _, err = s.store.UpdateCredentials(ctx, creds, now); err != nil {
				return err
			}
			if err = s.event(ctx, domain.EventElevationFailed, Act{}); err != nil {
				return err
			}
			if delay > 0 {
				if err = s.event(ctx, domain.EventLockedOut, Act{After: strconv.Itoa(int(delay.Seconds())) + "s"}); err != nil {
					return err
				}
				outcome = locked(delay)
				return nil
			}
			code := domain.CodeWrongPIN
			if kind == secretRecovery {
				code = domain.CodeWrongRecoveryCode
			}
			outcome = errs.Permission(code, "wrong").
				// Wrong attempts left before one brings a wait.
				WithParam("remaining", strconv.Itoa(domain.FreeAttempts-creds.FailedAttempts))
			return nil
		}

		creds.FailedAttempts = 0
		creds.LockedUntil = time.Time{}
		if needsRehash && kind == secretPIN {
			// The stored parameters fell below policy; the plaintext is in hand exactly now.
			// Hashed in the normalised form — the form Verify is given — or a PIN typed in Arabic-Indic
			// digits would stop verifying after the upgrade.
			if rehashed, hashErr := s.hasher.Hash(rawSecretFor(kind, rawSecret)); hashErr == nil {
				creds.PINHash = rehashed
			}
		}
		if err = onSuccess(ctx, &creds); err != nil {
			return err
		}
		_, err = s.store.UpdateCredentials(ctx, creds, now)
		return err
	})
	if err != nil {
		return err
	}
	return outcome
}

// rawSecretFor normalises what was typed the way it was hashed. A PIN typed in Arabic-Indic digits must
// verify against the Latin-digit PIN it was set as; an invalid PIN simply fails to match.
func rawSecretFor(kind secretKind, raw string) string {
	if kind == secretRecovery {
		return raw
	}
	if pin, err := domain.NormalisePIN(raw); err == nil {
		return pin
	}
	return raw
}

func locked(wait time.Duration) error {
	return errs.Permission(domain.CodeLocked, "too many attempts").
		WithParam("seconds", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
}

func (s *Service) hashNew(pin string) (pinHash, recoveryHash, code string, err error) {
	code, err = domain.NewRecoveryCode(s.random)
	if err != nil {
		return "", "", "", err
	}
	if pinHash, err = s.hasher.Hash(pin); err != nil {
		return "", "", "", err
	}
	if recoveryHash, err = s.hasher.Hash(domain.CanonicalRecoveryCode(code)); err != nil {
		return "", "", "", err
	}
	return pinHash, recoveryHash, code, nil
}

func (s *Service) elevatedFor(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Before(s.elevatedUntil) {
		return s.elevatedUntil.Sub(now)
	}
	return 0
}

func (s *Service) event(ctx context.Context, kind domain.EventKind, act Act) error {
	eventID, err := s.newID()
	if err != nil {
		return err
	}
	return s.store.AppendEvent(ctx, Event{
		ID: eventID, OccurredAt: s.clk.Now(), Kind: kind,
		Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After,
	})
}
