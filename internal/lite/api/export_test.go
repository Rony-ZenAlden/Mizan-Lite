package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/documents"
)

// CallForTest runs fn through the same guard every binding uses.
func CallForTest[T any](s *Set, method string, fn func(context.Context, *bootstrap.App) (T, error)) envelope.Result[T] {
	return call(s.core, method, fn)
}

// ExportDocument is an export's PDF document, as Export would render it — for the test that holds every figure isolated.
func ExportDocument(set *Set, kind string, in any) (documents.Document, error) {
	ctx, app, err := set.core.ready()
	if err != nil {
		return documents.Document{}, err
	}
	w, err := newWords(ctx, app)
	if err != nil {
		return documents.Document{}, err
	}
	var x exportable
	switch v := in.(type) {
	case ExportReportInput:
		x, err = reportContent(ctx, app, w, v)
	case ExportStatementInput:
		x, err = statementContent(ctx, app, w, v)
	case ExportRangeInput:
		if kind == "ledger" {
			x, err = ledgerContent(ctx, app, w, v)
		} else {
			x, err = salesContent(ctx, app, w, v)
		}
	}
	return documents.Document{Direction: w.dir, Running: w.settings.ShopName, Blocks: append([]documents.Block{documents.Title{Text: x.title, Subtitle: x.subtitle}}, x.blocks...)}, err
}

// PrintDocument is the document a print would render.
func PrintDocument(set *Set, kind, rawID string) (documents.Document, error) {
	ctx, app, err := set.core.ready()
	if err != nil {
		return documents.Document{}, err
	}
	w, err := newWords(ctx, app)
	if err != nil {
		return documents.Document{}, err
	}
	p, err := build(ctx, app, w, kind, rawID)
	return p.doc, err
}

// Graph is the attached graph, for tests that need a service beneath the bindings.
func Graph(set *Set) *bootstrap.App {
	_, app, _ := set.core.ready()
	return app
}
