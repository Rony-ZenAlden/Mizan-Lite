// Package locales embeds the message catalogs shipped with the binary.
//
// The JSON files live here, at the module root rather than under internal/, for two reasons.
// go:embed cannot reach above its own directory, so a package under internal/platform could
// not embed a top-level locales/ tree — the same constraint that put migrations/ here. And
// ARCHITECTURE_v1 §22.1 requires ONE set of files consumed by both Go and React: a translator
// editing a catalog should not have to find it inside a Go package, and the frontend build
// imports these exact files.
//
// One directory per locale, discovered at runtime rather than enumerated in code: adding
// Kurdish is dropping in locales/ku/, with no Go change.
package locales

import (
	"embed"
	"io/fs"
)

//go:embed en ar
var catalogFS embed.FS

// FS returns the embedded catalogs, rooted so paths read "<locale>/<file>.json".
func FS() fs.FS { return catalogFS }
