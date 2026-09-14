package rules

import (
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
	"github.com/mizan-erp/mizan/tools/archlint/internal/match"
)

// ImportBoundary enforces which packages may import which other packages, per the
// `import-boundary` block in arch-rules.yml. This is how kernel purity, domain
// purity, and "no driver outside infra" are enforced by tooling.
func ImportBoundary(cfg *config.Config) (*analysis.Analyzer, bool) {
	r := cfg.Rules.ImportBoundary
	if !r.Enabled || len(r.Boundaries) == 0 {
		return nil, false
	}
	return &analysis.Analyzer{
		Name: "importboundary",
		Doc:  "enforces layer/module import boundaries declared in arch-rules.yml",
		Run: func(pass *analysis.Pass) (any, error) {
			pkgPath := pass.Pkg.Path()
			for _, b := range r.Boundaries {
				if !match.Any(b.AppliesTo, pkgPath) || match.Any(b.Except, pkgPath) {
					continue
				}
				for _, f := range pass.Files {
					for _, spec := range f.Imports {
						if skipPos(pass, spec.Pos()) {
							continue
						}
						ip, err := strconv.Unquote(spec.Path.Value)
						if err != nil {
							continue
						}
						if violates(b, ip) {
							pass.Reportf(spec.Pos(), "[import-boundary/%s] %s (imports %q)",
								b.Name, msgOr(b.Message, "import not permitted here"), ip)
						}
					}
				}
			}
			return nil, nil
		},
	}, true
}

func violates(b config.Boundary, importPath string) bool {
	// allow_only: the import must match at least one allowed pattern.
	if len(b.AllowOnly) > 0 && !importAllowed(b.AllowOnly, importPath) {
		return true
	}
	// forbid: the import must not match any forbidden pattern.
	if len(b.Forbid) > 0 && importForbidden(b.Forbid, importPath) {
		return true
	}
	return false
}

func importAllowed(patterns []string, importPath string) bool {
	for _, p := range patterns {
		if p == "std" {
			if match.IsStd(importPath) {
				return true
			}
			continue
		}
		if match.Glob(p, importPath) {
			return true
		}
	}
	return false
}

func importForbidden(patterns []string, importPath string) bool {
	for _, p := range patterns {
		if p == "std" {
			if match.IsStd(importPath) {
				return true
			}
			continue
		}
		if match.Glob(p, importPath) {
			return true
		}
	}
	return false
}
