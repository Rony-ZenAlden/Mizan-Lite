# archlint — Mizan's extensible architecture-rule framework

Enforces project-wide architectural invariants that `golangci-lint` cannot express,
so the architecture is guaranteed by tooling instead of code review.

## Run

```bash
go run ./tools/archlint ./...          # from the module root (reads ./arch-rules.yml)
make arch                               # same, via the Makefile
```

Exit code is non-zero on any violation — it is a CI gate.

## Design

Rules are **data** (`arch-rules.yml`); the framework is **code**. The two are kept apart so
that tuning enforcement is a config edit and adding a new *kind* of rule is a small, local
code change — the framework is never redesigned.

```
main.go            → loads config, asks the engine to build enabled analyzers, runs multichecker
internal/config    → arch-rules.yml schema + loader (expands `self` → module path)
internal/match     → doublestar glob matching (* within a segment, ** across segments) + std detection
internal/engine    → Builder type + Build(): turns config into the set of enabled analyzers
internal/rules     → the rule kinds; AllBuilders is the registry
```

## Rule kinds shipped

| Kind | Enforces | Config block |
|---|---|---|
| `import-boundary` | Who may import whom (kernel purity, domain purity, no driver outside infra) | `import-boundary` |
| `forbid-call` | Forbidden calls scoped by package + file (`panic`, `os.Exit`, `fmt.Print*`, `time.Now`) | `forbid-call` |
| `no-float` | No `float32`/`float64` in money/quantity/domain packages | `no-float` |

Callees in `forbid-call` are resolved through **type information**, so a local variable named
`panic` or a shadowed `os` never produces a false positive.

## Adding a new rule kind

Three steps — and nothing else in the tool changes:

1. **Config** — add the rule's shape to `internal/config/config.go` (`Rules` struct + a typed
   sub-struct), and document it in `arch-rules.yml`.
2. **Builder** — add `internal/rules/<yourrule>.go` exposing
   `func YourRule(*config.Config) (*analysis.Analyzer, bool)`. Return `(nil, false)` when the
   rule is disabled or empty. Use `inspect.Analyzer` for AST walks and `match` for scoping.
3. **Register** — append `YourRule` to `AllBuilders` in `internal/rules/rules.go`.

That is the "extensible without redesigning the tooling" contract in full.

## Planned rule kinds (whole-program passes)

These need a package-graph pass over the whole module rather than a per-package analyzer, and
will be added behind an `-graph` mode without disturbing the per-package rules above:

- `module-isolation` — a module may import another module only through its `/contract` package.
- `max-dependency-depth` — cap the import-chain depth of any package.
- `domain-tests-required` — every `.../domain` package must ship a `_test.go`.
