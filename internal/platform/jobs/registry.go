package jobs

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Registry holds the jobs a build knows how to run.
//
// Jobs are declared in code, like settings (Step 0.5): the set of jobs is a property of the
// binary, and the table holds only their state. Registration is explicit at bootstrap — no
// discovery — so "what runs in the background?" is answerable by reading one file.
type Registry struct {
	mu   sync.RWMutex
	defs map[string]entry
}

type entry struct {
	def     Def
	handler Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{defs: map[string]entry{}} }

// Register declares a job.
//
// A duplicate key is an error rather than an overwrite: two modules claiming
// "reports.rebuild" would otherwise resolve by package initialisation order and could differ
// between builds.
func (r *Registry) Register(def Def, handler Handler) error {
	// Defaults are applied BEFORE validation, not after. Validating the raw struct rejected
	// every job that left CatchUp at its zero value — which is to say, every job declared the
	// ordinary way. The zero value must be usable, or the defaults are decoration.
	def = def.withDefaults()
	if err := def.validate(); err != nil {
		return err
	}
	if handler == nil {
		return errs.Validation(CodeInvalidDef, "a job needs a handler").WithParam("key", def.Key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.defs[def.Key]; exists {
		return errs.Conflict(CodeDuplicateJob, "job key declared more than once").
			WithParam("key", def.Key)
	}
	r.defs[def.Key] = entry{def: def, handler: handler}
	return nil
}

func (r *Registry) lookup(key string) (entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.defs[key]
	return e, ok
}

// Defs returns every declared job, ordered by key.
func (r *Registry) Defs() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(r.defs))
	for _, e := range r.defs {
		out = append(out, e.def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Keys returns the declared job keys, ordered.
func (r *Registry) Keys() []string {
	defs := r.Defs()
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Key
	}
	return out
}

// ── reconciliation ──────────────────────────────────────────────────────────────

// ReconcileReport summarises what startup reconciliation changed.
type ReconcileReport struct {
	Inserted int
	Updated  int
	// Disabled counts rows whose job no longer exists in this build.
	Disabled int
}

// Reconcile brings the jobs table into line with what this build declares.
//
// Three rules, each chosen for a specific failure it prevents:
//
//   - A new declaration inserts a row, due immediately, so adding a job needs no manual step.
//   - An existing row has its schedule and limits refreshed from code, but keeps its
//     is_enabled and next_run_at. An admin who disabled the nightly backup must not find it
//     silently re-enabled by an upgrade.
//   - A row with no declaration — a job removed in a new release, or a downgrade — is
//     DISABLED and reported, never fatal. Same reasoning as unknown settings keys (0.5, D3):
//     the row is inert once disabled, and refusing to start would keep a shop closed over
//     something harmless.
func (s *Scheduler) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var rep ReconcileReport

	existing, err := s.loadJobRows(ctx)
	if err != nil {
		return rep, err
	}

	for _, def := range s.reg.Defs() {
		row, found := existing[def.Key]
		if !found {
			if err := s.insertJobAt(ctx, def); err != nil {
				return rep, err
			}
			rep.Inserted++
			continue
		}
		if err := s.updateJobDefinition(ctx, row.id, def); err != nil {
			return rep, err
		}
		rep.Updated++
	}

	declared := map[string]bool{}
	for _, k := range s.reg.Keys() {
		declared[k] = true
	}
	for key, row := range existing {
		if declared[key] || !row.enabled {
			continue
		}
		if err := s.disableJob(ctx, row.id); err != nil {
			return rep, err
		}
		rep.Disabled++
		s.log.WarnContext(ctx, "disabling a scheduled job this build no longer declares",
			slog.String("job_key", key))
	}
	return rep, nil
}

func (s *Scheduler) insertJobAt(ctx context.Context, def Def) error {
	newID, err := id.New()
	if err != nil {
		return err
	}
	ts := clock.Format(s.clk.Now())
	// next_run_at = now: a newly declared job is due immediately, so a fresh install does
	// not wait a full interval before its first dispatch.
	_, err = s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO jobs
		  (id, job_key, schedule_cron, is_enabled, is_singleton, catch_up_policy,
		   timeout_seconds, max_attempts, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		newID.String(), def.Key, def.Schedule.String(),
		boolToInt(!def.AllowConcurrent), string(def.CatchUp),
		int64(def.Timeout.Seconds()), int64(def.MaxAttempts), ts, ts, ts)
	if err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeReconcileFailed, "inserting job "+def.Key)
	}
	return nil
}

// updateJobDefinition refreshes the code-owned fields only. is_enabled and next_run_at are
// state, owned by the admin and the scheduler respectively.
func (s *Scheduler) updateJobDefinition(ctx context.Context, jobID id.ID, def Def) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE jobs
		   SET schedule_cron = ?, is_singleton = ?, catch_up_policy = ?,
		       timeout_seconds = ?, max_attempts = ?, updated_at = ?
		 WHERE id = ?`,
		def.Schedule.String(), boolToInt(!def.AllowConcurrent), string(def.CatchUp),
		int64(def.Timeout.Seconds()), int64(def.MaxAttempts),
		clock.Format(s.clk.Now()), jobID.String())
	if err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeReconcileFailed, "updating job "+def.Key)
	}
	return nil
}

func (s *Scheduler) disableJob(ctx context.Context, jobID id.ID) error {
	_, err := s.db.Writer(ctx).ExecContext(ctx,
		`UPDATE jobs SET is_enabled = 0, updated_at = ? WHERE id = ?`,
		clock.Format(s.clk.Now()), jobID.String())
	if err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeReconcileFailed, "disabling job")
	}
	return nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
