// Package migrations embeds Mizan Lite's schema.
//
// # One migration per phase, not one for the whole design
//
// The design document (§5) lists Lite's complete schema as a single file. It is applied instead
// as one migration per phase that needs it — 0001 here, catalogue in L1, stock in L2 — for a
// reason that only became concrete once answers arrived after the design was approved: the owner
// PIN (Q7) and opening packages (Q8) both change tables §5 had already drawn. A single 0001 would
// have had to be edited after being applied to development databases, and editing an applied
// migration is precisely what the runner's checksum refuses on every boot. Forward-only has to be
// true from the first file, or it is a habit nobody formed.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed sqlite/*.sql
var sqliteFS embed.FS

// SQLite returns the migrations rooted so filenames are bare, which is what the runner parses the
// version from.
func SQLite() fs.FS {
	sub, err := fs.Sub(sqliteFS, "sqlite")
	if err != nil {
		// Unreachable: the path is the constant the go:embed directive matched at compile time.
		return sqliteFS
	}
	return sub
}
