package bootstrap_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The tests in this file exist because the Phase 8 Definition-of-Done review found each of them
// missing.

// ── Criterion 8 ─────────────────────────────────────────────────────────────────
//
// "No report writes anything: every method in this phase is safe to call twice."
//
// # Nothing tested this, and it is the criterion the whole phase rests on
//
// Phase 8's D2 says a report is a QUERY, never a document — no numbering, no audit entry, no
// posting. Every other guarantee in the phase assumes it: the drills re-run reports freely, the
// dashboard calls six of them on every mount, and a screen refreshes on every focus.
//
// A report that wrote would not fail loudly. It would allocate a number, or leave an audit row,
// or touch `updated_at` — and the symptom would appear months later as a gap in an invoice
// sequence nobody can explain.
//
// The check is the whole database, not a list of tables somebody remembered.
func TestNoReportWritesAnything(t *testing.T) {
	app, ctx, companyID := traded(t)

	// A company with something in it, so the reports have work to do rather than returning
	// early. An empty database cannot detect a write that only happens when there are rows.
	seedForReports(t, app, ctx, companyID)

	before := databaseFingerprint(t, app, ctx)

	runEveryReport(t, app, ctx, companyID)
	runEveryReport(t, app, ctx, companyID)

	after := databaseFingerprint(t, app, ctx)

	if len(before) != len(after) {
		t.Fatalf("the reports changed the SHAPE of the database: %d tables before, %d after",
			len(before), len(after))
	}
	for table, was := range before {
		if now := after[table]; now != was {
			t.Errorf("running the reports changed %s: %q became %q — a report wrote, and the "+
				"symptom will arrive months later as a gap nobody can explain", table, was, now)
		}
	}

	// The fingerprint actually looked at something. A query that matched no tables would pass
	// this test while checking nothing — 7.6's D162 and 8.6's tile count, in a third place.
	if len(before) < 10 {
		t.Fatalf("only %d tables were fingerprinted; this schema has far more", len(before))
	}
}

// databaseFingerprint summarises every table's contents.
//
// Row count plus the MAX of every timestamp-ish column would be more precise, and it would also
// be a list of column names to keep in step with the schema. Counting rows and summing the
// row_version column where one exists catches both a new row and a modified one, and needs no
// list.
func databaseFingerprint(t *testing.T, app *bootstrap.App, ctx context.Context) map[string]string {
	t.Helper()

	rows, err := app.DB.Reader(ctx).QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		  ORDER BY name`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatalf("listing tables: %v", err)
		}
		tables = append(tables, name)
	}
	_ = rows.Close()

	fingerprint := make(map[string]string, len(tables))
	for _, table := range tables {
		var count int
		if err = app.DB.Reader(ctx).QueryRowContext(ctx,
			`SELECT COUNT(*) FROM "`+table+`"`).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}

		// row_version moves on any update the codebase makes through its repositories, so
		// summing it catches a modification that leaves the count alone.
		versions := "n/a"
		var total int64
		if err = app.DB.Reader(ctx).QueryRowContext(ctx,
			`SELECT COALESCE(SUM(row_version), 0) FROM "`+table+`"`).Scan(&total); err == nil {
			versions = itoa(total)
		}
		fingerprint[table] = itoa(int64(count)) + ":" + versions
	}
	return fingerprint
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// ── Criterion 9 ─────────────────────────────────────────────────────────────────
//
// "No new document table, and every index added is justified by a query that exists."
//
// # This check was a point in time, and the point has passed
//
// `TestPhaseEightAddedNoSchema` asserted that no migration numbered above 0036 — Phase 7's last —
// existed. It passed when Phase 8 closed, which is what it was for.
//
// Phase 9 then added 0037 for notice dismissals, legitimately, and the test failed. There is no
// version of it that survives: the bound it needs is "the highest migration when Phase 8 closed",
// which is 36, and the window between 36 and Phase 9's 37 is EMPTY. A test that cannot fail is
// not a test, and weakening this one to keep it green would have been worse than deleting it —
// a check nobody can make fail is a claim nobody has verified.
//
// It is recorded here rather than removed silently, because the criterion it proved is real and a
// later phase adding a reporting projection would now go unnoticed. The honest statement is:
// **Phase 8 added no schema, verified at the time; nothing enforces it going forward.**

// ── Criterion 3 ─────────────────────────────────────────────────────────────────
//
// "Gross margin and net profit are both reported, both named, and neither is called 'profit'
// alone."
//
// # Why a structural check and not only a screen test
//
// The frontend test asserts the STATEMENTS screen says "Net profit" and never "Profit". It says
// nothing about the next screen somebody writes, or about a DTO field named `profitMinor` that a
// screen would then have to label.
//
// The word is the trap: gross margin excludes rent and wages, net profit includes them, and a
// figure labelled "profit" is ambiguous between two numbers that differ by everything the shop
// spends. Naming it out of the codebase is the only version of this rule that holds.
func TestNothingInThePhaseIsCalledProfitAlone(t *testing.T) {
	// A SUBSTRING match, not a word-bounded one.
	//
	// The first version used `\bprofit\b`, which matches nothing inside `NetProfitMinor` — the
	// P is preceded by a letter and followed by one, so there is no word boundary. Every
	// identifier this check exists to police is a compound, so every one of them was invisible
	// to it, and the "did the scan find anything" guard is what said so.
	bare := regexp.MustCompile(`(?i)profit`)

	// # What the criterion is actually about, narrowed after the first version was too broad
	//
	// "ProfitAndLoss" is the STATEMENT's proper name and is not ambiguous — it is what
	// accountants call the document, and a permission guarding it is named after it. The second
	// version of this test flagged all seven occurrences of it, which was the test being wrong
	// rather than the code.
	//
	// What the criterion forbids is a FIGURE called profit: an amount somebody would put on a
	// screen, where the reader cannot tell whether rent has been deducted. So the check applies
	// to identifiers naming a quantity — those ending in Minor, Micro, or Amount.
	namesAFigure := regexp.MustCompile(`(?:Minor|Micro|Amount)$`)

	files := []string{
		"../modules/accounting/statements.go",
		"../modules/sales/analysis.go",
		"../modules/purchasing/analysis.go",
		"../api/bindings/insight.go",
	}

	var checked int
	for _, path := range files {
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if !bare.MatchString(identifier.Name) {
				return true
			}
			checked++
			if !namesAFigure.MatchString(identifier.Name) {
				return true
			}
			// NetProfitMinor is the one acceptable form: it says which of the two numbers it is.
			if !strings.Contains(identifier.Name, "NetProfit") &&
				!strings.Contains(identifier.Name, "netProfit") {
				t.Errorf("%s declares %q — 'profit' alone is ambiguous between gross margin and "+
					"net profit, which differ by everything the shop spends",
					filepath.Base(path), identifier.Name)
			}
			return true
		})
	}

	// The scan found the identifiers it exists to police.
	if checked == 0 {
		t.Fatal("no identifier containing 'profit' was found in any of the phase's files, " +
			"which means this check is looking in the wrong place")
	}
}

// seedForReports gives the reports something to report on.
func seedForReports(t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID) {
	t.Helper()
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	if err := app.Accounting.ApplyRules(ctx, companyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
}

// runEveryReport calls every read this phase added.
//
// Listed by hand, and that is a known weakness: a report added later and left out of this list is
// a report nobody proves is read-only. The alternative was reflection over the Insight façade,
// which needs a session and a permission set — real work for a check whose failure mode is
// "somebody forgot", and 8.8 records it as a gap rather than pretending otherwise.
func runEveryReport(t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID) {
	t.Helper()

	if _, err := app.Accounting.ProfitAndLoss(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("ProfitAndLoss: %v", err)
	}
	if _, err := app.Accounting.BalanceSheet(ctx, companyID, "2026-12-31"); err != nil {
		t.Fatalf("BalanceSheet: %v", err)
	}
	if _, err := app.Sales.SalesByPeriod(ctx, companyID, "2026-01-01", "2026-12-31", "day"); err != nil {
		t.Fatalf("SalesByPeriod: %v", err)
	}
	if _, err := app.Sales.SalesByProduct(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if _, err := app.Sales.SalesByPartner(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("SalesByPartner: %v", err)
	}
	if _, err := app.Purchasing.SpendByPeriod(ctx, companyID, "2026-01-01", "2026-12-31", "day"); err != nil {
		t.Fatalf("SpendByPeriod: %v", err)
	}
	if _, err := app.Purchasing.SpendByProduct(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("SpendByProduct: %v", err)
	}
	if _, err := app.Purchasing.SpendBySupplier(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("SpendBySupplier: %v", err)
	}
	if _, err := app.Inventory.Valuation(ctx, companyID); err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if _, err := app.Search.Search(ctx, companyID, "an", 10); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := app.Dashboard(ctx, companyID, "2026-01-01", "2026-12-31"); err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
}
