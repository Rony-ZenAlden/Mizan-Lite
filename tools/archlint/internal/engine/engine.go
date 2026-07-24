// Package engine assembles the enabled architecture analyzers from config.
//
// The extensibility contract lives here: a Builder turns config into an optional
// analyzer. Adding a project-wide rule means writing one Builder and listing it in
// rules.AllBuilders — nothing in this package changes.
package engine

import (
	"golang.org/x/tools/go/analysis"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
)

// Builder constructs an analyzer for a rule kind. It returns (nil, false) when the
// rule is disabled or has no configured entries.
type Builder func(*config.Config) (*analysis.Analyzer, bool)

// Build returns every enabled analyzer.
func Build(cfg *config.Config, builders []Builder) []*analysis.Analyzer {
	out := make([]*analysis.Analyzer, 0, len(builders))
	for _, b := range builders {
		if a, ok := b(cfg); ok {
			out = append(out, a)
		}
	}
	return out
}
