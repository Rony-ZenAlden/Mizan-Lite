package metadata_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
)

// A fixture table shaped exactly like a real metadata table (§CFG.3): the contract
// columns, the universal columns, and one table-specific column.
const createRateTypes = `
CREATE TABLE rate_types (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  code        VARCHAR(60)  NOT NULL,
  name        VARCHAR(200) NOT NULL,
  is_system   SMALLINT     NOT NULL DEFAULT 0,
  is_active   SMALLINT     NOT NULL DEFAULT 1,
  sort_order  BIGINT       NOT NULL DEFAULT 0,
  created_at  CHAR(24)     NOT NULL,
  updated_at  CHAR(24)     NOT NULL,
  row_version BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_rate_types_code UNIQUE (code),
  CONSTRAINT ck_rate_types_system CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_rate_types_active CHECK (is_active IN (0, 1))
)`

func newSeeder(t *testing.T) (*metadata.Seeder, *database.Store) {
	t.Helper()
	st, err := database.Open(database.Config{Path: filepath.Join(t.TempDir(), "meta.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.WriterPool().ExecContext(context.Background(), createRateTypes); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	return metadata.NewSeeder(st, clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))), st
}

func spec(rows ...metadata.Row) metadata.SeedSpec {
	return metadata.SeedSpec{Table: "rate_types", Rows: rows, ExtraColumns: []string{"sort_order"}}
}

func systemRow(code, name string, order int64) metadata.Row {
	return metadata.Row{
		Code: code, Name: name, IsSystem: true,
		Columns: map[string]any{"sort_order": order},
	}
}

func readRow(t *testing.T, st *database.Store, code string) (name string, isSystem, isActive int64, order int64) {
	t.Helper()
	err := st.WriterPool().QueryRowContext(context.Background(),
		`SELECT name, is_system, is_active, sort_order FROM rate_types WHERE code = ?`, code).
		Scan(&name, &isSystem, &isActive, &order)
	if err != nil {
		t.Fatalf("read %q: %v", code, err)
	}
	return
}

// ── the idempotency drill ───────────────────────────────────────────────────────

func TestSeedingTwiceChangesNothing(t *testing.T) {
	// The guarantee: re-running a seed on every launch must be inert. A seeder that
	// rewrites rows each boot produces audit noise indistinguishable from corruption.
	ctx := context.Background()
	seeder, st := newSeeder(t)
	s := spec(
		systemRow("official", "Official Rate", 1),
		systemRow("market", "Market Rate", 2),
	)

	first, err := seeder.Seed(ctx, s)
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if first.Inserted != 2 || first.Updated != 0 {
		t.Fatalf("first run = %+v, want 2 inserted", first)
	}

	second, err := seeder.Seed(ctx, s)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 0 {
		t.Errorf("second run = %+v, want nothing inserted or updated", second)
	}
	if second.Unchanged != 2 {
		t.Errorf("second run Unchanged = %d, want 2", second.Unchanged)
	}

	var rows int
	if err := st.WriterPool().QueryRowContext(ctx, `SELECT COUNT(*) FROM rate_types`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("table has %d rows after two identical seeds, want 2", rows)
	}
}

func TestSeedMatchesByCodeNotByID(t *testing.T) {
	// The same seed must apply identically to a fresh install and to a three-year-old
	// database whose primary keys are entirely different.
	ctx := context.Background()
	seeder, st := newSeeder(t)

	if _, err := st.WriterPool().ExecContext(ctx,
		`INSERT INTO rate_types (id, code, name, is_system, is_active, sort_order, created_at, updated_at)
		 VALUES ('an-id-from-another-install', 'official', 'Stale Label', 1, 1, 99,
		   '2023-01-01T00:00:00.000Z', '2023-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}

	rep, err := seeder.Seed(ctx, spec(systemRow("official", "Official Rate", 1)))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rep.Inserted != 0 {
		t.Errorf("inserted %d rows; the existing row should have matched by code", rep.Inserted)
	}
	if rep.Updated != 1 {
		t.Errorf("updated %d rows, want 1", rep.Updated)
	}

	name, _, _, order := readRow(t, st, "official")
	if name != "Official Rate" || order != 1 {
		t.Errorf("system row not refreshed: name=%q sort_order=%d", name, order)
	}

	var rows int
	if err := st.WriterPool().QueryRowContext(ctx, `SELECT COUNT(*) FROM rate_types`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("table has %d rows, want 1 — matching by id would have duplicated it", rows)
	}
}

func TestSeedNeverTouchesAdminOwnedRows(t *testing.T) {
	// Silently reverting an admin's edits on upgrade is the fastest way to lose trust.
	ctx := context.Background()
	seeder, st := newSeeder(t)

	if _, err := st.WriterPool().ExecContext(ctx,
		`INSERT INTO rate_types (id, code, name, is_system, is_active, sort_order, created_at, updated_at)
		 VALUES ('user-made', 'custom', 'My Custom Rate', 0, 1, 5,
		   '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}

	// A seed that names the same code must leave the admin's row alone.
	rep, err := seeder.Seed(ctx, spec(systemRow("custom", "Vendor Label", 99)))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if rep.SkippedUserOwned != 1 {
		t.Errorf("SkippedUserOwned = %d, want 1", rep.SkippedUserOwned)
	}
	if rep.Updated != 0 {
		t.Errorf("updated %d admin-owned rows, want 0", rep.Updated)
	}

	name, isSystem, _, order := readRow(t, st, "custom")
	if name != "My Custom Rate" || order != 5 {
		t.Errorf("admin's row was modified: name=%q sort_order=%d", name, order)
	}
	if isSystem != 0 {
		t.Error("seeding promoted an admin row to a system row")
	}
}

func TestSeedRefreshesChangedSystemRows(t *testing.T) {
	ctx := context.Background()
	seeder, st := newSeeder(t)

	if _, err := seeder.Seed(ctx, spec(systemRow("official", "Old Name", 1))); err != nil {
		t.Fatal(err)
	}
	rep, err := seeder.Seed(ctx, spec(systemRow("official", "Corrected Name", 3)))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Updated != 1 {
		t.Errorf("Updated = %d, want 1 — a corrected system label must reach existing installs", rep.Updated)
	}
	name, _, _, order := readRow(t, st, "official")
	if name != "Corrected Name" || order != 3 {
		t.Errorf("system row not refreshed: name=%q sort_order=%d", name, order)
	}
}

func TestSeedRespectsIsActive(t *testing.T) {
	ctx := context.Background()
	seeder, st := newSeeder(t)

	inactive := false
	row := systemRow("legacy", "Legacy Rate", 9)
	row.IsActive = &inactive

	if _, err := seeder.Seed(ctx, spec(row)); err != nil {
		t.Fatal(err)
	}
	if _, _, isActive, _ := readRow(t, st, "legacy"); isActive != 0 {
		t.Errorf("is_active = %d, want 0", isActive)
	}

	// Defaults to active when the seed omits it — a system row may be deactivated but is
	// never deleted.
	if _, err := seeder.Seed(ctx, spec(systemRow("current", "Current", 1))); err != nil {
		t.Fatal(err)
	}
	if _, _, isActive, _ := readRow(t, st, "current"); isActive != 1 {
		t.Errorf("is_active = %d, want 1 by default", isActive)
	}
}

func TestSeedNeverDeletes(t *testing.T) {
	// Removing a row that documents already reference would break historical records.
	ctx := context.Background()
	seeder, st := newSeeder(t)

	if _, err := seeder.Seed(ctx, spec(
		systemRow("official", "Official", 1),
		systemRow("retired", "Retired", 2),
	)); err != nil {
		t.Fatal(err)
	}
	// A later release drops "retired" from the seed entirely.
	if _, err := seeder.Seed(ctx, spec(systemRow("official", "Official", 1))); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := st.WriterPool().QueryRowContext(ctx, `SELECT COUNT(*) FROM rate_types`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("table has %d rows, want 2 — a row dropped from a seed must not be deleted", rows)
	}
}

func TestSeedRejectsInvalidSpecs(t *testing.T) {
	ctx := context.Background()
	seeder, _ := newSeeder(t)

	t.Run("no table", func(t *testing.T) {
		_, err := seeder.Seed(ctx, metadata.SeedSpec{Rows: []metadata.Row{systemRow("a", "A", 1)}})
		if errs.CodeOf(err) != metadata.CodeInvalidSeed {
			t.Fatalf("code = %q, want %q", errs.CodeOf(err), metadata.CodeInvalidSeed)
		}
	})
	t.Run("empty code", func(t *testing.T) {
		_, err := seeder.Seed(ctx, spec(metadata.Row{Code: "  ", Name: "x", IsSystem: true}))
		if errs.CodeOf(err) != metadata.CodeInvalidSeed {
			t.Fatalf("code = %q, want %q", errs.CodeOf(err), metadata.CodeInvalidSeed)
		}
	})
	t.Run("duplicate code within one seed", func(t *testing.T) {
		_, err := seeder.Seed(ctx, spec(
			systemRow("same", "First", 1),
			systemRow("same", "Second", 2),
		))
		if errs.CodeOf(err) != metadata.CodeDuplicateRow {
			t.Fatalf("code = %q, want %q", errs.CodeOf(err), metadata.CodeDuplicateRow)
		}
	})
}

func TestSeedRunsInsideAUnitOfWork(t *testing.T) {
	// A seed that fails part-way must leave nothing behind.
	ctx := context.Background()
	seeder, st := newSeeder(t)

	err := st.Do(ctx, func(ctx context.Context) error {
		if _, seedErr := seeder.Seed(ctx, spec(systemRow("official", "Official", 1))); seedErr != nil {
			return seedErr
		}
		return errs.Internal("test.abort", "something later in the transaction failed")
	})
	if err == nil {
		t.Fatal("expected the unit of work to fail")
	}

	var rows int
	if err := st.WriterPool().QueryRowContext(ctx, `SELECT COUNT(*) FROM rate_types`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("table has %d rows after a rolled-back seed, want 0", rows)
	}
}
