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

## L5 — approved 2026-09-14, with the owner's answers

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L5.1 | A `customers` module owns `customers` and `debt_entries`; reached by the till through a `Debts` port | approved | [L5 §10.1](phases/L5_CUSTOMERS.md) |
| D-L5.2 | One insert-only chain per customer and currency, ordered by `seq`, balance before/after; never summed across currencies | approved | [L5 §4.1](phases/L5_CUSTOMERS.md) |
| D-L5.3 | Six kinds (opening, charge, payment, write_off, refund, reversal) held by CHECKs, nullable comparisons guarded | approved | [L5 §8](phases/L5_CUSTOMERS.md) |
| D-L5.4 | The debt currency is the currency the sale is charged in; the credit default is a setting | approved | [L5 §5.1](phases/L5_CUSTOMERS.md) |
| D-L5.5 | A credit sale's total is the cash total; optional paid now in either currency; paid in full is cash | approved | [L5 §5.2](phases/L5_CUSTOMERS.md) |
| D-L5.6 | The charge is written in the checkout transaction; exactly one charge per credit sale (UNIQUE + verifier) | approved | [L5 §5.4](phases/L5_CUSTOMERS.md) |
| D-L5.7 | Voiding a credit sale reverses its charge in the void's transaction | approved | [L5 §5.6](phases/L5_CUSTOMERS.md) |
| D-L5.8 | Repayments at the rate in force when paid, rounded once, change by L4's rule, quoted with a token | approved | [L5 §6](phases/L5_CUSTOMERS.md) |
| D-L5.9 | Pay all settles the balance exactly, recording the rounded note taken (gap ≤ half a note) | approved | [L5 §6.3](phases/L5_CUSTOMERS.md) |
| D-L5.10 | A balance below zero only from a reversal; used by the next charge or refunded, never past zero | approved | [L5 §7.3](phases/L5_CUSTOMERS.md) |
| D-L5.11 | Any entry but a charge reversed once with PIN and reason; a charge only by voiding its sale | approved | [L5 §7.4](phases/L5_CUSTOMERS.md) |
| D-L5.12 | PIN for openings, write-offs, refunds, reversals and deactivating a customer who owes | approved | [L5 §7.5](phases/L5_CUSTOMERS.md) |
| D-L5.13 | Customer names unique after normalisation; phone in any digits; the charge snapshots the name | approved | [L5 §3](phases/L5_CUSTOMERS.md) |
| D-L5.14 | Owed since and last payment from the chain; who owes what never totals across currencies | approved | [L5 §4.3](phases/L5_CUSTOMERS.md) |
| D-L5.15 | Every money movement snapshots the rate; no rate, no payment | approved | [L5 §2.4](phases/L5_CUSTOMERS.md) |
| D-L5.16 | Conversion and note rounding move to a pure `internal/lite/tender` package | approved | [L5 §10.2](phases/L5_CUSTOMERS.md) |
| D-L5.17 | A debt verifier; the sales verifier learns credit in the same change | approved | [L5 §9](phases/L5_CUSTOMERS.md) |
| D-L5.18 | `lite-customers-isolated`; every other isolation rule forbids `customers` | approved | [L5 §10.3](phases/L5_CUSTOMERS.md) |
| Q-L5.1 | US dollars by default, one tap to pounds | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.2 | Part paid at the counter on a credit sale: yes | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.3 | No credit limits; the balance shown before the sale is completed | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.4 | Anyone at the counter; no PIN for credit sales or repayments | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.5 | Repayments at the rate in force on the day they are made | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.6 | Voiding a partly repaid credit sale allowed; the excess is the customer's credit, used or refunded | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.7 | Write-offs with the owner PIN and a required reason | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.8 | **Each currency's balance separately, each labelled with its reference at the rate** (changed from the recommendation) | approved | [L5 §15](phases/L5_CUSTOMERS.md) |
| Q-L5.9 | Name, phone and note; names unique | approved | [L5 §15](phases/L5_CUSTOMERS.md) |

### L5 — decisions made while building (approved 2026-09-14)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L5.i1 | `debt_entries.cash_note_minor` on payments and refunds, for the verifier's half-note bound | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i2 | Q-L5.8 as answered: each balance with its own labelled reference; no figure adds currencies | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i3 | A credit quote without a customer still prices; checkout refuses it | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i4 | `payment` and `customerId` are part of the cart and its token | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i5 | *On credit* switches the till to the default debt currency and opens the picker | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i6 | A payment's value rounds to the debt's minor unit; change to the note | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i7 | Pay all that rounds to nothing in the other currency is refused | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i8 | *On credit* on the Sales screen counts credit sales rung up that day | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i9 | Today's debt book per currency on the Customers screen | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i10 | Statements newest first; a charge links to its receipt | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i11 | The 50,000-entry timing test: 1 s, 10 s under the race detector | approved | [L5 §17](phases/L5_CUSTOMERS.md) |
| D-L5.i12 | The seeder's sold figure includes credit; its summary counts credit sales and both voids | approved | [L5 §17](phases/L5_CUSTOMERS.md) |

## L6 — approved 2026-09-14, with the owner's answers

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L6.1 | Profit read from what each sale stored, never re-priced; the C6 table a fixture | approved | [L6 §3.1](phases/L6_REPORTS.md) |
| D-L6.2 | Dollars primary; pounds at each sale's own rate beside; a pounds sale's rounding is pounds revenue only | approved | [L6 §3.1](phases/L6_REPORTS.md) |
| D-L6.3 | A day's profit = sales rung up that day − sales voided that day; ranges sum days | approved | [L6 §3.3](phases/L6_REPORTS.md) |
| D-L6.4 | Per-product from line nets, one reconciling row for whole-sale discounts and rounding | approved | [L6 §3.4](phases/L6_REPORTS.md) |
| D-L6.5 | Net profit = gross − stock losses + count gains − bad debts − expenses; openings, revaluations, package openings not profit | approved | [L6 §4.1](phases/L6_REPORTS.md) |
| D-L6.6 | Stock losses and gains at the unit cost each ledger row moved at, by reason | approved | [L6 §4.2](phases/L6_REPORTS.md) |
| D-L6.7 | Events without a rate convert at the rate of their business day, never today's, never by clock | approved | [L6 §4.3](phases/L6_REPORTS.md) |
| D-L6.8 | Takings per day and month per currency | approved | [L6 §5](phases/L6_REPORTS.md) |
| D-L6.9 | Stock value on a past date from each product's last ledger row; negative stock no value | approved | [L6 §6.2](phases/L6_REPORTS.md) |
| D-L6.10 | A period's stock movements reconciled opening to closing, difference shown with causes | approved | [L6 §6.3](phases/L6_REPORTS.md) |
| D-L6.11 | Expected profit on the shelf (Q3), naming what it leaves out | approved | [L6 §6.4](phases/L6_REPORTS.md) |
| D-L6.12 | The drawer per currency sums money in the currency it moved in; nothing converted | approved | [L6 §7.1](phases/L6_REPORTS.md) |
| D-L6.13 | A void's cash return derived from the sale and shown in the void dialog | approved | [L6 §7.2](phases/L6_REPORTS.md) |
| D-L6.14 | A cash book (expenses, withdrawals, deposits, counts, reversals), insert-only, seq-ordered, money entries snapshot their rate | approved | [L6 §7.3](phases/L6_REPORTS.md) |
| D-L6.15 | A count closes its day | approved | [L6 §7.3](phases/L6_REPORTS.md) |
| D-L6.16 | Read-only `reports` module over facts ports; `cashbook` module; both isolated | approved | [L6 §9](phases/L6_REPORTS.md) |
| D-L6.17 | The Sales screen's `refunded` renamed `voided` | approved | [L6 §1.2](phases/L6_REPORTS.md) |
| D-L6.18 | A year of sales within stated bounds before done | approved | [L6 §12.1](phases/L6_REPORTS.md) |
| Q-L6.1 | Credit sale profit counts **when the goods are sold** | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.2 | Sales with no recorded cost **shown apart**, left out of profit and margins | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.3 | Expenses **recorded in the app**, with the owner PIN | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.4 | The drawer kept: **expected cash, closing counts and the difference** | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.5 | A void **states the cash to hand back**, matching what was paid at the counter (D-L6.i2) | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.6 | **Dollars first**, pounds beside at each sale's historic rate | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.7 | Profit, margins, costs, stock value and expenses **owner only**; the cash count **at the counter** | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.8 | **Calendar months** by business date | approved | [L6 §15](phases/L6_REPORTS.md) |
| Q-L6.9 | **On screen** in L6; printing in L7; no export | approved | [L6 §15](phases/L6_REPORTS.md) |

### L6 — decisions made while building (approved 2026-09-15)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L6.i1 | `ck_cash_money_is_positive` amended from the verified draft to `amount_minor >= 0 AND (kind IN ('count','reversal') OR amount_minor > 0)`: a reversal copies its entry's amount, and a count may be zero | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i2 | Q-L6.5 as built: a cash sale hands back its total, a credit sale what was paid now (total − the debt added), both in the currency the sale was charged in — one rule, `Sale.VoidReturn()`, shown before the PIN and summed by the drawer | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i3 | A reversed debt payment or refund moves its cash back on the reversal's day, as a void does | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i4 | At the counter the drawer shows drawer-paid expenses together with withdrawals as *taken out by the owner*, and the cash book only its counts | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i5 | A count records the drawer's expected figure at the moment of counting; the day's count is the newest not reversed; a reversed count no longer opens the next day | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i6 | `Reports.Stock` takes a range (month to date by default): value on its last day, the range reconciled, the shelf now | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i7 | The reconciliation's classes are exact value changes that telescope, so its only possible differences are stock below zero and rounding (bounded and shown) | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i8 | Shelf profit counts products priced below cost (negative, marked and counted); leaves out inactive, no stock, no cost, and pounds prices with no rate | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i9 | Reports read their facts without a transaction, so a year's report never holds the till's single writer | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i10 | A month lists only days with activity; its totals are the sum of all its days | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i11 | Takings net each reversal (collected, refunded, written off) on the reversal's day | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i12 | Reports use their own fact types; the composition root copies each module's records, and a test holds the kind strings equal | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i13 | The Cash screen shows a past day read-only; counts and entries are made on today | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i14 | The seeder's history has no credit: L5's paper book is adopted today, as before | approved | [L6 §17](phases/L6_REPORTS.md) |
| D-L6.i15 | The year-of-sales bounds ×10 under the race detector, rows generated in SQL (as D-L5.i11) | approved | [L6 §17](phases/L6_REPORTS.md) |

## L7 — approved 2026-09-15

| ID | Decision | Status | Where |
|---|---|---|---|
| A-L7.1 | Export is in scope (the owner, 2026-09-15) | **supersedes Q-L6.9** "no export" | [L7 §2.4](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.1 | Documents laid out and rendered in Go; the webview's print dialog not used | approved | [L7 §1.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.2 | One Document model → PDF, 1-bit raster, workbook; PDF and raster share one line layout | approved | [L7 §3.1](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.3 | Every printed or exported figure isolated left to right (LRI…PDI) | approved | [L7 §3.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.4 | PDFs embed IBM Plex Sans Arabic (OFL); ToUnicode skips bidi controls; A4; repeated headings | approved | [L7 §4.3](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.5 | Workbooks by Lite: sheet per section, RTL, frozen headers, typed cells | approved | [L7 §4.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.6 | Exports and printouts built from the screen's own results; nothing re-queried | approved | [L7 §4.1](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.7 | Go builds and writes files after the OS Save dialog, atomically, through a port | approved | [L7 §4.4](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.8 | Thermal receipts as ESC/POS `GS v 0` rasters in bands | approved | [L7 §5.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.9 | Raw path by default, driver path as fallback, from the same bitmap | approved — **the default amended to the driver path** by Q-L7.1 (D-L7.i2) | [L7 §5.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.10 | Printing after commit; a failure never undoes a sale | approved | [L7 §5.4](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.11 | The receipt dialog previews the printed bitmap | approved | [L7 §5.5](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.12 | Reprints stamped as copies from an insert-only `print_jobs` log | approved | [L7 §7](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.13 | Backups daily, every few hours open, on close, before migration and restore, on demand; kept per reason | approved — **amended by Q-L7.8**: no every-few-hours backup, 7 per reason (D-L7.i3) | [L7 §6.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.14 | A verified outside copy in a folder the owner picks; its age shown and warned | approved | [L7 §6.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.15 | Restore with PIN, the loss counted, a snapshot of what is replaced, applied at start | approved | [L7 §6.4](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.16 | Modules `exports`, `printing`, `backups`; pure `documents`, `sheets`; `typeset` and `printers` confined; five new rules | approved — **built without an `exports` module** (D-L7.i1) | [L7 §8](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.17 | The edition boundary admits go-text/typesetting and x/image for `typeset` only | approved | [L7 §8.2](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.18 | Receipt header lines are settings | approved | [L7 §2.1](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.1 | **80 mm thermal printers via USB and the OS driver, printed as an image** — the driver path the default (D-L7.i2) | approved | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.2 | credit sales, payments and refunds print themselves; cash sales on a button | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.3 | shop-wide voucher numbers without gaps | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.4 | a signature line on credit receipts (and vouchers) | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.5 | a statement anyone; the debt ledger, sales history and drawer the owner | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.6 | Money in Excel as **real numbers** | approved | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.7 | **A4** — the owner's export answer names "A4 PDF" | approved | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.8 | **Daily and on-close** backups kept; an **outside destination** the owner picks; **status on Home**. No every-4-hours backup; 7 kept per reason (D-L7.i3) | approved | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.9 | **The owner PIN and the warning summary** of what was recorded since, then the safety snapshot — one confirmation, no typed shop name | approved | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.10 | not encrypted | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.11 | a setting, off by default; cash sales and payments, raw path | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |
| Q-L7.12 | name, phone, address, footer; no logo | approved with L7 (2026-09-15) — built as recommended, not answered individually | [L7 §15](phases/L7_HARDWARE_BACKUP.md) |

### L7 — decisions made while building (approved 2026-09-15)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L7.i1 | Exports and printouts laid out in the `api` layer from the screens' DTOs; no `exports` module; `printing` records jobs and voucher numbers only | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i2 | The driver path is the default print path; raw ESC/POS a setting | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i3 | Backups daily, on close, before migration or restore, on demand; 7 kept per reason | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i4 | A staged restore applied in `bootstrap.Start` before the database opens; the application restarts in-process | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i5 | Text the shop typed isolated (FSI…PDI) in printouts and exports, beside the figures | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i6 | The receipt's rate line has its own template, `doc.rate` | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i7 | Workbook cells carry no bidi marks | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i8 | PDF glyphs written in logical order | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i9 | The shared archlint no-float rule gains an `except` list (`typeset`, `documents`) | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i10 | `golang.org/x/image v0.40.0`, the version already in the graph | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i11 | The restore's loss counted through the modules' facts, by time recorded after the backup | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i12 | The sales history's PDF one row per sale; lines in the workbook only | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i13 | `Print.Preview` takes the document and id; the width from the settings | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i14 | Backup ages measured by Go (`ageSeconds`) | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i15 | The restore notice on Home and the Backups screen, not the boot screen | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i16 | A payment's or refund's voucher opens after it is recorded, and from a statement row | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i17 | Choosing an outside folder takes a backup at once; a missing folder warns, local backups continue | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i18 | A4 table cells a 5 pt gutter; the products table's amounts without currency names; an empty cash book a sentence | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |
| D-L7.i19 | A print recorded only when a printer is named; a failed print does not consume a copy number | approved | [L7 §17](phases/L7_HARDWARE_BACKUP.md) |

## L8 — approved 2026-09-16, with the owner's answers (the Windows build cross-compiled, the pilot unsigned, الصندوق / حركة الصندوق, DD/MM/YYYY, an Excel import)

| ID | Decision | Status | Where |
|---|---|---|---|
| A-L8.1 | L8 adds end-to-end testing, UI polish, the support file and the documentation set to DESIGN §10's installers, seeder, DoD review and pilot | approved | [L8 §2.3](phases/L8_RELEASE.md) |
| A-L8.2 | "The screens looked at in Arabic and English" closed for L0–L7 by the visual pack and the owner's review | approved | [L8 §2.3](phases/L8_RELEASE.md) |
| A-L8.3 | The counter lands on the Till; Status becomes About | approved | [L8 §2.3](phases/L8_RELEASE.md) |
| D-L8.1 | E2E: the real built frontend against the real Go graph through a test-only loopback bridge | approved | [L8 §3.2](phases/L8_RELEASE.md) |
| D-L8.2 | `playwright-core` with the installed Chrome or Edge; no browser = NOT RUN | approved | [L8 §3.3](phases/L8_RELEASE.md) |
| D-L8.3 | Ten journeys in both languages, each ending with Go-state assertions | approved | [L8 §3.4](phases/L8_RELEASE.md) |
| D-L8.4 | A visual pack of every view, both languages, two sizes, looked at each release; structural checks automated; no screen pixel goldens | approved | [L8 §3.4](phases/L8_RELEASE.md) |
| D-L8.5 | Packaged-app smoke scripts for macOS and Windows | approved | [L8 §3.5](phases/L8_RELEASE.md) |
| D-L8.6 | Fixture databases for schemas 1–8, upgraded and verified on every run | approved | [L8 §3.6](phases/L8_RELEASE.md) |
| D-L8.7 | A seven-point review of every view; S1–S8 fixed with a test each | approved | [L8 §4.1](phases/L8_RELEASE.md) |
| D-L8.8 | A bilingual glossary; one rule for dates and numbers everywhere | approved | [L8 §4.3](phases/L8_RELEASE.md) |
| D-L8.9 | The till fully usable by keyboard, with a key legend | approved | [L8 §4.4](phases/L8_RELEASE.md) |
| D-L8.10 | One version source, `lite-v…` tags, `-dev.<sha>` otherwise | approved | [L8 §5.2](phases/L8_RELEASE.md) |
| D-L8.11 | Per-machine NSIS installer with offline WebView2; refuses while running; never touches the data | approved | [L8 §5.3](phases/L8_RELEASE.md) |
| D-L8.12 | Universal DMG with `hdiutil` | approved | [L8 §5.4](phases/L8_RELEASE.md) |
| D-L8.13 | Signing opt-in; unsigned bypasses documented | approved | [L8 §5.5](phases/L8_RELEASE.md) |
| D-L8.14 | `make lite-release` from a clean tag, listing what was NOT RUN | approved | [L8 §5.6](phases/L8_RELEASE.md) |
| D-L8.15 | Third-party notices generated offline, shipped and shown | approved | [L8 §5.7](phases/L8_RELEASE.md) |
| D-L8.16 | `docs/mizan_lite/guide/`, Arabic first with English parity gated; the shop guide as a PDF rendered by Lite | approved | [L8 §6](phases/L8_RELEASE.md) |
| D-L8.17 | About screen; the counter lands on the Till | approved | [L8 §9.1](phases/L8_RELEASE.md) |
| D-L8.18 | A support file saved by the owner; the database only when chosen; no secret | approved | [L8 §9.2](phases/L8_RELEASE.md) |
| D-L8.19 | The Windows protocol and the pilot week are release criteria; 0.9.0 pilot, 1.0.0 at exit | approved | [L8 §7, §8](phases/L8_RELEASE.md) |
| D-L8.20 | A Definition of Done review over L0–L8 in Mizan's form | approved | [L8 §12.3](phases/L8_RELEASE.md) |
| Q-L8.1 | A Windows PC or VM before release — **answered: cross-compile here; the owner runs and verifies it** (O1) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.2 | Code signing certificates — **answered: the pilot ships unsigned**; signing stays opt-in | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.3 | Who installs in a shop (recommended: write for a relative or technician) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.4 | The pilot shop, its machine and printer; paper book in parallel (recommended: three days) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.5 | Navigation names and the first screen — **answered: الصندوق for selling, حركة الصندوق for the drawer**; land on the Till | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.6 | Arabic dates and times — **answered: DD/MM/YYYY everywhere**, 24-hour, no month names (D-L8.i5) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.7 | Installer per-machine or per-user, and its language (recommended: per-machine, Arabic) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.8 | The database in the support file when ticked (recommended: yes, off by default) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.9 | Product import from a spreadsheet — **answered: yes, a simple Excel import in L8** (D-L8.i8) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.10 | Versions (recommended: 0.9.0 pilot, 1.0.0 at exit) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.11 | No automatic update (recommended: confirm) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.12 | Minimum computer (recommended: Windows 10 22H2/11 64-bit, 4 GB, 1366×768; macOS 13+) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.13 | Playwright as a frontend dev dependency — built as `@playwright/test` with no browser download (D-L8.i2) | approved | [L8 §14.2](phases/L8_RELEASE.md) |
| Q-L8.14 | Who reviews the Arabic (recommended: the owner, on the visual pack) | approved | [L8 §14.2](phases/L8_RELEASE.md) |

### L8 — decisions made while building (approved 2026-09-16)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L8.i1 | The bridge is an ordinary package plus `cmd/lite-e2e`, not a build-tagged one; `lite-e2e-test-only` keeps it out of the application | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i2 | `@playwright/test` driving the installed Chrome (Edge on Windows); no browser downloaded | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i3 | The journeys assert Go's state through the same bridge they drive | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i4 | The counter lands on the Till; Status became About; the backups' state warns from every screen | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i5 | One date rule everywhere — `15/09/2026`, 24-hour, digits only, on paper and in the guides | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i6 | Values put into an Arabic sentence are isolated by the translator, not at the call site | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i7 | Rate sentences carry both sides as figures (`{usd} = {local}`) | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i8 | The import's preview runs the real import in a rolled-back transaction; the PIN is asked for last | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i9 | Every list a binding returns is empty, never nil | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i10 | A pay key pressed while Go is pricing pays when the price arrives; a change to the cart cancels it | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i11 | The Excel template's example rows are named "(مثال)" / "(example)" | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i12 | The guides live as Markdown in `docs/` and ship as PDFs rendered by Lite, checked for drift | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i13 | The support file holds diagnostics and a week of logs; the database only when ticked, in owner mode | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i14 | The notices are generated for both systems' module graphs | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i15 | The upgrade matrix compares the columns a table had, not the columns it has | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i16 | The Windows installer is per-machine, bilingual, refuses while running, and never touches the shop's data | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i17 | No custom disk-image background; the installation guide travels inside the image | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i18 | A build off a `lite-v<version>` tag calls itself `<version>-dev.<sha>` everywhere it names itself | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i19 | The smoke test waits for *ready* on a new shop and for the frontend's own call on a shop that is set up | approved | [L8 §17](phases/L8_RELEASE.md) |
| D-L8.i20 | The seeded shop for the journeys is seeded once and copied per test | approved | [L8 §17](phases/L8_RELEASE.md) |

### Authentication — asked and settled 2026-09-16

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L8.i21 | **No JWT and no login.** A single-computer offline application has no server to authenticate against; the counter must never be locked out mid-sale. One-time setup, the owner PIN on the owner's actions, two minutes of owner mode, a recovery code written down once | approved — the owner chose this over three alternatives | [L8 §15](phases/L8_RELEASE.md) |

## 0.9.1 — the owner's changes after using 0.9.0 (approved 2026-09-16)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-091.1 | The till grid lists **every** product, pinned ones first, scrolling in its own box — the quick-slot filter was the limit, not a number | approved | `TillScreen.tsx` |
| D-091.2 | The shop chooses how often it is backed up by itself: daily, weekly, monthly, manual. Daily stays the default | approved | [L8 §5](phases/L8_RELEASE.md) |
| D-091.3 | The backups on close, before a migration and before a restore ignore that setting — they are the ones that save a shop from what is about to happen | approved | `bootstrap.scheduledBackupDue` |
| D-091.4 | **The PIN is reserved for a restore and for bringing in a backup file.** 26 of the 28 guarded acts are open at the counter; changing the PIN still needs the current one, as it always did | approved — the owner chose this over two narrower options with the trade-off stated | `owner.ReservedActs` |
| D-091.5 | Every act is still written to the owner's history, reserved or not. The record is what survives the gate, and is what now proves each module reaches the real owner service | approved | `owner.Require` |
| D-091.6 | Quick pay is the whole sale with no change; the tender, discount and change fields are collapsed, and closing them clears them so nothing hidden can price a sale | approved | `TillScreen.tsx` |
| D-091.7 | The shop's name, address and telephone print on A4 reports and workbooks, not only on receipts | approved | `api.paperHeader` |

## L9 — the price a shop paid (approved 2026-09-16, shipped 0.9.2)

| ID | Decision | Status | Where |
|---|---|---|---|
| D-L9.1 | The typed cost price is the profit basis, **snapshotted at checkout**; sales already made keep the cost they recorded | approved, with the departure from "recompute history" stated to the owner | [L9 §2.2](phases/L9_COST_AND_AUDIT.md) |
| D-L9.2 | A cost typed in pounds converts to dollars at that sale's own rate, so both profit figures still reconcile (C6) | approved | [L9 §2.3](phases/L9_COST_AND_AUDIT.md) |
| D-L9.3 | The margin is computed from cost and price, never stored | approved | [L9 §2.1](phases/L9_COST_AND_AUDIT.md) |
| D-L9.4 | The cost price is held in the product's own selling currency | approved | [L9 §2.1](phases/L9_COST_AND_AUDIT.md) |
| D-L9.5 | A margin that would take the price below nothing is refused; selling below cost is shown | approved | [L9 §2.1](phases/L9_COST_AND_AUDIT.md) |
| D-L9.6 | An audit is scoped by a free-text section, not a category scheme the catalogue does not have | approved — **designed, not built** | [L9 §3](phases/L9_COST_AND_AUDIT.md) |
| D-L9.7 | Only a closed audit moves stock; each line snapshots the system quantity when it is added | proposed — **designed, not built** | [L9 §3](phases/L9_COST_AND_AUDIT.md) |
| D-L9.8 | The audit schema ships as migration 0010 with the module that uses it, not before | approved | [L9_AUDIT_DRAFT.sql](phases/L9_AUDIT_DRAFT.sql) |
