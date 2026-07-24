# Mizan ERP

> *Mizan… Where Precision Meets Simplicity.*

An offline-first, commercial-grade desktop ERP/POS platform. Generic by design — adaptable to
many business types and countries through **configuration and metadata, not code changes**.

- **Backend:** Go (Clean Architecture, modular monolith)
- **Desktop shell:** Wails
- **Frontend:** React + Vite + TypeScript + Tailwind
- **Database:** SQLite (portable schema; PostgreSQL/MySQL/SQL Server ready)

## Status

**Phase 0 — Foundation (application kernel).** No business features yet; building the
reusable kernel that future modules plug into. See [`docs/architecture/`](docs/architecture/):

| Document | Purpose |
|---|---|
| `ARCHITECTURE_v1.md` | System architecture (all 23 areas + tax/accounting/inventory) |
| `ADDENDUM_v1.1_resolved_design.md` | Variants, UoM, country profiles, costing, currency |
| `PHASE_0_FOUNDATION.md` | The current phase's detailed design |

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | pure-Go build (no cgo) |
| Node | 22+ | frontend |
| Wails CLI | v2 latest | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |

`sqlc` and other build tools are invoked via `go run` (pinned in `go.mod`) — no global install.

## Common tasks

```bash
make lint        # golangci-lint + the Mizan architecture-rule framework
make arch        # architecture rules only (tools/archlint)
make test        # go test ./... with race + coverage
make dev         # wails dev (hot-reload desktop app)
make build       # production desktop binary
make check       # everything CI runs: lint + arch + test + typecheck
```

## Architecture enforcement

Architectural invariants are enforced by tooling, not code review. Standard rules run through
`golangci-lint`; Mizan-specific rules (layer boundaries, no-float-in-money, forbidden calls by
location) run through an **extensible rule framework** in [`tools/archlint/`](tools/archlint/).
Adding a project-wide rule is one new file plus one config block — see
[`tools/archlint/README.md`](tools/archlint/README.md).
