package locales_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/lite/locales"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// reachable lists the packages whose error codes can reach a Lite screen, relative to the
// repository root.
//
// # Why these and not every package Lite imports
//
// A code reaches a screen only through a binding's envelope or the boot status. Lite's own packages
// produce both. platform/database's codes pass through a binding unwrapped (a driver error translated
// by the dialect); platform/migrate's and platform/i18n's reach the boot screen as the failure's
// specific REASON. platform/backup and platform/jobs report to the log in L0 — no binding exposes
// them yet — so they join this list in the phase that first binds a backup or job surface.
var reachable = []string{
	"internal/lite",
	"internal/platform/database",
	"internal/platform/migrate",
	"internal/platform/i18n",
}

// TestEveryReachableErrorCodeIsTranslated is Mizan 0.8's coverage gate, scoped to Lite. On its first
// run in Mizan it found four missing codes that step itself had added.
func TestEveryReachableErrorCodeIsTranslated(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	codes := map[string]string{envelope.CodeInternal: "internal/api/envelope"}
	for _, dir := range reachable {
		for code, where := range declaredCodes(t, filepath.Join(root, dir), isCodeName) {
			codes[code] = where
		}
	}
	if len(codes) < 25 {
		t.Fatalf("found only %d codes; the scan is not matching", len(codes))
	}

	catalog, err := i18n.LoadFS(locales.FS())
	if err != nil {
		t.Fatalf("loading Lite's catalogs: %v", err)
	}
	var missing []string
	for code, where := range codes {
		for _, l := range []locale.Locale{"ar", "en"} {
			if !catalog.Has(l, code) {
				missing = append(missing, l.String()+": "+code+" (declared in "+where+")")
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("error codes a screen can receive with no translation:\n  %s", strings.Join(missing, "\n  "))
	}
}

// isCodeName is an error code (Code*) or a verifier finding (Finding*), which a screen shows the same way.
func isCodeName(name string) bool {
	return strings.HasPrefix(name, "Code") || strings.HasPrefix(name, "Finding")
}

// TestEveryGuardedActIsNamedInTheOwnersHistory holds each module's owner-only acts (Act* constants) to a label in the
// owner's history, where OwnerScreen shows them as owner.action.<act>. Added in L4, when the till's three acts would
// otherwise have shown as raw keys.
func TestEveryGuardedActIsNamedInTheOwnersHistory(t *testing.T) {
	acts := declaredCodes(t, filepath.Join("..", "..", "..", "internal", "lite"), func(name string) bool { return strings.HasPrefix(name, "Act") })
	if len(acts) < 12 {
		t.Fatalf("found only %d acts; the scan is not matching", len(acts))
	}
	catalog, err := i18n.LoadFS(locales.FS())
	if err != nil {
		t.Fatal(err)
	}
	for act, where := range acts {
		for _, l := range []locale.Locale{"ar", "en"} {
			if !catalog.Has(l, "owner.action."+act) {
				t.Errorf("%s: owner.action.%s has no label (declared in %s)", l, act, where)
			}
		}
	}
}

// declaredCodes returns every string constant whose name matches in the non-test Go files under dir.
func declaredCodes(t *testing.T, dir string, match func(string) bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !match(name.Name) || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(lit.Value)
					if err == nil {
						out[value] = filepath.ToSlash(path)
					}
				}
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("scanning %s: %v", dir, err)
	}
	return out
}

func TestTheCatalogsLoadWithBothLocales(t *testing.T) {
	catalog, err := i18n.LoadFS(locales.FS())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, l := range catalog.Locales() {
		got[l.String()] = true
	}
	if !got["ar"] || !got["en"] || len(got) != 2 {
		t.Fatalf("locales = %v, want exactly ar and en", got)
	}
}
