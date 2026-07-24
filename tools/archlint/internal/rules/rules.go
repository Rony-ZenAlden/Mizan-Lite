// Package rules implements the architecture rule kinds.
//
// To add a project-wide rule:
//  1. add its config shape to config.Rules,
//  2. write a Builder in this package (see importboundary.go for the pattern),
//  3. append it to AllBuilders below.
//
// The framework (engine, config, match, main) needs no other change. This is the
// "extensible without redesigning the tooling" contract.
package rules

import (
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/mizan-erp/mizan/tools/archlint/internal/engine"
)

// AllBuilders is the registry of every architecture rule. Order is irrelevant;
// each builder self-reports whether it is enabled.
var AllBuilders = []engine.Builder{
	ImportBoundary, // layer & module import boundaries
	ForbidCall,     // forbidden calls scoped by location (panic, os.Exit, fmt.*, time.Now)
	NoFloat,        // no float32/float64 in money/domain
	// Planned whole-program rules (see arch-rules.yml): module-isolation,
	// max-dependency-depth, domain-tests-required.
}

// msgOr returns msg if non-empty, otherwise fallback.
func msgOr(msg, fallback string) string {
	if msg != "" {
		return msg
	}
	return fallback
}

// skipPos reports whether the file at pos should be exempt from architecture rules.
// Architecture rules govern production source: test files and machine-generated
// files (the go-test main, dependency sources in the module cache) are not the
// project's architecture and are skipped.
func skipPos(pass *analysis.Pass, pos token.Pos) bool {
	f := pass.Fset.Position(pos).Filename
	return strings.HasSuffix(f, "_test.go") ||
		strings.Contains(f, "/go-build/") ||
		strings.Contains(f, "/pkg/mod/")
}
