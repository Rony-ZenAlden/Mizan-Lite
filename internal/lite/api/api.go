// Package api is Mizan Lite's binding layer: the only Go surface JavaScript can call.
//
// Wails publishes every struct in Set.Bindings under window.go.api — the PACKAGE name. Nothing in
// this repository types that string: the frontend imports the files Wails generates from this
// package, so a rename here fails TypeScript's compile rather than a customer's launch (design C2,
// Mizan 10.17).
//
// # The shape every method has
//
// A façade method does three things and nothing else: refuse if the application is not ready,
// call one service, and wrap the answer in envelope.Result. No business rule lives here, and no
// English crosses the boundary — errors travel as codes the frontend translates.
//
// # Why the façades exist before the graph does
//
// Wails binds a fixed set of structs when the window is created, and the window must open BEFORE
// the graph is built, or a migration has nowhere to show progress and a failed start nowhere to
// explain itself. So the façades are constructed empty, handed to Wails, and attached when boot
// succeeds. Until then every graph-backed method returns CodeNotReady (Mizan 0.11 D2).
package api

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// Stable error codes. They double as i18n keys.
const (
	CodeNotReady      = "lite.api.not_ready"
	CodeStartupFailed = "lite.api.startup_failed"
	CodeInternal      = "lite.api.internal"
)

// Set is every façade Wails binds, and the shared state they read. Set itself is NOT bound.
type Set struct {
	App      *App
	Settings *Settings
	Catalog  *Catalog
	Owner    *Owner
	Stock    *Stock

	core *core
}

// New builds the façades, unattached.
func New(version string, log *slog.Logger) *Set {
	c := &core{version: version, log: log, ctx: context.Background(), state: stateStarting}
	return &Set{
		App:      &App{core: c},
		Settings: &Settings{core: c},
		Catalog:  &Catalog{core: c},
		Owner:    &Owner{core: c},
		Stock:    &Stock{core: c},
		core:     c,
	}
}

// Bindings is exactly what Wails binds. TestEveryFacadeIsBound holds it equal to every exported
// struct in this package that has methods, so a façade left out of this list — which would
// generate no TypeScript and therefore slip past every gate that reads the generated files — fails
// a Go test instead.
func (s *Set) Bindings() []any {
	return []any{s.App, s.Settings, s.Catalog, s.Owner, s.Stock}
}

// SetContext installs the context the window's lifetime runs under. Every call derives from it, so
// closing the window cancels work still in flight.
func (s *Set) SetContext(ctx context.Context) {
	s.core.mu.Lock()
	defer s.core.mu.Unlock()
	s.core.ctx = ctx
}

// Progress records migration progress for the boot screen.
func (s *Set) Progress(p migrate.Progress) {
	s.core.mu.Lock()
	defer s.core.mu.Unlock()
	s.core.progress = p
}

// Fail records that boot failed. The shell stays on the failure screen; every graph-backed method
// keeps refusing.
func (s *Set) Fail(err error) {
	s.core.mu.Lock()
	defer s.core.mu.Unlock()
	s.core.state = stateFailed
	s.core.bootErr = err
}

// Attach makes the built graph available. Called once, when boot succeeds.
func (s *Set) Attach(app *bootstrap.App) {
	s.core.mu.Lock()
	defer s.core.mu.Unlock()
	s.core.app = app
	s.core.state = stateReady
}

type bootState string

const (
	stateStarting bootState = "starting"
	stateReady    bootState = "ready"
	stateFailed   bootState = "failed"
)

type core struct {
	mu       sync.RWMutex
	ctx      context.Context
	state    bootState
	progress migrate.Progress
	bootErr  error
	app      *bootstrap.App
	version  string
	log      *slog.Logger
}

// ready returns the graph and the call context, or a typed refusal.
func (c *core) ready() (context.Context, *bootstrap.App, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.state {
	case stateReady:
		return c.ctx, c.app, nil
	case stateFailed:
		return nil, nil, errs.Conflict(CodeStartupFailed, "the application did not start")
	default:
		return nil, nil, errs.Conflict(CodeNotReady, "the application is still starting")
	}
}

// call runs one binding: refuse if not ready, recover a panic, and wrap the result.
//
// # Why a panic becomes an envelope rather than a crash
//
// A panic in a bound method would otherwise unwind through Wails' IPC dispatcher. The webview would
// see a rejected promise with a Go stack in it, and the transaction the service had open is rolled
// back by the Unit of Work — so no data is harmed — but the application would present a developer
// string to a cashier. It is logged with its stack here, where it can be diagnosed, and the screen
// receives a code it can translate.
func call[T any](c *core, method string, fn func(ctx context.Context, app *bootstrap.App) (T, error)) (result envelope.Result[T]) {
	ctx, app, err := c.ready()
	if err != nil {
		return envelope.Fail[T](err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			c.log.ErrorContext(ctx, "a binding panicked",
				slog.String("method", method),
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())))
			result = envelope.Fail[T](errs.Internal(CodeInternal, "a binding panicked"))
		}
	}()

	value, err := fn(ctx, app)
	if err != nil {
		if _, typed := errs.AsError(err); !typed {
			// The envelope deliberately drops an untyped error's text (it is a developer string); the
			// log is the only place it survives.
			c.log.ErrorContext(ctx, "a binding returned an untyped error",
				slog.String("method", method), slog.Any("error", err))
		}
		return envelope.Fail[T](err)
	}
	return envelope.Ok(value)
}

// reasonOf returns the innermost typed error under a wrapper — the specific cause ("the disk is
// full") beneath the general statement ("the database update failed").
func reasonOf(err error) *errs.Error {
	var innermost *errs.Error
	for e := err; e != nil; e = errors.Unwrap(e) {
		if typed, ok := e.(*errs.Error); ok {
			innermost = typed
		}
	}
	return innermost
}
