package migrate

// Phase names the stage a migration run is in. The bootstrap forwards these to the UI
// so a long run shows real progress instead of appearing hung.
type Phase string

const (
	PhaseChecking  Phase = "checking"  // integrity + version gate
	PhaseBackup    Phase = "backup"    // taking and verifying the safety snapshot
	PhaseMigrating Phase = "migrating" // applying a migration
	PhaseRestoring Phase = "restoring" // a migration failed; rolling the database back
	PhaseDone      Phase = "done"
)

// Progress is emitted as a run advances.
type Progress struct {
	Phase   Phase
	Current int    // 1-based index of the migration being applied
	Total   int    // number of pending migrations
	Name    string // migration file name, when applicable
}

func (r *Runner) emit(p Progress) {
	if r.progress != nil {
		r.progress(p)
	}
}
