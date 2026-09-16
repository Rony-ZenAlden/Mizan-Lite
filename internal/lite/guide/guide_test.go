package guide_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/guide"
	guidepdf "github.com/mizan-erp/mizan/internal/lite/guide/pdf"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

func source(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "mizan_lite", "guide", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestTheEmbeddedGuidesAreTheDocuments: a guide edited and not regenerated fails here, with the command that fixes it.
func TestTheEmbeddedGuidesAreTheDocuments(t *testing.T) {
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range guide.Sources {
		want := documents.PDF(ts, guide.Render(source(t, s.Markdown), s.Direction, s.Footer))
		got, err := guidepdf.Guide(s.Name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not %s rendered — run: go run ./cmd/lite-guides", s.Name, s.Markdown)
		}
	}
	if _, err := guidepdf.Guide("secret-plans"); err == nil {
		t.Error("an unknown guide was served")
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	md := source(t, "SHOP_GUIDE.ar.md")
	if !bytes.Equal(documents.PDF(ts, guide.Render(md, documents.RTL, "x")), documents.PDF(ts, guide.Render(md, documents.RTL, "x"))) {
		t.Fatal("the same guide rendered twice differs")
	}
}

func TestAGuideReadsAsItsMarkdown(t *testing.T) {
	md := "# دليل المحل\n\nافتح الصندوق في 8:30 كل يوم.\n\n## البيع\n\n- اضغط **ادفع** أو F9\n1. امسح الباركود\n\n| المفتاح | العمل |\n|---|---|\n| F9 | الدفع |\n\n![صورة](img/till.png)\n\n> ملاحظة\n"
	doc := guide.Render(md, documents.RTL, "صفحة {page}")
	if doc.Meta.Title != "دليل المحل" || doc.Meta.Created != guide.Created {
		t.Fatalf("meta %+v", doc.Meta)
	}
	var kinds []string
	for _, b := range doc.Blocks {
		switch x := b.(type) {
		case documents.Title:
			kinds = append(kinds, "title")
		case documents.Heading:
			kinds = append(kinds, "heading")
		case documents.Paragraph:
			if strings.ContainsAny(x.Text, "*`") {
				t.Errorf("markdown left in %q", x.Text)
			}
			kinds = append(kinds, "paragraph")
		case documents.Table:
			if len(x.Rows) != 1 || x.Rows[0][0].Text != typeset.Isolate("9") && !strings.Contains(x.Rows[0][0].Text, "F") {
				t.Errorf("table %+v", x)
			}
			kinds = append(kinds, "table")
		}
	}
	if strings.Join(kinds, ",") != "title,paragraph,heading,paragraph,paragraph,table,paragraph" {
		t.Fatalf("blocks %v", kinds)
	}
	if bad := documents.Unisolated(doc); len(bad) > 0 {
		t.Fatalf("figures not isolated in an Arabic guide: %q", bad)
	}
}

func TestEveryGuideHasItsSource(t *testing.T) {
	for _, s := range guide.Sources {
		if !strings.HasPrefix(strings.TrimSpace(source(t, s.Markdown)), "# ") {
			t.Errorf("%s does not start with a title", s.Markdown)
		}
	}
}
