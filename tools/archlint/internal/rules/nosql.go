package rules

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
	"github.com/mizan-erp/mizan/tools/archlint/internal/match"
)

// sqlVerbs start a statement; sqlKeywords must also appear for a literal to count as SQL.
//
// Requiring TWO tokens is what keeps this rule usable. "update the display name" is prose and
// stays legal; "UPDATE branches SET name = ?" is not. A single-keyword match would fire on
// ordinary English within a week, and a rule people switch off is worse than no rule at all
// (the lesson recorded in Step 0.5 §11.3, from the other direction).
var (
	sqlVerbs    = []string{"select ", "insert ", "update ", "delete ", "create ", "alter ", "drop "}
	sqlKeywords = []string{" from ", " into ", " set ", " where ", " values", " table ", " join "}
)

// NoSQL forbids SQL string literals in the packages listed under `no-sql` in arch-rules.yml.
//
// This is REPO.3 rule 7 restated. The original — "no raw SQL outside infra packages" — was
// written before platform/* packages owned their own tables, and Step 0.12 found it both
// unenforced and too narrow to enforce as worded. The enforceable intent is that SQL never
// appears in domain, app, or api: business rules and the API boundary must not know how
// anything is stored (§5.1, §5.3, §5.4).
//
// Deliberately a tripwire, not a proof: SQL assembled by concatenation or fmt.Sprintf evades a
// literal scan. The structural defence remains domain-purity, which stops domain packages
// importing the database at all. Overstating what a rule proves is its own kind of defect.
func NoSQL(cfg *config.Config) (*analysis.Analyzer, bool) {
	r := cfg.Rules.NoSQL
	if !r.Enabled || len(r.AppliesTo) == 0 {
		return nil, false
	}

	return &analysis.Analyzer{
		Name:     "nosql",
		Doc:      "forbids SQL literals in domain, app, and api packages (arch-rules.yml)",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			if !match.Any(r.AppliesTo, pass.Pkg.Path()) {
				return nil, nil
			}
			insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			insp.Preorder([]ast.Node{(*ast.BasicLit)(nil)}, func(n ast.Node) {
				lit := n.(*ast.BasicLit)
				if lit.Kind != token.STRING || skipPos(pass, lit.Pos()) {
					return
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return
				}
				if !looksLikeSQL(value) {
					return
				}
				pass.Reportf(lit.Pos(), "[no-sql] %s",
					msgOr(r.Message,
						"SQL belongs to infra/platform — domain, app, and api must not contain it"))
			})
			return nil, nil
		},
	}, true
}

// looksLikeSQL reports whether s opens with a SQL verb and also contains a second SQL keyword.
func looksLikeSQL(s string) bool {
	// Collapse whitespace so a multi-line query reads as one line, and lower-case once.
	normalised := " " + strings.ToLower(strings.Join(strings.Fields(s), " ")) + " "
	trimmed := strings.TrimPrefix(normalised, " ")

	started := false
	for _, verb := range sqlVerbs {
		if strings.HasPrefix(trimmed, verb) {
			started = true
			break
		}
	}
	if !started {
		return false
	}
	for _, kw := range sqlKeywords {
		if strings.Contains(normalised, kw) {
			return true
		}
	}
	return false
}
