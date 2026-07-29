// Package migrations embeds the schema migrations shipped with the binary.
//
// The files live here, at the module root rather than under internal/, because go:embed
// can only reach files in or below its own directory: a package under internal/platform
// could not embed a top-level migrations/ tree, and burying the SQL inside a Go package
// directory would hide the schema from anyone reading the repository.
//
// One subdirectory per dialect (ARCHITECTURE_v1 §10.5). A future PostgreSQL port adds
// migrations/postgres/ and a Postgres() accessor; nothing else changes.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed sqlite/*.sql
var sqliteFS embed.FS

// SQLite returns the SQLite migrations, rooted so filenames are bare (e.g.
// "0001_platform.sql"). The runner parses the version from the filename, so the
// directory prefix must not be part of it.
func SQLite() fs.FS {
	sub, err := fs.Sub(sqliteFS, "sqlite")
	if err != nil {
		// Unreachable: the path is a compile-time constant matched by the go:embed
		// directive above, so a failure here would mean the binary was built without
		// its migrations — which cannot happen without a compile error.
		return sqliteFS
	}
	return sub
}
