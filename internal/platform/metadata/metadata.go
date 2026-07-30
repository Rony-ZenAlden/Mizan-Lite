// Package metadata is the second configuration mechanism: reference data that defines the
// KINDS of things the system operates on — rate types, payment methods, units, print
// templates, tax codes (PHASE_0_FOUNDATION §CFG.3).
//
// Every metadata table shares one contract, so the system treats "kinds of things"
// uniformly:
//
//	code       stable machine key; ALL references from code and seeds are by code, never id
//	name       translatable label
//	is_system  seeded and protected — may be deactivated, never deleted
//	is_active  soft availability
//
// `is_system` is the load-bearing column: it is what lets code depend on RATE_TYPE_OFFICIAL
// existing without hardcoding its UUID, which is the difference between "configurable" and
// "fragile".
//
// This package ships the contract and the seeder. Business metadata TABLES arrive with the
// modules that own them (currency and rate types in Step 0.9).
package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeSeedFailed   = "metadata.seed_failed"
	CodeInvalidSeed  = "metadata.invalid_seed"
	CodeSystemRow    = "metadata.system_row_protected"
	CodeDuplicateRow = "metadata.duplicate_code"
)

// Contract columns present on every metadata table.
const (
	ColCode     = "code"
	ColName     = "name"
	ColIsSystem = "is_system"
	ColIsActive = "is_active"
)

// Row is one metadata record in a seed.
type Row struct {
	// Code is the stable machine key. Seeds match on this, never on id.
	Code string
	// Name is the label. Translations for other locales live in the translations table.
	Name string
	// IsSystem marks a row the product depends on: protected from deletion, and kept up
	// to date by re-seeding.
	IsSystem bool
	// IsActive is soft availability. Defaults to true when a seed omits it.
	IsActive *bool
	// Columns carries table-specific scalar columns beyond the contract.
	Columns map[string]any
}

// SeedSpec describes one table's seed.
type SeedSpec struct {
	// Table is the unquoted table name.
	Table string
	// Rows are the records to apply, matched by Code.
	Rows []Row
	// ExtraColumns lists the table-specific columns this seed manages, in a stable order.
	// A column absent here is never written or compared, so a seed cannot silently take
	// ownership of a column it does not know about.
	ExtraColumns []string
}

// Report summarises what a seed run changed. A second run of the same seed must report
// zero inserted and zero updated — that is the idempotency guarantee, and it is asserted
// by test.
type Report struct {
	Inserted int
	Updated  int
	// Unchanged counts system rows already matching the seed.
	Unchanged int
	// SkippedUserOwned counts non-system rows left alone because an admin owns them.
	SkippedUserOwned int
}

// Seeder applies metadata seeds. It holds the clock so seed timestamps are
// deterministic under test.
type Seeder struct {
	db  database.DB
	clk clock.Clock
}

// NewSeeder returns a Seeder. A nil clock uses the system clock.
func NewSeeder(db database.DB, clk clock.Clock) *Seeder {
	if clk == nil {
		clk = clock.System()
	}
	return &Seeder{db: db, clk: clk}
}

// Seed applies spec idempotently.
//
// Rules, each of which exists because its absence produces a specific support disaster:
//
//   - Rows are matched by `code`, never by primary key, so the same seed file applies
//     identically to a fresh install and to a three-year-old database whose ids differ.
//   - Re-running changes nothing when values already match. A seeder that rewrites rows on
//     every launch generates pointless audit noise and looks like corruption.
//   - A non-system row is NEVER modified. Those belong to the admin who created them, and
//     silently reverting their edits on upgrade is the fastest way to lose their trust.
//   - Nothing is ever deleted. Removing a row that documents already reference would break
//     historical records.
func (s *Seeder) Seed(ctx context.Context, spec SeedSpec) (Report, error) {
	var rep Report

	if strings.TrimSpace(spec.Table) == "" {
		return rep, errs.Validation(CodeInvalidSeed, "seed spec has no table")
	}
	if err := checkDuplicateCodes(spec); err != nil {
		return rep, err
	}

	managed := append([]string{ColName, ColIsActive}, spec.ExtraColumns...)

	for _, row := range spec.Rows {
		if strings.TrimSpace(row.Code) == "" {
			return rep, errs.Validation(CodeInvalidSeed,
				"seed row has an empty code").WithParam("table", spec.Table)
		}
		existing, found, err := s.load(ctx, spec.Table, managed, row.Code)
		if err != nil {
			return rep, err
		}

		switch {
		case !found:
			if err := s.insert(ctx, spec, row, managed); err != nil {
				return rep, err
			}
			rep.Inserted++

		case !existing.isSystem:
			// Admin-owned. Leave it exactly as it is.
			rep.SkippedUserOwned++

		default:
			desired := desiredValues(row, spec.ExtraColumns)
			if sameValues(existing.values, desired, managed) {
				rep.Unchanged++
				continue
			}
			if err := s.update(ctx, spec, row, managed, desired); err != nil {
				return rep, err
			}
			rep.Updated++
		}
	}
	return rep, nil
}

type existingRow struct {
	isSystem bool
	values   map[string]string
}

func (s *Seeder) load(ctx context.Context, table string, managed []string, code string) (existingRow, bool, error) {
	d := s.db.Dialect()
	cols := make([]string, 0, len(managed)+1)
	cols = append(cols, d.QuoteIdentifier(ColIsSystem))
	for _, c := range managed {
		cols = append(cols, d.QuoteIdentifier(c))
	}
	query := d.Rebind(fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		strings.Join(cols, ", "), d.QuoteIdentifier(table), d.QuoteIdentifier(ColCode)))

	targets := make([]any, len(cols))
	holders := make([]any, len(cols))
	for i := range targets {
		holders[i] = &targets[i]
	}
	if err := s.db.Reader(ctx).QueryRowContext(ctx, query, code).Scan(holders...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return existingRow{}, false, nil
		}
		return existingRow{}, false, errs.Wrap(d.TranslateError(err), errs.CategoryInternal,
			CodeSeedFailed, "reading metadata row "+table+"."+code)
	}

	out := existingRow{isSystem: normalize(targets[0]) == "1", values: map[string]string{}}
	for i, name := range managed {
		out.values[name] = normalize(targets[i+1])
	}
	return out, true, nil
}

func (s *Seeder) insert(ctx context.Context, spec SeedSpec, row Row, managed []string) error {
	d := s.db.Dialect()
	newID, err := id.New()
	if err != nil {
		return err
	}
	now := clock.Format(s.clk.Now())

	cols := []string{"id", ColCode, ColIsSystem, "created_at", "updated_at"}
	args := []any{newID.String(), row.Code, boolToInt(row.IsSystem), now, now}

	desired := desiredValues(row, spec.ExtraColumns)
	for _, name := range managed {
		cols = append(cols, name)
		args = append(args, desired[name])
	}

	quoted := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.QuoteIdentifier(c)
		marks[i] = "?"
	}
	query := d.Rebind(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		d.QuoteIdentifier(spec.Table), strings.Join(quoted, ", "), strings.Join(marks, ", ")))

	if _, err := s.db.Writer(ctx).ExecContext(ctx, query, args...); err != nil {
		return errs.Wrap(d.TranslateError(err), errs.CategoryInternal, CodeSeedFailed,
			"inserting metadata row "+spec.Table+"."+row.Code)
	}
	return nil
}

func (s *Seeder) update(ctx context.Context, spec SeedSpec, row Row, managed []string, desired map[string]any) error {
	d := s.db.Dialect()
	sets := make([]string, 0, len(managed)+1)
	args := make([]any, 0, len(managed)+2)
	for _, name := range managed {
		sets = append(sets, d.QuoteIdentifier(name)+" = ?")
		args = append(args, desired[name])
	}
	sets = append(sets, d.QuoteIdentifier("updated_at")+" = ?")
	args = append(args, clock.Format(s.clk.Now()))
	args = append(args, row.Code)

	// The is_system guard is in the statement as well as in the caller's branch: a seed
	// must not be able to overwrite an admin's row even if the calling logic is wrong.
	query := d.Rebind(fmt.Sprintf("UPDATE %s SET %s WHERE %s = ? AND %s = 1",
		d.QuoteIdentifier(spec.Table), strings.Join(sets, ", "),
		d.QuoteIdentifier(ColCode), d.QuoteIdentifier(ColIsSystem)))

	if _, err := s.db.Writer(ctx).ExecContext(ctx, query, args...); err != nil {
		return errs.Wrap(d.TranslateError(err), errs.CategoryInternal, CodeSeedFailed,
			"updating metadata row "+spec.Table+"."+row.Code)
	}
	return nil
}

func desiredValues(row Row, extra []string) map[string]any {
	active := true
	if row.IsActive != nil {
		active = *row.IsActive
	}
	out := map[string]any{
		ColName:     row.Name,
		ColIsActive: boolToInt(active),
	}
	for _, name := range extra {
		v, ok := row.Columns[name]
		if !ok {
			out[name] = nil
			continue
		}
		if b, isBool := v.(bool); isBool {
			// Booleans follow the portable SMALLINT(0/1) contract (§8.1) everywhere.
			out[name] = boolToInt(b)
			continue
		}
		out[name] = v
	}
	return out
}

// sameValues compares stored text against what the seed would write.
//
// Comparison is on normalised strings because a driver returns int64 where a seed supplies
// int, and []byte where it supplies string. Comparing the Go values directly would report
// a difference on every run and destroy idempotency.
func sameValues(stored map[string]string, desired map[string]any, managed []string) bool {
	for _, name := range managed {
		if stored[name] != normalize(desired[name]) {
			return false
		}
	}
	return true
}

func normalize(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(t)
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func checkDuplicateCodes(spec SeedSpec) error {
	seen := map[string]bool{}
	var dupes []string
	for _, r := range spec.Rows {
		if seen[r.Code] {
			dupes = append(dupes, r.Code)
		}
		seen[r.Code] = true
	}
	if len(dupes) == 0 {
		return nil
	}
	sort.Strings(dupes)
	return errs.Validation(CodeDuplicateRow, "seed declares the same code more than once").
		WithParam("table", spec.Table).WithParam("codes", strings.Join(dupes, ", "))
}
