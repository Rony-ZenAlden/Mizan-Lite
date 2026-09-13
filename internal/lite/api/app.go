package api

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
)

// App is the application's own state: whether it has started, and what it is running on.
type App struct{ core *core }

// BootStatusDTO is what the boot screen renders.
type BootStatusDTO struct {
	// State is "starting", "ready" or "failed".
	State string `json:"state"`
	// Phase is the migration runner's phase while starting: checking, backup, migrating, restoring
	// or done. Empty before the runner reports anything.
	Phase   string `json:"phase"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	// Error is the failure, as the boot screen's headline. Present only when State is "failed".
	Error *envelope.APIError `json:"error,omitempty"`
	// Reason is the specific cause beneath Error, when there is one — "there is not enough disk
	// space" under "the database update failed".
	Reason *envelope.APIError `json:"reason,omitempty"`
	// BackupPath is where the pre-migration snapshot is, when a failed update left one.
	BackupPath string `json:"backupPath,omitempty"`
}

// BootStatus reports how far startup has got. It always succeeds, in every state: it is the one
// method the boot screen must be able to call before anything else works.
func (a *App) BootStatus() envelope.Result[BootStatusDTO] {
	c := a.core
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := BootStatusDTO{
		State:   string(c.state),
		Phase:   string(c.progress.Phase),
		Current: c.progress.Current,
		Total:   c.progress.Total,
	}
	if c.state == stateFailed && c.bootErr != nil {
		out.Error = envelope.FromError(c.bootErr)
		if reason := reasonOf(c.bootErr); reason != nil && reason.Code != out.Error.Code {
			out.Reason = envelope.FromError(reason)
		}
		out.BackupPath = out.Error.Params["backup"]
	}
	return envelope.Ok(out)
}

// HealthDTO proves the frontend reached the Go backend, and says what it reached.
type HealthDTO struct {
	Version       string `json:"version"`
	SchemaVersion int64  `json:"schemaVersion"`
	// Platform is the Go GOOS value: "windows" or "darwin" for a shipped build.
	Platform string `json:"platform"`
	// DataDir is shown so a support conversation can find the shop's data without guessing.
	DataDir string `json:"dataDir"`
}

// Health reports the running application.
//
// It is logged, once per call, because it is the observable proof that a packaged build's webview
// reached its own backend — the property Mizan's 10.17 shipped without.
func (a *App) Health() envelope.Result[HealthDTO] {
	return call(a.core, "App.Health", func(ctx context.Context, app *bootstrap.App) (HealthDTO, error) {
		a.core.log.InfoContext(ctx, "health requested by the frontend",
			slog.String("version", a.core.version))
		return HealthDTO{
			Version:       a.core.version,
			SchemaVersion: app.SchemaVersion,
			Platform:      runtime.GOOS,
			DataDir:       app.Paths.Data,
		}, nil
	})
}
