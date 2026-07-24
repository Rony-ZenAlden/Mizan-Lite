package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
	"github.com/mizan-erp/mizan/tools/archlint/internal/match"
)

// ForbidCall forbids specific function calls in specific locations, per the
// `forbid-call` block in arch-rules.yml. Callees are resolved through type
// information, so it never confuses a local `panic` variable for the builtin, nor
// a shadowed package name for the real one.
func ForbidCall(cfg *config.Config) (*analysis.Analyzer, bool) {
	r := cfg.Rules.ForbidCall
	if !r.Enabled || len(r.Calls) == 0 {
		return nil, false
	}
	return &analysis.Analyzer{
		Name:     "forbidcall",
		Doc:      "forbids specific function calls in specific locations (arch-rules.yml)",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			pkgPath := pass.Pkg.Path()

			insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
				call := n.(*ast.CallExpr)
				if skipPos(pass, call.Pos()) {
					return
				}
				name := calleeName(pass.TypesInfo, call)
				if name == "" {
					return
				}
				filename := pass.Fset.Position(call.Pos()).Filename
				for _, c := range r.Calls {
					if c.Selector != name {
						continue
					}
					if len(c.AppliesTo) > 0 && !match.Any(c.AppliesTo, pkgPath) {
						continue
					}
					if match.Any(c.Exclude, filename) {
						continue
					}
					pass.Reportf(call.Pos(), "[forbid-call] %s",
						msgOr(c.Message, name+"() is forbidden here"))
				}
			})
			return nil, nil
		},
	}, true
}

// calleeName returns the fully-qualified name of a called function, e.g. "panic",
// "os.Exit", "fmt.Println", "time.Now". It returns "" for calls it cannot resolve
// to a package-level function or builtin (method calls, func values, etc.).
func calleeName(info *types.Info, call *ast.CallExpr) string {
	obj := typeutil.Callee(info, call)
	if obj == nil {
		return ""
	}
	switch o := obj.(type) {
	case *types.Builtin:
		return o.Name() // "panic", "print", ...
	case *types.Func:
		// FullName yields "os.Exit", "fmt.Println", "time.Now" for package funcs.
		return o.FullName()
	default:
		return ""
	}
}
