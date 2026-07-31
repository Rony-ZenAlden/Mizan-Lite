package i18n_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// These are the completeness gates: the part of Step 0.8 most likely to prevent a real,
// customer-visible defect. They assert things about the whole repository, not about one
// function.

// repoRoot walks up from this package to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, statErr := filepath.Glob(filepath.Join(dir, "go.mod")); statErr == nil {
			if matches, _ := filepath.Glob(filepath.Join(dir, "go.mod")); len(matches) == 1 {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate the module root")
	return ""
}

// declaredErrorCodes walks internal/ and collects every `Code… = "…"` constant.
//
// The AST rather than a regex: a constant spelled across two lines, or a value containing an
// escape, would slip past a grep, and this gate is worthless if it silently misses codes.
func declaredErrorCodes(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	codes := map[string]string{} // code value → where it was declared

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, _ := filepath.Rel(root, path)

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
					if !strings.HasPrefix(name.Name, "Code") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, unquoteErr := strconv.Unquote(lit.Value)
					if unquoteErr != nil || value == "" {
						continue
					}
					codes[value] = rel
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	if len(codes) < 20 {
		// A guard on the guard: if the walk silently stopped finding constants, this test
		// would pass vacuously and the gate would be gone without anyone noticing.
		t.Fatalf("only found %d error codes; the AST walk is probably broken", len(codes))
	}
	return codes
}

func TestEveryErrorCodeHasATranslation(t *testing.T) {
	// The backend returns codes and the frontend renders them (§22.2). A code with no catalog
	// entry therefore reaches the user as raw text like "outbox.handler_panicked".
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	codes := declaredErrorCodes(t)

	for _, loc := range catalog.Locales() {
		var missing []string
		for code := range codes {
			if !catalog.Has(loc, code) {
				missing = append(missing, code+"  (declared in "+codes[code]+")")
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("locale %q is missing %d error translations:\n  %s",
				loc, len(missing), strings.Join(missing, "\n  "))
		}
	}
}

func TestNoOrphanErrorTranslations(t *testing.T) {
	// The other direction: a translation for a code that no longer exists is dead weight that
	// a translator will keep maintaining forever.
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	codes := declaredErrorCodes(t)

	// Only keys that look like error codes are checked; common.json holds UI vocabulary that
	// has no corresponding constant.
	errorPrefixes := map[string]bool{}
	for code := range codes {
		if i := strings.Index(code, "."); i > 0 {
			errorPrefixes[code[:i]] = true
		}
	}

	var orphans []string
	for _, key := range catalog.Keys(locale.Default) {
		i := strings.Index(key, ".")
		if i <= 0 || !errorPrefixes[key[:i]] {
			continue
		}
		if _, declared := codes[key]; !declared {
			orphans = append(orphans, key)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Errorf("errors.json defines %d keys no code declares:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

func TestEveryLocaleDefinesTheSameKeys(t *testing.T) {
	// A locale missing keys another has means blank labels in one language only — found by a
	// customer, not by us.
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}

	reference := catalog.Keys(locale.Default)
	referenceSet := map[string]bool{}
	for _, k := range reference {
		referenceSet[k] = true
	}

	for _, loc := range catalog.Locales() {
		if loc == locale.Default {
			continue
		}
		keys := catalog.Keys(loc)
		keySet := map[string]bool{}
		for _, k := range keys {
			keySet[k] = true
		}

		var missing, extra []string
		for _, k := range reference {
			if !keySet[k] {
				missing = append(missing, k)
			}
		}
		for _, k := range keys {
			if !referenceSet[k] {
				extra = append(extra, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("locale %q is missing %d keys:\n  %s", loc, len(missing), strings.Join(missing, "\n  "))
		}
		if len(extra) > 0 {
			t.Errorf("locale %q defines %d keys %q does not:\n  %s",
				loc, len(extra), locale.Default, strings.Join(extra, "\n  "))
		}
	}
}

func TestGoAndFrontendShareTheSameCatalog(t *testing.T) {
	// ARCHITECTURE_v1 §22.1: "one set of files, one key namespace, no drift between backend
	// error messages and frontend labels." This asserts it rather than trusting it.
	//
	// The frontend imports locales/*.json directly, so the check is that no second catalog has
	// reappeared in the frontend source.
	root := repoRoot(t)
	strayPath := filepath.Join(root, "frontend", "src", "i18n", "locales.ts")
	if matches, _ := filepath.Glob(strayPath); len(matches) > 0 {
		t.Errorf("frontend/src/i18n/locales.ts still exists; the frontend must import "+
			"locales/*.json so there is one catalog, not two (§22.1). Found: %s", strayPath)
	}

	// And the shared files the frontend imports must be the ones Go embeds.
	for _, rel := range []string{
		"locales/en/common.json", "locales/en/errors.json",
		"locales/ar/common.json", "locales/ar/errors.json",
	} {
		if matches, _ := filepath.Glob(filepath.Join(root, rel)); len(matches) == 0 {
			t.Errorf("%s is missing; the frontend build imports it", rel)
		}
	}
}
