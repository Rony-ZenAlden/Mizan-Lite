package imports_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTheImporterContainsNoSQL
//
// # DoD criterion 8, checked structurally
//
// The whole design rests on one sentence: **an import calls the same service a screen calls and
// has no SQL of its own.** A comment saying so does not stop the next person writing an INSERT
// when an import is slow and a deadline is close.
//
// This reads the package and refuses any statement keyword, any database import, and any
// reference to a query method. Blunt, and blunt is what makes it survive.
//
// The value it protects is concrete: `CreateProduct` refuses a duplicate code, resolves the unit,
// creates the default variant and writes the audit entry, inside one transaction. A row inserted
// around it gets none of that, and it is indistinguishable from a good one until somebody tries to
// sell it.
func TestTheImporterContainsNoSQL(t *testing.T) {
	// # Why this reads STRING LITERALS and not the file's bytes
	//
	// The first version matched the whole source, and failed on this package's own doc comment —
	// which says an importer must not "`INSERT` the rows, done". The comment is the argument for
	// the rule; failing on it would have meant deleting the explanation to satisfy the check.
	//
	// SQL has to be in a string to be executed, so a literal is where it can hide and the only
	// place worth looking. Still blunt: any literal containing a statement keyword fails.
	statements := regexp.MustCompile(`\b(INSERT|UPDATE|DELETE|SELECT)\b`)

	forbidden := []string{
		"database/sql",
		"platform/database",
		"infra/sqlite",
		"ExecContext",
		"QueryContext",
		"QueryRowContext",
	}

	var scanned int
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			for _, match := range statements.FindAllString(literal.Value, -1) {
				t.Errorf("%s has a string literal containing %q — an import that writes its own "+
					"rows skips every invariant the service keeps, and the rows are "+
					"indistinguishable from good ones until somebody tries to use them",
					path, match)
			}
			return true
		})
		for _, name := range forbidden {
			if strings.Contains(string(source), name) {
				t.Errorf("%s references %q, which is a way to reach the database directly",
					path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the package: %v", err)
	}

	// The walk found the package. A path that matched nothing would pass while checking nothing.
	if scanned == 0 {
		t.Fatal("no source files were scanned, so this check proves nothing")
	}
}
