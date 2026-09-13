// Package sqlite is the settings table.
package sqlite

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Store implements settings.Store over the settings table.
type Store struct {
	db database.DB
}

// NewStore builds the store.
func NewStore(db database.DB) *Store { return &Store{db: db} }

// Load returns every stored row.
func (s *Store) Load(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT setting_key, value FROM settings`)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	return out, nil
}

// Save writes one row, creating or replacing it.
//
// UPDATE then INSERT rather than an upsert statement: `ON CONFLICT` is SQLite and PostgreSQL
// syntax that MySQL and SQL Server do not accept, and the portability contract (ARCHITECTURE_v1
// §8) forbids an engine-specific statement where a portable pair does the same work. The pair is
// race-free because Lite writes through a single writer connection.
func (s *Store) Save(ctx context.Context, key, value string, at time.Time) error {
	stamp := clock.Format(at)
	res, err := s.db.Writer(ctx).ExecContext(ctx,
		`UPDATE settings SET value = ?, updated_at = ? WHERE setting_key = ?`, value, stamp, key)
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	if affected > 0 {
		return nil
	}
	if _, err := s.db.Writer(ctx).ExecContext(ctx,
		`INSERT INTO settings (setting_key, value, updated_at) VALUES (?, ?, ?)`,
		key, value, stamp); err != nil {
		return s.db.Dialect().TranslateError(err)
	}
	return nil
}

// PeekLocale reads the stored interface language from a database file BEFORE the application
// starts, so the first frame the window paints is already in the right language and direction.
//
// # Why this exists
//
// The window opens before the object graph is built (so migration progress has somewhere to
// show), which means the settings service does not exist yet when index.html is served. Without
// this, an English-speaking shop would see an Arabic, right-to-left frame on every launch until
// boot finished and the layout flipped — the classic bilingual defect.
//
// # Why it can never stop a launch
//
// It returns the default on every failure: no file (a fresh install — and it does NOT create
// one), no settings table (a database from before this migration), a damaged value, or a database
// it cannot open. Each of those is logged here rather than returned, because there is nothing a
// caller could do with it except log it. The real settings are read again, properly, once boot
// completes.
//
//nolint:contextcheck // Store.Close takes no context by design: a peek must release the file regardless
func PeekLocale(ctx context.Context, dbPath string, log *slog.Logger) domain.Locale {
	fallback := domain.Defaults().Locale

	if _, err := os.Stat(dbPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.WarnContext(ctx, "could not inspect the database before launch", slog.Any("error", err))
		}
		return fallback
	}

	store, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		log.WarnContext(ctx, "could not open the database to read the language", slog.Any("error", err))
		return fallback
	}
	// Closed before returning. On Windows a handle still open when the boot goroutine later opens
	// the same file for a migration's restore would make the file impossible to replace.
	defer func() { _ = store.Close() }()

	var raw string
	err = store.Reader(ctx).QueryRowContext(ctx,
		`SELECT value FROM settings WHERE setting_key = ?`, domain.KeyLocale).Scan(&raw)
	if err != nil {
		// sql.ErrNoRows is the common case (the language was never changed) and a missing table is
		// a database from before 0001; neither is worth a warning.
		log.DebugContext(ctx, "no stored language before launch", slog.Any("error", err))
		return fallback
	}
	locale, err := domain.ParseLocale(raw)
	if err != nil {
		log.WarnContext(ctx, "the stored language is not valid; using the default",
			slog.String("value", raw))
		return fallback
	}
	return locale
}
