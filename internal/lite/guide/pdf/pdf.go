// Package pdf embeds the shop's guides as cmd/lite-guides rendered them (L8 §6.2). It is apart from package guide so the
// generator, which uses guide, builds before any PDF exists.
package pdf

import (
	"embed"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/guide"
)

// CodeUnknown is asking for a guide that does not exist.
const CodeUnknown = "lite.guide.unknown"

//go:embed *.pdf
var files embed.FS

// Guide is a shipped guide's bytes.
func Guide(name string) ([]byte, error) {
	for _, s := range guide.Sources {
		if s.Name == name {
			return files.ReadFile(name + ".pdf")
		}
	}
	return nil, errs.Validation(CodeUnknown, "no such guide").WithParam("name", name)
}
