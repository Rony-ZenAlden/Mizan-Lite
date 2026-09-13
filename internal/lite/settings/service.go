// Package settings reads and changes Mizan Lite's settings.
package settings

import (
	"context"
	"log/slog"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Store persists settings rows. infra/sqlite implements it for the application; settingstest
// implements it in memory, and ONE contract suite runs against both, so the fake cannot quietly
// behave differently from the database it stands in for.
type Store interface {
	// Load returns every stored row.
	Load(ctx context.Context) (map[string]string, error)
	// Save writes one row, creating or replacing it.
	Save(ctx context.Context, key, value string, at time.Time) error
}

// Transactor runs fn atomically. platform/database.Store satisfies it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the settings use cases.
type Service struct {
	tx    Transactor
	store Store
	clk   clock.Clock
	log   *slog.Logger
}

// NewService builds the service. Every dependency is required.
func NewService(tx Transactor, store Store, clk clock.Clock, log *slog.Logger) *Service {
	return &Service{tx: tx, store: store, clk: clk, log: log}
}

// Get resolves the current settings.
//
// Rows that could not be used are logged, never returned as an error (domain.FromStored).
func (s *Service) Get(ctx context.Context) (domain.Settings, error) {
	rows, err := s.store.Load(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	resolved, problems := domain.FromStored(rows)
	for _, p := range problems {
		s.log.WarnContext(ctx, "a stored setting was not used",
			slog.String("key", p.Key), slog.String("kind", string(p.Kind)))
	}
	return resolved, nil
}

// Update applies a partial change and returns the settings as they now are.
//
// Read and write happen in one transaction, so two updates cannot each validate against the same
// starting state and then both write.
func (s *Service) Update(ctx context.Context, u domain.Update) (domain.Settings, error) {
	var out domain.Settings
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.Get(ctx)
		if err != nil {
			return err
		}
		next, changes, err := current.Apply(u)
		if err != nil {
			return err
		}
		now := s.clk.Now()
		for _, c := range changes {
			if err := s.store.Save(ctx, c.Key, c.Value, now); err != nil {
				return err
			}
		}
		out = next
		return nil
	})
	return out, err
}
