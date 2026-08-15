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
	"github.com/mizan-erp/mizan/internal/api/setup"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/modules"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
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
	Paths    paths.Paths
	DB       *database.Store
	Settings *config.Settings
	// Messages is the i18n message catalogue. Renamed from Catalog when the catalog MODULE
	// arrived in 3.1: two fields called Catalog on one struct is a collision waiting for
	// whoever reads it next.
	Messages   *i18n.Catalog
	Trans      *i18n.Translations
	Bus        *eventbus.Bus
	Outbox     *outbox.Store
	Subs       *outbox.Subscribers
	Dispatch   *outbox.Dispatcher
	Scheduler  *jobs.Scheduler
	Currency   *currency.Service
	Org        *org.Service
	Identity   *identity.Service
	Audit      *audit.Service
	Profile    *profile.Service
	Accounting *accounting.Service
	Tax        *tax.Service
	Catalog    *catalog.Service
	Partner    *partner.Service
	Pricing    *pricing.Service
	Inventory  *inventory.Service
	Sales      *sales.Service
	Purchasing *purchasing.Service
	Expenses   *expenses.Service
	Setup      *setup.Service
	Modules    []modules.Module
	// There is no Bindings field: the structs handed to Wails are a property of the BUILD, not
	// of the graph, and they are assembled statically in internal/api/bindings (0.11 D2). This
	// field held whatever Module.Bindings() returned, which for two phases was nothing at all
	// — removed with that method by the Phase 1 DoD review (1.12).
	//
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

	// 4. Migrate — before anything else reads a table.
	//
	// The modules are constructed here with NIL services, purely to read their declarations:
	// a migration is a property of the module, not of a built graph, and the graph cannot exist
	// before the schema it needs does.
	if err = app.runMigrations(ctx, DeclarationModules()...); err != nil {
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

	// 6b. Org and identity services, constructed EARLY.
	//
	// Out of §BOOT's numbered order for one reason: config.Open takes the Authorizer that gates
	// permissioned settings, and the real one is the identity module. Neither service reads a
	// setting at CONSTRUCTION — only at call time, through handles that resolve against a bound
	// context — so building them here is safe, and it is the alternative to handing config a
	// mutable holder to fill in later, which is exactly the kind of late-bound state §6 warns
	// against.
	//
	// Their MODULES are still assembled at step 10 with everything else.
	app.Org = org.NewService(db, opts.Clock, app.Bus)
	// Identity reaches org through the two-method Organisation port it declares itself — no
	// import of org from identity, and the wiring is visible here.
	app.Identity = identity.NewService(db, app.Org, opts.Clock, app.Bus, identityActors{})

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
		// The real RBAC authorizer replaces Step 0.5's AllowAll stub (D8 there, D7 here). The
		// port is scope-free because a setting is a company-wide fact; the adapter supplies
		// global scope.
		Authorizer: identity.NewSettingsAuthorizer(app.Identity),
		Logger:     opts.Logger,
		Clock:      opts.Clock,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed, "loading settings")
	}
	app.Settings = settings
	app.Ctx = config.Bind(ctx, settings)

	// 8. i18n.
	messages, err := i18n.Load()
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"loading the message catalogs")
	}
	app.Messages = messages
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
	//
	// Nothing is provisioned here (Step 1.1, D1): a fresh install has no company until the
	// setup wizard runs, and Start must succeed against exactly that state.
	app.Currency = currency.NewService(db, opts.Clock, app.Trans)
	currencyModule := currency.NewModule(app.Currency)
	orgModule := org.NewModule(app.Org)
	identityModule := identity.NewModule(app.Identity)
	app.Audit = audit.NewService(db, opts.Clock, auditActors{})
	auditModule := audit.NewModule(app.Audit)

	// Profiles are loaded from FILES, in two layers: what this binary embeds, and whatever the
	// administrator dropped into the data directory. The second layer is what makes Addendum
	// §C's "adding a country is dropping in a JSON file — no code, no release" true.
	//
	// This returns an error only for a file WE ship (1.8, D4). A customer's broken file is
	// logged and skipped, because a seed file must not be able to stop a shop from opening.
	app.Profile, err = profile.NewService(db, profile.Options{
		UserFS:   profile.UserFS(opts.Paths.Data),
		Settings: settings,
		Clock:    opts.Clock,
		Bus:      app.Bus,
		Logger:   opts.Logger,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"loading the country and business profiles")
	}
	profileModule := profile.NewModule(app.Profile)

	// The chart of accounts rides the same layered loader country profiles do (1.8), so an
	// accountant's own chart in the data directory replaces the shipped template.
	app.Accounting, err = accounting.NewService(db, accounting.Options{
		UserFS: profile.UserFS(opts.Paths.Data),
		Clock:  opts.Clock,
		Bus:    app.Bus,
		Logger: opts.Logger,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"loading the charts of accounts")
	}
	accountingModule := accounting.NewModule(app.Accounting)

	// Tax ships with no rates (§19.1, §C.3) and disabled by default (§19.5). The engine is
	// complete; the data is the customer's.
	app.Tax = tax.NewService(db, opts.Clock)
	taxModule := tax.NewModule(app.Tax)

	// Units of measure ride the same layered loader country profiles and charts do, so a trade
	// that measures in bushels adds a file rather than waiting for a release.
	app.Catalog, err = catalog.NewService(db, catalog.Options{
		UserFS: profile.UserFS(opts.Paths.Data),
		Clock:  opts.Clock,
		Bus:    app.Bus,
		Logger: opts.Logger,
	})
	if err != nil {
		abandon(db)
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"loading the units of measure")
	}
	catalogModule := catalog.NewModule(app.Catalog)

	// Partners: customers and suppliers in one table, because a business that both buys from
	// and sells to the same company is ordinary (decision 9).
	app.Partner = partner.NewService(db, partner.Options{
		Clock: opts.Clock, Bus: app.Bus, Logger: opts.Logger,
	})
	partnerModule := partner.NewModule(app.Partner)

	// Pricing: multiple lists with resolution (Phase 3 D1). A single-price shop never sees it —
	// one default list, no picker, no concept.
	app.Pricing = pricing.NewService(db, pricing.Options{
		Clock: opts.Clock, Bus: app.Bus, Logger: opts.Logger,
	})
	pricingModule := pricing.NewModule(app.Pricing)

	// Inventory: an append-only movement ledger with a verified projection, the same shape the
	// general ledger has (Phase 2). Costing goes through a strategy port so that FIFO is a
	// setting rather than a rewrite (§D.1).
	app.Inventory = inventory.NewService(db, inventory.Options{
		Clock: opts.Clock, Bus: app.Bus, Settings: settings,
		Actors: inventoryActors{}, Products: inventoryProducts{catalog: app.Catalog},
		// The reconciliation's one port: what the books say. Which companies to sweep comes
		// from inventory's own levels — a company with no stock has nothing to reconcile.
		Ledger: inventoryLedger{accounting: app.Accounting},
		Logger: opts.Logger,
	})
	inventoryModule := inventory.NewModule(app.Inventory)

	// Sales: the phase where every earlier seam finds out whether it was built for a real
	// caller. Numbers are allocated at posting, inside the document's own transaction.
	app.Sales = sales.NewService(db, sales.Options{
		Clock: opts.Clock, Bus: app.Bus, Actors: salesActors{},
		Catalog: salesCatalog{catalog: app.Catalog},
		// The four ports posting needs. Each adapter is a few lines: the ports sales declared
		// are narrower than the services behind them, and this is where the two shapes meet.
		Pricing: salesPricing{pricing: app.Pricing},
		Tax:     salesTax{tax: app.Tax, currency: app.Currency},
		Stock:   salesStock{inventory: app.Inventory},
		Credit:  salesCredit{partner: app.Partner},
		Logger:  opts.Logger,
	})
	salesModule := sales.NewModule(app.Sales)

	// Purchasing: the other half of the stock cycle, and the phase where PHASE 4's seams find
	// out whether they were built for a real caller — `Revaluation`, `Allocate`, the document
	// link, and `inventory_layers`. One of them was not (6.4).
	app.Purchasing = purchasing.NewService(db, purchasing.Options{
		Clock: opts.Clock, Bus: app.Bus, Actors: purchasingActors{},
		Catalog: purchasingCatalog{catalog: app.Catalog},
		Pricing: purchasingPricing{pricing: app.Pricing},
		Tax:     purchasingTax{tax: app.Tax, currency: app.Currency},
		Stock:   purchasingStock{inventory: app.Inventory},
		// The PLATFORM allocator, shared with sales. Two against one table would race.
		Numbers: purchasingNumbering{allocator: numbering.New(db, opts.Clock)},
		Logger:  opts.Logger,
	})
	purchasingModule := purchasing.NewModule(app.Purchasing)

	// Expenses: money out that buys no stock, which is most of what a small business spends.
	// Not a purchase bill — a bill line must name a delivery, and an electricity bill has none.
	app.Expenses = expenses.NewService(db, expenses.Options{
		Clock: opts.Clock, Bus: app.Bus, Actors: expensesActors{},
		Tax:     expensesTax{tax: app.Tax, currency: app.Currency},
		Numbers: purchasingNumbering{allocator: numbering.New(db, opts.Clock)},
		Logger:  opts.Logger,
	})
	expensesModule := expenses.NewModule(app.Expenses)

	// The wizard's service. Not a module (§1.9 D1): it composes four of them in one
	// transaction, which module-isolation forbids from inside internal/modules — correctly,
	// because setup owns no entities and is not a domain.
	// The partner module gains its balance ports LAST, because they need sales, purchasing and
	// expenses — all of which need partner. The cycle is broken the way it always is: the ports
	// are set after construction, not passed into it.
	app.Partner.AttachLedgers(
		partnerReceivables{sales: app.Sales},
		partnerPayables{purchasing: app.Purchasing, expenses: app.Expenses},
		partnerControl{accounting: app.Accounting},
	)

	// Inventory gains the ledger side of its valuation check the same way, and for the same
	// reason: accounting is built before inventory, so this could be a constructor argument —
	// but making the two attachments look different would invite a reader to wonder why.
	app.Inventory.AttachControlLedger(inventoryControl{accounting: app.Accounting})

	app.Setup = setup.NewService(db, app.Org, app.Identity, app.Profile, app.Currency,
		app.Accounting, app.Catalog, settings, app.Messages, app.Bus)

	// Handed over in a deliberately WRONG order so the topological sort has to do real work:
	// identity depends on org, which depends on currency.
	ordered, err := modules.Order([]modules.Module{
		auditModule, expensesModule, purchasingModule, salesModule, inventoryModule, pricingModule, partnerModule, catalogModule, taxModule, accountingModule, profileModule,
		identityModule, orgModule, currencyModule})
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

	// 11b. Permission sync (Step 1.4). Permissions are CODE-DEFINED (§14.1): each module says
	// what it protects and this reconciles the table, which is what stops the permission list
	// drifting from what the code actually checks.
	if err := app.syncPermissions(app.Ctx, ordered); err != nil {
		abandon(db)
		return nil, err
	}

	// 11c. Number series (Step 7.6). Declared per module, reconciled here, for the same reason
	// permissions are: the alternative is a list in the composition root, which is a second place
	// to forget — and forgetting is exactly what happened. Nothing created a series until this
	// existed, so a real company could not post a single numbered document.
	if err := app.syncSeries(app.Ctx, ordered, opts.Clock); err != nil {
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

// syncPermissions reconciles the permissions table with what this build declares.
//
// A duplicate code across two modules is FATAL — a code defect that must fail on the
// developer's machine (0.10 D3). A row nothing declares is marked obsolete and REPORTED, never
// deleted: it is an inert data leftover, and deleting it would cascade to role_permissions and
// silently change what every role grants.
func (a *App) syncPermissions(ctx context.Context, mods []modules.Module) error {
	byModule := make(map[string][]auth.PermissionDef, len(mods))
	for _, m := range mods {
		if defs := m.Permissions(); len(defs) > 0 {
			byModule[m.Name()] = defs
		}
	}
	report, err := a.Identity.SyncPermissions(ctx, byModule)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeRegistryInvalid,
			"the declared permissions are invalid")
	}
	if report.Obsolete > 0 {
		a.log.WarnContext(ctx, "permissions no longer declared by any module were marked obsolete",
			slog.Int("count", report.Obsolete))
	}
	a.log.InfoContext(ctx, "permissions synced", slog.Int("declared", report.Declared))
	return nil
}

// syncSeries creates any declared number series that does not exist yet.
//
// Idempotent and run on every boot, like the seeder and for the same reason: always running it
// self-heals an install where a series was never created, including every install made before
// this function existed.
//
// A duplicate code across two modules is FATAL, matching syncPermissions. Two modules allocating
// from one sequence is not a conflict to resolve at runtime — it is a code defect, and one that
// would show up as a purchase order and an invoice sharing a number.
func (a *App) syncSeries(
	ctx context.Context, mods []modules.Module, clk clock.Clock,
) error {
	var (
		specs = make([]numbering.SeriesSpec, 0, 16)
		owner = make(map[string]string, 16)
	)
	for _, m := range mods {
		for _, spec := range m.Series() {
			if previous, taken := owner[spec.Code]; taken {
				return errs.Internal(CodeRegistryInvalid,
					"two modules declare the same number series").
					WithParam("code", spec.Code).WithParam("modules", previous+" and "+m.Name())
			}
			owner[spec.Code] = m.Name()
			specs = append(specs, spec)
		}
	}

	created, err := numbering.New(a.DB, clk).Ensure(ctx, specs)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeStartupFailed,
			"creating the declared number series")
	}
	a.log.InfoContext(ctx, "number series synced",
		slog.Int("declared", len(specs)), slog.Int("created", created))
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
		for _, reg2 := range m.Jobs() {
			// The handler comes from the module, paired with its declaration. Passing nil here
			// (as this did until Step 1.3) is rejected by Register, so the module-jobs path
			// had never actually worked — no module declared a job until identity's sweep.
			if err := reg.Register(reg2.Def, reg2.Handler); err != nil {
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
