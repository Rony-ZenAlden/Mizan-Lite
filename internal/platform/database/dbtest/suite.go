// Package dbtest provides the dialect-agnostic repository contract suite. It defines
// what a correct database.Store does; any future dialect must pass the identical suite
// by providing a factory that opens a Store against that engine. This is the mechanism
// that keeps the portability promise honest.
package dbtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// A self-referencing table gives us UNIQUE (code) for the duplicate test, a foreign
// key (parent_id) for the reference test, and row_version for optimistic concurrency.
const createWidgets = `
CREATE TABLE IF NOT EXISTS widgets (
  id          TEXT    NOT NULL PRIMARY KEY,
  code        TEXT    NOT NULL UNIQUE,
  parent_id   TEXT    REFERENCES widgets(id),
  qty         INTEGER NOT NULL DEFAULT 0,
  row_version INTEGER NOT NULL DEFAULT 1
)`

// NewStore opens a Store the suite can exercise. Implementations register their own
// cleanup (Close) on t.
type NewStore func(t *testing.T) *database.Store

// RunContractSuite runs the full behavioural contract against newStore.
func RunContractSuite(t *testing.T, newStore NewStore) {
	t.Run("InsertGet", func(t *testing.T) { testInsertGet(t, newStore) })
	t.Run("UniqueViolationIsConflict", func(t *testing.T) { testUniqueViolation(t, newStore) })
	t.Run("ForeignKeyViolationIsConflict", func(t *testing.T) { testForeignKey(t, newStore) })
	t.Run("NotNullViolationIsValidation", func(t *testing.T) { testNotNull(t, newStore) })
	t.Run("OptimisticConcurrency", func(t *testing.T) { testOptimistic(t, newStore) })
	t.Run("DoCommits", func(t *testing.T) { testDoCommits(t, newStore) })
	t.Run("DoRollsBackOnError", func(t *testing.T) { testDoRollbackError(t, newStore) })
	t.Run("DoRollsBackOnPanic", func(t *testing.T) { testDoRollbackPanic(t, newStore) })
	t.Run("NestedDoJoins", func(t *testing.T) { testNestedJoin(t, newStore) })
	t.Run("ReadYourWrites", func(t *testing.T) { testReadYourWrites(t, newStore) })
	t.Run("LimitOffsetPaging", func(t *testing.T) { testPaging(t, newStore) })
	t.Run("Upsert", func(t *testing.T) { testUpsert(t, newStore) })
	t.Run("ConcurrentWritesSerialize", func(t *testing.T) { testConcurrentWrites(t, newStore) })
}

// ── helpers ─────────────────────────────────────────────────────────────────────

func prepare(t *testing.T, newStore NewStore) *database.Store {
	t.Helper()
	st := newStore(t)
	if _, err := st.WriterPool().ExecContext(context.Background(), createWidgets); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return st
}

func insertWidget(ctx context.Context, exec database.Executor, id, code string, qty int64) error {
	_, err := exec.ExecContext(ctx,
		"INSERT INTO widgets (id, code, qty) VALUES (?, ?, ?)", id, code, qty)
	return err
}

func insertChild(ctx context.Context, exec database.Executor, id, code, parent string) error {
	_, err := exec.ExecContext(ctx,
		"INSERT INTO widgets (id, code, qty, parent_id) VALUES (?, ?, 0, ?)", id, code, parent)
	return err
}

func count(ctx context.Context, exec database.Executor) (int, error) {
	var n int
	err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM widgets").Scan(&n)
	return n, err
}

// ── tests ───────────────────────────────────────────────────────────────────────

func testInsertGet(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	if err := insertWidget(ctx, st.Writer(ctx), "w1", "alpha", 42); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var code string
	var qty int64
	err := st.Reader(ctx).
		QueryRowContext(ctx, "SELECT code, qty FROM widgets WHERE id = ?", "w1").
		Scan(&code, &qty)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if code != "alpha" || qty != 42 {
		t.Fatalf("got (%q, %d), want (alpha, 42)", code, qty)
	}
}

func testUniqueViolation(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	if err := insertWidget(ctx, st.Writer(ctx), "w1", "dup", 1); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	raw := insertWidget(ctx, st.Writer(ctx), "w2", "dup", 1)
	got := st.Dialect().TranslateError(raw)
	if !errs.IsCategory(got, errs.CategoryConflict) {
		t.Fatalf("want Conflict, got %v", got)
	}
	if errs.CodeOf(got) != dialect.CodeDuplicate {
		t.Fatalf("want code %q, got %q", dialect.CodeDuplicate, errs.CodeOf(got))
	}
}

func testForeignKey(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	raw := insertChild(ctx, st.Writer(ctx), "c1", "child", "missing-parent")
	got := st.Dialect().TranslateError(raw)
	if !errs.IsCategory(got, errs.CategoryConflict) {
		t.Fatalf("want Conflict (FK enforced), got %v — is foreign_keys ON?", got)
	}
	if errs.CodeOf(got) != dialect.CodeReferenceViolation {
		t.Fatalf("want code %q, got %q", dialect.CodeReferenceViolation, errs.CodeOf(got))
	}
}

func testNotNull(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	// code is NOT NULL; inserting NULL violates it and must map to Validation.
	_, raw := st.Writer(ctx).ExecContext(ctx,
		"INSERT INTO widgets (id, code, qty) VALUES (?, NULL, ?)", "w1", 1)
	got := st.Dialect().TranslateError(raw)
	if !errs.IsCategory(got, errs.CategoryValidation) {
		t.Fatalf("want Validation, got %v", got)
	}
	if errs.CodeOf(got) != dialect.CodeConstraintViolation {
		t.Fatalf("want code %q, got %q", dialect.CodeConstraintViolation, errs.CodeOf(got))
	}
}

func testOptimistic(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	if err := insertWidget(ctx, st.Writer(ctx), "w1", "v", 1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	const upd = "UPDATE widgets SET qty = ?, row_version = row_version + 1 WHERE id = ? AND row_version = ?"

	res, err := st.Writer(ctx).ExecContext(ctx, upd, 5, "w1", 1)
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if verr := database.VersionedUpdateResult(res); verr != nil {
		t.Fatalf("first update should succeed: %v", verr)
	}

	// Stale version (row is now at version 2).
	res2, err := st.Writer(ctx).ExecContext(ctx, upd, 9, "w1", 1)
	if err != nil {
		t.Fatalf("stale update exec: %v", err)
	}
	got := database.VersionedUpdateResult(res2)
	if !errs.IsCategory(got, errs.CategoryConflict) {
		t.Fatalf("stale update: want Conflict, got %v", got)
	}
	if errs.CodeOf(got) != dialect.CodeConcurrentModification {
		t.Fatalf("want code %q, got %q", dialect.CodeConcurrentModification, errs.CodeOf(got))
	}
}

func testDoCommits(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	err := st.Do(ctx, func(ctx context.Context) error {
		return insertWidget(ctx, st.Writer(ctx), "w1", "c", 1)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != 1 {
		t.Fatalf("after commit count = %d, want 1", n)
	}
}

func testDoRollbackError(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	sentinel := errors.New("business rule failed")
	err := st.Do(ctx, func(ctx context.Context) error {
		if e := insertWidget(ctx, st.Writer(ctx), "w1", "c", 1); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != 0 {
		t.Fatalf("error should roll back, count = %d, want 0", n)
	}
}

func testDoRollbackPanic(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = st.Do(ctx, func(ctx context.Context) error {
			if e := insertWidget(ctx, st.Writer(ctx), "w1", "c", 1); e != nil {
				t.Fatalf("insert in panic tx: %v", e)
			}
			panic("boom")
		})
	}()

	if !panicked {
		t.Fatal("Do should re-panic after rolling back")
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != 0 {
		t.Fatalf("panic should roll back, count = %d, want 0", n)
	}
}

func testNestedJoin(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	// Outer fails after a successful nested Do; because the nested Do joined the outer
	// transaction, BOTH inserts must roll back together.
	err := st.Do(ctx, func(ctx context.Context) error {
		if e := insertWidget(ctx, st.Writer(ctx), "w1", "a", 1); e != nil {
			return e
		}
		if e := st.Do(ctx, func(ctx context.Context) error {
			return insertWidget(ctx, st.Writer(ctx), "w2", "b", 1)
		}); e != nil {
			return e
		}
		return errors.New("outer fails")
	})
	if err == nil {
		t.Fatal("expected outer error")
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != 0 {
		t.Fatalf("nested must roll back with outer, count = %d, want 0", n)
	}
}

func testReadYourWrites(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	bg := context.Background()
	err := st.Do(bg, func(ctx context.Context) error {
		if e := insertWidget(ctx, st.Writer(ctx), "w1", "x", 1); e != nil {
			return e
		}
		// Inside the transaction (Reader(ctx) resolves to the tx) the write is visible.
		if n, _ := count(ctx, st.Reader(ctx)); n != 1 {
			t.Errorf("read-your-writes: in-tx count = %d, want 1", n)
		}
		// From outside the transaction (background ctx → reader pool) it is not yet.
		if n, _ := count(bg, st.Reader(bg)); n != 0 {
			t.Errorf("isolation: out-of-tx count = %d, want 0", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if n, _ := count(bg, st.Reader(bg)); n != 1 {
		t.Fatalf("after commit count = %d, want 1", n)
	}
}

func testPaging(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := insertWidget(ctx, st.Writer(ctx), fmt.Sprintf("w%d", i), fmt.Sprintf("c%d", i), int64(i)); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	q := "SELECT code FROM widgets ORDER BY code " + st.Dialect().LimitOffset(2, 1)
	rows, err := st.Reader(ctx).QueryContext(ctx, q)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("paging got %v, want [c1 c2]", got)
	}
}

func testUpsert(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	stmt := st.Dialect().Upsert(dialect.UpsertSpec{
		Table:        "widgets",
		Columns:      []string{"id", "code", "qty"},
		ConflictCols: []string{"code"},
		UpdateCols:   []string{"qty"},
	})
	if _, err := st.Writer(ctx).ExecContext(ctx, stmt, "w1", "u", 1); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Same code → conflict → update qty to the attempted value.
	if _, err := st.Writer(ctx).ExecContext(ctx, stmt, "w2", "u", 7); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	var qty int64
	if err := st.Reader(ctx).QueryRowContext(ctx, "SELECT qty FROM widgets WHERE code = ?", "u").Scan(&qty); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if qty != 7 {
		t.Fatalf("upsert qty = %d, want 7", qty)
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != 1 {
		t.Fatalf("upsert should not add a row, count = %d, want 1", n)
	}
}

func testConcurrentWrites(t *testing.T, newStore NewStore) {
	st := prepare(t, newStore)
	ctx := context.Background()
	const g = 20
	var wg sync.WaitGroup
	errCh := make(chan error, g)
	for i := 0; i < g; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- st.Do(ctx, func(ctx context.Context) error {
				return insertWidget(ctx, st.Writer(ctx), fmt.Sprintf("w%d", i), fmt.Sprintf("c%d", i), int64(i))
			})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent write failed (single-writer pool should serialise, not error): %v", err)
		}
	}
	if n, _ := count(ctx, st.Reader(ctx)); n != g {
		t.Fatalf("after %d concurrent writes count = %d", g, n)
	}
}
