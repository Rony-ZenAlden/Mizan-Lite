// Command lite-guides renders the shop's guides (docs/mizan_lite/guide/*.md) to the A4 PDFs the application embeds
// (internal/lite/guide/pdf), with Lite's own document renderer (L8 §6.2). Run it after editing a guide:
//
//	go run ./cmd/lite-guides
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/guide"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

func main() {
	ts, err := typeset.Default()
	if err != nil {
		fail(err)
	}
	for _, s := range guide.Sources {
		raw, err := os.ReadFile(filepath.Join("docs", "mizan_lite", "guide", s.Markdown))
		if err != nil {
			fail(err)
		}
		out := documents.PDF(ts, guide.Render(string(raw), s.Direction, s.Footer))
		path := filepath.Join("internal", "lite", "guide", "pdf", s.Name+".pdf")
		if err := os.WriteFile(path, out, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("✔ %s (%d KB)\n", path, len(out)/1024)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lite-guides:", err)
	os.Exit(1)
}
