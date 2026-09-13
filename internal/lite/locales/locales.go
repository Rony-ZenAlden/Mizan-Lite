// Package locales embeds Mizan Lite's message catalogs.
//
// One set of files for both sides (Mizan 0.8, ARCHITECTURE_v1 §22.1): Go embeds them here and the
// Vite build imports these exact files through the `@locales` alias, so a word the backend uses and
// a word a screen shows cannot come from two catalogs that drifted.
//
// Arabic is the primary language of this product and the one the application opens in. English is
// complete, not a fallback: the parity gate refuses a key that exists in only one of them.
package locales

import (
	"embed"
	"io/fs"
)

//go:embed ar en
var catalogFS embed.FS

// FS returns the catalogs, rooted so paths read "<locale>/<file>.json".
func FS() fs.FS { return catalogFS }
