// Package bootstrap is the composition root: the one place the object graph is built.
//
// Manual constructor injection, no container, no reflection, no code generation
// (ARCHITECTURE_v1 §6). A reflection container would turn a missing dependency into a runtime
// panic during startup on a customer's machine, which is the one class of failure a desktop
// ERP cannot afford. Explicit wiring in one file is boring, greppable, and compiler-checked.
//
// The ORDER below is load-bearing, not stylistic. Two edges in particular:
//
//   - migrate runs before anything else touches the database, because no component may read a
//     schema that has not been brought up to date;
//   - subscriptions are registered before the scheduler starts, because the outbox dispatcher
//     treats an unregistered handler as "skip" (Step 0.6) — start it first and deliveries are
//     silently dropped.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
	"github.com/mizan-erp/mizan/internal/platform/paths"
	"github.com/mizan-erp/mizan/migrations"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeStartupFailed   = "bootstrap.startup_failed"
	CodeMigrationFatal  = "bootstrap.migration_failed"
	CodeRegistryInvalid = "bootstrap.registry_invalid"
)

// Options configures the composition root.
type Options struct {
	// Paths locates the customer's data. Required.
	Paths  paths.Paths
	Logger *slog.Logger
	Clock  clock.Clock
	// SkipBackup disables the pre-migration snapshot. TESTS ONLY.
	SkipBackup bool
	// StartScheduler runs the job loop. Tests drive Tick directly instead, so that
	// scheduling is deterministic rather than a race against wall time.
	StartScheduler bool
	// DispatchInterval is how often the outbox dispatch job runs. Default 5s.
	DispatchInterval time.Duration
	// HeartbeatInterval is how often the heartbeat job runs. Default 1m.
	HeartbeatInterval time.Duration
	// Progress, if set, receives migration progress.
	//
	// Step 0.4 has emitted these since it was built and nothing consumed them, because until
	// Step 0.11 inverted the boot sequence there was no window to draw on while migrating.
	// The shell forwards them to the boot screen.
	Progress func(migrate.Progress)
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.DispatchInterval <= 0 {
		o.DispatchInterval = 5 * time.Second
	}
	if o.HeartbeatInterval <= 0 {
		o.HeartbeatInterval = time.Minute
	}
	return o
}

// App is the built graph. It holds what the shell needs and nothing else.
//
// There is deliberately no service locator and no Get(name) method: a component that needs
// another received it in its constructor. Fields here are for the shell and for tests.
type App struct {
	Paths     paths.Paths
	DB        *database.Store
	Settings  *config.Settings
	Catalog   *i18n.Catalog
	Trans     *i18n.Translations
	Bus       *eventbus.Bus
	Outbox    *outbox.Store
	Subs      *outbox.Subscribers
	Dispatch  *outbox.Dispatcher
	Scheduler *jobs.Scheduler
	Currency  *currency.Service
	Modules   []modules.Module
	// Bindings are the structs handed to Wails.
	Bindings []any
	// Ctx is the root context with settings bound, from which per-call contexts derive.
	Ctx context.Context

	log      *slog.Logger
	opts     Options
	shutdown []shutdownStep
}

type shutdownStep struct {
	name string
	fn   func(context.Context) error
}

// abandon tears down a partially-built graph after a startup failure.
//
// database.Store.Close takes no context by design — it checkpoints the WAL and closes two
// pools, work that must complete regardless of whether the caller's context is already
// cancelled. Failing to close here would leak the file handle and the WAL.
func abandon(db *database.Store) {
	_ = db.Close()
}

// Start builds the graph in the order §BOOT specifies.
//
// contextcheck is disabled for this function for two reasons the linter cannot see. app.Ctx
// IS derived from ctx — it is ctx with the settings bound — but the derivation goes through
// config.Bind, which the analyser does not recognise as inheritance. And the teardown on a
// failed start deliberately closes the database regardless of ctx: a cancelled context must
// not leave the WAL unchecked and the file handle leaked.
//
//nolint:contextcheck // app.Ctx derives from ctx via config.Bind; teardown must not be cancellable
func Start(ctx context.Context, opts Options) (*App, error) {
	opts = opts.withDefaults()
	if opts.Paths.DBFile == "" {
		return nil, errs.Internal(CodeStartupFailed, "bootstrap: Options.Paths is required")
	}
	app := &App{Paths: opts.Paths, log: opts.Logger, opts: opts}

	// 3. Database.
	db, err := database.Open(database.Config{Path: opts.Paths.DBFile})
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed, "opening the database")
	}
	app.DB = db

	// The currency module is constructed early only to obtain its migrations; its service is
	// built after the schema exists.
	currencyModule := currency.NewModule(nil)

	// 4. Migrate — before anything else reads a table.
	if err = app.runMigrations(ctx, currencyModule); err != nil {
		abandon(db)
		return nil, err
	}

	// 5–6. Buses.
	app.Bus = eventbus.New(eventbus.Options{Logger: opts.Logger})
	app.Subs = outbox.NewSubscribers()
	app.Outbox = outbox.NewStore(db, app.Subs, opts.Clock, opts.Logger)
	app.Dispatch = outbox.NewDispatcher(db, app.Subs, outbox.DispatcherOptions{
		Clock: opts.Clock, Logger: opts.Logger,
	})

	// 7. Settings — validate declarations first: a duplicate key is a code defect and must
	// fail on the developer's machine, not resolve arbitrarily on a customer's (D3).
	if err = config.Default().Validate(); err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeRegistryInvalid,
			"the configuration registry is invalid")
	}
	settings, err := config.Open(ctx, db, config.Options{
		Scopes:   appctx.Scopes{},
		Notifier: config.NewBusNotifier(app.Bus, opts.Logger),
		Logger:   opts.Logger,
		Clock:    opts.Clock,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed, "loading settings")
	}
	app.Settings = settings
	app.Ctx = config.Bind(ctx, settings)

	// 8. i18n.
	catalog, err := i18n.Load()
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"loading the message catalogs")
	}
	app.Catalog = catalog
	app.Trans = i18n.NewTranslations(db, opts.Clock, opts.Logger)

	// 9. Scheduler — constructed, NOT started.
	scheduler, err := jobs.New(db, jobs.Options{
		Clock: opts.Clock, Logger: opts.Logger,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"building the job scheduler")
	}
	app.Scheduler = scheduler

	// 10. Modules.
	app.Currency = currency.NewService(db, opts.Clock, app.Trans)
	currencyModule = currency.NewModule(app.Currency)

	ordered, err := modules.Order([]modules.Module{currencyModule})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeRegistryInvalid,
			"the module set is invalid")
	}
	if err := modules.Validate(ordered); err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeRegistryInvalid,
			"the module set is invalid")
	}
	app.Modules = ordered

	// 11. Wiring — subscriptions BEFORE the scheduler starts (D5).
	for _, m := range ordered {
		if err := m.Subscribe(app.Bus, app.Subs); err != nil {
			abandon(db)
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
				"registering subscriptions for module "+m.Name())
		}
	}
	if err := app.registerJobs(ordered); err != nil {
		abandon(db)
		return nil, err
	}

	// 12. Seeding — every boot, no first-run flag (D4). The seeder is idempotent, and always
	// seeding self-heals a system row someone deleted.
	if err := app.seed(app.Ctx, ordered); err != nil {
		abandon(db)
		return nil, err
	}

	// 13. Start the scheduler, now that every handler is registered.
	if opts.StartScheduler {
		if err := scheduler.Start(app.Ctx); err != nil {
			abandon(db)
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
				"starting the job scheduler")
		}
	} else if _, err := scheduler.Reconcile(app.Ctx); err != nil {
		// Even when the loop is not running, the jobs table must reflect what this build
		// declares — otherwise a test driving Tick would find nothing due.
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"reconciling jobs")
	}

	// 14. Bindings.
	for _, m := range ordered {
		if b := m.Bindings(); b != nil {
			app.Bindings = append(app.Bindings, b)
		}
	}

	app.registerShutdown()
	opts.Logger.InfoContext(ctx, "mizan started",
		slog.String("data_dir", opts.Paths.Data),
		slog.Int("modules", len(ordered)))
	return app, nil
}

// runMigrations applies the platform's and every module's schema, merged and globally ordered.
//
// A failure here is FATAL and the graph is abandoned (D2). Step 0.4's restore path CLOSES the
// store, so every later step would be operating on a closed handle — and more importantly, a
// database that has just failed a migration is one a human should look at. The error carries
// the backup path, because the only thing the user needs to know is that their data is safe
// and where it is.
func (a *App) runMigrations(ctx context.Context, mods ...modules.Module) error {
	merged := migrations.SQLite()
	for _, m := range mods {
		merged = migrate.Merge(merged, m.Migrations())
	}

	runner, err := migrate.New(a.DB, migrate.Options{
		FS:         merged,
		DBPath:     a.Paths.DBFile,
		BackupDir:  a.Paths.Backups,
		SkipBackup: a.opts.SkipBackup,
		Progress:   a.opts.Progress,
		Clock:      a.opts.Clock,
		Logger:     a.log,
	})
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeMigrationFatal,
			"preparing the database update")
	}

	result, err := runner.Up(ctx)
	if err != nil {
		wrapped := errs.Wrap(err, errs.CategoryInternal, CodeMigrationFatal,
			"the database update failed; the application cannot start")
		// Carry the cause's parameters onto the wrapper.
		//
		// errs.AsError finds the OUTERMOST *Error, so without this the backup path Step 0.4
		// attaches to the migration failure is lost the moment it is wrapped — and the backup
		// path is the one thing the user actually needs ("your data is safe, here it is").
		// The UI renders Params; it cannot walk the wrapped chain.
		if inner, ok := errs.AsError(err); ok {
			for k, v := range inner.Params {
				wrapped = wrapped.WithParam(k, v)
			}
		}
		return wrapped
	}
	if result.Applied > 0 {
		a.log.InfoContext(ctx, "database updated",
			slog.Int("applied", result.Applied),
			slog.Int64("to_version", result.ToVersion))
	}
	return nil
}

// registerJobs declares the platform's own jobs and every module's.
func (a *App) registerJobs(mods []modules.Module) error {
	reg := a.Scheduler.Registry()

	if err := jobs.RegisterOutboxDispatch(reg, a.Dispatch, a.opts.DispatchInterval); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"registering the outbox dispatch job")
	}
	if err := jobs.RegisterHeartbeat(reg, a.opts.HeartbeatInterval); err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"registering the heartbeat job")
	}
	for _, m := range mods {
		for _, def := range m.Jobs() {
			if err := reg.Register(def, nil); err != nil {
				return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
					"registering jobs for module "+m.Name())
			}
		}
	}
	return nil
}

// seed applies every module's reference data, idempotently, on every boot (D4).
func (a *App) seed(ctx context.Context, mods []modules.Module) error {
	seeder := metadata.NewSeeder(a.DB, a.opts.Clock)
	for _, m := range mods {
		for _, spec := range m.Metadata() {
			if _, err := seeder.Seed(ctx, spec); err != nil {
				return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
					"seeding reference data for module "+m.Name())
			}
		}
	}
	// Reference-data labels in other languages. Currency owns this because the labels are
	// its data; a generic hook arrives when a second module needs one.
	if a.Currency != nil {
		mod := currency.NewModule(a.Currency)
		if err := mod.SeedTranslations(ctx, a.Currency, a.Trans); err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
				"seeding reference-data translations")
		}
	}
	return nil
}

// registerShutdown records the teardown steps in reverse construction order.
func (a *App) registerShutdown() {
	a.shutdown = []shutdownStep{
		{name: "scheduler", fn: func(ctx context.Context) error {
			return a.Scheduler.Stop(ctx)
		}},
		{name: "outbox", fn: func(ctx context.Context) error {
			// One final pass so work already committed goes out before the process ends.
			// Best-effort: if it fails, the rows are still there for the next launch.
			_, err := a.Dispatch.DispatchOnce(ctx)
			return err
		}},
		{name: "database", fn: func(context.Context) error {
			//nolint:contextcheck // Close checkpoints the WAL; it must not be cancellable
			return a.DB.Close()
		}},
	}
}

// Shutdown tears the graph down in reverse order, bounded at every step.
//
// Shutdown is POLITENESS, NOT A DURABILITY MECHANISM (D6). A power cut or a force-quit skips
// all of it and the system is still correct: the outbox commits with its business transaction
// (0.6), abandoned job runs are reclaimed on the next start (0.7), and the WAL recovers (0.3).
// This makes the next launch faster and quieter; it is never what makes it correct.
//
// Every step is bounded and a failure does not stop the rest. An ERP that will not close is a
// worse bug than one that leaves a job to be re-run.
func (a *App) Shutdown(ctx context.Context) error {
	var failures []error
	for _, step := range a.shutdown {
		stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := step.fn(stepCtx); err != nil {
			a.log.WarnContext(ctx, "shutdown step failed; continuing",
				slog.String("step", step.name), slog.Any("error", err))
			failures = append(failures, err)
		}
		cancel()
	}
	a.shutdown = nil // a second Shutdown is a no-op rather than a double-close
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

// Context returns a per-call context: settings bound, locale and correlation id stamped.
func (a *App) Context() context.Context { return appctx.Enrich(a.Ctx) }
