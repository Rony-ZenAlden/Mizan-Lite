package backup

import "os"

// SetRenameForTest replaces the rename used by Apply.
//
// In an _test.go-adjacent file rather than the package's public API, because the ability to
// replace a filesystem operation is not something a caller should have — it exists so the one
// branch that cannot be reached by arranging files can be exercised.
func SetRenameForTest(f func(from, to string) error) { rename = f }

// ResetRenameForTest restores the real one.
func ResetRenameForTest() { rename = os.Rename }
