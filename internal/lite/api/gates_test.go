package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

// boundTypes is what Wails actually binds: the dynamic types handed to it.
func boundTypes(t *testing.T) []reflect.Type {
	t.Helper()
	var out []reflect.Type
	for _, b := range api.New("v", litetest.Logger()).Bindings() {
		out = append(out, reflect.TypeOf(b))
	}
	return out
}

// TestEveryFacadeIsBound is gate G0.
//
// # The gap it closes
//
// Wails generates TypeScript only for structs in its Bind list. A façade written and left out of
// Bindings() generates nothing — so every gate that reads the generated files, and every gate that
// compares the client against them, passes while checking nothing. Mizan named this failure in
// 7.6 (D162): a gate whose input is missing passes while checking nothing.
//
// So the INPUT is checked here, from the source: every exported struct in this package that has an
// exported method is a façade, and must be bound.
func TestEveryFacadeIsBound(t *testing.T) {
	declared := facadesInSource(t)
	if len(declared) < 2 {
		t.Fatalf("found %d façades in the source; the parser is not matching", len(declared))
	}

	bound := map[string]bool{}
	for _, typ := range boundTypes(t) {
		if typ.Kind() != reflect.Pointer {
			// Wails binds the METHOD SET of what it is given; a value receiver passed by value would
			// expose only value-receiver methods, silently hiding the rest.
			t.Errorf("%v is bound by value; bind a pointer", typ)
			continue
		}
		bound[typ.Elem().Name()] = true
	}

	for _, name := range declared {
		if !bound[name] {
			t.Errorf("%s has exported methods but is not in Set.Bindings(): it would generate no "+
				"TypeScript, and every other gate would pass without seeing it", name)
		}
	}
	for name := range bound {
		if !contains(declared, name) {
			t.Errorf("%s is bound but has no exported methods in the source", name)
		}
	}
}

// TestTheLifecycleIsNeverReachableFromJavaScript: Set's own methods — Attach, Fail, Progress —
// decide whether the application is running. Binding Set would let any script in the webview mark
// a healthy application failed, or attach nothing.
func TestTheLifecycleIsNeverReachableFromJavaScript(t *testing.T) {
	for _, typ := range boundTypes(t) {
		if typ == reflect.TypeOf(&api.Set{}) {
			t.Fatal("Set is bound; its lifecycle methods would be callable from JavaScript")
		}
	}
}

// facadesInSource lists exported struct types, declared in this package's non-test files, that have
// at least one exported method. Set is excluded by name, and only Set: it is the container, and the
// test above holds it OUT of the bind list.
func facadesInSource(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	structs := map[string]bool{}
	withMethods := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					if _, isStruct := ts.Type.(*ast.StructType); isStruct {
						structs[ts.Name.Name] = true
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil || !d.Name.IsExported() || len(d.Recv.List) == 0 {
					continue
				}
				withMethods[receiverName(d.Recv.List[0].Type)] = true
			}
		}
	}
	var out []string
	for name := range structs {
		if withMethods[name] && name != "Set" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr: // a generic receiver
		return receiverName(e.X)
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestEveryBoundMethodHasAClientFunction is gate G2.
//
// For EVERY method Wails exposes — reads included, where Mizan's gate checks only methods named
// with a write verb — the frontend client must import the façade's generated module and reference
// the method. A bound method nothing references is either a feature nobody can reach or dead code;
// the brief names both as defects (design §7).
//
// The methods come from reflection over what is bound, not from the source, because reflection
// sees exactly the method set Wails does.
func TestEveryBoundMethodHasAClientFunction(t *testing.T) {
	clientPath := filepath.Join("..", "..", "..", "apps", "lite", "frontend", "src", "api", "client.ts")
	raw, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatalf("reading the frontend client %s: %v", clientPath, err)
	}
	client := string(raw)

	checked := 0
	for _, typ := range boundTypes(t) {
		facade := typ.Elem().Name()
		importLine := regexp.MustCompile(
			`import \* as ` + facade + ` from "[^"]*wailsjs/go/api/` + facade + `"`)
		if !importLine.MatchString(client) {
			t.Errorf("client.ts does not import the generated module for %s "+
				"(expected: import * as %s from \".../wailsjs/go/api/%s\")", facade, facade, facade)
		}
		for i := 0; i < typ.NumMethod(); i++ {
			method := typ.Method(i).Name
			checked++
			// Both halves are matched, so a method name shared by two façades cannot satisfy the
			// check for the wrong one.
			if !regexp.MustCompile(`\b` + facade + `\.` + method + `\b`).MatchString(client) {
				t.Errorf("%s.%s is bound and client.ts never references it — unreachable or dead",
					facade, method)
			}
		}
	}
	if checked < 4 {
		t.Fatalf("checked only %d methods; the reflection is not seeing the bound façades", checked)
	}
}
