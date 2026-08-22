package bindings_test

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

// TestEveryWriteBindingHasAFrontEndCaller
//
// # The recurring defect this exists to end
//
// Five times in this project, something was built, tested, and never connected to a caller:
//
//   - 7.6  thirteen number series, and nothing that created them
//   - 10.1 supplier returns and landed costs, with no bindings
//   - 10.9 eleven screens, with no routes
//   - 10.10 product creation and stock counting, with no bindings and no forms
//   - the 10.12 audit six purchasing write bindings with no client function, so deliveries,
//     bills and supplier payments could be READ and not created
//
// Each was found by somebody going looking, and three of the five by a request to do something
// else. The pattern is not carelessness in one place: **connecting is a separate act from
// building, and nothing was checking the connection.**
//
// This checks it. A method that CHANGES something and that no frontend function calls is either
// unreachable — a feature the user cannot use — or dead. Both are worth failing a build over.
func TestEveryWriteBindingHasAFrontEndCaller(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}

	client, err := os.ReadFile(filepath.Join(root, "frontend/src/lib/wails/index.ts"))
	if err != nil {
		t.Fatalf("reading the frontend client: %v", err)
	}
	// The client calls a binding as call<T>("Facade", "Method", …). Both halves are captured so a
	// method name shared by two façades cannot satisfy the check for the wrong one.
	calls := regexp.MustCompile(`call<[^>]*>\(\s*"([A-Za-z]+)"\s*,\s*"([A-Za-z]+)"`)
	called := map[string]bool{}
	for _, match := range calls.FindAllStringSubmatch(string(client), -1) {
		called[match[1]+"."+match[2]] = true
	}
	if len(called) < 50 {
		t.Fatalf("only %d binding calls found in the client; the pattern is not matching",
			len(called))
	}

	// A method is a WRITE when its name begins with one of these. Deliberately a verb list rather
	// than "not a getter": a binding called `Status` reads, and one called `Apply` does not.
	writeVerbs := regexp.MustCompile(`^(Create|Add|Draft|Post|Place|Confirm|Apply|Adjust|Count|` +
		`Pay|Cancel|Close|Assign|Grant|Revoke|Set|Reset|Change|Dismiss|Restore|Prepare|Take|` +
		`Import|Open|Receive|Remove|Return|Settle|Record|Delete|Update|Deactivate|Activate)`)

	// Methods that legitimately have no frontend caller, each with the reason. A list that could
	// be added to without justification would let this check be silenced one line at a time.
	exempt := map[string]string{
		// The wizard calls this through its own typed helper rather than the generic client.
		"Setup.Apply": "called by the setup wizard's own client module",
	}

	var missing []string
	err = filepath.WalkDir(filepath.Join(root, "internal/api/bindings"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() ||
				!strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if parseErr != nil {
				return parseErr
			}

			for _, decl := range parsed.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				facade, ok := star.X.(*ast.Ident)
				if !ok || !writeVerbs.MatchString(fn.Name.Name) {
					continue
				}

				key := facade.Name + "." + fn.Name.Name
				if called[key] || exempt[key] != "" {
					continue
				}
				missing = append(missing, key+"  ("+filepath.Base(path)+")")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("walking the bindings: %v", err)
	}

	for _, method := range missing {
		t.Errorf("%s changes something and no frontend function calls it — the feature is "+
			"unreachable from the interface, or the binding is dead", method)
	}
}
