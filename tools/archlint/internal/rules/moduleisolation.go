package rules

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
)

// ModuleIsolation enforces ARCHITECTURE_v1 §3.2: a module may reach another module only
// through its published `contract` package.
//
// §3.2 states the stake: a module "exports a small contract package containing interfaces and
// DTOs. It never exports its aggregates. Sales must not be able to construct an
// inventory.StockLevel." Without this, module-to-module coupling grows silently — which §3.1
// names as "the single most common way an ERP becomes unmaintainable".
//
// # Why this needs no whole-program pass
//
// arch-rules.yml carried this rule under "planned whole-program rules" for three steps, and
// Step 0.12 repeated the assumption. It is wrong: deciding the rule needs only the owning
// module of the file (from its own package path) and the owning module of each import (from
// the import path). Both are prefix arithmetic. The graph-pass caveat genuinely applies to
// max-dependency-depth and no-package-cycles, and was over-applied to this one.
func ModuleIsolation(cfg *config.Config) (*analysis.Analyzer, bool) {
	r := cfg.Rules.ModuleIsolation
	if !r.Enabled || r.ModulesRoot == "" {
		return nil, false
	}
	root := r.ModulesRoot + "/"
	contract := r.ContractDir

	return &analysis.Analyzer{
		Name: "moduleisolation",
		Doc:  "a module may import another module only via its contract package (arch-rules.yml)",
		Run: func(pass *analysis.Pass) (any, error) {
			owner, ok := moduleOf(pass.Pkg.Path(), root)
			if !ok {
				// Not inside a module. The composition root is *supposed* to import every
				// module, and platform is already barred by platform-independent-of-modules.
				return nil, nil
			}

			// Walk each FILE's imports rather than the package's, for two reasons: it lets
			// skipPos exempt _test.go and the synthetic packages the go tool builds (an
			// external test package `foo_test` would otherwise read as a module importing
			// `foo`), and it reports the offending import line instead of the file head.
			for _, file := range pass.Files {
				for _, spec := range file.Imports {
					if skipPos(pass, spec.Pos()) {
						continue
					}
					path, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						continue
					}
					other, ok := moduleOf(path, root)
					if !ok || other == owner {
						continue // not a module, or this module's own package
					}
					if isContractPath(path, root, other, contract) {
						continue // the published channel
					}
					pass.Reportf(spec.Pos(),
						"[module-isolation] module %q imports %q — %s",
						owner, path,
						msgOr(r.Message,
							"a module may import another module only through its /"+contract+" package"))
				}
			}
			return nil, nil
		},
	}, true
}

// moduleOf returns the module a package path belongs to, e.g.
// ".../internal/modules/sales/domain" → "sales".
func moduleOf(pkgPath, root string) (string, bool) {
	if !strings.HasPrefix(pkgPath, root) {
		return "", false
	}
	rest := pkgPath[len(root):]
	if rest == "" {
		return "", false
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

// isContractPath reports whether path is module `mod`'s contract package or below it.
//
// The module ROOT is deliberately not accepted: allowing it would allow exactly what §3.2
// forbids, since a module's root package is where its service and aggregates live.
func isContractPath(path, root, mod, contract string) bool {
	prefix := root + mod + "/" + contract
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
