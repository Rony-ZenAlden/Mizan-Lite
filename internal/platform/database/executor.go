package database

import (
	"context"
	"database/sql"
)

// Executor is the subset of *sql.DB and *sql.Tx that repositories use. Both types
// satisfy it, so resolving one from the context is zero-cost: a repository method is
// written once and runs correctly whether or not it is inside a transaction.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// txContextKey is unexported so business code can neither forge nor extract the
// active transaction — the opacity is deliberate.
type txContextKey struct{}

func withTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

func txFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(*sql.Tx)
	return tx, ok
}
