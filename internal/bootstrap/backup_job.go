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

// backupDatabase is the narrow surface the backup package asked for.
type backupDatabase struct{ app *App }

func (b backupDatabase) WriterPool() *sql.DB     { return b.app.DB.WriterPool() }
func (b backupDatabase) Dialect() backup.Dialect { return b.app.DB.Dialect() }
