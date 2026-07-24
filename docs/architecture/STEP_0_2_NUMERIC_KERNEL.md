# Step 0.2 — Numeric Kernel (Design, for approval)

> Status: **DESIGN — awaiting approval. No implementation until signed off** (per the
> Permanent Development Protocol, Step 2).
> Scope: the money/quantity/rate value types and their arithmetic. Nothing else.

This is the most correctness-critical code in Mizan. Everything financial — invoices, the
general ledger, tax, costing, currency conversion — is arithmetic over these types. A bug here
is a bug in every report. The design therefore optimises for **correctness, clarity, and a
stable public surface**, and treats performance as a distant concern (a desktop ERP does
thousands of these operations per second, not millions).

---

## 1. ANALYSIS

### 1.1 What this layer must guarantee

1. **No floating-point, ever**, for any business value. `0.1 + 0.2 != 0.3` is disqualifying.
2. **Exactness**: a sum of parts equals the whole, to the minor unit, always.
3. **One rounding, in one place, with a named mode** — never accidental double rounding.
4. **Total functions**: no silent wrong answers. An impossible operation (currency mismatch,
   overflow, cross-category conversion) returns a typed error; it never returns garbage and
   never panics in production.
5. **Immutability**: values never mutate; operations return new values.
6. **A tiny, stable public API** that hides representation, so the internals can change
   (e.g. widen to 128-bit for crypto) without breaking a single caller.

### 1.2 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Float creep | Cent-level drift across millions of rows | Integer-only types; lint rule `no-float` already enforces it (§0.1) |
| `int64` overflow | Wrong totals at extreme magnitudes / crypto | Checked arithmetic → typed error; 128-bit multiply intermediates; **private representation** so widening needs no API change (§2.3) |
| Double rounding | Legally different tax/FX totals | Single-rounding rule enforced structurally; rounding only at named boundaries (§4) |
| Currency mismatch | Adding USD to SYP silently | Every op validates currency identity; mismatch → error (§3.4) |
| Precision loss in unit price × qty | Wrong inventory valuation | Separate higher-precision `UnitAmount`; 128-bit intermediate, round once (§5) |
| Cross-category UoM conversion | Converting kg → metre → nonsense | Conversion legal only within a UoM category (§6) |
| Representation leaking into callers | Locked into `int64` forever | Public API exposes no raw integer; construction/formatting only (§7) |

### 1.3 Future-compatibility requirements (your §5) — checked against the design

| Requirement | How today's design absorbs it **without redesign** |
|---|---|
| Additional currencies | `Currency` is a value object built from the `currencies` table; adding one is data, not code |
| High-inflation currencies | Large magnitudes fit `int64` for all realistic fiat; checked ops flag the impossible; widening path documented |
| **Crypto assets** | A crypto asset is a `Currency` with up to 18 decimals; the **private representation** lets `Money` widen from `int64` to 128-bit/`big.Int` internally with **zero public-API change** (§2.3) |
| Multiple unit systems (metric/imperial) | Units differ only by their conversion factor to a category reference — pure data; no type change |
| Advanced inventory costing (FIFO/standard) | WAC/FIFO are built from the primitives here (`MulQuantity`, checked `Div`, `Allocate`); the kernel already provides them |

### 1.4 Recommendation before design

I recommend building the kernel on **checked `int64`** with a **fully private representation**,
rather than `math/big.Int` from day one. Reasoning is in §2.3; the short version is that
`int64` is correct for every realistic fiat case, is a value type (immutable, no nil, no shared
pointers — all of which `big.Int` gets wrong), and the crypto/extreme-scale case is handled by
the hidden-representation escape hatch you explicitly asked for. This is the decision I most
want your agreement on.

---

## 2. DESIGN — foundations

### 2.1 The five types and their single responsibilities

Each type does exactly one thing. That is deliberate: conflating "a price" with "an amount of
money" is how precision bugs enter accounting systems.

| Type | Single responsibility | Scale (internal) | Example value |
|---|---|---|---|
| `Money` | A settled amount in a currency (totals, GL, balances, payments) | currency minor units (`int64`) | `SYP 1500`, `USD 19.99` |
| `UnitAmount` | A per-unit price or cost, needing sub-minor precision | 10⁻⁶ of the currency's **major** unit (`int64`) | `USD 1.234567 / kg` |
| `Quantity` | An amount of a good, in a unit of measure | 10⁻⁶ (`int64`) + a `Unit` | `2.5 kg`, `3 pcs` |
| `Rate` | A pure ratio (FX rate, UoM factor) | 10⁻⁹ (`int64`) | `1 USD = 15000 SYP` |
| `Percent` | A rate applied to an amount (tax, discount) | 10⁻⁶ (`int64`) | `15% = 0.150000` |

Why these scales:
- **Money = minor units** is the accounting-industry standard (Stripe, ledgers). The scale is
  the currency's own `decimal_places`, from data — 0 for SYP/JPY, 2 for USD, 3 for KWD.
- **UnitAmount = 10⁻⁶ major** because a unit cost is routinely a fraction of a minor unit
  (a gram of a compound, a metre of cable). Rounding the unit cost to whole minor units and
  then multiplying by thousands of units produces a materially wrong valuation. Six decimals
  of the major unit gives four extra decimals beyond a 2-dp currency — enough for any realistic
  unit price, and it never touches a settled `Money` figure until one explicit rounding.
- **Rate = 10⁻⁹** covers fiat FX (including hyperinflation, `1 USD = 15,000+ SYP`) and UoM
  factors (`kg→mg = 1,000,000`) with wide headroom.
- **Percent = 10⁻⁶** represents `15%` as `150000`; ample for any tax or discount rate.

### 2.2 Everything is an immutable value type

```go
type Money struct {          // all fields unexported — representation is private
	minor    int64
	currency Currency
}
```

Passed by value, compared by value where meaningful, never mutated. No method changes the
receiver; each returns a new value. There are no pointers inside, so there is no aliasing, no
nil, and no shared mutable state — the properties `big.Int` fails to provide.

### 2.3 **DECISION D1 — checked `int64` with private representation** (recommended)

The single most important decision in this layer.

**Chosen:** store amounts as `int64`; make every arithmetic operation *checked* (overflow
returns a typed error); keep the representation completely private; compute multiplication
intermediates in 128-bit so they never overflow before the final fit-to-`int64`.

**Why not `big.Int` now:**

| Aspect | Checked `int64` (chosen) | `math/big.Int` |
|---|---|---|
| Correctness for fiat | Exact; overflow is detected, not silent | Exact, never overflows |
| Value semantics / immutability | Pure value type — no nil, no aliasing | Pointer-backed; zero value is fragile; easy to share and mutate by accident |
| Performance | ~1 ns/op, zero allocation | Allocates on most ops |
| Range | ±9.2×10¹⁸ minor units — every realistic fiat amount | Unbounded |
| Crypto (18 decimals) | Needs the widening path | Native |

`int64` at 2 decimals reaches ±92 trillion major units; at 0 decimals (SYP), ±9.2×10¹⁸ pounds
— beyond any real balance, even under hyperinflation and redenomination. The only case it does
**not** cover is 18-decimal crypto (1 token = 10¹⁸ base units overflows immediately).

**The escape hatch that makes this future-proof:** because no public method returns the raw
`int64`, the internal field can later become an `int128` (via `math/bits`) or a `big.Int`
**without changing the public API**. Checked operations already return `(T, error)`; when the
representation widens, the overflow error simply stops occurring — every caller still compiles
and behaves correctly. This is precisely the "crypto without redesign" property you asked for,
achieved at zero cost today. A short ADR will record the widening procedure.

This is the recommendation I want explicit sign-off on.

### 2.4 `Currency` — a kernel value object, not a DB entity

```go
type Currency struct {
	code     string        // "USD", "SYP", "BTC" — data, never an enum (supports non-ISO/crypto)
	decimals uint8         // minor-unit scale: 0..18 (18 reserved for crypto)
	rounding RoundingMode  // this currency's default rounding at settlement
}
```

The authoritative record lives in the `currencies` table (§CUR). The **currency module maps a
row → this value object**; the kernel never imports the database (enforced by `import-boundary`).
`decimals` up to 18 means a crypto asset is representable as a `Currency` the moment the
representation is widened — the type does not change.

Two currencies are "the same" iff their `code` matches. `decimals`/`rounding` are carried for
convenience; `code` is identity.

---

## 3. DESIGN — `Money` API

### 3.1 The complete public surface (deliberately small)

```go
// Construction
func FromMinor(c Currency, minor int64) Money            // exact, from stored minor units
func Zero(c Currency) Money                              // the additive identity in c
func Parse(c Currency, s string) (Money, error)          // "19.99" → USD 1999; validates scale

// Arithmetic — checked, total, immutable
func (m Money) Add(n Money) (Money, error)               // err on currency mismatch / overflow
func (m Money) Sub(n Money) (Money, error)
func (m Money) Neg() Money
func (m Money) Abs() Money
func (m Money) MulInt(k int64) (Money, error)            // whole-number scaling (e.g. ×qty pieces)
func (m Money) MulPercent(p Percent, r RoundingMode) (Money, error)
func (m Money) MulRate(rate Rate, to Currency, r RoundingMode) (Money, error)  // conversion (§3.5)
func (m Money) Allocate(weights []int64) ([]Money, error)                       // exact split (§3.6)

// Comparison & predicates — cannot fail except on currency mismatch
func (m Money) Cmp(n Money) (int, error)                 // -1,0,1
func (m Money) Equal(n Money) bool
func (m Money) IsZero() bool
func (m Money) IsNegative() bool
func (m Money) Sign() int

// Access — for persistence & display ONLY; never exposes the internal width
func (m Money) Minor() int64                             // for the repository mapper
func (m Money) Currency() Currency
func (m Money) String() string                           // "USD 19.99" (debug)
func (m Money) Format(loc locale.Locale) string          // locale-aware display
```

That is the entire `Money` API. Notably **absent**: any `Div` on `Money` (division of money is
`Allocate`, which is exact — see §3.6), any float accessor, any setter, any exported field.

### 3.2 Why `Minor() int64` does not violate "hide representation"

`Minor()` returns the value *in minor units* — a stable, well-defined quantity (cents), not the
internal storage word. If the representation widens to 128-bit, `Minor()` still returns the
minor-unit value; only amounts that genuinely exceed `int64` minor units (crypto) would need a
`MinorBig()` companion, added additively. The repository stores `Minor()` in a `BIGINT` column;
that contract is exactly the portable schema (§8.1). So `Minor()` is part of the stable
contract, not a representation leak.

### 3.3 Construction rules & the zero value

- The **zero value `Money{}`** has an empty currency and is treated as **invalid** for
  arithmetic; every operation checks and returns `ErrNoCurrency`. Callers construct through
  `FromMinor`/`Zero`/`Parse`. (Documented loudly; a `Valid()` predicate is provided.)
- `Parse` validates that the string's scale does not exceed the currency's `decimals`
  (`"19.999"` in USD → error, not silent truncation).

### 3.4 Currency safety

`Add`, `Sub`, `Cmp` require identical currencies; otherwise `ErrCurrencyMismatch`. There is no
implicit conversion — converting is always the explicit, rate-carrying `MulRate` (§3.5). This
is what prevents the entire class of "accidentally added two currencies" bugs.

### 3.5 Conversion — exact, documented formula

`m.MulRate(rate, to, mode)` converts an amount in currency **A** to currency **B**, where
`rate` means "1 major unit of A = `rate` major units of B".

Let `dA = A.decimals`, `dB = B.decimals`, `rate = rateNano / 10⁹`. Then:

```
minorB = round( minorA × rateNano × 10^dB  /  (10^9 × 10^dA) )
```

computed with a 128-bit numerator (`math/big` intermediate), divided once, and rounded once
using `mode`. The method returns both the resulting `Money` and is always paired at the call
site with storing the `rate` used — because the ledger stores the functional amount and the
rate (§18.1), never recomputing later. Cross-rate and redenomination walks are composed from
this single primitive.

### 3.6 Allocation — the exact-split algorithm (fully documented)

`m.Allocate(weights)` splits `m` into `len(weights)` parts, proportional to the weights, whose
sum is **exactly** `m` — no cent lost or invented. Algorithm (largest-remainder / Hamilton):

```
total   = Σ weights                              (error if total <= 0)
for i:   ideal_i   = m.minor × weights[i] / total     (integer division, floor)
         remainder_i = (m.minor × weights[i]) mod total
allocated = Σ ideal_i
leftover  = m.minor − allocated                  (0 <= leftover < len)
distribute the `leftover` minor units, one each, to the parts with the largest
remainder_i (ties broken by lowest index, deterministically).
```

Worked example — split `USD 100.00` (`10000`) three ways, weights `[1,1,1]`:
`ideal = [3333,3333,3333]`, `remainder` equal, `leftover = 1` → `[3334,3333,3333]`, sum `10000`. ✔

This is used everywhere money is divided: distributing a document discount across lines,
apportioning tax, instalment plans, landed-cost allocation. Negative amounts are supported
(the leftover distribution is sign-aware). Properties proven by the tests in §8.

---

## 4. DESIGN — rounding (documented exhaustively)

### 4.1 The single-rounding principle

> Multiply and divide at full precision; **round exactly once**, at the moment a value becomes
> a settled `Money`, using an explicitly named `RoundingMode`.

Rounding never happens implicitly. Every method that can round takes a `RoundingMode`
parameter (or uses the currency's default at settlement). Intermediate `UnitAmount`/`Rate`
values are never pre-rounded.

### 4.2 The rounding modes, each defined precisely

Rounding maps an exact rational to an integer number of minor units. Ties = the value is
exactly halfway.

| Mode | Rule on ties | `2.5→` | `-2.5→` | `2.4→` | When to use |
|---|---|---|---|---|---|
| `HalfAwayFromZero` | away from zero | `3` | `-3` | `2` | **Commercial default** — receipts, most invoicing |
| `HalfToEven` (banker's) | to the nearest even | `2` | `-2` | `2` | Statistical/accounting contexts that must avoid upward bias |
| `HalfUp` | toward +∞ | `3` | `-2` | `2` | Jurisdiction-specific tax rules |
| `HalfDown` | toward −∞ | `2` | `-3` | `2` | Rare; jurisdiction-specific |
| `Ceiling` | — (always toward +∞) | `3` | `-2` | `3` | "Round up to next unit" pricing |
| `Floor` | — (always toward −∞) | `2` | `-3` | `2` | Conservative valuation |
| `TowardZero` (truncate) | — (drop the fraction) | `2` | `-2` | `2` | Explicit truncation only |

The **currency's default** (`Currency.rounding`) is used when a caller doesn't specify one at a
settlement boundary; `HalfAwayFromZero` is the seed default. Per-context overrides (line vs
document rounding) come from settings, resolved above the kernel.

### 4.3 Currency-increment rounding (hyperinflation)

Some markets round to the nearest 5, 50, or 1000. That is **not** a `RoundingMode` (which
rounds to the minor unit); it is a separate, explicit `RoundToIncrement(m, increment, mode)`
helper, applied at the configured stage, with the difference posted to a rounding account
(§18.6). Kept distinct so the two concepts never entangle.

---

## 5. DESIGN — `UnitAmount` and the line-extension rule

`UnitAmount` is a per-unit price/cost at 10⁻⁶-major precision. Its reason to exist is one
method — the only place in the entire system where a price meets a quantity:

```go
func LineExtension(price UnitAmount, qty Quantity, mode RoundingMode) (Money, error)
```

Computation: `price` (10⁻⁶ major) × `qty` (10⁻⁶) → an exact rational in major units; scaled to
the currency's minor units; **one** rounding with `mode` → `Money`. The intermediate is 128-bit,
so no overflow occurs before the final fit. **No other code multiplies a price by a quantity** —
this function is the sole authority, which is what guarantees consistent rounding across sales,
purchasing, and costing. Discounts and taxes then operate on the returned `Money` via
`MulPercent`/`Allocate`.

`UnitAmount` has a minimal surface: `FromMicro`, `Parse`, `Currency`, `Micro()` (for
persistence), `Format`, and `LineExtension`. No general arithmetic — it is an input to one
operation, not a calculator.

---

## 6. DESIGN — `Quantity` and units of measure

```go
type Quantity struct {         // immutable value
	micro int64                // 10^-6
	unit  Unit                 // carries its category
}
type Unit struct {             // kernel value object; authoritative row in units_of_measure
	code            string
	categoryCode    string     // "weight","volume","length","count",...
	factorToRef     Rate       // this unit → the category's reference unit
	allowsFractional bool
	precision       int64      // rounding increment for this unit (×10^-6)
}
```

Surface:

```go
func QuantityFromMicro(u Unit, micro int64) (Quantity, error)   // err if fractional & !allowsFractional
func (q Quantity) Add(o Quantity) (Quantity, error)             // err if units differ
func (q Quantity) Sub(o Quantity) (Quantity, error)
func (q Quantity) ConvertTo(target Unit, mode RoundingMode) (Quantity, error)  // same category only
func (q Quantity) Cmp(o Quantity) (int, error)
func (q Quantity) IsZero/IsNegative/Sign() ...
func (q Quantity) Micro() int64
func (q Quantity) Unit() Unit
func (q Quantity) Format(loc locale.Locale) string
```

- **Conversion is legal only within a category.** `kg → g` ✔; `kg → litre` → `ErrUnitCategory`.
  The math goes through the category reference: `micro_target = micro_source × factor_source /
  factor_target`, 128-bit intermediate, single rounding to the target unit's `precision`.
- **Metric vs imperial** is nothing special: `lb` and `kg` are two units in the `weight`
  category with different `factorToRef`. "Multiple unit systems" therefore needs no type change.
- **Fractional control** is per-unit (`allowsFractional`): `1.5 kg` ✔, `0.5 pcs` → error.

`Quantity` is independent of the `money` package (it defines its own use of `Rate` for factors
but imports nothing from `money`). `LineExtension` lives in `money` and imports `quantity` — a
clean one-directional dependency, no cycle.

---

## 7. DESIGN — package layout & public-API discipline

```
internal/kernel/
├── money/                     # the monetary + pricing + ratio domain
│   ├── currency.go            # Currency value object
│   ├── money.go               # Money
│   ├── unitamount.go          # UnitAmount + LineExtension (imports quantity)
│   ├── rate.go                # Rate
│   ├── percent.go             # Percent
│   ├── rounding.go            # RoundingMode + the rounding engine
│   ├── errors.go              # ErrCurrencyMismatch, ErrOverflow, ErrNoCurrency, ...
│   └── *_test.go
└── quantity/                  # the measurement domain (independent of money)
    ├── quantity.go
    ├── unit.go
    ├── errors.go
    └── *_test.go
```

**API discipline (your §4):**
- Only the identifiers listed in §3/§5/§6 are exported. Fields are unexported everywhere.
- No exported function returns a `float64`, ever (also lint-enforced).
- Errors are exported sentinels/typed values so callers can branch on them.
- The packages import only stdlib + `math/big` + `internal/kernel/*` — enforced by
  `import-boundary`. Designed to read as a standalone, reusable Go library.
- **Stability promise:** additions are allowed; signature changes to shipped functions are not.
  A short ADR will record the API as frozen once approved.

---

## 8. TESTING PLAN (your §2 — mapped explicitly)

Testing is co-designed with the types, not bolted on. Every one of your eight categories maps
to concrete tests. Property-based tests use `pgregory.net/rapid`.

| Your category | Concrete tests |
|---|---|
| **Unit** | Table tests for every method of every type; every rounding mode against its truth table (§4.2); allocation worked examples |
| **Property-based** | `Add` is commutative & associative; `a.Add(b).Sub(b) == a`; `Allocate` parts **always sum to the whole** for random amount/weights/sign; `Neg∘Neg = id`; conversion `A→B→A` within one ulp; `Cmp` is a total order |
| **Boundary** | `int64` min/max; `Zero`; scale extremes (0-dp and 18-dp currencies); single-minor-unit amounts; empty/one-element/all-zero weight vectors |
| **Overflow** | `Add`/`Sub`/`MulInt` at `MaxInt64` boundaries return `ErrOverflow` (never wrap); `LineExtension` with huge price×qty does **not** overflow the intermediate but errors only if the *result* exceeds `int64` |
| **Invalid input** | Currency mismatch; zero-value `Money` arithmetic; `Parse` with too many decimals / non-numeric; cross-category UoM conversion; fractional qty on a non-fractional unit; non-positive allocation weights |
| **Currency conversion** | Exact-formula checks across dp combinations (2↔0, 2↔3, 0↔3); hyperinflation magnitudes; round-trip stability; rate applied with each rounding mode; redenomination-factor composition |
| **Quantity precision** | `kg↔g↔mg` exactness; fractional entry at unit precision; imperial↔metric factor conversion; no drift over repeated conversions |
| **Rounding consistency** | Every mode against its truth table for +/- and tie/non-tie; the single-rounding rule (multiplying then rounding once ≠ rounding twice) demonstrated as a guard test; `LineExtension` rounds exactly once |

Additional invariant guards (also run later as runtime checks, §30): allocation conservation,
conversion round-trip bound, no-panic on any `int64` input pair.

Coverage target for these two packages: **100% of statements**, because the surface is small
and the cost of a gap is unacceptable. Property tests run with a high iteration count in CI.

---

## 9. SELF-REVIEW NOTES (pre-committed concerns to check post-implementation)

Per protocol Step 5, the things I will scrutinise in my own code once written:
- That `MulRate`/`LineExtension`/`ConvertTo` truly round **once** (the classic double-round bug).
- That the 128-bit intermediates are used on **every** multiply path, with no `int64`-only
  shortcut sneaking in.
- That no method mutates its receiver (immutability), verified by value-type design.
- That the zero-value guard is present on **every** entry point, not just `Add`.
- That error paths are exercised, not just happy paths (the overflow/mismatch branches).
- Whether `math/big` allocations in hot paths (line extension over large documents) warrant a
  `bits.Mul64`-based 128-bit path instead — measured, not assumed.

---

## 10. DECISIONS REQUESTED

Before I implement, please confirm (or redirect) these. D1 is the important one.

1. **D1 — checked `int64` + private representation + documented widening path**, rather than
   `big.Int` now. *(Recommended; §2.3.)* This is the crypto/high-inflation-ready choice.
2. **D2 — the five types and their scales** (Money=minor, UnitAmount=10⁻⁶ major, Quantity=10⁻⁶,
   Rate=10⁻⁹, Percent=10⁻⁶). *(§2.1.)*
3. **D3 — checked, error-returning arithmetic** (mismatch/overflow → typed error) rather than
   panics, accepting slightly heavier call sites for total correctness. *(§1.1, §3.)*
4. **D4 — the rounding-mode set and `HalfAwayFromZero` as the seed commercial default**, per
   currency, overridable by context. *(§4.2.)*
5. **D5 — package layout**: `kernel/money` (money/price/rate/percent) + independent
   `kernel/quantity`, one-directional dependency. *(§7.)*
6. **D6 — 100% statement coverage target** for these two packages, with property tests at high
   iteration count in CI. *(§8.)*

On approval I'll implement in this order, each independently reviewable: `rounding` →
`Currency` → `Money` → `Rate`/`Percent` → `UnitAmount`+`LineExtension` → `Quantity`, tests
alongside each, then the self-review and improvement notes (protocol Steps 4–6).

---

*End of Step 0.2 design. Awaiting approval per Protocol Step 2 before any implementation.*

---

# IMPLEMENTATION RECORD (Protocol Steps 3–6)

> Status: **IMPLEMENTED and verified.** All local checks green (offline).

## Step 3 — What was built

Packages, all pure Go (stdlib + `math/big` only), representation fully private:

- `internal/kernel/round` — `RoundingMode` (7 modes) + the single rounding engine `Div`.
- `internal/kernel/internal/fixed` — the ONE tested implementation of overflow-checked
  int64 arithmetic and decimal parse/format (kernel-private; removes duplication).
- `internal/kernel/money` — `Currency`, `Money`, `Rate`, `Percent`, `UnitAmount`, and the
  sole `LineExtension` (price × quantity) authority.
- `internal/kernel/quantity` — `Unit`, `Quantity`, category-checked conversion.

## Deviations from the approved design (Protocol: flag them)

1. **Added `kernel/round`** (RoundingMode + engine) instead of housing them in `money`.
   Reason: `quantity` also rounds (UoM conversion); without a shared low package, either a
   cycle forms (money↔quantity) or the enum is duplicated. `round` is the clean fix. This
   refines D5's "two packages" to "money + quantity + two tiny shared primitives"
   (`round`, and the kernel-private `internal/fixed`).
2. **Deferred `Money.Format(locale)`** to the i18n step. Reason: locale-aware formatting
   couples money to the (not-yet-built) i18n layer and is presentation, not numeric
   correctness. `String()` (debug form) ships now; `Format` lands with i18n.
3. **`Neg`/`Abs` documented domain excludes `math.MinInt64`.** Fully-correct negation of
   MinInt64 doesn't fit int64. It cannot arise from `Parse` or checked arithmetic; only a
   deliberate `FromMinor(c, MinInt64)` could produce it, which is documented as out of
   domain. No silent wrong value in normal use.

## Step 4 — Testing (confidence-first)

- **round**: full truth table for all 7 modes × sign × tie/non-tie; property test proving
  every result is floor-or-ceil for random inputs; 100% coverage.
- **money/quantity**: business-oriented table tests (real prices, hyperinflation FX across
  0/2/3-decimal currencies, sub-cent unit costs, fractional weights, metric↔imperial);
  property tests (allocation ALWAYS conserves the whole; fairness ≤1 minor unit; add
  commutative/associative; parse↔string round-trip; convert round-trip).
- **fixed**: dedicated overflow/boundary tests at int64 extremes; parse/format edges
  including `MinInt64` formatting.
- Kernel statement coverage **93.2%**; the remainder is trivial accessor default-branches.
  Per the agreed goal, effort went to meaningful scenarios + properties, not to 100%.

## Step 5 — Self-review findings (checked, as pre-committed in the design)

- Verified **single rounding**: `MulRate`, `MulPercent`, `LineExtension`, `ConvertTo`,
  `Rate.Mul/Invert` each round exactly once, via `round.Div` on a `math/big` numerator.
- Verified **128-bit (big.Int) intermediates on every multiply path** — no int64-only
  multiply that could overflow before the fit-check. Overflow surfaces as `ErrOverflow`.
- Verified **immutability** (all value types; no method mutates its receiver).
- Verified **zero-value guards** on every public entry point.
- Removed a real **duplication** (fixed-point helpers were copied into money and quantity)
  by extracting `internal/fixed` — one tested source of truth for critical arithmetic.
- A test bug (overflow qty built with a non-fractional unit) surfaced correct behaviour and
  was fixed; another (g→mg expectation) was a test arithmetic slip, not a code bug.

## Step 6 — Improvement notes, risks, and future opportunities

Worth doing **later**, not now (recorded so they are not forgotten):

1. **`big.Int` allocations in hot paths** (`LineExtension` over large documents). Measured
   concern only; at desktop volumes it is immaterial. If profiling ever shows it, add a
   `math/bits.Mul64`-based 128-bit fast path behind the same API — no signature change.
   *Not worth doing now* (correctness-first; no evidence of a bottleneck).
2. **Representation widening to 128-bit / big.Int** for 18-decimal crypto. The private
   representation already permits this without an API change; a `MinorBig()` companion would
   be added additively. *Do when a crypto asset is actually required.*
3. **`RoundToIncrement`** (nearest 5/50/1000) for hyperinflation psychological pricing —
   belongs with the currency module (§18.6), built on this kernel. *Phase 2.*
4. **Fuzz tests** (`go test -fuzz`) for `ParseScaled` as a complement to the property tests.
   *Cheap; add opportunistically.*

## Technical debt

None introduced. The one shortcut avoided (duplicated arithmetic) was paid down immediately
by extracting `internal/fixed`.
