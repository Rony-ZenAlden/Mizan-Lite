package main

import (
	"context"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/buildinfo"
)

// App is the Wails-bound application surface exposed to the frontend.
//
// In Phase 0 it exposes only health/build info — enough to prove the JS↔Go bridge.
// From Phase 1, the composition root (internal/bootstrap) builds the module graph
// and this struct delegates to per-module binding structs.
type App struct {
	ctx context.Context
}

// NewApp constructs the application shell.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	slog.Info("mizan starting", "version", buildinfo.Version)
}

func (a *App) shutdown(ctx context.Context) {
	slog.Info("mizan shutting down")
}

// Health returns build metadata. Bound to the frontend as window.go.main.App.Health().
func (a *App) Health() buildinfo.Info {
	return buildinfo.Current()
}
