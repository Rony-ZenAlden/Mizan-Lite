// Command archlint enforces Mizan's project-wide architecture rules.
//
// Rules are data-driven (arch-rules.yml). Run it like go vet:
//
//	go run ./tools/archlint ./...
//	ARCHLINT_CONFIG=arch-rules.yml go run ./tools/archlint ./...
//
// It exits non-zero if any rule is violated, so it plugs straight into CI.
package main

import (
	"fmt"
	"os"

	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/mizan-erp/mizan/tools/archlint/internal/config"
	"github.com/mizan-erp/mizan/tools/archlint/internal/engine"
	"github.com/mizan-erp/mizan/tools/archlint/internal/rules"
)

func main() {
	cfg, err := config.Load(os.Getenv("ARCHLINT_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	analyzers := engine.Build(cfg, rules.AllBuilders)
	if len(analyzers) == 0 {
		fmt.Fprintln(os.Stderr, "archlint: no rules enabled in config")
		os.Exit(2)
	}
	multichecker.Main(analyzers...)
}
