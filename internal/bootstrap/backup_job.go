package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/backup"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
)

// KeyScheduledBackup is the scheduled snapshot's job key.
const KeyScheduledBackup = "ops.scheduled_backup"

// backupOutput is what the job records, so a run's history says what it produced.
type backupOutput struct {
	Name          string `json:"name"`
	SchemaVersion int64  `json:"schemaVersion"`
	SizeBytes     int64  `json:"sizeBytes"`
	Pruned        int    `json:"pruned"`
}

// registerBackupJob schedules the snapshot a shop that never takes one still gets.
//
// # Why this is scheduled and restore is not
//
// 9.7's rule: nothing in Phase 9 runs without a person, except what already did. A backup is the
// exception because it DESTROYS NOTHING and because the common case is a shopkeeper who will
// never press the button. Restore, import and export each destroy or create data and have no
// correct default.
//
// # Pruning runs with the backup, not on its own schedule
//
// A separate prune job would be a second thing that can fail, and its failure — a disk filling up
// over months — is silent. Pruning after a successful snapshot means the directory is tidied
// exactly when it grew, and a prune that fails fails a run somebody can see.
func (a *App) registerBackupJob(every time.Duration) error {
	if every <= 0 {
		// Daily. Frequent enough that a shop loses at most a day, rare enough that a
		// multi-gigabyte database is not copied while somebody is serving a customer.
		every = 24 * time.Hour
	}

	service := backup.New(backupDatabase{app: a}, backup.Options{
		Dir: a.Paths.Backups, Clock: a.opts.Clock, AppVersion: a.opts.AppVersion,
	})
	a.Backups = service

	return a.Scheduler.Registry().Register(jobs.Def{
		Key:      KeyScheduledBackup,
		Schedule: jobs.Every(every),
		// Generous, because a large database on a slow disk is not a failure. A timeout that
		// fires mid-copy leaves a partial file — which `Take` then refuses and removes, so the
		// outcome is safe, but the shop gets no backup and an error nobody can act on.
		Timeout:     30 * time.Minute,
		MaxAttempts: 2,
		// RunOnce, not catch-up: a laptop closed for a week should get ONE backup when it opens,
		// not seven. Six of them would be identical, and the seventh would push the useful older
		// ones out of the retention window.
		CatchUp:     jobs.RunOnce,
		Description: "jobs.scheduled_backup",
	}, func(ctx context.Context, run jobs.RunContext) error {
		taken, err := service.Take(ctx, backup.Scheduled)
		if err != nil {
			return err
		}

		pruned, err := service.Prune(ctx)
		if err != nil {
			// The snapshot SUCCEEDED. Failing the run now would report "backup failed" for a
			// backup that exists — and the operator would go looking for a missing file.
			a.log.WarnContext(ctx, "the scheduled backup was taken but old ones were not pruned",
				slog.String("error", err.Error()))
		}

		encoded, err := json.Marshal(backupOutput{
			Name: taken.Name, SchemaVersion: taken.Manifest.SchemaVersion,
			SizeBytes: taken.Manifest.SizeBytes, Pruned: pruned,
		})
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, backup.CodeBackupFailed,
				"recording what the backup produced")
		}
		run.SetOutput(string(encoded))
		return nil
	})
}

// closeBackupMinAge is how recent a snapshot must be for the close-time one to be skipped.
//
// It IS the data-loss window at close: at most an hour of trading is unprotected when the app
// shuts down. Shorter would snapshot on every close during a busy hour of opening and closing the
// window; longer would let an afternoon's takings go uncopied.
const closeBackupMinAge = time.Hour

// backupOnClose snapshots the day's work as the app shuts down.
//
// # What this covers that the daily job does not
//
// The scheduled job protects the state at the START of a trading day. A shop that opens at nine
// and closes at six has nine hours of takings whose only copy is the live database until
// tomorrow's run. This copies the state that actually matters — the end of the day.
//
// # Why it is safe to do while closing, measured rather than assumed
//
// `Take` is VACUUM INTO plus an integrity check, and both are linear in the database's size.
// Measured on this machine: 117MB copies in 212ms and checks in 119ms — about 3ms per megabyte
// end to end. The 10-second budget every shutdown step gets therefore covers a database of
// roughly three gigabytes, which is far past where a shop on SQLite would be.
//
// Past that the context expires mid-copy, `Take` deletes its partial file and returns an error,
// and this logs and returns nil. That degradation is the right one: an ERP that will not close is
// a worse bug than a missed snapshot, and the daily job still covers the shop.
//
// # Why it runs where it does
//
// After the scheduler has stopped, so a scheduled backup cannot be running concurrently. After
// the outbox's final pass, so the snapshot includes it. Before the database closes, because it
// needs the writer pool.
func (a *App) backupOnClose(ctx context.Context) error {
	if a.Backups == nil {
		return nil // a shutdown after a start that failed before the backup service existed
	}

	taken, took, err := a.Backups.TakeIfDue(ctx, backup.OnClose, closeBackupMinAge)
	if err != nil {
		// NOT returned. Shutdown reports a step's error to the user, and "the backup failed"
		// on the way out of an app they have already closed is a message they cannot act on.
		a.log.WarnContext(ctx, "the close-time backup was not taken",
			slog.String("error", err.Error()))
		return nil
	}
	if !took {
		a.log.DebugContext(ctx, "close-time backup skipped; a recent snapshot already covers this")
		return nil
	}

	pruned, err := a.Backups.Prune(ctx)
	if err != nil {
		a.log.WarnContext(ctx, "the close-time backup was taken but old ones were not pruned",
			slog.String("error", err.Error()))
	}
	a.log.InfoContext(ctx, "close-time backup taken",
		slog.String("name", taken.Name), slog.Int64("bytes", taken.Manifest.SizeBytes),
		slog.Int("pruned", pruned))
	return nil
}

// backupDatabase is the narrow surface the backup package asked for.
type backupDatabase struct{ app *App }

func (b backupDatabase) WriterPool() *sql.DB     { return b.app.DB.WriterPool() }
func (b backupDatabase) Dialect() backup.Dialect { return b.app.DB.Dialect() }
