package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
	"github.com/mizan-erp/mizan/tools/archlint/internal/match"
)

// NoFloat forbids the universe float types in money/quantity/domain packages, per
// the `no-float` block in arch-rules.yml. Money must never be a floating-point
// number anywhere in the system.
func NoFloat(cfg *config.Config) (*analysis.Analyzer, bool) {
	r := cfg.Rules.NoFloat
	if !r.Enabled || len(r.AppliesTo) == 0 {
		return nil, false
	}
	float32Obj := types.Universe.Lookup("float32")
	float64Obj := types.Universe.Lookup("float64")

	return &analysis.Analyzer{
		Name:     "nofloat",
		Doc:      "forbids float32/float64 in money and domain packages (arch-rules.yml)",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			if !match.Any(r.AppliesTo, pass.Pkg.Path()) {
				return nil, nil
			}
			insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
				id := n.(*ast.Ident)
				if skipPos(pass, id.Pos()) {
					return
				}
				if obj := pass.TypesInfo.Uses[id]; obj != nil &&
					(obj == float32Obj || obj == float64Obj) {
					pass.Reportf(id.Pos(), "[no-float] %s",
						msgOr(r.Message, "float32/float64 are forbidden here"))
				}
			})
			return nil, nil
		},
	}, true
}
