// Package notices carries the licences of everything Mizan Lite is built from (L8 D-L8.15), generated offline by
// scripts/lite-notices.sh and shown in the About screen. The OFL requires the font's licence to travel with every copy.
package notices

import _ "embed"

//go:embed THIRD_PARTY_NOTICES.txt
var text string

// Text is the notices, as shipped.
func Text() string { return text }
