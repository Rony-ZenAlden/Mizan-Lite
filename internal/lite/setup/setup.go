// Package setup is first run: the shop's name, its language, the owner's PIN and today's exchange rate, written together
// or not at all (L1 §8, D-L1.11; L3 Q-L3.3).
//
// It is an orchestrator — like bootstrap and api, one of the few Lite packages allowed to reach more than one
// module — and it reaches them only through the three ports below.
package setup

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	settings "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// CodeAlreadyComplete refuses a second first run.
const CodeAlreadyComplete = "lite.setup.already_complete"

// Settings is what first run needs from the settings module.
type Settings interface {
	Get(ctx context.Context) (settings.Settings, error)
	Update(ctx context.Context, u settings.Update) (settings.Settings, error)
}

// Owner is what first run needs from the owner module.
type Owner interface {
	IsSetUp(ctx context.Context) (bool, error)
	SetUp(ctx context.Context, pin string) (recoveryCode string, err error)
}

// Rates is what first run needs from the exchange-rate module: the opening rate, recorded in first run's transaction.
type Rates interface {
	RecordFirstRun(ctx context.Context, rate string) error
}

// Transactor runs fn atomically.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is first run.
type Service struct {
	tx       Transactor
	settings Settings
	owner    Owner
	rates    Rates
}

// NewService builds the service.
func NewService(tx Transactor, s Settings, o Owner, r Rates) *Service {
	return &Service{tx: tx, settings: s, owner: o, rates: r}
}

// Complete reports whether first run has happened.
//
// Read from the DATA — a credential exists and a shop name is set — never from a flag, which is a second
// record of one fact and the first thing to disagree with it.
func (s *Service) Complete(ctx context.Context) (bool, error) {
	ownerSetUp, err := s.owner.IsSetUp(ctx)
	if err != nil || !ownerSetUp {
		return false, err
	}
	current, err := s.settings.Get(ctx)
	if err != nil {
		return false, err
	}
	return current.ShopName != "", nil
}

// Input is what the first-run screen collects.
type Input struct {
	ShopName string
	Locale   string
	PIN      string
	// Rate is today's exchange rate, local currency per US dollar — typed, or fetched and confirmed (Q-L3.3).
	Rate string
}

// Run completes first run and returns the recovery code — the only time it is ever readable.
//
// One transaction: the shop name, the language and the credential are written together, so a PIN refused as
// weak leaves no shop name behind and a failure after the PIN leaves no PIN.
func (s *Service) Run(ctx context.Context, in Input) (string, error) {
	var recovery string
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		done, err := s.Complete(ctx)
		if err != nil {
			return err
		}
		if done {
			return errs.Conflict(CodeAlreadyComplete, "first run has already been completed")
		}
		if _, err = s.settings.Update(ctx, settings.Update{Locale: &in.Locale, ShopName: &in.ShopName}); err != nil {
			return err
		}
		if recovery, err = s.owner.SetUp(ctx, in.PIN); err != nil {
			return err
		}
		return s.rates.RecordFirstRun(ctx, in.Rate)
	})
	if err != nil {
		return "", err
	}
	return recovery, nil
}
