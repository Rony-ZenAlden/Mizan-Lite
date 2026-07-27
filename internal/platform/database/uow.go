package database

import "context"

// Do runs fn inside a single write transaction. See the UnitOfWork interface for the
// contract. Nested calls join the in-flight transaction; commit/rollback happens only
// at the outermost Do.
func (s *Store) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	// Already inside a transaction → join it. Do not open a second (it would deadlock
	// the single writer), and do not commit/rollback here — the outermost Do owns that.
	if _, ok := txFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return s.dialect.TranslateError(err)
	}

	committed := false
	defer func() {
		if p := recover(); p != nil {
			// A panic must never leave the single writer holding an open transaction.
			_ = tx.Rollback()
			panic(p)
		}
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		return err // deferred rollback runs
	}
	if err := tx.Commit(); err != nil {
		return s.dialect.TranslateError(err)
	}
	committed = true
	return nil
}
