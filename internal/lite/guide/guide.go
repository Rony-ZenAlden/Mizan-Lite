package guide

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/lite/documents"
)

// A Source is one shipped guide: its name, the Markdown it is made from (relative to docs/mizan_lite/guide), and its direction.
type Source struct {
	Name      string
	Markdown  string
	Direction documents.Direction
	// Footer is the page line, {page} and {pages} replaced.
	Footer string
}

// Sources lists every guide the application ships.
var Sources = []Source{
	{Name: "shop-guide-ar", Markdown: "SHOP_GUIDE.ar.md", Direction: documents.RTL, Footer: "ميزان لايت — دليل المحل · صفحة {page} من {pages}"},
	{Name: "shop-guide-en", Markdown: "SHOP_GUIDE.md", Direction: documents.LTR, Footer: "Mizan Lite — Shop guide · page {page} of {pages}"},
	{Name: "quick-card-ar", Markdown: "QUICK_CARD.ar.md", Direction: documents.RTL, Footer: "ميزان لايت — بطاقة الصندوق"},
	{Name: "install-ar", Markdown: "INSTALL.ar.md", Direction: documents.RTL, Footer: "ميزان لايت — التثبيت · صفحة {page} من {pages}"},
	{Name: "install-en", Markdown: "INSTALL.md", Direction: documents.LTR, Footer: "Mizan Lite — Installation · page {page} of {pages}"},
	{Name: "troubleshooting-ar", Markdown: "TROUBLESHOOTING.ar.md", Direction: documents.RTL, Footer: "ميزان لايت — حل المشكلات · صفحة {page} من {pages}"},
	{Name: "troubleshooting-en", Markdown: "TROUBLESHOOTING.md", Direction: documents.LTR, Footer: "Mizan Lite — Troubleshooting · page {page} of {pages}"},
}

// Names is every shipped guide's name, in a stable order.
func Names() []string {
	out := make([]string, 0, len(Sources))
	for _, s := range Sources {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// FileName is the name a saved guide is proposed under.
func FileName(name string) string {
	return "Mizan Lite - " + strings.ReplaceAll(name, "-", " ") + ".pdf"
}
