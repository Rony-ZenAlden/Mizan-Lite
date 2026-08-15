// Package bindings is the Wails-facing surface: the structs JavaScript can call.
//
// # Why these are façades rather than constructed objects
//
// Wails binds a fixed []any at Run time. Step 0.11 (D2) opens the window BEFORE the object
// graph is built, so that a long migration has somewhere to draw progress and a failed start
// has somewhere to show an error — neither of which existed when the graph was built first and
// a failure could only log and exit.
//
// That means bindings must be constructible before the graph exists. The resolution is the
// pattern this project already uses for settings (0.5) and jobs (0.7):
//
//	The set of bindings is a property of the built binary, not of a runtime object graph.
//
// So the structs are declared statically, handed to Wails, and have the graph ATTACHED when
// boot succeeds. Every method guards: before Attach it returns a typed not-ready error, never
// a nil dereference. That is the same judgement as strategy.Resolve returning a typed NotFound
// rather than a zero value (0.5 §5) — data (or in this case timing) can be wrong, and the
// answer is a clear error, not a panic three frames later.
//
// # What this does NOT do
//
// Module.Bindings() (0.9 D5) returns a constructed instance per module, which cannot happen
// before Run. In Phase 0 that path is unused — currency's Bindings() returns nil. Reconciling
// the module contract with static binding belongs to Phase 1, with the first module that
// actually ships one; designing it now against zero implementors is the speculation 0.9 D5
// declined.
package bindings

import (
	"sync"

	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// CodeNotReady is returned by every binding that needs the object graph, until boot completes.
//
// A stable code, so the shell renders it through the catalog like any other error rather than
// special-casing startup.
const CodeNotReady = "app.not_ready"

// notReady builds the typed error a guarded method returns before Attach.
func notReady() error {
	return errs.Conflict(CodeNotReady, "the application is still starting")
}

// graph is embedded by every façade that needs the object graph.
//
// The mutex is not ceremony: Attach runs on the boot goroutine while the webview may already
// be calling bindings, so this is a genuine cross-goroutine handoff.
type graph struct {
	mu  sync.RWMutex
	app *bootstrap.App
	// policies are the declarations for THIS façade's methods, and session is the window's
	// current token. Both are set at construction and never change, so guard needs no extra
	// locking for them.
	policies map[string]policy.Policy
	session  *currentSession
}

// declaredPolicies exposes this façade's declarations to ValidatePolicies.
func (g *graph) declaredPolicies() map[string]policy.Policy { return g.policies }

func (g *graph) attach(app *bootstrap.App) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.app = app
}

// resolve returns the graph, or false when it has not been attached yet.
func (g *graph) resolve() (*bootstrap.App, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.app, g.app != nil
}

// Set is every binding struct, constructed before the graph exists.
type Set struct {
	Boot       *Boot
	System     *System
	Config     *Config
	Ops        *Ops
	Money      *Money
	Auth       *Auth
	Audit      *Audit
	Setup      *Setup
	Identity   *Identity
	Accounting *Accounting
	Catalog    *Catalog
	Inventory  *Inventory
	Partners   *Partners
	Sales      *Sales
	Purchasing *Purchasing
	Expenses   *Expenses
	session    *currentSession
	// remember is the "stay signed in" token store. Zero until Attach, because it needs the
	// data directory and the set is built before the graph exists (0.11 D2).
	remember rememberedToken
}

// New constructs the binding set. No database, no graph, no I/O.
func New() *Set {
	// One session holder shared by every façade: the window has one signed-in user (D2).
	session := &currentSession{}
	mk := func(policies map[string]policy.Policy) graph {
		return graph{policies: policies, session: session}
	}
	return &Set{
		Boot:       newBoot(),
		System:     &System{graph: mk(systemPolicies())},
		Config:     &Config{graph: mk(configPolicies())},
		Ops:        &Ops{graph: mk(opsPolicies())},
		Money:      &Money{graph: mk(moneyPolicies())},
		Audit:      &Audit{graph: mk(auditPolicies())},
		Setup:      &Setup{graph: mk(setupPolicies())},
		Identity:   &Identity{graph: mk(identityPolicies())},
		Accounting: &Accounting{graph: mk(accountingPolicies())},
		Catalog:    &Catalog{graph: mk(catalogPolicies())},
		Inventory:  &Inventory{graph: mk(inventoryPolicies())},
		Partners:   &Partners{graph: mk(partnerPolicies())},
		Sales:      &Sales{graph: mk(salesPolicies())},
		Purchasing: &Purchasing{graph: mk(purchasingPolicies())},
		Expenses:   &Expenses{graph: mk(expensePolicies())},
		Auth:       &Auth{graph: mk(authPolicies()), session: session},
		session:    session,
	}
}

// All returns the structs handed to Wails.
//
// The order is stable so the generated JavaScript bindings are stable.
func (s *Set) All() []any {
	return []any{s.Boot, s.System, s.Setup, s.Auth, s.Identity, s.Accounting,
		s.Catalog, s.Partners, s.Inventory, s.Sales, s.Purchasing, s.Expenses,
		s.Config, s.Ops, s.Money, s.Audit}
}

// Attach wires the built graph into every façade and marks boot ready.
//
// Called exactly once, from the boot goroutine, on success.
func (s *Set) Attach(app *bootstrap.App) {
	// "Stay signed in" is restored HERE, before anything can call a binding, so the first
	// question the frontend asks (Auth.Me) already has the right answer. Restoring it later
	// would flash the login screen at someone who explicitly asked not to see it.
	s.remember = newRememberedToken(app.Paths.Data)
	s.Auth.remember = s.remember
	s.restoreRemembered(app)

	s.System.attach(app)
	s.Config.attach(app)
	s.Ops.attach(app)
	s.Money.attach(app)
	s.Auth.attach(app)
	s.Audit.attach(app)
	s.Setup.attach(app)
	s.Identity.attach(app)
	s.Accounting.attach(app)
	s.Catalog.attach(app)
	s.Inventory.attach(app)
	s.Partners.attach(app)
	s.Sales.attach(app)
	s.Purchasing.attach(app)
	s.Expenses.attach(app)
	// Ready is set LAST, after every façade can serve. The shell treats "ready" as permission
	// to mount and immediately calls bindings; marking ready first would open a window in
	// which those calls fail with not-ready for no reason.
	s.Boot.markReady()
}

// restoreRemembered re-establishes a persisted session, or discards it.
//
// Validated once, here, rather than trusted: the stored token may have expired, or an
// administrator may have revoked it from another machine since this one was last open. A dead
// token is removed rather than left to fail on the next call — the file is a convenience, and a
// convenience that lingers after it stops working is a support question.
func (s *Set) restoreRemembered(app *bootstrap.App) {
	token := s.remember.read()
	if token == "" {
		return
	}
	if _, _, err := app.Identity.Validate(app.Context(), token); err != nil {
		s.remember.clear()
		return
	}
	s.session.set(token)
}

// Progress records a migration progress update for the boot screen.
func (s *Set) Progress(p migrate.Progress) { s.Boot.setProgress(p) }

// Fail records a failed boot. backupPath may be empty when the failure was not a migration.
//
// Nothing is attached, so every graph-backed binding keeps returning not-ready and the shell
// stays on the failure screen. That is deliberate: a database that has just failed a migration
// is one a human should look at, not one a half-mounted UI should start querying (0.10 D2).
func (s *Set) Fail(err error, backupPath string) { s.Boot.markFailed(err, backupPath) }

// State reports the boot state, so the shell process can branch without unwrapping an envelope.
func (s *Set) State() string { return s.Boot.state() }
