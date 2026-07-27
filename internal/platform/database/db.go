// Package database is the persistence platform: connection pools, the dialect shim,
// the Unit of Work, and the executor abstraction every repository uses. It contains
// no business logic and no business tables.
//
// It is the only package permitted to import a database driver; the dialect shim is
// the entire database-specific surface (see the dialect subpackage).
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registered as "sqlite" (no cgo)

	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// DB is the persistence surface repositories depend on.
type DB interface {
	// Writer returns the active transaction if ctx is inside a Unit of Work, else the
	// single-writer pool. Use for writes and aggregate loads.
	Writer(ctx context.Context) Executor
	// Reader returns the active transaction if ctx is inside a Unit of Work (so a use
	// case reads its own uncommitted writes), else the read-only pool. Use for
	// list/report queries.
	Reader(ctx context.Context) Executor
	// Dialect exposes the database-specific behaviour.
	Dialect() dialect.Dialect
	// WriterPool exposes the raw writer pool for the migration runner and admin tasks.
	WriterPool() *sql.DB
	// Ping verifies connectivity on both pools.
	Ping(ctx context.Context) error
	// Close checkpoints the WAL and closes both pools in order.
	Close() error
}

// UnitOfWork runs work inside a single write transaction.
type UnitOfWork interface {
	// Do runs fn inside one write transaction, committing on nil error and rolling
	// back on any error or panic. A nested Do (ctx already in a transaction) JOINS the
	// in-flight transaction rather than opening a second — one user action is one
	// atomic unit, and a second transaction would deadlock SQLite's single writer.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Store is the concrete SQLite-backed implementation of DB and UnitOfWork.
type Store struct {
	writer  *sql.DB
	reader  *sql.DB
	dialect dialect.Dialect
}

var (
	_ DB         = (*Store)(nil)
	_ UnitOfWork = (*Store)(nil)
)

// Open constructs the store: a single-writer pool and a read pool, both with the
// portable PRAGMAs applied per connection.
func Open(cfg Config) (*Store, error) {
	cfg = cfg.withDefaults()
	if cfg.Path == "" {
		return nil, fmt.Errorf("database: config.Path is required")
	}
	dsn := buildDSN(cfg)

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: open writer: %w", err)
	}
	// SQLite permits exactly one writer; a single connection turns write contention
	// into a bounded queue wait instead of a SQLITE_BUSY error.
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("database: open reader: %w", err)
	}
	reader.SetMaxOpenConns(cfg.ReaderPoolSize)
	reader.SetMaxIdleConns(cfg.ReaderPoolSize)
	reader.SetConnMaxLifetime(0)

	s := &Store{writer: writer, reader: reader, dialect: dialect.NewSQLite()}
	if err := s.Ping(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// buildDSN assembles the SQLite DSN with the portable PRAGMAs. modernc applies each
// _pragma to every new connection, which is exactly the per-connection requirement
// (foreign_keys in particular must be set per connection).
func buildDSN(cfg Config) string {
	ms := int(cfg.BusyTimeout / time.Millisecond)
	path := strings.ReplaceAll(cfg.Path, " ", "%20")
	pragmas := []string{
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"foreign_keys(1)",
		"busy_timeout(" + strconv.Itoa(ms) + ")",
		"temp_store(MEMORY)",
		"cache_size(-64000)",
		"journal_size_limit(67108864)",
	}
	var b strings.Builder
	b.WriteString("file:")
	b.WriteString(path)
	for i, p := range pragmas {
		if i == 0 {
			b.WriteString("?_pragma=")
		} else {
			b.WriteString("&_pragma=")
		}
		b.WriteString(p)
	}
	return b.String()
}

// Writer resolves the write executor from context (§executor.go).
func (s *Store) Writer(ctx context.Context) Executor {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return s.writer
}

// Reader resolves the read executor from context.
func (s *Store) Reader(ctx context.Context) Executor {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return s.reader
}

// Dialect returns the SQLite dialect.
func (s *Store) Dialect() dialect.Dialect { return s.dialect }

// WriterPool exposes the raw writer pool (migrations, admin).
func (s *Store) WriterPool() *sql.DB { return s.writer }

// Ping verifies both pools.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.writer.PingContext(ctx); err != nil {
		return fmt.Errorf("database: ping writer: %w", err)
	}
	if err := s.reader.PingContext(ctx); err != nil {
		return fmt.Errorf("database: ping reader: %w", err)
	}
	return nil
}

// Close checkpoints the WAL and closes both pools. A desktop app killed mid-write
// must reopen clean; an unbounded -wal file is a support call.
func (s *Store) Close() error {
	_, _ = s.writer.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(s.reader.Close(), s.writer.Close())
}
