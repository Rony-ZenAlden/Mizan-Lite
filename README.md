# Mizan

> *Mizan… Where Precision Meets Simplicity.*

An offline-first, commercial-grade desktop ERP/POS platform. Generic by design — adaptable to
many business types and countries through **configuration and metadata, not code changes**.

- **Backend:** Go (Clean Architecture, modular monolith)
- **Desktop shell:** Wails
- **Frontend:** React + Vite + TypeScript + Tailwind
- **Database:** SQLite (portable schema; PostgreSQL/MySQL/SQL Server ready)

## Status

**v1.0.0 — released.** Phases 0–10 built. The application keeps double-entry books, runs a till, manages stock,
purchasing and expenses, reports on all of it, and looks after itself.

| Phase | What it built |
|-------|---------------|
| 0 | The kernel: numerics, database, migrations, config, events, jobs, i18n, currency, composition root, frontend foundation |
| 1 | Core data: organisation, identity, RBAC, audit, profiles |
| 2 | The financial spine: chart of accounts, posting rules, the ledger, tax |
| 3 | Master data: units, categories, products, variants, partners, price lists |
| 4 | Inventory: movements, costing, lots and serials, counts, valuation |
| 5 | Sales and POS: quotations to invoices, returns, payments, shifts, printing |
| 6 | Purchasing: orders, receipts, bills, landed costs, returns, payments |
| 7 | Money out: expenses, settlements, debts, partner balances |
| 8 | Insight: financial statements, sales and spend analysis, valuation, search, dashboard |
| 9 | Operations: verified backup and restore, CSV import and export, notifications |
| 10 | Polish: performance bounds, RTL and accessibility gates, **built installers**, this document |

### Installers

Both are built and validated. `dist/` holds:

| Artefact | What it is |
|----------|------------|
| `Mizan <version>.dmg` | macOS disk image, drag-to-Applications, **Universal** (`x86_64` + `arm64`) |
| `Mizan <version> Setup.exe` | Windows NSIS installer, cross-built from macOS |
| `Mizan <version>.exe` | the bare Windows binary, for a portable install |

```bash
make package-macos            # .app → .dmg
./scripts/package-windows.sh  # .exe → NSIS Setup.exe
make release                  # ci + both, everything shippable
```

**[docs/RELEASE.md](docs/RELEASE.md)** covers what is verified, what is not, and the two steps that
need a certificate. The short version: the macOS artefact has been **first-run on real hardware**
against a clean data directory — 37 migrations, 13 number series, zero errors — and the Windows
installer has **never been executed**, because no Windows machine or VM exists in the build
environment.

The Windows cross-build works because Step 0.3 chose a pure-Go SQLite driver over the cgo one —
a decision made for the offline constraint that turned out to make a Windows release buildable
from a Mac.

### Known gaps

Recorded rather than implied, and set out in full with recommended next steps in
**[docs/architecture/KNOWN_GAPS.md](docs/architecture/KNOWN_GAPS.md)**:

- **Both installers are unsigned.** The hooks are tested end to end and activate on an identity
  being set — but a certificate is tied to a legal identity and a payment, so it cannot be
  supplied here. **Signing alone is also not enough on macOS:** an ad-hoc-signed image verifies
  and Gatekeeper still rejects it, because notarization is a separate step. See
  [docs/RELEASE.md §4](docs/RELEASE.md).
- **The Windows installer has never been run.** Structure and metadata are verified; first-run
  behaviour on a clean Windows machine is not. [docs/RELEASE.md §3](docs/RELEASE.md) is the
  checklist.
- **Nobody has used this.** Every guarantee is proven by a test; no shopkeeper has opened it.
- **`price_list_items.variant_id` is nullable**, against §1.2 — narrowed by a CHECK constraint
  that makes the feared state unrepresentable, and read by exactly one function.
- **Phase 6's criterion 13 is permanently false.** One of Phase 4's four seams needed reshaping;
  the code is correct and the record stands.
- **The performance ceiling catches structural regressions, not drift.** A report that goes from
  46ms to five seconds still passes. The elapsed times are logged so a person can see it climbing.
- **Accounting is exposed read-only** (§20.6 tier v1.1). Manual journal entries arrive with the
  release that introduces them.

## For the person using it

- **[docs/guide/GETTING_STARTED.md](docs/guide/GETTING_STARTED.md)** — installing, first-run
  setup, backups, and what to do when something looks wrong.
- **[docs/guide/GETTING_STARTED.ar.md](docs/guide/GETTING_STARTED.ar.md)** — the same, in Arabic.

A user guide and a maintainer guide are different artefacts, and conflating them serves neither.
Everything below this line is for whoever maintains the code.

## Reading this repository

Thirty-seven design documents in `docs/architecture/`, in the order they are worth reading:

1. **[ARCHITECTURE_v1.md](docs/architecture/ARCHITECTURE_v1.md)** — the system, all 23 areas.
   Everything else assumes it.
2. **[ADDENDUM_v1.1_resolved_design.md](docs/architecture/ADDENDUM_v1.1_resolved_design.md)** —
   variants, units, country profiles, costing, currency. The decisions the architecture deferred.
3. **[PHASE_0_FOUNDATION.md](docs/architecture/PHASE_0_FOUNDATION.md)** — the kernel, and the
   `STEP_0_*.md` documents beneath it. Read these before any module: they explain why money is an
   integer, why migrations back themselves up, and why the composition root looks as it does.
4. **The phase documents in order**, `PHASE_1` through `PHASE_10`. Each records its analysis, its
   design decisions with reasons, what it deliberately did NOT do, and a Definition-of-Done review
   that says plainly where a criterion was not met.

**Read a phase's DoD review first if you are short of time.** It is the most honest section: it
lists what was proven, by which test, and what was found wrong while proving it.

### The rules this codebase accumulated

Written where they were learned, and worth knowing before changing anything:

- *Before declaring a new place for a fact to live, look for the one an earlier phase already
  left.* (4.4, and every phase since)
- *A seam is only proven by a caller.* (Phase 6 — and 7.6, where thirteen number series existed
  that nothing ever created)
- *When a test needs a helper that imitates a production mechanism, that mechanism is untested.*
  (5.4)
- *A scale conversion tested only at scale 1 is a conversion nobody has tested.* (6.6)
- *A test whose fixture cannot distinguish the right answer from the wrong one is not evidence.*
  (Phase 8, five times)
- *A detector tested only where it should stay silent is a detector nobody has heard.* (8.1)
- *A check nobody can make fail is a claim nobody has verified.* (9.5)
- *A path that cannot be tested is a path that has never run.* (9.7)

### Mutation drills

Every guarantee in this codebase is watched to fail. A drill changes production code so a test
SHOULD break; a drill that passes is a defect in the test, the code, or the mutation, and each is
recorded in its phase document with which of five resolutions it took.

255 drills across ten phases. Roughly one in seven passed, and every one produced a change.

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | pure-Go build (no cgo) |
| Node | 22+ | frontend |
| Wails CLI | v2 latest | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |

`sqlc` and other build tools are invoked via `go run` (pinned in `go.mod`) — no global install.

## Common tasks

```bash
make ci          # full LOCAL, OFFLINE CI: Go core + architecture + frontend
make lint        # golangci-lint + the Mizan architecture-rule framework
make arch        # architecture rules only (tools/archlint)
make test        # go test with race + coverage (core packages)
make dev         # wails dev (hot-reload desktop app)
make build       # production desktop binary
./scripts/package-windows.sh   # NSIS installer (.exe), cross-built from macOS
./scripts/package-macos.sh     # disk image (.dmg), Universal
make vendor      # vendor Go deps for fully offline, self-contained builds
```

## Local & offline by design

Mizan builds, tests, and runs **entirely on your machine**. There is no dependency on
GitHub, remote CI, or any cloud service.

- **CI is local:** `scripts/check.sh` (`make ci`) runs every check offline. Once Go modules
  and `frontend/node_modules` are fetched once, no internet connection is needed.
- **No auto-push, no remote:** version control is local Git only. No remote is configured.
- **GitHub is optional and inert:** an opt-in Actions template lives in
  [ci/optional-github-actions/](ci/optional-github-actions/); it does nothing unless you
  deliberately enable it in the future. Nothing in the project requires it.
- **Fully self-contained builds:** `make vendor` vendors Go dependencies so builds need no
  downloads at all.

## Architecture enforcement

Architectural invariants are enforced by tooling, not code review. Standard rules run through
`golangci-lint`; Mizan-specific rules (layer boundaries, no-float-in-money, forbidden calls by
location) run through an **extensible rule framework** in [`tools/archlint/`](tools/archlint/).
Adding a project-wide rule is one new file plus one config block — see
[`tools/archlint/README.md`](tools/archlint/README.md).
