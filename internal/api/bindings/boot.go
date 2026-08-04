package bindings

import (
	"sync"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
)

// Boot state values, as the shell sees them.
const (
	BootStarting = "starting"
	BootReady    = "ready"
	BootFailed   = "failed"
)

// BootStatusDTO is the boot sequence as the shell sees it.
//
// Flat, JSON-shaped, no domain types (§5.4). The error is a code and parameters — never prose
// — so a startup failure renders in the user's language like every other error (§22.2). That
// matters most here: a failed migration is the one message a customer is guaranteed to read
// under stress.
type BootStatusDTO struct {
	State string `json:"state"`
	Phase string `json:"phase"`
	// Current and Total describe migration progress; both zero when nothing is pending.
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Name    string `json:"name"`
	// Error is nil unless State is "failed".
	Error *envelope.APIError `json:"error"`
	// BackupPath is where the pre-migration snapshot was written, when a migration failed.
	// It is the one piece of a failure the user actually needs: their data is safe, and here.
	BackupPath string `json:"backupPath"`
}

// Boot reports startup progress.
//
// It is the ONE binding callable before the object graph exists, because it is what reports
// whether the graph exists. It therefore embeds no graph and has no not-ready guard.
type Boot struct {
	mu     sync.RWMutex
	status BootStatusDTO
}

func newBoot() *Boot {
	return &Boot{status: BootStatusDTO{State: BootStarting, Phase: string(migrate.PhaseChecking)}}
}

// Status returns the current boot state.
//
// The shell polls this on mount as well as subscribing to events, because events fired before
// the webview finished loading are lost — a fast boot on a warm database beats the frontend to
// the subscription almost every time. Events make the screen update sooner; this makes it
// correct.
func (b *Boot) Status() envelope.Result[BootStatusDTO] {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return envelope.Ok(b.status)
}

// setProgress records a migration progress update.
func (b *Boot) setProgress(p migrate.Progress) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.State != BootStarting {
		return
	}
	b.status.Phase = string(p.Phase)
	b.status.Current = p.Current
	b.status.Total = p.Total
	b.status.Name = p.Name
}

// markReady records a successful boot.
func (b *Boot) markReady() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.State = BootReady
	b.status.Phase = string(migrate.PhaseDone)
	b.status.Error = nil
}

// markFailed records a failed boot, with the backup path when one exists.
func (b *Boot) markFailed(err error, backupPath string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.State = BootFailed
	b.status.Error = envelope.FromError(err)
	b.status.BackupPath = backupPath
}

// State reports the current boot state, for the shell process to branch on without unwrapping
// an envelope.
func (b *Boot) state() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status.State
}
