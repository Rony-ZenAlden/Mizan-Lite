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
	"github.com/mizan-erp/mizan/internal/lite/backups"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	cashbookdb "github.com/mizan-erp/mizan/internal/lite/cashbook/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	catalogdb "github.com/mizan-erp/mizan/internal/lite/catalog/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	customersdb "github.com/mizan-erp/mizan/internal/lite/customers/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	fxdb "github.com/mizan-erp/mizan/internal/lite/fx/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/locales"
	"github.com/mizan-erp/mizan/internal/lite/migrations"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	ownerdb "github.com/mizan-erp/mizan/internal/lite/owner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/printing"
	printingdb "github.com/mizan-erp/mizan/internal/lite/printing/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/reports"
	reportsdomain "github.com/mizan-erp/mizan/internal/lite/reports/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	salesdb "github.com/mizan-erp/mizan/internal/lite/sales/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/settings"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
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

// KeyRateRefresh is the exchange-rate fetch's job key (L3 §14.8).
const KeyRateRefresh = "lite.fx.refresh"

const (
	defaultBackupInterval = 24 * time.Hour
	// closeBackupMinAge is the data-loss window at close: a snapshot is taken as the window closes
	// unless one under this age already exists. Mizan 10.16 measured the cost at ~3ms per megabyte.
	closeBackupMinAge = time.Hour
	// defaultShutdownStep bounds each shutdown step. An ERP that will not close is a worse defect
	// than a skipped close-time snapshot; the daily one still covers the shop.
	defaultShutdownStep = 10 * time.Second
	// defaultRateRefresh is how often the rate is fetched in the background (L3 §14.4).
	defaultRateRefresh = time.Hour
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
	// RateSource fetches the exchange rate from the internet. nil — the default — registers no provider and no fetch
	// job: tests and the demo seeder are offline by construction, and only apps/lite passes the real providers
	// (D-L3.26).
	RateSource fx.Source
	// RateRefreshEvery is how often the background job fetches the rate. Default 1h.
	RateRefreshEvery time.Duration
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
	if o.RateRefreshEvery <= 0 {
		o.RateRefreshEvery = defaultRateRefresh
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
	FX        *fx.Service
	Sales     *sales.Service
	Customers *customers.Service
	Cashbook  *cashbook.Service
	Reports   *reports.Service
	Printing  *printing.Service
	// Safety is the backups module: the outside copy, the status, restores (L7). Backups is platform/backup beneath it.
	Safety *backups.Service
	// Restored is the restore applied at this start, or nil.
	Restored *backup.Intent
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

	// A restore staged by the Backups screen is swapped in before anything opens the database (L7 A-L7.5). A failure leaves the
	// live file where it was and stops the start with its reason.
	if intent, applied, applyErr := backup.Apply(opts.Paths.DBFile); applyErr != nil {
		return nil, wrapKeepingParams(applyErr, CodeStartupFailed, "applying the staged restore")
	} else if applied {
		app.Restored = &intent
		opts.Logger.InfoContext(ctx, "restored a backup", slog.String("from", intent.From), slog.String("safety_snapshot", intent.SafetyBackup))
	}

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
	app.FX = fx.NewService(db, fxdb.NewStore(db), fxSettings{settings: app.Settings}, fxGate{owner: app.Owner},
		opts.RateSource, opts.Clock, opts.Location)
	app.Setup = setup.NewService(db, app.Settings, app.Owner, setupRates{fx: app.FX})
	app.Stock = stock.NewService(db, stockdb.NewStore(db, opts.Clock), stockCatalogue{catalog: app.Catalog},
		stockGate{owner: app.Owner}, opts.Clock, opts.Location)
	app.Customers = customers.NewService(db, customersdb.NewStore(db, opts.Clock), customersRates{fx: app.FX}, customersSettings{settings: app.Settings},
		customersCurrencies{catalog: app.Catalog}, customersGate{owner: app.Owner}, opts.Clock, opts.Location)
	// The till is built last: its adapters hold the services above, so each must already exist.
	app.Sales = sales.NewService(db, salesdb.NewStore(db, opts.Clock), salesCatalogue{catalog: app.Catalog}, salesStock{stock: app.Stock},
		salesDebts{customers: app.Customers}, salesRates{fx: app.FX}, salesSettings{settings: app.Settings}, salesGate{owner: app.Owner},
		opts.Clock, opts.Location)
	// The cash book records a count against the reports' expected figure, and the reports read the cash book: the cash
	// book reaches the reports through an adapter holding the app, filled in on the next line.
	app.Cashbook = cashbook.NewService(db, cashbookdb.NewStore(db, opts.Clock), customersRates{fx: app.FX}, cashbookCurrencies{catalog: app.Catalog},
		cashbookGate{owner: app.Owner}, cashbookExpected{app: app}, opts.Clock, opts.Location)
	app.Reports = reports.NewService(reportsSales{sales: app.Sales}, reportsStock{stock: app.Stock}, reportsDebts{customers: app.Customers},
		reportsRates{fx: app.FX}, reportsCatalogue{catalog: app.Catalog}, reportsCash{cashbook: app.Cashbook}, reportsGate{owner: app.Owner},
		opts.Clock, opts.Location)

	app.Printing = printing.NewService(db, printingdb.NewStore(db), opts.Clock)
	app.Customers.SetVouchers(app.Printing)
	if app.Restored != nil {
		if err = app.Owner.RecordRestored(ctx, owner.Act{Action: backups.ActRestore, Before: app.Restored.SafetyBackup, After: app.Restored.From}); err != nil {
			_ = db.Close()
			return nil, wrapKeepingParams(err, CodeStartupFailed, "recording the restore")
		}
	}

	scheduler, err := jobs.New(db, jobs.Options{Clock: opts.Clock, Logger: opts.Logger})
	if err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "building the job scheduler")
	}
	app.Scheduler = scheduler
	app.Backups = backup.New(backupDatabase{db: db}, backup.Options{
		Dir: opts.Paths.Backups, Clock: opts.Clock, AppVersion: opts.AppVersion,
	})
	app.Safety = backups.NewService(app.Backups, backupSettings{settings: app.Settings}, backupsGate{owner: app.Owner},
		backupActivity{app: app}, opts.Clock, backups.Config{LivePath: opts.Paths.DBFile, SchemaVersion: app.SchemaVersion,
			IntegrityStatement: db.Dialect().IntegrityCheckStatement(), BackupDir: opts.Paths.Backups})
	if err = app.registerBackupJob(); err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "declaring the backup job")
	}
	if err = app.registerRateJob(); err != nil {
		_ = db.Close()
		return nil, wrapKeepingParams(err, CodeStartupFailed, "declaring the rate job")
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
		due, err := a.scheduledBackupDue(ctx)
		if err != nil {
			return err
		}
		if !due {
			return nil
		}
		taken, err := a.Backups.Take(ctx, backup.Scheduled)
		if err != nil {
			return err
		}
		a.copyOutside(ctx, taken)
		if _, err := a.Backups.Prune(ctx); err != nil {
			// The snapshot exists. Failing the run would report a backup failure for a backup that
			// is on disk.
			a.log.WarnContext(ctx, "the scheduled backup was taken but old ones were not pruned",
				slog.Any("error", err))
		}
		return nil
	})
}

// scheduledBackupDue reads the shop's chosen frequency (the owner's request, 2026-09-16) and says whether the unattended
// backup is owed. The job itself keeps its daily beat; this decides whether that beat takes a snapshot, so a shop can
// change the setting without the scheduler being rebuilt.
//
// The backups on close, before a migration and before a restore are NOT governed by this: they are the ones that save a
// shop from the thing about to happen, and a shop that chose "manual only" still gets them.
func (a *App) scheduledBackupDue(ctx context.Context) (bool, error) {
	stored, err := a.Settings.Get(ctx)
	if err != nil {
		return false, err
	}
	var every time.Duration
	switch stored.BackupEvery {
	case settingsdomain.BackupManual:
		return false, nil
	case settingsdomain.BackupWeekly:
		every = 7 * 24 * time.Hour
	case settingsdomain.BackupMonthly:
		every = 30 * 24 * time.Hour
	default: // daily, and anything a future version writes that this one does not know
		return true, nil
	}
	taken, err := a.Safety.List(ctx)
	if err != nil {
		return false, err
	}
	for _, b := range taken {
		if b.Reason == backup.Scheduled {
			return a.Now().Sub(b.TakenAt) >= every, nil // the list is newest first
		}
	}
	return true, nil // never backed up by itself
}

// registerRateJob fetches the exchange rate in the background: at launch (RunOnce catch-up — a laptop opened in the
// morning fetches once, not once per missed hour) and hourly after. Registered only when a provider is. A failed fetch is
// logged in fx_fetches and is not a failed job: being offline is ordinary, and the header already says so.
func (a *App) registerRateJob() error {
	if !a.FX.CanFetch() {
		return nil
	}
	return a.Scheduler.Registry().Register(jobs.Def{
		Key:         KeyRateRefresh,
		Schedule:    jobs.Every(a.opts.RateRefreshEvery),
		Timeout:     time.Minute,
		MaxAttempts: 1,
		CatchUp:     jobs.RunOnce,
		Description: "lite.jobs.rate_refresh",
	}, func(ctx context.Context, _ jobs.RunContext) error {
		fetch, err := a.FX.Refresh(ctx)
		if err != nil {
			return err
		}
		a.log.InfoContext(ctx, "exchange rate fetched",
			slog.String("outcome", string(fetch.Outcome)), slog.String("provider", fetch.Provider),
			slog.String("error_code", fetch.ErrorCode))
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
		a.copyOutside(ctx, taken)
		if _, err := a.Backups.Prune(ctx); err != nil {
			a.log.WarnContext(ctx, "the close-time backup was taken but old ones were not pruned",
				slog.Any("error", err))
		}
		a.log.InfoContext(ctx, "close-time backup taken", slog.String("name", taken.Name))
	}
}

// Location is the shop's time zone: the one printouts state times in.
func (a *App) Location() *time.Location { return a.opts.Location }

// Now is the graph's clock, for the "printed at" line of a document.
func (a *App) Now() time.Time { return a.opts.Clock.Now() }

// copyOutside copies a backup to the owner's outside folder. A failure is logged and shown on the Backups screen and Home; it
// never fails the backup (D-L7.14).
func (a *App) copyOutside(ctx context.Context, taken backup.Backup) {
	if err := a.Safety.AfterBackup(ctx, taken); err != nil && errs.CodeOf(err) != backups.CodeNoFolder {
		a.log.WarnContext(ctx, "the backup was taken but not copied to the outside folder", slog.String("code", errs.CodeOf(err)), slog.Any("error", err))
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

// fxGate satisfies fx's OwnerGate port with the owner service.
type fxGate struct{ owner *owner.Service }

func (g fxGate) Require(ctx context.Context, act fx.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After})
}

// fxSettings satisfies fx's Settings port with the settings service: the mode and the local currency.
type fxSettings struct{ settings *settings.Service }

func (s fxSettings) RateMode(ctx context.Context) (fxdomain.Mode, error) {
	current, err := s.settings.Get(ctx)
	if err != nil {
		return "", err
	}
	return fxdomain.ParseMode(string(current.RateMode))
}

func (s fxSettings) SetRateMode(ctx context.Context, mode fxdomain.Mode) error {
	value := string(mode)
	_, err := s.settings.Update(ctx, settingsdomain.Update{RateMode: &value})
	return err
}

func (s fxSettings) LocalCurrency(ctx context.Context) (string, error) {
	current, err := s.settings.Get(ctx)
	return current.LocalCurrency, err
}

// setupRates satisfies first run's Rates port with the fx service.
type setupRates struct{ fx *fx.Service }

func (r setupRates) RecordFirstRun(ctx context.Context, rate string) error {
	_, err := r.fx.RecordFirstRun(ctx, rate)
	return err
}

// salesGate satisfies the till's OwnerGate port with the owner service.
type salesGate struct{ owner *owner.Service }

func (g salesGate) Require(ctx context.Context, act sales.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After})
}

func (g salesGate) Allowed(ctx context.Context) bool { return g.owner.Allowed(ctx) }

// salesCatalogue satisfies the till's Catalogue port: what a till line needs of a product, and nothing else.
type salesCatalogue struct{ catalog *catalog.Service }

func (c salesCatalogue) product(ctx context.Context, p catalogdomain.Product) (salesdomain.Product, error) {
	ref, err := c.catalog.Reference(ctx)
	if err != nil {
		return salesdomain.Product{}, err
	}
	return salesdomain.Product{
		ID: p.ID, NameAR: p.NameAR, NameEN: p.NameEN, UnitCode: p.UnitCode, UnitDecimals: ref.Units[p.UnitCode].InputDecimals,
		PriceCurrency: p.PriceCurrency, PriceMicro: p.PriceMicro, CostMicro: p.CostMicro, HasCost: p.HasCost,
		Active: p.Active, RowVersion: p.RowVersion,
	}, nil
}

func (c salesCatalogue) Product(ctx context.Context, productID id.ID) (salesdomain.Product, error) {
	p, err := c.catalog.Get(ctx, productID)
	if err != nil {
		return salesdomain.Product{}, err
	}
	return c.product(ctx, p)
}

func (c salesCatalogue) ByBarcode(ctx context.Context, code string) (salesdomain.Product, bool, error) {
	p, found, err := c.catalog.ByBarcode(ctx, code)
	if err != nil || !found {
		return salesdomain.Product{}, false, err
	}
	out, err := c.product(ctx, p)
	return out, err == nil, err
}

func (c salesCatalogue) Currencies(ctx context.Context) ([]salesdomain.Currency, error) {
	currencies, err := c.catalog.Currencies(ctx)
	out := make([]salesdomain.Currency, 0, len(currencies))
	for _, cur := range currencies {
		out = append(out, salesdomain.Currency{Code: cur.Code, Decimals: cur.Decimals})
	}
	return out, err
}

// salesStock satisfies the till's Stock port with the stock service.
type salesStock struct{ stock *stock.Service }

func (s salesStock) Stocked(ctx context.Context, productID id.ID) (salesdomain.Stocked, error) {
	st, err := s.stock.Stocked(ctx, productID)
	return salesdomain.Stocked{OnHandMicro: st.OnHandMicro, CostKnown: st.CostKnown, AvgCostMicro: st.AvgCostMicro}, err
}

func (s salesStock) RecordSale(ctx context.Context, line sales.StockLine) error {
	_, err := s.stock.RecordSale(ctx, stock.SaleInput{ProductID: line.ProductID, QuantityMicro: line.QuantityMicro, SaleID: line.SaleID, SaleLineID: line.SaleLineID})
	return err
}

func (s salesStock) RecordSaleVoid(ctx context.Context, saleLineID id.ID) error {
	_, err := s.stock.RecordSaleVoid(ctx, saleLineID)
	return err
}

func (s salesStock) EachSaleMovement(ctx context.Context, fn func(salesdomain.StockMovement) error) error {
	return s.stock.EachSaleMovement(ctx, func(m stockdomain.Movement) error {
		return fn(salesdomain.StockMovement{Kind: string(m.Kind), ProductID: m.ProductID, QuantityMicro: m.QuantityMicro, SaleID: m.SaleID, SaleLineID: m.SaleLineID})
	})
}

// salesRates satisfies the till's Rates port with the fx service.
type salesRates struct{ fx *fx.Service }

func (r salesRates) InForce(ctx context.Context) (salesdomain.Rate, string, bool, error) {
	c, err := r.fx.Current(ctx)
	if err != nil || !c.Found {
		return salesdomain.Rate{}, c.Local, false, err
	}
	return salesdomain.Rate{ID: c.Rate.ID, Nano: c.Rate.Nano, RecordedAt: c.Rate.RecordedAt, Stale: c.Stale}, c.Local, true, nil
}

// salesSettings satisfies the till's Settings port with the settings service.
type salesSettings struct{ settings *settings.Service }

func (s salesSettings) ShopName(ctx context.Context) (string, error) {
	current, err := s.settings.Get(ctx)
	return current.ShopName, err
}

func (s salesSettings) CashNote(ctx context.Context) (int64, error) {
	current, err := s.settings.Get(ctx)
	return current.CashNote, err
}

func (s salesSettings) SetCashNote(ctx context.Context, raw string) (int64, error) {
	next, err := s.settings.Update(ctx, settingsdomain.Update{CashNote: &raw})
	return next.CashNote, err
}

// salesDebts satisfies the till's Debts port with the debt book (L5 §10.1).
type salesDebts struct{ customers *customers.Service }

func (d salesDebts) Customer(ctx context.Context, customerID id.ID) (salesdomain.Customer, bool, error) {
	c, err := d.customers.Customer(ctx, customerID)
	if errs.CodeOf(err) == customersdomain.CodeNotFound {
		return salesdomain.Customer{}, false, nil
	}
	if err != nil {
		return salesdomain.Customer{}, false, err
	}
	return salesdomain.Customer{ID: c.ID, Name: c.Name, Active: c.Active}, true, nil
}

func (d salesDebts) Balances(ctx context.Context, customerID id.ID) (map[string]int64, error) {
	return d.customers.Balances(ctx, customerID)
}

func creditOf(e customersdomain.Entry, reversed bool) salesdomain.Credit {
	return salesdomain.Credit{CustomerID: e.CustomerID, CustomerName: e.CustomerName, Currency: e.Currency, AmountMinor: e.AmountMinor,
		BalanceAfterMinor: e.BalanceAfterMinor, Reversed: reversed}
}

func (d salesDebts) Charge(ctx context.Context, in sales.ChargeInput) (salesdomain.Credit, error) {
	e, err := d.customers.Charge(ctx, customers.ChargeInput{CustomerID: in.CustomerID, SaleID: in.SaleID, Currency: in.Currency,
		AmountMinor: in.AmountMinor, BusinessDate: in.BusinessDate, At: in.At})
	return creditOf(e, false), err
}

func (d salesDebts) ReverseCharge(ctx context.Context, saleID id.ID, reason string, at time.Time, businessDate string) error {
	_, err := d.customers.ReverseCharge(ctx, saleID, reason, at, businessDate)
	return err
}

func (d salesDebts) CreditOf(ctx context.Context, saleID id.ID) (salesdomain.Credit, bool, error) {
	view, found, err := d.customers.ChargeOf(ctx, saleID)
	if err != nil || !found {
		return salesdomain.Credit{}, false, err
	}
	return creditOf(view.Charge, view.Reversed), true, nil
}

func (d salesDebts) EachCharge(ctx context.Context, fn func(salesdomain.Charge) error) error {
	return d.customers.EachCharge(ctx, func(v customers.ChargeView) error {
		return fn(salesdomain.Charge{SaleID: v.Charge.SaleID, CustomerID: v.Charge.CustomerID, Currency: v.Charge.Currency,
			AmountMinor: v.Charge.AmountMinor, Reversed: v.Reversed})
	})
}

// customersRates satisfies the debt book's Rates port with the fx service.
type customersRates struct{ fx *fx.Service }

func (r customersRates) InForce(ctx context.Context) (id.ID, int64, string, bool, error) {
	c, err := r.fx.Current(ctx)
	if err != nil || !c.Found {
		return "", 0, c.Local, false, err
	}
	return c.Rate.ID, c.Rate.Nano, c.Local, true, nil
}

// customersSettings satisfies the debt book's Settings port.
type customersSettings struct{ settings *settings.Service }

func (s customersSettings) CashNote(ctx context.Context) (int64, error) {
	current, err := s.settings.Get(ctx)
	return current.CashNote, err
}

// customersCurrencies satisfies the debt book's Currencies port with the catalogue.
type customersCurrencies struct{ catalog *catalog.Service }

func (c customersCurrencies) Currencies(ctx context.Context) ([]customersdomain.Currency, error) {
	currencies, err := c.catalog.Currencies(ctx)
	out := make([]customersdomain.Currency, 0, len(currencies))
	for _, cur := range currencies {
		out = append(out, customersdomain.Currency{Code: cur.Code, Decimals: cur.Decimals})
	}
	return out, err
}

// customersGate satisfies the debt book's OwnerGate port with the owner service.
type customersGate struct{ owner *owner.Service }

func (g customersGate) Require(ctx context.Context, act customers.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After})
}

func (g customersGate) Allowed(ctx context.Context) bool { return g.owner.Allowed(ctx) }

// cashbookCurrencies satisfies the cash book's Currencies port with the catalogue.
type cashbookCurrencies struct{ catalog *catalog.Service }

func (c cashbookCurrencies) Currencies(ctx context.Context) ([]cashbookdomain.Currency, error) {
	currencies, err := c.catalog.Currencies(ctx)
	out := make([]cashbookdomain.Currency, 0, len(currencies))
	for _, cur := range currencies {
		out = append(out, cashbookdomain.Currency{Code: cur.Code, Decimals: cur.Decimals})
	}
	return out, err
}

// cashbookGate satisfies the cash book's OwnerGate port with the owner service.
type cashbookGate struct{ owner *owner.Service }

func (g cashbookGate) Require(ctx context.Context, act cashbook.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{Action: act.Action, SubjectID: act.SubjectID, Before: act.Before, After: act.After})
}

// cashbookExpected satisfies the cash book's Expected port with the reports, reached through the app because the reports
// are built after the cash book they read.
type cashbookExpected struct{ app *App }

func (e cashbookExpected) ExpectedCash(ctx context.Context, businessDate, currency string) (int64, error) {
	return e.app.Reports.ExpectedCash(ctx, businessDate, currency)
}

// reportsGate satisfies the reports' OwnerGate port.
type reportsGate struct{ owner *owner.Service }

func (g reportsGate) Allowed(ctx context.Context) bool { return g.owner.Allowed(ctx) }

// reportsSales copies the till's sales into the reports' facts.
type reportsSales struct{ sales *sales.Service }

func (r reportsSales) Facts(ctx context.Context, from, to string) ([]reportsdomain.Sale, error) {
	all, err := r.sales.Facts(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.Sale, 0, len(all))
	for _, s := range all {
		returnCurrency, returnMinor := s.VoidReturn()
		f := reportsdomain.Sale{ID: s.ID, ReceiptNo: s.ReceiptNo, BusinessDate: s.BusinessDate, Voided: s.Status == salesdomain.StatusVoided,
			VoidBusinessDate: s.VoidBusinessDate, Credit: s.Payment == salesdomain.PaymentCredit, SettlementCurrency: s.SettlementCurrency,
			RateNano: s.RateNano, DiscountLocalMinor: s.DiscountLocalMinor, DiscountUSDMinor: s.DiscountUSDMinor, RoundingMinor: s.RoundingMinor,
			TotalMinor: s.TotalMinor, TenderedCurrency: s.TenderedCurrency, TenderedMinor: s.TenderedMinor, ChangeCurrency: s.ChangeCurrency,
			ChangeMinor: s.ChangeMinor, CreditMinor: s.Credit.AmountMinor, VoidReturnCurrency: returnCurrency, VoidReturnMinor: returnMinor,
			Lines: make([]reportsdomain.Line, 0, len(s.Lines))}
		for _, l := range s.Lines {
			f.Lines = append(f.Lines, reportsdomain.Line{ProductID: l.ProductID, NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode,
				QuantityMicro: l.QuantityMicro, NetLocalMinor: l.NetLocalMinor(), NetUSDMinor: l.NetUSDMinor(), DiscountLocalMinor: l.DiscountLocalMinor,
				DiscountUSDMinor: l.DiscountUSDMinor, CostKnown: l.CostKnown, CostUSDMinor: l.CostUSDMinor, CostLocalMinor: l.CostLocalMinor})
		}
		out = append(out, f)
	}
	return out, nil
}

// reportsStock copies stock ledger rows into the reports' facts.
type reportsStock struct{ stock *stock.Service }

func movementFacts(rows []stockdomain.Movement) []reportsdomain.Movement {
	out := make([]reportsdomain.Movement, 0, len(rows))
	for _, m := range rows {
		out = append(out, reportsdomain.Movement{ProductID: m.ProductID, Seq: m.Seq, BusinessDate: m.BusinessDate, Kind: string(m.Kind),
			Reason: string(m.Reason), QuantityMicro: m.QuantityMicro, UnitCostMicro: m.UnitCostMicro, OnHandBeforeMicro: m.OnHandBeforeMicro,
			AvgCostBeforeMicro: m.AvgCostBeforeMicro, OnHandAfterMicro: m.OnHandAfterMicro, AvgCostAfterMicro: m.AvgCostAfterMicro})
	}
	return out
}

func (r reportsStock) Between(ctx context.Context, from, to string) ([]reportsdomain.Movement, error) {
	rows, err := r.stock.Between(ctx, from, to)
	return movementFacts(rows), err
}

func (r reportsStock) LastOnOrBefore(ctx context.Context, businessDate string) ([]reportsdomain.Movement, error) {
	rows, err := r.stock.LastOnOrBefore(ctx, businessDate)
	return movementFacts(rows), err
}

// reportsDebts copies debt book entries into the reports' facts.
type reportsDebts struct{ customers *customers.Service }

func debtCash(c customersdomain.Cash) reportsdomain.DebtCash {
	return reportsdomain.DebtCash{TenderedCurrency: c.TenderedCurrency, TenderedMinor: c.TenderedMinor, ChangeCurrency: c.ChangeCurrency, ChangeMinor: c.ChangeMinor}
}

func (r reportsDebts) EntriesBetween(ctx context.Context, from, to string) ([]reportsdomain.DebtEntry, error) {
	facts, err := r.customers.EntriesBetween(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.DebtEntry, 0, len(facts))
	for _, f := range facts {
		e := f.Entry
		out = append(out, reportsdomain.DebtEntry{ID: e.ID, CustomerName: e.CustomerName, Currency: e.Currency, BusinessDate: e.BusinessDate,
			Kind: string(e.Kind), AmountMinor: e.AmountMinor, Cash: debtCash(e.Cash), ReversesKind: string(f.Reverses.Kind),
			ReversesCash: debtCash(f.Reverses.Cash), ReversesMinor: f.Reverses.AmountMinor})
	}
	return out, nil
}

// reportsRates copies the recorded rates into the reports' facts.
type reportsRates struct{ fx *fx.Service }

func (r reportsRates) AllRates(ctx context.Context) ([]reportsdomain.Rate, string, error) {
	rates, local, err := r.fx.AllRates(ctx)
	out := make([]reportsdomain.Rate, 0, len(rates))
	for _, rate := range rates {
		out = append(out, reportsdomain.Rate{ID: rate.ID, Seq: rate.Seq, Nano: rate.Nano, BusinessDate: rate.BusinessDate})
	}
	return out, local, err
}

// reportsCatalogue copies products and currencies into the reports' facts.
type reportsCatalogue struct{ catalog *catalog.Service }

func (c reportsCatalogue) Products(ctx context.Context) ([]reportsdomain.Product, error) {
	all, err := c.catalog.All(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := c.catalog.Reference(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.Product, 0, len(all))
	for _, p := range all {
		out = append(out, reportsdomain.Product{ID: p.ID, NameAR: p.NameAR, NameEN: p.NameEN, UnitCode: p.UnitCode,
			UnitDecimals: ref.Units[p.UnitCode].InputDecimals, PriceCurrency: p.PriceCurrency, PriceMicro: p.PriceMicro, Active: p.Active})
	}
	return out, nil
}

func (c reportsCatalogue) Currencies(ctx context.Context) ([]reportsdomain.Currency, error) {
	currencies, err := c.catalog.Currencies(ctx)
	out := make([]reportsdomain.Currency, 0, len(currencies))
	for _, cur := range currencies {
		out = append(out, reportsdomain.Currency{Code: cur.Code, Decimals: cur.Decimals})
	}
	return out, err
}

// reportsCash copies the cash book into the reports' facts.
type reportsCash struct{ cashbook *cashbook.Service }

func cashFact(e cashbookdomain.Entry) reportsdomain.CashEntry {
	return reportsdomain.CashEntry{ID: e.ID, Seq: e.Seq, BusinessDate: e.BusinessDate, OccurredAt: e.OccurredAt, Kind: string(e.Kind),
		Currency: e.Currency, AmountMinor: e.AmountMinor, ExpectedMinor: e.ExpectedMinor, Category: e.Category, FromDrawer: e.FromDrawer,
		RateNano: e.RateNano, Note: e.Note}
}

func (r reportsCash) Between(ctx context.Context, from, to string) ([]reportsdomain.CashEntry, error) {
	facts, err := r.cashbook.Between(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]reportsdomain.CashEntry, 0, len(facts))
	for _, f := range facts {
		e := cashFact(f.Entry)
		e.Reversed = f.Reversed
		if f.Entry.Kind == cashbookdomain.KindReversal {
			original := cashFact(f.Reverses)
			e.Reverses = &original
		}
		out = append(out, e)
	}
	return out, nil
}

func (r reportsCash) LastCountBefore(ctx context.Context, businessDate, currency string) (reportsdomain.CashEntry, bool, error) {
	e, found, err := r.cashbook.LastCountBefore(ctx, businessDate, currency)
	return cashFact(e), found, err
}

// backupSettings satisfies the backups module's Settings port.
type backupSettings struct{ settings *settings.Service }

func (s backupSettings) BackupFolder(ctx context.Context) (string, error) {
	current, err := s.settings.Get(ctx)
	return current.BackupFolder, err
}

func (s backupSettings) BackupEvery(ctx context.Context) (string, error) {
	current, err := s.settings.Get(ctx)
	return current.BackupEvery, err
}

// backupsGate satisfies the backups module's OwnerGate port.
type backupsGate struct{ owner *owner.Service }

func (g backupsGate) Require(ctx context.Context, act backups.GuardedAct) error {
	return g.owner.Require(ctx, owner.Act{Action: act.Action, Before: act.Before, After: act.After})
}

func (g backupsGate) Allowed(ctx context.Context) bool { return g.owner.Allowed(ctx) }

// backupActivity counts what was recorded after an instant, from the facts each module already supplies the reports.
type backupActivity struct{ app *App }

func (a backupActivity) Since(ctx context.Context, at time.Time) (backups.Loss, error) {
	from := bizdate.Date(at, a.app.opts.Location)
	const end = "9999-12-31"
	var loss backups.Loss
	sales, err := a.app.Sales.Facts(ctx, from, end)
	if err != nil {
		return loss, err
	}
	for _, s := range sales {
		if s.SoldAt.After(at) {
			loss.Sales++
		}
		if !s.VoidedAt.IsZero() && s.VoidedAt.After(at) {
			loss.Voids++
		}
	}
	debts, err := a.app.Customers.EntriesBetween(ctx, from, end)
	if err != nil {
		return loss, err
	}
	for _, d := range debts {
		if d.Entry.OccurredAt.After(at) {
			loss.DebtEntries++
		}
	}
	cash, err := a.app.Cashbook.Between(ctx, from, end)
	if err != nil {
		return loss, err
	}
	for _, c := range cash {
		if c.Entry.OccurredAt.After(at) {
			loss.CashEntries++
		}
	}
	moves, err := a.app.Stock.Between(ctx, from, end)
	if err != nil {
		return loss, err
	}
	for _, m := range moves {
		if m.OccurredAt.After(at) {
			loss.StockMovements++
		}
	}
	return loss, nil
}
