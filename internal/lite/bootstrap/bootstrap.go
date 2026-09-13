// Package bootstrap is Mizan Lite's composition root: the one place the object graph is built.
//
// There is no container and no service locator. A component that needs another receives it in its
// constructor here, so every dependency in the application is visible in one file (Mizan 0.10).
package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	catalogdb "github.com/mizan-erp/mizan/internal/lite/catalog/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/locales"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	ownerdb "github.com/mizan-erp/mizan/internal/lite/owner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/settings"
	settingsdb "github.com/mizan-erp/mizan/internal/lite/settings/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
	stockdb "github.com/mizan-erp/mizan/internal/lite/stock/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/backup"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// Stable error codes. They double as i18n keys.
const (
	CodeStartupFailed   = "lite.bootstrap.startup_failed"
	CodeMigrationFailed = "lite.bootstrap.migration_failed"
)

// KeyScheduledBackup is the daily snapshot's job key.
const KeyScheduledBackup = "lite.scheduled_backup"

const (
	defaultBackupInterval = 24 * time.Hour
	// closeBackupMinAge is the data-loss window at close: a snapshot is taken as the window closes
	// unless one under this age already exists. Mizan 10.16 measured the cost at ~3ms per megabyte.
	closeBackupMinAge = time.Hour
	// defaultShutdownStep bounds each shutdown step. An ERP that will not close is a worse defect
	// than a skipped close-time snapshot; the daily one still covers the shop.
	defaultShutdownStep = 10 * time.Second
)

// Options configures the composition root.
type Options struct {
	// Paths locates the shop's data. Required.
	Paths  paths.Paths
	Logger *slog.Logger
	Clock  clock.Clock
	// AppVersion is recorded in every backup manifest.
	AppVersion string
	// Progress, if set, receives migration progress for the boot screen.
	Progress func(migrate.Progress)
	// StartScheduler runs the job loop. Tests leave it off and drive jobs directly, so a test is
	// never a race against wall time.
	StartScheduler bool
	// SkipMigrationBackup disables the pre-migration snapshot. TESTS ONLY.
	SkipMigrationBackup bool
	// BackupInterval is how often the scheduled snapshot runs. Default 24h.
	BackupInterval time.Duration
	// ShutdownStepTimeout bounds each shutdown step. Default 10s.
	ShutdownStepTimeout time.Duration
	// PINHasher hashes the owner's PIN and recovery code. Default: Argon2id at password strength
	// (crypto.DefaultParams). Tests pass a cheap one; nothing about the owner's logic depends on the cost.
	PINHasher crypto.Hasher
	// Random draws recovery codes. Default crypto/rand.Reader.
	Random io.Reader
	// Location is the shop's time zone, which decides a movement's business date. Default time.Local — which Go reads
	// from the operating system on Windows and macOS alike. Never time.LoadLocation: Windows ships no zone database.
	Location *time.Location
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.BackupInterval <= 0 {
		o.BackupInterval = defaultBackupInterval
	}
	if o.ShutdownStepTimeout <= 0 {
		o.ShutdownStepTimeout = defaultShutdownStep
	}
	if o.PINHasher == nil {
		o.PINHasher = crypto.NewArgon2id(crypto.DefaultParams())
	}
	if o.Random == nil {
		o.Random = rand.Reader
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	return o
}

// App is the built graph. Fields are for the binding layer and for tests.
type App struct {
	Paths     paths.Paths
	DB        *database.Store
	Settings  *settings.Service
	Messages  *i18n.Catalog
	Scheduler *jobs.Scheduler
	Backups   *backup.Service
	Catalog   *catalog.Service
	Owner     *owner.Service
	Setup     *setup.Service
	Stock     *stock.Service
	// SchemaVersion is the migration the database is at, read from the runner's RESULT rather than
	// from the migration list: what the file holds and what the binary targets can differ.
	SchemaVersion int64

	log          *slog.Logger
	opts         Options
	shutdownOnce sync.Once
	shutdownErr  error
}

// Start builds the graph: database → migrations → catalogs → settings → backups and jobs.
//
// Any failure closes whatever was opened and returns a typed error. A migration failure carries the
// runner's parameters — including the path of the pre-migration backup — because "your data is
// safe, and here it is" is the one thing a shopkeeper needs from a failed start.
//
// contextcheck is disabled for this function, as for Mizan's composition root: every failure path closes
// the database with Store.Close, which takes no context by design — a cancelled context must not leave the
// write-ahead log unmerged and, on Windows, the file locked.
//
//nolint:contextcheck // teardown on a failed start must not be cancellable
func Start(ctx context.Context, opts Options) (*App, error) {
	opts = opts.withDefaults()
	if opts.Paths.DBFile == "" || opts.Paths.Backups == "" {
		return nil, errs.Internal(CodeStartupFailed, "bootstrap: Options.Paths is required")
	}
	app := &App{Paths: opts.Paths, log: opts.Logger, opts: opts}

	db, err := database.Open(database.Config{Path: opts.Paths.DBFile})
	if err != nil {
		return nil, wrapKeepingParams(err, CodeStartupFailed, "opening the database")
	}
	app.DB = db

	if err = app.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	messages, err := i18n.LoadFS(locales.FS())
	if err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "loading the message catalogs")
	}
	app.Messages = messages

	app.Settings = settings.NewService(db, settingsdb.NewStore(db), opts.Clock, opts.Logger)
	// Read once now. A settings table that cannot be read is found here, at launch, rather than by
	// the first screen that asks — and a damaged VALUE never fails this (settings.FromStored).
	if _, err = app.Settings.Get(ctx); err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "reading settings")
	}

	app.Owner = owner.NewService(db, ownerdb.NewStore(db), opts.PINHasher, opts.Clock, opts.Random, opts.Logger)
	app.Catalog = catalog.NewService(db, catalogdb.NewStore(db, opts.Clock), ownerGate{owner: app.Owner})
	app.Setup = setup.NewService(db, app.Settings, app.Owner)
	app.Stock = stock.NewService(db, stockdb.NewStore(db, opts.Clock), stockCatalogue{catalog: app.Catalog},
		stockGate{owner: app.Owner}, opts.Clock, opts.Location)

	scheduler, err := jobs.New(db, jobs.Options{Clock: opts.Clock, Logger: opts.Logger})
	if err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "building the job scheduler")
	}
	app.Scheduler = scheduler
	app.Backups = backup.New(backupDatabase{db: db}, backup.Options{
		Dir: opts.Paths.Backups, Clock: opts.Clock, AppVersion: opts.AppVersion,
	})
	if err = app.registerBackupJob(); err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "declaring the backup job")
	}

	if opts.StartScheduler {
		if err = scheduler.Start(ctx); err != nil {
			_ = db.Close()
			return nil, wrapKeepingParams(err, CodeStartupFailed, "starting the job scheduler")
		}
	} else if _, err = scheduler.Reconcile(ctx); err != nil {
		// Even with no loop, the jobs table must reflect this build, or a test driving jobs directly
		// finds nothing declared.
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "reconciling jobs")
	}

	opts.Logger.InfoContext(ctx, "mizan lite started",
		slog.String("data_dir", opts.Paths.Data),
		slog.Int64("schema_version", app.SchemaVersion))
	return app, nil
}

func (a *App) migrate(ctx context.Context) error {
	runner, err := migrate.New(a.DB, migrate.Options{
		FS:         migrations.SQLite(),
		DBPath:     a.Paths.DBFile,
		BackupDir:  a.Paths.Backups,
		SkipBackup: a.opts.SkipMigrationBackup,
		Progress:   a.opts.Progress,
		Clock:      a.opts.Clock,
		Logger:     a.log,
	})
	if err != nil {
		return wrapKeepingParams(err, CodeMigrationFailed, "preparing the database update")
	}
	result, err := runner.Up(ctx)
	if err != nil {
		return wrapKeepingParams(err, CodeMigrationFailed, "the database update failed")
	}
	a.SchemaVersion = result.ToVersion
	if a.SchemaVersion == 0 {
		a.SchemaVersion = result.FromVersion
	}
	if result.Applied > 0 {
		a.log.InfoContext(ctx, "database updated",
			slog.Int("applied", result.Applied), slog.Int64("to_version", result.ToVersion))
	}
	return nil
}

// registerBackupJob schedules the snapshot a shop that never presses a button still gets.
//
// RunOnce catch-up: a laptop closed for a week gets ONE backup when it opens, not seven identical
// ones pushing the useful older snapshots out of retention (Mizan 9.1).
func (a *App) registerBackupJob() error {
	return a.Scheduler.Registry().Register(jobs.Def{
		Key:         KeyScheduledBackup,
		Schedule:    jobs.Every(a.opts.BackupInterval),
		Timeout:     30 * time.Minute,
		MaxAttempts: 2,
		CatchUp:     jobs.RunOnce,
		Description: "lite.jobs.scheduled_backup",
	}, func(ctx context.Context, _ jobs.RunContext) error {
		if _, err := a.Backups.Take(ctx, backup.Scheduled); err != nil {
			return err
		}
		if _, err := a.Backups.Prune(ctx); err != nil {
			// The snapshot exists. Failing the run would report a backup failure for a backup that
			// is on disk.
			a.log.WarnContext(ctx, "the scheduled backup was taken but old ones were not pruned",
				slog.Any("error", err))
		}
		return nil
	})
}

// Shutdown stops the scheduler, takes a close-time snapshot if none is recent, and closes the
// database. It is safe to call more than once; only the first call does anything.
//
// # The order is load-bearing
//
// The scheduler stops first, so a scheduled backup cannot be running while the close-time one
// starts. The snapshot comes before the database closes, because it needs the writer. And the
// database closes last and ALWAYS — even if everything before it failed — because on Windows an
// unclosed handle leaves the write-ahead log unmerged and the file locked.
//
//nolint:contextcheck // Store.Close takes no context by design; see Start
func (a *App) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		var failures []error

		stepCtx, cancel := context.WithTimeout(ctx, a.opts.ShutdownStepTimeout)
		if err := a.Scheduler.Stop(stepCtx); err != nil {
			failures = append(failures, err)
		}
		cancel()

		stepCtx, cancel = context.WithTimeout(ctx, a.opts.ShutdownStepTimeout)
		a.backupOnClose(stepCtx)
		cancel()

		if err := a.DB.Close(); err != nil {
			failures = append(failures, err)
		}
		a.shutdownErr = errors.Join(failures...)
	})
	return a.shutdownErr
}

// backupOnClose snapshots the end of the trading day. It reports nothing to the caller: "the backup
// failed" on the way out of an application somebody has already closed is a message nobody can act
// on, so it goes to the log.
func (a *App) backupOnClose(ctx context.Context) {
	taken, took, err := a.Backups.TakeIfDue(ctx, backup.OnClose, closeBackupMinAge)
	switch {
	case err != nil:
		a.log.WarnContext(ctx, "the close-time backup was not taken", slog.Any("error", err))
	case !took:
		a.log.DebugContext(ctx, "close-time backup skipped; a recent snapshot covers this")
	default:
		if _, err := a.Backups.Prune(ctx); err != nil {
			a.log.WarnContext(ctx, "the close-time backup was taken but old ones were not pruned",
				slog.Any("error", err))
		}
		a.log.InfoContext(ctx, "close-time backup taken", slog.String("name", taken.Name))
	}
}

// wrapKeepingParams wraps err under code and carries the cause's parameters onto the wrapper.
//
// errs.AsError finds the OUTERMOST error, so without this the backup path the migration runner
// attaches is lost the moment it is wrapped — and the UI renders parameters, it cannot walk a chain.
func wrapKeepingParams(err error, code, msg string) error {
	wrapped := errs.Wrap(err, errs.CategoryInternal, code, msg)
	if inner, ok := errs.AsError(err); ok {
		for k, v := range inner.Params {
			wrapped = wrapped.WithParam(k, v)
		}
	}
	return wrapped
}

// backupDatabase adapts the store to the narrow surface platform/backup declares. The adapter
// exists because Store.Dialect returns the dialect package's interface and backup declares its own
// smaller one; Go interfaces match on exact method signatures.
type backupDatabase struct{ db *database.Store }

func (b backupDatabase) WriterPool() *sql.DB     { return b.db.WriterPool() }
func (b backupDatabase) Dialect() backup.Dialect { return b.db.Dialect() }

// ownerGate satisfies the catalogue's OwnerGate port with the owner service. The port is declared by the
// module that needs it, so catalog never imports owner and owner never learns what a product is (D-L1.9);
// this adapter is the only place the two meet.
type ownerGate struct{ owner *owner.Service }

func (g ownerGate) Require(ctx context.Context, act catalog.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{
		Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After,
	})
}

// stockGate satisfies stock's OwnerGate port with the owner service, as ownerGate does for the catalogue.
type stockGate struct{ owner *owner.Service }

func (g stockGate) Require(ctx context.Context, act stock.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{
		Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After,
	})
}

func (g stockGate) Allowed(ctx context.Context) bool { return g.owner.Allowed(ctx) }

// stockCatalogue satisfies stock's Catalogue port with the catalogue service: what stock needs to know about a
// product, and nothing else of it.
type stockCatalogue struct{ catalog *catalog.Service }

func (c stockCatalogue) Product(ctx context.Context, productID id.ID) (stockdomain.Product, error) {
	p, err := c.catalog.Get(ctx, productID)
	if err != nil {
		return stockdomain.Product{}, err
	}
	ref, err := c.catalog.Reference(ctx)
	if err != nil {
		return stockdomain.Product{}, err
	}
	return stockProduct(p, ref), nil
}

func (c stockCatalogue) Products(ctx context.Context) ([]stockdomain.Product, error) {
	all, err := c.catalog.All(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := c.catalog.Reference(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]stockdomain.Product, 0, len(all))
	for _, p := range all {
		out = append(out, stockProduct(p, ref))
	}
	return out, nil
}

func (c stockCatalogue) Package(ctx context.Context, packageProductID id.ID) (stockdomain.Package, bool, error) {
	link, found, err := c.catalog.Package(ctx, packageProductID)
	if err != nil || !found {
		return stockdomain.Package{}, false, err
	}
	return stockdomain.Package{ContentProductID: link.ContentProductID, ContentQuantityMicro: link.ContentQuantityMicro}, true, nil
}

func (c stockCatalogue) Currencies(ctx context.Context) ([]stockdomain.Currency, error) {
	currencies, err := c.catalog.Currencies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]stockdomain.Currency, 0, len(currencies))
	for _, cur := range currencies {
		out = append(out, stockdomain.Currency{Code: cur.Code, Decimals: cur.Decimals})
	}
	return out, nil
}

func stockProduct(p catalogdomain.Product, ref catalogdomain.Reference) stockdomain.Product {
	return stockdomain.Product{ID: p.ID, UnitDecimals: ref.Units[p.UnitCode].InputDecimals, Active: p.Active}
}
