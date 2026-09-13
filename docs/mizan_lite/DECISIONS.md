# Mizan Lite — decision register

Every decision that shapes Lite, in one list. The argument lives in the linked section; this register says
**what** was decided, **whether it was approved**, and **whether it still stands**.

Status: **approved** (owner approved it) · **made** (made during implementation within approved scope, recorded
for review) · **proposed** (awaiting approval) · **superseded** (replaced; the replacement is named).

---

## Edition design (approved 2026-09-13)

| ID | Decision | Status | Where |
|---|---|---|---|
| D1 | Second Wails app in the same Go module, reusing kernel + named platform packages (Option B) | approved | [DESIGN §2](DESIGN.md) |
| D2 | Scope: strip per §3.2; add receive, adjust, repay, void, opening balances | approved | [DESIGN §3](DESIGN.md) |
| D3 | Arabic default and RTL default; `name_ar` required, `name_en` optional | approved | [DESIGN §5, §8](DESIGN.md) |
| D4 | Unit cost in USD; profit in USD and local currency at the sale's own rate | approved | [DESIGN §1.2 C6, §4.6](DESIGN.md) |
| D5 | FX fetcher off by default, proposes only | **superseded** by Q1: no fetcher in v1 | [DESIGN §13.1](DESIGN.md) |
| D6 | Kernel additions `LineExtensionConverted`, `WeightedAverageUnit` | approved | [DESIGN §4.4, §6.6](DESIGN.md) |
| D7 | Copy Mizan's UI primitives | **superseded** by A11: build primitives when a screen needs them | [DESIGN §13.3](DESIGN.md) |
| D8 | Reuse `platform/jobs`, carrying its tables verbatim | approved | [DESIGN §6.7](DESIGN.md) |
| D9 | The frontend performs no money arithmetic; every figure from Go | approved | [DESIGN §6.4](DESIGN.md) |
| D10 | Lite's own data directory and database file | approved | [DESIGN §6.1](DESIGN.md) |
| C1–C9 | Corrections to the brief: Vite not Next.js; no typed namespace; no core in `cmd/`; FX proposes; receipts gated by a setting; profit reading; debt currency; SYP as data; no floats | approved | [DESIGN §1.2](DESIGN.md) |

## Owner's answers (2026-09-13)

| ID | Answer | Status | Where |
|---|---|---|---|
| Q1 | Manual exchange rate only in v1 | approved | [DESIGN §13.1](DESIGN.md) |
| Q2 | Debts in USD or SYP, chosen per sale | approved | [DESIGN §13.1](DESIGN.md) |
| Q3 | Expected = potential profit of stock on the shelf; actual = profit at the price charged | approved | [DESIGN §13.1](DESIGN.md) |
| Q4 | Sell beyond stock, with a visible warning | approved | [DESIGN §13.1](DESIGN.md) |
| Q5 | SYP cash rounding to the nearest paper denomination | approved; value confirmed in L4 | [DESIGN §13.1](DESIGN.md) |
| Q6 | Latin digits 0–9 in both languages | approved (confirmed by owner) | [DESIGN §13.1](DESIGN.md) |
| Q7 | Owner PIN for void, exchange-rate change, profit reports | approved; mechanism proposed in L1 | [phases/L1_CATALOGUE.md §7](phases/L1_CATALOGUE.md) |
| Q8 | Open packages and sell loose | approved; lands in L2 | [DESIGN §13.1](DESIGN.md) |
| Q9 | Fast-forward `main`; ignore the `demoseed` binary | approved; done | [DESIGN §13.1](DESIGN.md) |
| R1 | Windows and macOS equally | approved requirement | [DESIGN §13.2](DESIGN.md) |
| R2 | Comprehensive tests for every feature; nothing moves forward until all pass | approved requirement | [DESIGN §13.2](DESIGN.md) |
| R3 | All Lite documentation in `docs/mizan_lite/`, kept current | approved requirement | [README.md](README.md) |
| — | Upgrade golangci-lint to v2 (both editions) | approved; done | [phases/L0_SKELETON.md F2](phases/L0_SKELETON.md) |

## Amendments made while building L0

| ID | Decision | Status | Where |
|---|---|---|---|
| A1 | Own data-directory resolver; Local AppData on Windows | made | [DESIGN §13.3](DESIGN.md) |
| A2 | Catalogs in `internal/lite/locales/` | made | [DESIGN §13.3](DESIGN.md) |
| A3 | One migration per phase | made | [DESIGN §13.3](DESIGN.md) |
| A4 | `0001` carries `jobs`/`job_runs` verbatim; column `setting_key` | made | [DESIGN §13.3](DESIGN.md) |
| A5 | A setting is declared only when something reads it | made | [DESIGN §13.3](DESIGN.md) |
| A6 | "DB mock tests" are fakes held honest by a shared contract suite | made | [DESIGN §13.3](DESIGN.md) |
| A7 | G1 forbids the `go`/`runtime` property, not the `window` object | made | [DESIGN §13.3](DESIGN.md) |
| A8 | Language applied before first paint, server-side (`PeekLocale` + middleware) | made | [DESIGN §13.3](DESIGN.md) |
| A9 | macOS 13 minimum | made | [DESIGN §13.3](DESIGN.md) |
| A10 | The `pos`-only import rule deferred to L1 | made | [DESIGN §13.3](DESIGN.md) |
| A11 | Primitives built when needed | made | [DESIGN §13.3](DESIGN.md) |
| D-L0.1–7 | L0 implementation decisions (first paint, migrations, fakes, console gate, polling, native frame, primitives) | made | [phases/L0_SKELETON.md §3](phases/L0_SKELETON.md) |

## L1 — approved 2026-09-13

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L1.1 | One migration `0002_catalogue_owner.sql`; currencies and units seeded by it | approved | [L1 §3](phases/L1_CATALOGUE.md) |
| D-L1.2 | `name_key` UNIQUE across active and inactive products | approved | [L1 §3, §5](phases/L1_CATALOGUE.md) |
| D-L1.3 | Arabic normalisation, Go only; definite article kept | approved | [L1 §5](phases/L1_CATALOGUE.md) |
| D-L1.4 | Number normalisation in Go and TypeScript, bound by one shared fixture | approved | [L1 §6](phases/L1_CATALOGUE.md) |
| D-L1.5 | Price input decimals = currency decimals; zero allowed | approved | [L1 §4.2](phases/L1_CATALOGUE.md) |
| D-L1.6 | PIN hashed with `platform/crypto`; it joins the edition boundary | approved | [L1 §7.2](phases/L1_CATALOGUE.md) |
| D-L1.7 | Persisted lockout, 30 s doubling to 15 min, never permanent | approved | [L1 §7.3](phases/L1_CATALOGUE.md) |
| D-L1.8 | Elevation in Go memory, fixed window, Lock button, countdown | approved | [L1 §7.4](phases/L1_CATALOGUE.md) |
| D-L1.9 | The guard is a port; records the act in the caller's transaction | approved | [L1 §7.6](phases/L1_CATALOGUE.md) |
| D-L1.10 | `lite.owner.required` → PIN dialog → retry once | approved | [L1 §7.6](phases/L1_CATALOGUE.md) |
| D-L1.11 | First run in one transaction; status read from the data | approved | [L1 §8](phases/L1_CATALOGUE.md) |
| D-L1.12 | Seeder drives services, completes first run, refuses a set-up installation | approved | [L1 §9](phases/L1_CATALOGUE.md) |
| D-L1.13 | The PIN defends the counter, not the file — stated plainly | approved | [L1 §7.1](phases/L1_CATALOGUE.md) |
| Q-L1.1 | PIN guards price change and deactivation; no delete exists | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.2 | PIN 6–12 digits | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.3 | Owner mode 2 minutes from entry, plus Lock | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.4 | One-time recovery code at setup | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.5 | Nine units: kg, litre, piece, jar, container, tin, bag, bottle, box | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.6 | 24 quick-grid buttons | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |
| Q-L1.7 | Shop name required at first run | approved | [L1 §13.3](phases/L1_CATALOGUE.md) |

### L1 — decisions made while building (approved with the phase)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L1.i1 | `textkey`/`numinput` allowed in domains; `lite-pure-text` keeps them pure | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i2 | `setup` is an orchestrator, through two ports | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i3 | A failed PIN attempt commits its failure, then refuses | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i4 | Owner mode switched on only after the attempt commits | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i5 | Tests read the expected schema version from the migration set | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i6 | Cheap test hashing via `PINHasher`; password strength in production | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i7 | Product form reads fresh; sends only the acts that change something | approved | [L1 §15](phases/L1_CATALOGUE.md) |
| D-L1.i8 | `cmd/` excluded from golangci's `fmt.Print` rule | approved | [L1 §15](phases/L1_CATALOGUE.md) |
