package i18n_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// frontendOwnedCodes are error codes produced by the TypeScript transport layer rather than by
// Go, so no Go constant declares them.
//
// They are listed explicitly rather than pattern-matched, so adding one is a deliberate act.
// Their own coverage gate lives in frontend/src/lib/wails/call.test.ts, which asserts each has
// a translation in every locale — the same guarantee, enforced from the side that owns them.
var frontendOwnedCodes = map[string]string{
	"app.bridge_unavailable": "frontend/src/lib/wails/call.ts",
	"app.call_failed":        "frontend/src/lib/wails/call.ts",
	"app.malformed_response": "frontend/src/lib/wails/call.ts",
	"app.unknown_error":      "frontend/src/lib/wails/call.ts",
}

func TestNoOrphanErrorTranslations(t *testing.T) {
	// The other direction: a translation for a code that no longer exists is dead weight that
	// a translator will keep maintaining forever.
	//
	// Scoped to errors.json by READING THE FILE, rather than by deriving "error-looking"
	// prefixes from the declared codes. The prefix heuristic broke the moment a real code
	// shared a namespace with UI vocabulary: `app.not_ready` made `app` an error prefix, which
	// then swept in `app.title` and `app.tagline` from common.json — UI strings that will never
	// have a Go constant. Reading the file is both simpler and exact.
	codes := declaredErrorCodes(t)

	path := filepath.Join(repoRoot(t), "locales", locale.Default.String(), "errors.json")
	raw, err := os.ReadFile(path) //nolint:gosec // a fixed path under the module root
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(entries) < 20 {
		// A guard on the guard, matching declaredErrorCodes: a catalog that failed to parse
		// into something meaningful would make this test pass vacuously.
		t.Fatalf("errors.json only has %d entries; the read is probably wrong", len(entries))
	}

	var orphans []string
	for key := range entries {
		if _, declared := codes[key]; declared {
			continue
		}
		if _, owned := frontendOwnedCodes[key]; owned {
			continue
		}
		orphans = append(orphans, key)
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Errorf("errors.json defines %d keys no code declares:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

// TestFrontendOwnedCodesAreTranslated closes the loop on the allowance above: a code exempted
// from the orphan check must still have a translation, or the exemption becomes a hole.
func TestFrontendOwnedCodesAreTranslated(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for code, owner := range frontendOwnedCodes {
		for _, loc := range catalog.Locales() {
			if !catalog.Has(loc, code) {
				t.Errorf("locale %q has no translation for %q (declared in %s)", loc, code, owner)
			}
		}
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

// TestNoTranslationUsesDoubleBracePlaceholders
//
// # A defect that shipped in seventeen keys before a frontend test found it
//
// This catalogue interpolates `{name}`. Phases 8 and 9 wrote `{{name}}` throughout — the
// convention most i18n libraries use — and the renderer left the outer braces alone, so
// "3 hidden" came out as "{3} hidden" in both locales.
//
// Nothing caught it. `TestEveryErrorCodeHasATranslation` checks that a key EXISTS, which every
// one of them did; the Go tests assert on message keys rather than rendered text; and the one
// frontend test that happened to assert a rendered interpolation is what found it.
//
// The lesson is narrower than "test rendering": **a message catalogue has a syntax, and nothing
// was checking that the messages were written in it.** This is that check.
func TestNoTranslationUsesDoubleBracePlaceholders(t *testing.T) {
	doubled := regexp.MustCompile(`\{\{\s*\w+\s*\}\}`)

	var checked int
	for _, locale := range localeDirs(t) {
		for _, file := range []string{"common.json", "errors.json"} {
			path := filepath.Join(locale, file)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			var messages map[string]string
			if err = json.Unmarshal(raw, &messages); err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			for key, value := range messages {
				checked++
				if doubled.MatchString(value) {
					t.Errorf("%s: %q uses {{name}}, which this catalogue renders literally — "+
						"the placeholder is {name}", path, key)
				}
			}
		}
	}

	// The scan read the catalogues. A path that matched nothing would pass while checking
	// nothing at all.
	if checked < 100 {
		t.Fatalf("only %d messages were checked; the catalogues hold far more", checked)
	}
}

// localeDirs finds the shipped locale directories.
func localeDirs(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "locales")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the locales directory: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, filepath.Join(root, entry.Name()))
		}
	}
	if len(out) == 0 {
		t.Fatal("no locales found")
	}
	return out
}

// TestEveryBackupReasonHasALabelInEveryLocale
//
// # The defect this catches, which nothing else could
//
// The backup screen renders a reason with `t("backups.reason." + row.reason)`, so the key is
// BUILT AT RUNTIME from a Go constant. That defeats every check that looks for literal keys: a
// new `Reason` compiles, ships, and renders as the raw string `backups.reason.on_close` in the
// user's table. Nothing fails; it just looks broken.
//
// Adding the on-close reason is what made this reachable — it is the first new reason since the
// screen was written, and the first chance anyone had to find out the gap existed.
//
// It reads the constants from the SOURCE rather than from a hand-kept list. A list would be a
// second place to remember, and the person who forgets the label is exactly the person who
// forgets the list.
func TestEveryBackupReasonHasALabelInEveryLocale(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal", "platform", "backup", "backup.go")
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var reasons []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		ident, ok := spec.Type.(*ast.Ident)
		if !ok || ident.Name != "Reason" {
			return true
		}
		for _, value := range spec.Values {
			lit, isLit := value.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				continue
			}
			unquoted, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				continue
			}
			reasons = append(reasons, unquoted)
		}
		return true
	})

	// A parse that found nothing would pass while checking nothing — the failure mode this
	// whole file exists to prevent.
	if len(reasons) < 4 {
		t.Fatalf("only %d backup reasons were found in %s; the constants are declared there",
			len(reasons), path)
	}

	for _, dir := range localeDirs(t) {
		raw, readErr := os.ReadFile(filepath.Join(dir, "common.json"))
		if readErr != nil {
			t.Fatalf("reading %s: %v", dir, readErr)
		}
		var messages map[string]string
		if err = json.Unmarshal(raw, &messages); err != nil {
			t.Fatalf("parsing %s: %v", dir, err)
		}
		for _, reason := range reasons {
			key := "backups.reason." + reason
			if strings.TrimSpace(messages[key]) == "" {
				t.Errorf("%s has no %q; the backup screen would show a shopkeeper the raw key",
					dir, key)
			}
		}
	}
}
