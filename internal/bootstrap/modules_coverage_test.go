package bootstrap_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
)

// TestEveryModuleTheApplicationRunsAlsoDeclaresItself
//
// # Why this test exists
//
// There are TWO module lists. `bootstrap.Start` builds the real graph with real services;
// `DeclarationModules` builds the same modules with nil services, so permissions, settings and
// migrations can be enumerated without a database.
//
// Nothing connects them. A module added to one and not the other is silent in a specific way:
// every binding that requires one of its permissions becomes permanently unreachable, because
// the permission cannot be granted to anybody. Not a security hole — an outage that looks like a
// configuration problem.
//
// Purchasing was in exactly that state: wired into `Start`, absent from `DeclarationModules`, and
// the failure surfaced as "requires permission purchasing.order.view, which no module declares".
//
// This is drill 44's lesson in a second place. A hand-written list is a place to forget, and the
// answer is to compare it with the thing that cannot be forgotten — the running application.
func TestEveryModuleTheApplicationRunsAlsoDeclaresItself(t *testing.T) {
	app := boot(t)

	declared := make(map[string]bool)
	for _, module := range bootstrap.DeclarationModules() {
		declared[module.Name()] = true
	}

	if len(app.Modules) == 0 {
		t.Fatal("the application booted with no modules, so this test proves nothing")
	}
	for _, module := range app.Modules {
		if !declared[module.Name()] {
			t.Errorf("module %q runs in the application but is missing from "+
				"DeclarationModules — every binding requiring one of its permissions is "+
				"permanently unreachable", module.Name())
		}
	}

	// And the reverse. A module declared but never started would let a permission be granted
	// that nothing honours, which reads to an administrator as a permission that does not work.
	running := make(map[string]bool, len(app.Modules))
	for _, module := range app.Modules {
		running[module.Name()] = true
	}
	for name := range declared {
		if !running[name] {
			t.Errorf("module %q declares permissions but never runs", name)
		}
	}
}

// TestEverySeriesAModuleAllocatesFromIsOneItDeclares
//
// # Why this test exists
//
// The Phase 7 Definition-of-Done review found that no number series were ever created. Every
// module named its series in a constant, every module's TEST FIXTURE created it, and the
// composition root never did — so a freshly provisioned company could not post an invoice, a
// purchase order, or an expense. The fixtures had been standing in for a mechanism that did not
// exist, which is 5.4's rule with the worst possible answer.
//
// `Series()` on the module contract fixed it. This test is what stops it coming back in the
// narrower form: a fourteenth series added as a constant, allocated from in a service, and left
// out of the declaration. That fails at RUNTIME, on the first document, with "there is no number
// series for that kind of document" — which reads like a setup problem rather than a missing
// line of code.
//
// It reads the SOURCE rather than a list, for the same reason 5.8's façade check used reflection:
// a list of series to check against is a fourth place to forget.
func TestEverySeriesAModuleAllocatesFromIsOneItDeclares(t *testing.T) {
	declared := make(map[string]string)
	for _, module := range bootstrap.DeclarationModules() {
		for _, spec := range module.Series() {
			declared[spec.Code] = module.Name()
		}
	}

	// A constant named Series… whose value is a string literal is how every one of them is
	// written. Finding them by NAME rather than by use is deliberate: a series constant that
	// nothing allocates from yet is still a series somebody meant to declare.
	// `const SeriesExpense = "EXPENSE"` and a bare `SeriesInvoice = "SALES_INVOICE"` inside a
	// const block are both written; the optional `const` is not decoration. The first version of
	// this pattern omitted it, matched the eight constants inside blocks and none of the five
	// standalone ones, and still passed — which is why `scanned` below counts what the SCAN
	// found rather than what the modules declared.
	constants := regexp.MustCompile(`(?m)^\s*(?:const\s+)?Series[A-Za-z]*\s*=\s*"([A-Z_]+)"`)
	scanned := make(map[string]bool)

	root := filepath.Join("..", "modules")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		module := strings.Split(strings.TrimPrefix(filepath.ToSlash(path), "../modules/"), "/")[0]

		for _, match := range constants.FindAllStringSubmatch(string(source), -1) {
			code := match[1]
			scanned[code] = true
			owner, found := declared[code]
			if !found {
				t.Errorf("%s declares the series constant %q and no module declares it to the "+
					"composition root — the first document numbered from it will fail at "+
					"runtime with numbering.unknown_series", path, code)
				continue
			}
			if owner != module {
				t.Errorf("%s names series %q, which module %q declares", path, code, owner)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the modules: %v", err)
	}

	// The scan reached every declaration. A pattern that matched nothing — or that matched only
	// the constants written one of the two ways — would report no errors while checking almost
	// nothing, and this is the assertion that catches it.
	for code, module := range declared {
		if !scanned[code] {
			t.Errorf("%s declares series %q, and the source scan never found the constant it "+
				"is written as — this test is checking less than it appears to", module, code)
		}
	}
	if len(scanned) < len(declared) {
		t.Errorf("the scan found %d series constants against %d declared",
			len(scanned), len(declared))
	}
}

// TestAFreshInstallCanNumberEveryDocumentItDeclares
//
// The other half: the declaration reaching the TABLE. `Series()` returning the right list and
// nothing calling it would pass the test above and still leave a company unable to post.
//
// Booting twice is the idempotency assertion. Reconciliation runs on every start, so a second
// start that created a duplicate series would give two rows for one code — and `SeriesFor` takes
// whichever the ORDER BY reaches first, so two terminals could each get number 1.
func TestAFreshInstallCanNumberEveryDocumentItDeclares(t *testing.T) {
	dir := t.TempDir()
	app := bootIn(t, dir)
	ctx := app.Ctx

	// No company is provisioned, and that is the point: the series a company needs must exist
	// before it does, because Provision does not create them and the first document cannot wait.
	// A zero branch resolves the company-wide series, which is the row Ensure writes.
	allocator := numbering.New(app.DB, clock.System())
	var branchID id.ID
	for _, module := range bootstrap.DeclarationModules() {
		for _, spec := range module.Series() {
			// Preview, not Allocate: this asks whether the series RESOLVES for a real branch,
			// which is the question a document asks, without consuming anything.
			number, previewErr := allocator.Preview(ctx, branchID, spec.Code)
			if previewErr != nil {
				t.Errorf("%s: a fresh install cannot number %s: %v",
					module.Name(), spec.Code, previewErr)
				continue
			}
			if want := spec.Prefix + "000001"; number != want {
				t.Errorf("%s: first %s number is %q, want %q",
					module.Name(), spec.Code, number, want)
			}
		}
	}

	var rows int
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM number_series`).Scan(&rows); err != nil {
		t.Fatalf("counting series: %v", err)
	}

	// Boot again over the same database. Nothing may be added.
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	second := bootIn(t, dir)

	var after int
	if err := second.DB.Reader(second.Ctx).QueryRowContext(second.Ctx,
		`SELECT COUNT(*) FROM number_series`).Scan(&after); err != nil {
		t.Fatalf("counting series after a second start: %v", err)
	}
	if after != rows {
		t.Errorf("a second start took the series count from %d to %d — reconciliation is not "+
			"idempotent, and two rows for one code means two documents can share a number",
			rows, after)
	}
}
