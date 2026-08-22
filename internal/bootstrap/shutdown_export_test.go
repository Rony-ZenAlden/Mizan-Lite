package bootstrap

// ShutdownStepNamesForTest reports the teardown steps, in the order they will run.
//
// In a test-only file rather than the public API: the ORDER of teardown is an internal decision,
// and a caller able to read it is a caller that could come to depend on it. It is exposed because
// the ordering is a real guarantee — the snapshot must be taken while the database is still open
// — and an outcome-only test cannot see it, since a mis-ordered backup fails silently by design.
func ShutdownStepNamesForTest(a *App) []string {
	out := make([]string, 0, len(a.shutdown))
	for _, step := range a.shutdown {
		out = append(out, step.name)
	}
	return out
}
