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
| Q5 | SYP cash rounding to the nearest paper denomination | approved; value confirmed in L4: 500 pounds, nearest (Q-L4.1) | [DESIGN §13.1](DESIGN.md) |
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

## L2 — approved 2026-09-13

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L2.1 | Stock owns `stock_levels` and `stock_ledger`; catalogue owns `product_packages`; no stock columns on `products` (amends DESIGN §5) | approved | [L2 §2.3](phases/L2_STOCK.md) |
| D-L2.2 | Every ledger row records before and after; the chain is verifiable (amends DESIGN §5) | approved | [L2 §2.3](phases/L2_STOCK.md) |
| D-L2.3 | Sale kinds and `sale_id` arrive with the till, through a planned rebuild in L4 | approved | [L2 §2.3](phases/L2_STOCK.md) |
| D-L2.4 | Costing rules: receipts average in; counts, adjustments, issues never move the average; on hand ≤ 0 takes the receipt cost | approved | [L2 §3.2](phases/L2_STOCK.md) |
| D-L2.5 | Values through `money.LineExtension`; value tests at 0, 2, 3 decimals | approved | [L2 §3.4](phases/L2_STOCK.md) |
| D-L2.6 | Kernel: `SplitUnitCost`, `UnitCostFromTotal`, `DivideByRate` beside `WeightedAverageUnit` | approved | [L2 §3.6](phases/L2_STOCK.md) |
| D-L2.7 | Opening packages conserve value to within one cent, shown in history | approved | [L2 §3.5](phases/L2_STOCK.md) |
| D-L2.8 | Business dates from `time.Local`; no `LoadLocation` | approved | [L2 §3.7](phases/L2_STOCK.md) |
| D-L2.9 | Opening stock only as the first movement | approved | [L2 §5.2](phases/L2_STOCK.md) |
| D-L2.10 | A count takes the counted quantity | approved | [L2 §5.3](phases/L2_STOCK.md) |
| D-L2.11 | No raising stock that has no cost | approved | [L2 §3.2](phases/L2_STOCK.md) |
| D-L2.12 | No receiving into an inactive product | approved | [L2 §5.1](phases/L2_STOCK.md) |
| D-L2.13 | `owner.Allowed` for guarded reads, unrecorded | approved | [L2 §6](phases/L2_STOCK.md) |
| D-L2.14 | The verifier reports, never repairs; the seeder fails on a finding | approved | [L2 §7](phases/L2_STOCK.md) |
| Q-L2.1 | Rate typed on a pound receipt; pre-filled from L3 | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.2 | No negative stock outside the till | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.3 | Owner PIN for anything that lowers stock | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.4 | Cost and value in owner mode only | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.5 | Receipt reversal and owner cost correction | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.6 | Reasons: count, damaged, expired, own use, **gift or sample**, other | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.7 | Total or unit cost, total by default | approved | [L2 §12.3](phases/L2_STOCK.md) |
| Q-L2.8 | Single-level packages | approved | [L2 §12.3](phases/L2_STOCK.md) |

### L2 — decisions made while building (approved 2026-09-14)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L2.i1 | The ledger is ordered by a per-product `seq` under `UNIQUE (product_id, seq)`, not by `occurred_at`; `last_movement_id` gains its foreign key | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i2 | No count or adjustment on a product that never moved; no raise while on hand ≤ 0 and the average is 0 | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i3 | `ProductDTO` carries the package link as two flat strings, not a nullable object | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i4 | A stock act replies with quantities only, never the movement's costs | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i5 | Costs are cleared, and the reversible receipt chosen, in the stock service | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i6 | `Stock.Verify` is owner-guarded; `VerifyUnguarded` for tests | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i7 | An adjustment is an unsigned quantity and a direction | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i8 | An inactive product may be counted and written off, not received; no opening into inactive content | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i9 | A movement's time is held to the millisecond, the ledger's precision | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i10 | The shared number fixture gains `quantities`, read by Go and Vitest | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i11 | The error-code coverage gate reads `Finding*` constants too | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i12 | `bizdate` joins `lite-pure-text` | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i13 | The product form's package section saves on its own | approved | [L2 §14](phases/L2_STOCK.md) |
| D-L2.i14 | Guarded stock acts record quantities (and averages) before → after | approved | [L2 §14](phases/L2_STOCK.md) |

## L3 — approved 2026-09-14, with the owner's dual-mode requirement

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L3.1 | `fx` owns `fx_rates`; insert-only; local per USD at 10⁻⁹; inverse never stored | approved | [L3 §6](phases/L3_RATES.md) |
| D-L3.2 | Rate in force = newest place (`seq`), not latest timestamp; no scheduled or back-dated rates (amends DESIGN §4.3, §5) | approved | [L3 §2.3](phases/L3_RATES.md) |
| D-L3.3 | No `source`, `fetch_log_id` or `fx_fetch_log` in v1 (Q1) | **superseded** at approval by the dual-mode requirement (D-L3.15–26) | [L3 §2.3](phases/L3_RATES.md) |
| D-L3.4 | Kernel `LineExtensionMulRate`, `LineExtensionDivRate`, `Money.DivRate` replace D6's `LineExtensionConverted` | approved | [L3 §5](phases/L3_RATES.md) |
| D-L3.5 | A rate: positive, at most four decimals | approved | [L3 §4.1](phases/L3_RATES.md) |
| D-L3.6 | A change beyond the threshold refused in Go unless confirmed | approved | [L3 §4.2](phases/L3_RATES.md) |
| D-L3.7 | Every read carries age and staleness; stale warns, never refuses | approved | [L3 §4.3](phases/L3_RATES.md) |
| D-L3.8 | "No rate" is a state; nothing converts at 1 | approved | [L3 §4.4](phases/L3_RATES.md) |
| D-L3.9 | Setting a rate needs the owner PIN, recorded in its transaction; the same figure may be recorded again | approved | [L3 §8](phases/L3_RATES.md) |
| D-L3.10 | `currency.local` declared (default `SYP`), not editable on screen in v1 | approved | [L3 §3.2](phases/L3_RATES.md) |
| D-L3.11 | Conversions only as Go-computed strings; stock value in pounds summed per line | approved | [L3 §5.4](phases/L3_RATES.md) |
| D-L3.12 | Receipt form pre-fills the rate in force; the receipt keeps the rate typed on it | approved | [L3 §3.3](phases/L3_RATES.md) |
| D-L3.13 | Everyone sees the rate and its age in the header | approved | [L3 §10](phases/L3_RATES.md) |
| D-L3.14 | `lite-fx-isolated`; every other isolation rule forbids `fx`; setup reaches it through a port | approved | [L3 §11.1](phases/L3_RATES.md) |
| Q-L3.1 | Out of date when not updated today; prompt internet refresh or owner re-confirmation | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.2 | 20%; a fetched or typed shift beyond it needs the owner PIN and confirmation | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.3 | Rate required at first run, fetched or typed | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.4 | Code SYP; market thousands, ~15,000 per USD | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.5 | Prices in both currencies on Products | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.6 | Stock value in SYP beside USD in owner mode | approved | [L3 §14.2](phases/L3_RATES.md) |
| Q-L3.7 | A rate takes effect immediately, fetched or typed | approved | [L3 §14.2](phases/L3_RATES.md) |
| R-L3 | **Owner's requirement:** dual rate modes — automatic internet fetch (primary), manual owner rate with PIN (fallback, override) | approved | [L3 §14.1](phases/L3_RATES.md) |
| D-L3.15 | Modes `automatic` / `manual` in `fx.mode`; switching needs the owner PIN — **manual is the default** (owner, at commit) | approved | [L3 §14.4](phases/L3_RATES.md) |
| D-L3.16 | The fetch decision (manual mode → held; no rate → proposal; manual today → held; >20% → proposal; same today → unchanged; else applied) | approved | [L3 §14.5](phases/L3_RATES.md) |
| D-L3.17 | The first rate is always a person's | approved | [L3 §14.5](phases/L3_RATES.md) |
| D-L3.18 | A manual rate holds for the rest of its business day in automatic mode | approved | [L3 §14.3](phases/L3_RATES.md) |
| D-L3.19 | A fetch beyond 20% is a proposal, accepted with the PIN only while newest and still compared against the rate in force | approved | [L3 §14.5](phases/L3_RATES.md) |
| D-L3.20 | Every attempt logged in `fx_fetches` with a code; fetched rates name their fetch | approved | [L3 §14.7](phases/L3_RATES.md) |
| D-L3.21 | The network call never runs inside a transaction | approved | [L3 §14.5](phases/L3_RATES.md) |
| D-L3.22 | Three providers in order, explicitly scaled; exact decimal parsing; fetched rates rounded to 4 decimals | approved | [L3 §14.6](phases/L3_RATES.md) |
| D-L3.23 | `net/http` only in `fx/infra/httpsource`; no test reaches the internet; one opt-in live test | approved | [L3 §14.6](phases/L3_RATES.md) |
| D-L3.24 | Refresh open to everyone, one attempt a minute; accepting a proposal and switching mode are the owner's | approved | [L3 §14.8](phases/L3_RATES.md) |
| D-L3.25 | First run can fetch a quote into the rate field; the owner confirms it | approved | [L3 §14.2](phases/L3_RATES.md) |
| D-L3.26 | Providers registered by `apps/lite` only; seeder and tests offline by construction | approved | [L3 §14.6](phases/L3_RATES.md) |
| D-L3.i1 | archlint import boundaries gain an `except` list (shared tool) | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i2 | The last fetch's age is measured in Go | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i3 | `Refresh` split: last attempt → network → one-transaction record | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i4 | First run reads the local currency from Go | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i5 | A typed rate is read back without trailing zeros | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i6 | Provider names are catalog strings; ExchangeRate-API attributed | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i7 | An offline or unchanged fetch is not a failed job | approved | [L3 §16](phases/L3_RATES.md) |
| D-L3.i8 | Update now disabled, with a reason, when no provider exists | approved | [L3 §16](phases/L3_RATES.md) |

## L4 — approved 2026-09-14, with the owner's answers (discounts with the PIN)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L4.1 | A `sales` module owns `sales` and `sale_lines`, reaching other modules through ports | approved | [L4 §9](phases/L4_TILL.md) |
| D-L4.2 | Lines priced in both currencies from one exact product; printed lines add up exactly | approved | [L4 §3.1](phases/L4_TILL.md) |
| D-L4.3 | Cash rounding to a note, half up, recorded; dollars not cash-rounded; `total = lines + rounding` CHECK | approved | [L4 §3.2](phases/L4_TILL.md) |
| D-L4.4 | Tender in either currency; change in pounds unless total and tender are both dollars; change rounded to the note | approved | [L4 §3.3](phases/L4_TILL.md) |
| D-L4.5 | A quote token; checkout refuses a stale quote, writing nothing | approved | [L4 §4](phases/L4_TILL.md) |
| D-L4.6 | No rate, no sale | approved | [L4 §4](phases/L4_TILL.md) |
| D-L4.7 | Selling beyond stock with a warning; never-received products sell with `cost_known = 0` | approved | [L4 §5](phases/L4_TILL.md) |
| D-L4.8 | Cost snapshotted per line at the sale's rate; voids return stock at that cost | approved | [L4 §5](phases/L4_TILL.md) |
| D-L4.9 | Ledger rebuild in dependency order, `stock_levels` rebuilt too (amends A-L2.3's plan) | approved | [L4 §6.2](phases/L4_TILL.md) |
| D-L4.10 | `payment` allows `credit` now, refused until L5; the customer lives on L5's debt entry | approved | [L4 §2.4](phases/L4_TILL.md) |
| D-L4.11 | One price per line (Q3 reading b) | approved | [L4 §2.4](phases/L4_TILL.md) |
| D-L4.12 | One continuous receipt sequence in the checkout transaction; voids keep numbers | approved | [L4 §7.1](phases/L4_TILL.md) |
| D-L4.13 | The receipt is the stored, fully snapshotted sale | approved | [L4 §7.2](phases/L4_TILL.md) |
| D-L4.14 | Whole-sale void with PIN and reason, recorded on its own day | approved | [L4 §8.1](phases/L4_TILL.md) |
| D-L4.15 | A sales verifier that reports, never repairs; the seeder fails on a finding | approved | [L4 §8.3](phases/L4_TILL.md) |
| D-L4.16 | `lite-sales-isolated`; other isolation rules forbid `sales` | approved | [L4 §9.2](phases/L4_TILL.md) |
| Q-L4.1 | 500 pounds, rounded to the nearest 500 (half up); changeable with the PIN | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.2 | Change in pounds unless the customer asks for dollars | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.3 | Whole sales in dollars allowed; change strictly in dollars when paid in dollars | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.4 | Any past sale may be voided, with the PIN and a required reason | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.5 | No partial returns in v1 — void and re-ring | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.6 | **Item and total discounts, only with the owner PIN** (changed from the recommendation) | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.7 | Receipts on screen in L4; printing in L7 | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.8 | Sell never-received / out-of-stock products with a clear warning, cost unknown | approved | [L4 §14](phases/L4_TILL.md) |
| Q-L4.9 | 80 mm thermal receipt printer | approved | [L4 §14](phases/L4_TILL.md) |

### L4 — decisions made while building (approved 2026-09-14)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L4.i1 | A day's sales stay as rung up; a void counts on the day it is made (takings = charged − refunded) | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i2 | Discounts: percent per line, amount on the sale in its settlement currency, one guarded act at checkout | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i3 | `changeCurrency` on the cart (default pounds; dollars for all-dollar sales); empty tender = exact money | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i4 | The cash note is a setting (1 … 1,000,000), set with the PIN on the rate screen | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i5 | Till and sales DTOs carry no cost (held by a test) | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i6 | `Till.Scan` answers `found: false` for an unknown code; the till searches by name | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i7 | A counted product increments its line (whole counts, BigInt); each weighing is its own line | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i8 | Gate: every `Act*` has an `owner.action.*` label in both catalogs | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i9 | `TextField` forwards its ref; `CellInput` for table cells | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i10 | `bootstrap.TillWithStock`, a test-only export to fail a real transaction after a stock movement | approved | [L4 §16](phases/L4_TILL.md) |
| D-L4.i11 | The seeder's beyond-stock sale is white cheese, not labneh | approved | [L4 §16](phases/L4_TILL.md) |
