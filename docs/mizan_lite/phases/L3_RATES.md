# Mizan Lite — Phase L3: exchange rates and the currency system

> **Status: COMPLETE — committed 2026-09-14.** D-L3.15–26 and D-L3.i1–i8 approved; **manual mode is the default** (§21.1).
> §§1–13 are the note as presented. **§14 records what changed at approval and supersedes the sections it names**: rates
> are fetched automatically from the internet as the primary mode, with the owner's manual rate as the fallback and
> override. §§15–21 are the implementation record.
> **Date:** 2026-09-14. **Base:** L2 (`407fdbb`). **Design:** [../DESIGN.md](../DESIGN.md) §4.2–§4.4, §5 `fx_rates`,
> §6.3, §10 (L3), with §13's amendments — Q1 (manual rates only) and Q7 (owner PIN to change the rate).

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what L3 must make possible, and eight things harder than they look |
| 2 | Scope — in, out, and three amendments to the approved design |
| 3 | The currency system — two currencies, one direction, where conversion is allowed to happen |
| 4 | The rate rules — typing, confirming, age, no rate at all |
| 5 | The arithmetic — three kernel additions, and the one example that must pass |
| 6 | The schema — `0004_fx.sql`, verified |
| 7 | The acts and reads |
| 8 | Who may do what |
| 9 | Bindings, and the screen that calls each one |
| 10 | Screens |
| 11 | Architecture rules and demo data |
| 12 | Tests, drills, Definition of Done |
| 13 | **Decisions and questions for approval** |
| 14 | **At approval: the dual-mode requirement, the owner's answers, and the design they led to** |
| 15 | What was built |
| 16 | Decisions made while building |
| 17 | Findings |
| 18 | Evidence |
| 19 | Mutation drills |
| 20 | Not verified |
| 21 | Definition of Done, and next |

---

## 1. ANALYSIS

### 1.1 What L3 must make possible

A pantry shop owner:

1. sets **today's rate** — *1 dollar = 15,000 pounds* — with the owner PIN (Q7), and corrects it when it was typed
   wrongly, with the mistake and the correction both kept;
2. sees, as does anyone at the counter, **the rate in force and how old it is**, and is told plainly when it is out
   of date;
3. sees what a dollar-priced jar of honey costs **in pounds** today, and what a pound-priced kilo of bulgur is **in
   dollars**, without the screen doing arithmetic;
4. receives a delivery paid in pounds with the rate **already filled in** — L2's form asked for it by hand until
   rates existed (Q-L2.1);
5. in owner mode, sees what the stock is worth **in pounds at today's rate**, beside its dollar value.

And L4's till, next, gets one thing it cannot work without: **a rate it can snapshot**, with its age.

### 1.2 Eight things harder than they look

**H1 — the rate typed wrong by a factor of a thousand.** Many Arabic-speaking shopkeepers write fifteen thousand as
`15.000`, with a point as the thousands separator. `numinput` refuses commas but must read `.` as a decimal point,
so `15.000` is **fifteen**. A rate one thousand times too small reprices every dollar product at a thousandth of its
price, and the till sells a $12 jar of honey for 12 pounds. The same error in the other direction is typing the
**inverse** — `0.0000667` dollars per pound. Neither is a validation error in the ordinary sense: both are positive
numbers. §4.2.

**H2 — rounding twice across currencies.** DESIGN §4.4's example, checked for this note: half a litre at $3.33 is
$1.665, rounded to **$1.67**; $1.67 × 13,000 is **21,710** pounds. The exact figure, 0.5 × 3.33 × 13,000, is **21,645**.
Sixty-five pounds on one line, accumulating down a receipt. The conversion must be one big-integer product rounded
once. §5.

**H3 — dividing by a rate by inverting it first.** Converting pounds to dollars as `amount × (1 ÷ 15,000)` rounds the
inverse (0.0000666…) and then the product: two roundings again. L2 met this for unit costs and added
`UnitAmount.DivideByRate`; L3 needs the same for line values and money amounts. §5.

**H4 — "the rate in force", ordered by a clock.** The approved design resolves the rate as the one with the latest
`effective_at ≤ now`. L2's finding D-L2.i1 applies with more force here: if the shop PC's clock is set back an hour,
the rate the owner types *now* carries an earlier timestamp than the one it replaces, **and the old rate stays in
force** — silently, with the new one visible in the history. A future-dated rate has the mirror problem: typed today
for tomorrow, it is in force the moment a wrong clock says tomorrow has come. §2.3 A-L3.1.

**H5 — a rate that never expires.** Mizan's own currency step designed staleness correctly and then reported a rate's
age **only on the fallback path** (Mizan Step 0.9 §9.1): a three-day-old rate resolved as though fresh. In a market
that moves daily, an old rate is a pricing error with a delay. Every read of the rate returns its age, on every path.
§4.3.

**H6 — no rate at all.** An installation set up in L1 or L2 has no rate when L3 arrives, and a new installation has
none until one is typed. The easy answer — treat a missing rate as 1 — is the one Mizan's converter refuses ("a rate
is never fabricated"). Every conversion must be able to say **"no rate"**, and every screen must show it rather than
a number. §4.4.

**H7 — a same-day correction must win, and must not erase the mistake.** Rates are append-only (DESIGN §4.3): the
wrong rate stays in the history, because sales made at it in L4 carry it. Mizan Step 0.9's D3 found an approved
uniqueness rule that made correcting today's rate **impossible**. §6.

**H8 — the pound itself may change.** DESIGN C8 recorded the announced redenomination removing two zeros. Whether a
shop today quotes the dollar at *15,000* or at *150* decides the demo data, the plausibility guard's behaviour on the
day of the change, and whether a second local currency code is needed. **I cannot verify its current status from
here** (Q-L3.4). L3 is built so that nothing depends on the magnitude.

---

## 2. SCOPE

### 2.1 In L3

| Area | What |
|---|---|
| Rates | the rate in force, its age, its history; set and correct, append-only (§7) |
| Guard | setting a rate needs the owner PIN (Q7); a large change needs an explicit confirmation (§4.2) |
| First run | today's rate joins the shop name, language and PIN (Q-L3.3) |
| Kernel | `LineExtensionMulRate`, `LineExtensionDivRate`, `Money.DivRate` — one rounding each (§5) |
| Conversions on screen | a product's price in the other currency; stock value in pounds (owner mode); all computed in Go |
| Stock | the receipt form's rate pre-filled from the rate in force (Q-L2.1) |
| Settings | `currency.local` declared (§3.2) |
| Screens | header rate and age for everyone; an **Exchange rate** screen; the first-run field; a column on Products |
| Demo data | an opening rate at first run, a later rate, and a same-day correction |

### 2.2 Not in L3

| Not here | Why | Where |
|---|---|---|
| Fetching a rate | Q1: manual only in v1 | — |
| Sales at a rate, snapshots on sales | there is no till yet; L3 provides what it will snapshot | L4 |
| Cash rounding to a note | Q5, confirmed in L4's note | L4 |
| Debts converted between currencies | debts arrive in L5, each in its own currency (Q2) | L5 |
| Redenominating the pound | a data operation over prices, costs and debts, not a rate; not needed unless Q-L3.4 says so | later, if asked |
| Rates for currencies other than local ↔ USD | two currencies in v1 (DESIGN §4.2) | — |
| Scheduled or back-dated rates | A-L3.1, Q-L3.7 | — |

### 2.3 Three amendments to the approved design

**A-L3.1 — the rate in force is the newest one recorded, not the latest `effective_at ≤ now`.** DESIGN §4.3 and §5
resolve by effective time, with `created_at` breaking ties. Both are the shop PC's clock (H4). In v1 a rate takes
effect **when it is typed**, and nothing needs "the rate at a past moment": a sale in L4 snapshots the rate's value and
id, L2's receipts keep the rate typed on them, and L6 reports read the sale's own rate (DESIGN C6). So each rate gets
its **place** — `seq`, 1, 2, 3 … per local currency, under a unique constraint, as L2 did for the ledger — and the
rate in force is the highest place. The time it was recorded is kept, for the history and for its age, and plays no
part in which rate applies. A same-day correction wins because it is newer, whatever the clock says (DESIGN's L3
Definition of Done, "same-day correction wins", holds by construction and is still tested).

**A-L3.2 — `fx_rates` loses `source` and `fetch_log_id`, and `fx_fetch_log` is not created.** Q1 removed the fetcher
from v1. A column nobody writes for five phases is a guarantee nobody enforces; if a fetcher comes, its log table and a
nullable reference column are an additive migration — unlike L2's CHECK, nothing here would need a rebuild.

**A-L3.3 — D6's `LineExtensionConverted` becomes two functions and a sibling.** The approved single function takes a
rate "to the target currency", which for pounds → dollars means an inverted rate — H3's double rounding. L3 adds
`LineExtensionMulRate` (dollars → pounds) and `LineExtensionDivRate` (pounds → dollars), named after the kernel's
existing `Money.MulRate` and L2's `UnitAmount.DivideByRate`, plus `Money.DivRate`. Each is one expression and one
rounding.

---

## 3. THE CURRENCY SYSTEM

### 3.1 What exists, and what L3 adds to it

| Fact | Since |
|---|---|
| Two currencies are data: `currencies(code, decimal_places)` seeds `SYP` (0) and `USD` (2) | L1 |
| A product is priced in either; its price is held at 10⁻⁶ of the major unit | L1 |
| Every unit cost is held in USD; a pound-costed receipt is converted once, at the rate typed on it, and keeps what was typed | L2 |
| **One rate direction: local currency per one US dollar**, at 10⁻⁹ (`local_per_usd_nano`). The inverse is never stored | **L3** (DESIGN §4.3) |
| **Conversion happens only in Go, only through the kernel**, and only in a function that rounds once | **L3** (DESIGN D9, §4.4) |
| **Every converted figure says which rate it used, and when that rate was recorded** | **L3** |

At 15,000 the stored rate is 1.5 × 10¹³, far inside `int64`; after a redenomination to 150 it is 1.5 × 10¹¹. Nothing
in L3 depends on which.

### 3.2 Which currency is "local"

The rate needs to know which currency it prices. With one non-USD currency seeded, it could be inferred — until a
redenomination seeds a second one (C8), when inference silently picks one. L3 declares the setting DESIGN §6.3 sketched,
**`currency.local`**, default `SYP`, read by the rates module (DESIGN A5: declared when something reads it). It is not
editable on screen in v1: changing it would orphan every rate recorded against the old code, and that is the
redenomination operation §2.2 leaves out.

### 3.3 Where a conversion may appear, and where it may not

| Figure | Converted? | Why |
|---|---|---|
| A product's price in the other currency, on Products | **yes**, at the rate in force, marked as a reference ("≈") | what the owner and cashier need to see; the till in L4 charges from the same computation |
| Stock value in pounds, owner mode | **yes**, per line, each rounded once, then summed | replacement value in the currency the shop's cash is in |
| A unit cost | **no** — always USD | D4 |
| A receipt's cost | already converted at the rate **typed on it**, in L2 | that rate is what the shop paid; the rate in force only pre-fills it |
| Anything in the frontend | **never** | D9; `formatDecimal` groups digits, nothing more |

---

## 4. THE RATE RULES

### 4.1 Typing a rate

- Normalised by `numinput` (Arabic-Indic digits, `٫`); a comma is refused as ever.
- **Positive, and at most four decimal places.** A pounds-per-dollar rate is quoted in whole pounds today, and in
  pounds and piastres after a redenomination; nobody quotes more than four decimals. The limit is also what refuses the
  **inverse** (H1): `0.0000667` has seven.
- The note is optional, up to 200 characters ("correction — typed 1,500").

### 4.2 A large change must be confirmed (H1)

If a new rate differs from the rate in force by **more than 20%** (Q-L3.2) in either direction, Go refuses it with
`lite.fx.large_change`, carrying the rate in force, the new rate and the change as a percentage. The screen shows both
figures side by side — *"The rate in force is 15,000. You typed 15. That is 99.9% lower."* — and the owner can go back
or **confirm**, which resends the same rate with `confirmLargeChange: true`.

The rule lives in Go, not in the dialog, so no future caller can skip it. It is not a permission — the owner is already
elevated — but a second look at the one number that reprices the whole shop. The first rate ever recorded has nothing
to compare with; §4.1's decimals limit is its protection, and first run shows the typed rate back in words before it
completes.

### 4.3 Age and staleness (H5)

Every read of the rate in force returns **its age** (whole seconds since it was recorded; never negative, if the clock
went back) and **whether it is stale**. Recommended rule (Q-L3.1): **stale when it was not recorded on today's
business date** — "the rate is from yesterday" is how a shopkeeper thinks, and it reuses L2's `bizdate`, so 01:00 in
Damascus is today. Re-entering the same figure the next morning is allowed and records a new rate: *the rate is still
15,000 today* is information, and it is what makes the warning go away.

A stale rate is a **warning, never a refusal** — in the header, on the rate screen, in the receipt form, and in L4 at the
till. Refusing to trade because nobody typed the rate this morning would stop the shop.

### 4.4 No rate at all (H6)

"No rate" is a state, not an error and not a number:

| Where | With no rate |
|---|---|
| Header | *"No exchange rate — set one"*, linking to the rate screen |
| Products | the converted column shows "—" |
| Stock valuation | the pound column shows "—"; the dollar figures are unaffected |
| Receipt form | the rate field is empty, as in L2 |
| L4's till | will refuse a sale that needs a conversion, with the same message — its note decides |

Nothing converts at 1, and no function returns a zero rate: the reader returns `found = false`.

---

## 5. THE ARITHMETIC

### 5.1 Three kernel additions

All in `kernel/money`, integer-only, one rounding, `big.Int` intermediates, available to Mizan:

```
LineExtensionMulRate(price UnitAmount, qty Quantity, rate Rate, to Currency, mode) Money
    to.minor = round( price.micro × qty.micro × rate.nano × 10^to.decimals  ÷  (10⁶ × 10⁶ × 10⁹) )

LineExtensionDivRate(price UnitAmount, qty Quantity, rate Rate, to Currency, mode) Money
    to.minor = round( price.micro × qty.micro × 10^to.decimals × 10⁹  ÷  (10⁶ × 10⁶ × rate.nano) )

Money.DivRate(rate Rate, to Currency, mode) Money                      — the sibling of the existing MulRate
    to.minor = round( minor × 10^to.decimals × 10⁹  ÷  (10^from.decimals × rate.nano) )
```

`rate` is always *units of the local currency per one USD*; the caller picks multiply (USD → local) or divide
(local → USD). The rate is never inverted.

### 5.2 The example that must pass

`TestHalfALitreAt333At13000Is21645NotTheTwiceRounded21710` — DESIGN §4.4 as a test, and its drill: compose
`LineExtension` then `Money.MulRate` and the test must fail with 21,710.

### 5.3 Property tests, at 0, 2 and 3 decimals

- `LineExtensionMulRate` equals the exact rational value rounded once, for random prices, quantities and rates;
- converting to pounds and back with `LineExtensionDivRate` stays within the rounding the two steps allow — the bound is
  stated in the test, not assumed (L2's `SplitUnitCost` bound was first stated wrong and a property test corrected it);
- `Money.MulRate` then `Money.DivRate` returns the amount within one minor unit whenever the middle currency is at least
  as fine as the source — dollars → pounds → dollars comes back to the cent at 15,000 — and the test names the case where
  it cannot: pounds → dollars → pounds loses up to one cent's worth of pounds (150 at 15,000), by design.

### 5.4 Stock value in pounds

Per product: `LineExtensionMulRate(average cost, on hand, rate, local)`, then summed. Never the dollar total times the
rate: the dollar total is already rounded to cents, and rounding it again in pounds is H2 over a whole shop.

---

## 6. THE SCHEMA — `0004_fx.sql`

```sql
-- Exchange rates. INSERT-only (a test scans the store's SQL). Owned by the fx module.
-- One direction: local currency per ONE US dollar, at 10⁻⁹ (DESIGN §4.3). The inverse is never stored.
CREATE TABLE fx_rates (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  -- The rate's place: 1, 2, 3 … per local currency. The rate in force is the highest place — never the latest
  -- timestamp, which is the shop PC's clock (A-L3.1). Two acts that both read "the newest" cannot both append.
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),
  local_per_usd_nano  BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),
  business_date       CHAR(10)     NOT NULL,     -- the shop's day it was recorded on: what "stale" is measured by (§4.3)
  recorded_at         CHAR(24)     NOT NULL,     -- UTC; for the history and the age, never for resolution
  note                VARCHAR(200),
  CONSTRAINT ck_fx_rates_local_is_not_usd CHECK (local_currency <> 'USD'),
  CONSTRAINT ux_fx_rates_place UNIQUE (local_currency, seq)
);
```

**Notes.**

- **Verified against SQLite before this note was presented**, on top of `0001`–`0003`, with foreign keys on: two rates
  in places 1 and 2 accepted (the second a correction the same day); refused, each by the constraint named — a second
  rate in place 2 (`ux_fx_rates_place`), a USD "local" currency (`ck_fx_rates_local_is_not_usd`), a currency that does not
  exist (foreign key), a zero rate, place 0, and a NULL rate, currency or place (`NOT NULL`). The newest place resolved to
  the correction.
- **No nullable comparison in any CHECK** — L2's lesson. `note` is the only nullable column and no constraint reads it.
- **No index beyond the unique constraint**: "the newest rate" and "the history, newest first" are both
  `ORDER BY seq DESC` on it.
- **Why no `effective_at`** is A-L3.1. **Why no `source`** is A-L3.2.
- The history of **who** set a rate is the owner's event history (`owner_events`), where `Require` records the act with
  the rate before and after — not a column here.

---

## 7. THE ACTS AND READS

| Act or read | Input | Rules | Result |
|---|---|---|---|
| **Set the rate** | rate, note, `confirmLargeChange` | owner PIN (§8); §4.1; §4.2; one transaction: read the newest place, append the next | the rate in force, with its age |
| **Rate in force** | — | the highest place for `currency.local`; age and staleness (§4.3) | rate, recorded at, age, stale — or none |
| **History** | limit (≤ 500) | newest first; each row carries its change from the one before, as a percentage computed in Go | rows |
| **First run** | + rate | the opening rate is recorded **inside first run's transaction**, without owner mode — first run is where the PIN is created; a refused rate leaves no shop, no PIN (D-L1.11) | as L1 |
| **Convert for display** | a price, or a stock line | §5 functions at the rate in force; returns the figure **and** the rate's place, or none | used by Products and Stock |

A rate equal to the rate in force is **accepted and recorded** (§4.3): it confirms the rate for the day.

---

## 8. WHO MAY DO WHAT

| Act or read | Owner PIN | Why |
|---|---|---|
| See the rate, its age, its history | no | the cashier prices from it |
| See a price in the other currency | no | it is a price |
| **Set or correct the rate** | **yes** (Q7) | one number reprices every dollar product |
| Record the opening rate at first run | no — first run creates the PIN in the same transaction | D-L1.11 |
| See stock value in pounds | owner mode (Q-L2.4) | it is a cost figure |

The guard is the one L1 built: `fx` declares an `OwnerGate` port, `Require` records the act — `fx.rate.set`, before
*15,000*, after *15,200* — inside the rate's own transaction, and a rolled-back rate leaves no record.

---

## 9. BINDINGS AND THEIR CALLERS

| Façade.Method | Guard | Caller |
|---|---|---|
| **`FX.Current`** (query) — rate, recorded at, age in seconds, stale, or `set: false` | — | Shell header, RatesScreen, ReceiveDialog (pre-fill) |
| **`FX.History`** (limit) | — | RatesScreen |
| **`FX.SetRate`** ({rate, note, confirmLargeChange}) | owner | RatesScreen (through `withOwner`, then the large-change dialog) |
| `App.CompleteFirstRun` — input gains `rate` | — | FirstRun |
| `Catalog.Products` / `Catalog.Product` — `ProductDTO` gains `convertedPrice` and `convertedCurrency` (both empty with no rate) | — | ProductsScreen |
| `Stock.Valuation` — lines gain `valueLocal`; the total gains `totalLocal`, `localCurrency` and the rate used (all empty with no rate) | owner mode | StockScreen |

**3 new methods, 37 in total.** Route **`/rates`**. Rates and converted amounts cross as decimal strings formatted by
Go; the age crosses as whole seconds and is worded by `Intl.RelativeTimeFormat` with Latin digits (Q6), so Arabic's
plural forms ("منذ ساعة", "منذ ساعتين", "منذ 3 ساعات") come from the platform, not from hand-written rules.

---

## 10. SCREENS

| Screen | Change |
|---|---|
| **Header** (everyone) | `1 USD = 15,000 SYP · 3 hours ago`; amber *"from yesterday"* when stale; *"No exchange rate — set one"* when none; each links to `/rates` |
| **Exchange rate** (`/rates`, new) | the rate in force, large, with its age; *Set today's rate* (rate, note) through the PIN; the large-change confirmation showing both figures; history: date, time, rate, change %, note |
| **First run** | *Today's exchange rate (pounds per dollar)*, with the typed figure repeated beneath it, grouped, before submitting ("1 USD = 15,000 SYP") — so `15.000` reads back as "1 USD = 15 SYP" |
| **Products** | a column *In the other currency*: `≈ 97,500 SYP` for a $6.50 product, `≈ 1.20 USD` for an 18,000-pound one, "—" with no rate |
| **Receive stock** (L2 dialog) | the rate pre-filled from the rate in force, still editable, with the stale warning beside it |
| **Stock** (owner mode) | *Value (SYP at today's rate)* column and total, "—" with no rate |

---

## 11. ARCHITECTURE RULES AND DEMO DATA

### 11.1 Rules

| Change | Why |
|---|---|
| **`lite-fx-isolated`** (new): `fx` imports no other Lite module | it reaches the owner and the settings through ports |
| `lite-catalog-isolated`, `lite-owner-isolated`, `lite-settings-isolated`, `lite-stock-isolated`, `lite-setup-reaches-only-its-ports` forbid `fx` | setup reaches it through a port it declares, as it reaches settings and owner |
| `kernel-purity` unchanged | the additions import only the kernel |

Each gets a plant in `scripts/lite-arch-drill.sh`.

### 11.2 Demo data

`lite-demoseed` sets the opening rate **14,800** at first run; opening stock is typed at it (as L2's data already is);
then a new rate **15,200**, then a same-day **correction to 15,000** with the note *"typed wrong"* — so the history shows a
correction and the rate in force is the corrected one; then L2's pound deliveries at 15,000. The seeder asserts the rate
in force is 15,000 at place 3, that a dollar product shows its pound price, and that the verifier still finds nothing.
All three rates are recorded "today": the seeder drives the services on the real clock, as a person would.

---

## 12. TESTS, DRILLS, DEFINITION OF DONE

### 12.1 Tests (names are the contract)

| Layer | Tests |
|---|---|
| **kernel** | `TestHalfALitreAt333At13000Is21645NotTheTwiceRounded21710` · `TestLineExtensionMulRateRoundsOnce` (property, 0/2/3) · `TestLineExtensionDivRateNeverInvertsTheRate` (property) · `TestMoneyDivRateRoundTripsWithinTheStatedBound` · refusals: zero or negative rate, no currency, overflow |
| **fx/domain** | `TestARateTakesAtMostFourDecimals` (the inverse refused) · `TestALargeChangeNeedsConfirmationBothWays` · `TestTheFirstRateHasNothingToCompareWith` · `TestAgeIsNeverNegative` · `TestStaleMeansNotRecordedToday` (01:00 Damascus is today) · `TestChangePercentIsComputedOnce` |
| **fx service (fake + contract)** | `StoreContract` on fake and SQLite · `TestTheNewestPlaceIsInForceWhateverTheClockSays` (clock set back an hour) · `TestASameDayCorrectionWins` · `TestSettingTheRateNeedsTheOwner` · `TestTheSameRateAgainIsRecordedAndClearsStale` · `TestNoRateIsAStateNotANumber` · `TestEveryReadCarriesItsAge` (Mizan 0.9 §9.1 as a test) |
| **the database** | `TestRatesAreInsertOnly` (SQL scan) · `TestACheckRefusesAnImpossibleRate` — each §6 constraint by raw insert, asserting **which** constraint refused (L2 R3) · `TestTwoRatesCannotTakeOnePlace` |
| **first run** | `TestAnOpeningRateIsRecordedWithTheShop` · `TestARefusedRateLeavesNoShopBehind` |
| **conversions** | `TestAProductShowsItsPriceInTheOtherCurrency` (both directions) · `TestStockValueInPoundsIsSummedPerLineNotConverted` · `TestNoRateMeansNoConvertedFigure` |
| **bindings** | wire shapes · `FX.SetRate` refused outside owner mode · large change refused, then accepted with confirmation · `FX.Current` with and without a rate |
| **frontend** | header shows rate and age, stale warning, no-rate link · setting a rate goes through the PIN, then the large-change dialog, then succeeds · the history shows a correction · first run repeats the rate back · the receipt form pre-fills and stays editable · the Products column shows Go's string and "—" with no rate · the age is worded in Arabic and English |
| **seeder** | the §11.2 assertions |

### 12.2 Drills planned

At least: `LineExtension` then `MulRate` composed instead of one expression (the 21,645 test fails with 21,710); the rate
inverted before dividing; the rate in force chosen by `recorded_at` (fails with the clock set back); the large-change
check moved from Go to the dialog only (the binding test fails); `SetRate` without the guard; age reported only when
stale; a missing rate returned as 1; the four-decimal limit removed (the inverse accepted); stock value in pounds computed
from the dollar total; the first-run rate written outside its transaction.

### 12.3 Definition of Done

> `make lite-ci` green · every invariant drilled · the seeder's rates open in the packaged app and the header shows the
> corrected rate · the rate screen looked at in Arabic and English (owner, O5) · Windows `.exe` builds · Mizan's
> `scripts/check.sh` green (the kernel changes) · [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md)
> and this document updated.

---

## 13. DECISIONS AND QUESTIONS FOR APPROVAL

### 13.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L3.1 | The `fx` module owns `fx_rates`; insert-only; one direction, local per USD at 10⁻⁹; the inverse never stored | 3.1, 6 |
| D-L3.2 | The rate in force is the newest **place** (`seq`), not the latest timestamp; no scheduled or back-dated rates (amends DESIGN §4.3, §5) | 2.3 A-L3.1 |
| D-L3.3 | No `source`, no `fetch_log_id`, no `fx_fetch_log` in v1 (Q1) | 2.3 A-L3.2 |
| D-L3.4 | Kernel: `LineExtensionMulRate`, `LineExtensionDivRate`, `Money.DivRate` in place of D6's `LineExtensionConverted`; §4.4's example is a test | 2.3 A-L3.3, 5 |
| D-L3.5 | A rate is typed with at most four decimals and must be positive | 4.1 |
| D-L3.6 | A change beyond the threshold is refused in Go unless confirmed | 4.2 |
| D-L3.7 | Every read carries the rate's age and staleness; staleness warns, never refuses | 4.3 |
| D-L3.8 | "No rate" is a state; nothing converts at 1 | 4.4 |
| D-L3.9 | Setting a rate needs the owner PIN and is recorded in the owner's history in the rate's transaction; recording the same figure again is allowed | 7, 8 |
| D-L3.10 | `currency.local` declared (default `SYP`), read by `fx`, not editable on screen in v1 | 3.2 |
| D-L3.11 | Conversions appear only as Go-computed strings; stock value in pounds is summed per line | 3.3, 5.4 |
| D-L3.12 | The receipt form pre-fills the rate in force; the rate typed on the receipt is still what the receipt keeps (Q-L2.1) | 3.3 |
| D-L3.13 | Everyone sees the rate and its age in the header | 8, 10 |
| D-L3.14 | `lite-fx-isolated`; every other isolation rule forbids `fx`; setup reaches it through a port | 11.1 |

### 13.2 Questions only you can answer

**Q-L3.1 — When is a rate "out of date"?** *Recommended: when it was not set today* (the shop's day). The owner then
types the rate — or the same rate again — each morning, with the PIN, and the header warns until they do. The
alternative is an age in hours (for example, 24), which does not warn on a rate typed at 10 p.m. and used at 9 a.m., or a
setting the owner can change.

**Q-L3.2 — How large a change needs a second look, and does it apply to receipts?** *Recommended: 20%, for the rate; and
the same check on a rate typed on a pound receipt, against the rate in force.* A day's move beyond 20% is rare; a
factor of a thousand (`15.000`) is the error this exists for. On receipts, the typed rate becomes the cost of the stock,
so the same typo does lasting damage there — checking it costs one confirmation on a genuinely unusual delivery.

**Q-L3.3 — Must a new shop type the rate at first run?** *Recommended: yes.* A shop with dollar prices cannot price
anything in pounds without it, and first run is where the owner is already at the keyboard with the PIN. Installations
that already exist (set up in L1–L2) are not sent back to first run; the header asks them to set one.

**Q-L3.4 — Which pound does the counter use today?** The redenomination removing two zeros (DESIGN C8): is the dollar
quoted at around **15,000** (old pounds) or around **150** (new pounds)? And is the currency code still `SYP`? L3 works
with either, but the answer decides the demo data, what the large-change check does on the day of a change (a 100×
change will ask for confirmation — correctly), and whether a real redenomination operation over prices, costs and
debts must be planned before L8. *I cannot verify the current status from here.*

**Q-L3.5 — Show each product's price in the other currency on the Products screen?** *Recommended: yes, both directions*
— a dollar product in pounds and a pound product in dollars, marked "≈", computed by Go at the rate in force.

**Q-L3.6 — Show stock value in pounds, in owner mode?** *Recommended: yes*, beside the dollar value, at today's rate. It is
what the shelves would cost to refill in the currency the till takes. Profit in pounds remains L6's, at each sale's own
rate (DESIGN C6).

**Q-L3.7 — Is "a rate takes effect when it is typed" enough?** *Recommended: yes for v1.* No typing tomorrow's rate
today, and no back-dating. Both would bring back time-based resolution and the clock problem of H4 for a need nobody has
stated yet.

---

## 14. AT APPROVAL (2026-09-14): DUAL RATE MODES, THE OWNER'S ANSWERS, AND THE DESIGN THEY LED TO

### 14.1 The requirement

> **Primary / automatic mode:** fetch the exchange rate automatically from an external, international API when the
> laptop has an internet connection. **Secondary / manual mode (fallback):** the owner sets or overrides the rate
> manually with the owner PIN — especially offline, or to adjust for local market rates.

This reverses Q1 (manual only) for rates. It supersedes **A-L3.2 and D-L3.3** (no fetch log), extends §4, §6, §7, §8, §9
and §10 as below, and leaves the other thirteen approved decisions standing.

### 14.2 The owner's answers

| Q | Answer | Effect |
|---|---|---|
| Q-L3.1 | Out of date when not updated today; prompt for an internet refresh or the owner's re-confirmation | §4.3 as proposed; the header offers **Update now** and **Set manually** |
| Q-L3.2 | 20%; a shift beyond it — fetched or typed — needs the owner PIN and confirmation | a fetched rate beyond 20% is never applied automatically: it becomes a **proposal** the owner accepts (§14.5) |
| Q-L3.3 | A rate is required at first run, fetched or typed | first run offers **Fetch from the internet**, which fills the field for the owner to confirm |
| Q-L3.4 | Code `SYP`; market thousands, around 15,000 per USD | the demo data and the providers' scale (§14.3) |
| Q-L3.5 | Prices in both currencies on Products | as §10 |
| Q-L3.6 | Stock value in SYP beside USD in owner mode | as §5.4, §10 |
| Q-L3.7 | A rate takes effect immediately, fetched or typed | as A-L3.1 |

### 14.3 What the internet actually says — checked before designing (2026-09-14)

Every free, key-less source reachable from here was queried for USD → SYP:

| Source | Reports | Reading |
|---|---|---|
| `open.er-api.com` (ExchangeRate-API, open access) | **121.957862** | the **new**, redenominated pound — two zeros removed |
| `@fawazahmed0/currency-api` (jsDelivr, and its `pages.dev` mirror) | **13,007.53553648** | the **old** pound — an official-style figure |
| `frankfurter.app` (ECB) | no SYP | — |
| `exchangerate.host` | requires a key now | — |

Two findings follow, and the design answers both:

**F1 — the providers disagree by a factor of about 100.** One already quotes the new pound under the same code. A rate
taken from it as-is is H1 at 100×. The providers are therefore **ordered and scaled explicitly** (§14.6), and the 20%
guard means no scale mistake, today's or a provider's future change, can ever be applied without the owner.

**F2 — "the internet rate" is not the market rate.** 13,007 is **13.3% below** the market 15,000 the owner quotes (Q-L3.4)
— *inside* the 20% threshold. A naive automatic mode would replace the owner's market rate with it every hour and reprice
every dollar product about 13% cheaper, silently. This is the risk DESIGN C4 named. The design holds the owner's rate in
three ways (§14.5): a manual rate **holds for the rest of its business day**; **manual mode** keeps fetching for reference
but never applies; and every fetch is shown beside the rate in force, so the gap is visible, never absorbed.

### 14.4 The two modes

| | **Automatic** | **Manual** (the default — §21.1) |
|---|---|---|
| Fetches | at launch, then hourly, and on **Update now** | the same — for reference |
| A fetched rate | applied immediately (§14.5 rules) | never applied; shown as *"Internet rate: 13,007.5"* |
| A manual rate (PIN) | allowed; **holds until the next business day**, then fetching resumes | the only way a rate changes |
| Out of date | *Update now* or *Set manually* | *Set manually* |
| Switching mode | owner PIN, recorded in the owner's history | owner PIN |

The mode is a setting, `fx.mode` (`automatic` | `manual`), read and written only through the fx module, because changing
it needs the owner.

### 14.5 What a fetched rate does — one decision, in the domain

In this order, after the quote is fetched **outside any transaction** (a 10-second timeout must never hold the database's
single writer) and rounded half-up to four decimals (§4.1):

| # | Condition | Outcome | A rate is recorded? |
|---|---|---|---|
| 1 | mode is manual | `held_mode` | no |
| 2 | no rate in force | `proposed` — **the first rate is always a person's** | no |
| 3 | the rate in force was set **manually today** | `held_today` | no |
| 4 | the change from the rate in force exceeds 20% | `proposed` | no |
| 5 | same figure as the rate in force, and that rate is from today | `unchanged` | no |
| 6 | otherwise | `applied` — including the same figure on a new day, which is the daily confirmation Q-L3.1 asks for | **yes**, source `fetched` |

Every attempt, including a failure, is one row in `fx_fetches` with an error **code**, never provider text. A proposal
is accepted with the owner PIN, and only while it is still the newest fetch and the rate in force is still the one it was
compared against — a proposal from this morning is not accepted this afternoon over a rate set since.

### 14.6 Providers

A `Source` port in `fx`; one implementation, `fx/infra/httpsource`, the only Lite package allowed to import `net/http`
(a new architecture rule). Tried in order until one answers:

| # | Provider | URL | Scale to old pounds |
|---|---|---|---|
| 1 | `currency-api-jsdelivr` | `cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json` | × 1 |
| 2 | `currency-api-pages` | `latest.currency-api.pages.dev/v1/currencies/usd.json` (the same data, a second host) | × 1 |
| 3 | `exchangerate-api-open` | `open.er-api.com/v6/latest/USD` | **× 100** (it reports the new pound, F1) |

Numbers are read as JSON **numbers-as-text** (`json.Decoder.UseNumber`) and parsed as exact decimals — no float ever
holds a rate. A response over 1 MB, a non-200 status, a missing currency, or a non-positive rate is a typed failure. The
client has a 10-second timeout, honours the operating system's proxy settings (`http.ProxyFromEnvironment`), and uses the
system certificate store on both Windows and macOS (Go's default). "Offline" is not detected in advance: the attempt fails
with `lite.fx.fetch_offline`, the header says so, and manual entry is always available.

**No test touches the internet.** The composition root registers the real providers only when the application asks for
them (`apps/lite`); every test passes fakes or a local `httptest` server with the providers' real response shapes, captured
today. One live test, `TestTheRealProvidersAnswer`, runs only with `LITE_FX_LIVE=1` and is reported as NOT RUN otherwise.

### 14.7 The schema, revised — `0004_fx.sql`

```sql
CREATE TABLE fx_fetches (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),             -- attempts in order, never by the clock
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  attempted_at        CHAR(24)     NOT NULL,
  business_date       CHAR(10)     NOT NULL,
  outcome             VARCHAR(12)  NOT NULL CHECK (outcome IN
                        ('applied', 'unchanged', 'proposed', 'held_mode', 'held_today', 'failed')),
  provider            VARCHAR(32),                                          -- who answered; NULL when nobody did
  local_per_usd_nano  BIGINT,                                               -- scaled and rounded (§14.6)
  against_rate_id     CHAR(36)     REFERENCES fx_rates(id),                 -- the rate in force it was compared with
  error_code          VARCHAR(64),
  CONSTRAINT ux_fx_fetches_place UNIQUE (local_currency, seq),
  CONSTRAINT ck_fx_fetch_answered CHECK (
    (outcome = 'failed'  AND provider IS NULL AND local_per_usd_nano IS NULL AND error_code IS NOT NULL) OR
    (outcome <> 'failed' AND provider IS NOT NULL AND local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0
                         AND error_code IS NULL))
);

CREATE TABLE fx_rates (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),
  local_per_usd_nano  BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),
  source              VARCHAR(12)  NOT NULL CHECK (source IN ('first_run', 'manual', 'fetched')),
  fetch_id            CHAR(36)     REFERENCES fx_fetches(id),
  business_date       CHAR(10)     NOT NULL,
  recorded_at         CHAR(24)     NOT NULL,
  note                VARCHAR(200),
  CONSTRAINT ck_fx_rates_local_is_not_usd CHECK (local_currency <> 'USD'),
  CONSTRAINT ck_fx_rates_fetched_names_its_fetch CHECK ((source = 'fetched') = (fetch_id IS NOT NULL)),
  CONSTRAINT ux_fx_rates_place UNIQUE (local_currency, seq),
  CONSTRAINT ux_fx_rates_fetch_applied_once UNIQUE (fetch_id)
);
```

Both tables are insert-only. Every nullable column a CHECK reads is guarded with `IS NULL` / `IS NOT NULL` (L2's lesson),
and the implementation's schema test inserts each NULL case.

### 14.8 Acts, guards and bindings, revised

| Façade.Method | Guard | Caller |
|---|---|---|
| `FX.Current` — rate, source, age, stale, **mode**, and the **last fetch** (outcome, provider, rate, change, whether it can be accepted) | — | header, RatesScreen, ReceiveDialog |
| `FX.History` | — | RatesScreen |
| `FX.SetRate` | owner; 20% confirmation | RatesScreen |
| **`FX.Refresh`** — fetch now and decide (§14.5); a second press within 60 s answers from the last attempt without calling out | — | header *Update now*, RatesScreen |
| **`FX.AcceptProposal`** (fetch id) | owner | RatesScreen proposal banner |
| **`FX.SetMode`** (`automatic` \| `manual`) | owner | RatesScreen |
| **`FX.FetchQuote`** — fetch and return the quote, recording nothing | — | FirstRun *Fetch from the internet* |

**7 new methods, 41 in total.** A scheduled job, `lite.fx.refresh`, runs `Refresh` hourly, and once at launch
(`RunOnce` catch-up), only when providers are registered.

### 14.9 Decisions made at approval, to meet the requirement (for review)

| # | Decision | § |
|---|---|---|
| D-L3.15 | Two modes, `fx.mode`: automatic (default) and manual; switching needs the owner PIN | 14.4 |
| D-L3.16 | The fetch decision of §14.5, in the domain, in that order | 14.5 |
| D-L3.17 | The first rate is always a person's: with no rate in force a fetch is only a proposal | 14.5 |
| D-L3.18 | A manual rate holds for the rest of its business day in automatic mode | 14.3 F2, 14.5 |
| D-L3.19 | A fetch beyond 20% is a proposal accepted with the PIN, only while it is newest and its comparison still holds | 14.5 |
| D-L3.20 | Every attempt is logged in `fx_fetches` with a code; fetched rates name their fetch | 14.7 |
| D-L3.21 | The network call never runs inside a transaction | 14.5 |
| D-L3.22 | Three providers in order, explicitly scaled; exact decimal parsing; fetched rates rounded to four decimals | 14.6 |
| D-L3.23 | `net/http` only in `fx/infra/httpsource` (new architecture rule); no test reaches the internet; one opt-in live test | 14.6 |
| D-L3.24 | Refresh open to everyone, throttled to one attempt a minute; accepting a proposal and switching mode are the owner's | 14.8 |
| D-L3.25 | First run can fetch a quote that fills the rate field; the owner still confirms it | 14.2 |
| D-L3.26 | Providers are registered by `apps/lite`, not by default: the seeder and every test run offline by construction | 14.6 |

---

## 15. WHAT WAS BUILT

| Layer | Packages | What |
|---|---|---|
| Kernel | `kernel/money/exchange.go` | `LineExtensionMulRate`, `LineExtensionDivRate`, `Money.DivRate` — one big-integer expression, one rounding each; DESIGN §4.4's example as a test |
| Schema | `migrations/sqlite/0004_fx.sql` | `fx_fetches` and `fx_rates` (§14.7), insert-only |
| Settings | `settings/domain` | `fx.mode` (automatic \| manual, default automatic), `currency.local` (read-only, default `SYP`) |
| Rates | `fx`, `fx/domain`, `fx/infra/sqlite`, `fx/fxtest` | the rate in force by place; typed rates (4 decimals); the fetch decision (§14.5); proposals; modes; age and staleness; conversions; `Settings`, `Source` and `OwnerGate` ports; fake store, settings, scripted source; store contract on fake **and** SQLite |
| Internet | `fx/infra/httpsource` | three providers in order, explicitly scaled; exact decimal parsing; 10 s timeout; system proxy and certificate store; offline vs failed codes; response fixtures captured 2026-09-14; one opt-in live test |
| First run | `setup` | today's rate in first run's transaction, through a `Rates` port |
| Composition | `bootstrap`, `apps/lite` | `Options.RateSource` (nil by default) and `RateRefreshEvery`; job `lite.fx.refresh` hourly with launch catch-up, only when a source is registered; adapters `fxGate`, `fxSettings`, `setupRates`; the real providers registered in `apps/lite/main.go` only |
| Bindings | `api/fx.go`, `api/catalog.go`, `api/stock.go`, `api/app.go` | `FX` (7 methods) — **41 in total**; `ProductDTO.convertedPrice/convertedCurrency`; valuation `valueLocal`, `totalLocal`, `localCurrency`, `rate`; `FirstRunInput.rate`; every age in seconds from Go |
| Seeder | `demoseed` + `data/rates.json` | opening rate 14,800 at first run; 15,200 then a same-day correction to 15,000, through the PIN; no provider — offline |
| Frontend | `rates/RateProvider`, `screens/rates/RatesScreen`, `app/Shell`, `app/FirstRun`, `screens/products`, `screens/stock`, `i18n/time` | header rate with age and "not updated today"; `/rates`: rate in force, mode switch, last internet check and its outcome, **Update now**, proposal banner with **Accept**, manual rate with the large-change confirmation, history; first-run rate with **Fetch from the internet** and a read-back; products in the other currency; stock value in pounds; the receipt form pre-filled |
| Catalogs | `internal/lite/locales` | +15 error codes and +68 interface strings per language (293 and 109 keys now) |
| Rules | `arch-rules.yml`, `scripts/lite-arch-drill.sh`, `tools/archlint` | `lite-fx-isolated`; every other isolation rule forbids `fx`; **`lite-network-only-in-httpsource`**; archlint's import boundaries gained `except`; **30 rules seen failing on every run** |

## 16. DECISIONS MADE WHILE BUILDING (for review)

**D-L3.i1 — archlint's import boundaries gained an `except` list.** "Only `httpsource` may import `net/http`" could not be
written: an import rule applied to a package pattern with no way to leave one package out. The field is four lines in
the shared tool, and Mizan's `scripts/check.sh` is green with it.

**D-L3.i2 — the fetch's age is measured in Go.** A first draft of the rate screen computed the last check's age from its
timestamp and the webview's clock, which DESIGN D9 and L1's countdown rule forbid. `FetchDTO.ageSeconds` carries it.

**D-L3.i3 — `Refresh` is three steps in the code's shape, not only in its comments:** read the last attempt, call the
network, then `record` in one transaction. The linter's shadowing warning prompted the split; it makes D-L3.21 (no
network call inside a transaction) visible in the structure.

**D-L3.i4 — first run reads the local currency from Go** (`FX.Current().localCurrency`) for its label, rather than naming
SYP in the frontend (DESIGN C8).

**D-L3.i5 — the typed rate is read back without trailing zeros** ("15.000" shows as "1 USD = 15 …"), so H1's ambiguity is
seen before it is saved. Text, not arithmetic.

**D-L3.i6 — provider names are catalog strings**, and ExchangeRate-API's appears as *"Rates by Exchange Rate API"*
wherever its rate is shown, as its open-access terms ask.

**D-L3.i7 — a failed or unchanged fetch is not a failed job.** Being offline is ordinary; the job logs the outcome and the
header shows it. A job fails only on a database error.

**D-L3.i8 — the rate screen's "Update now" is disabled, with a sentence saying why, when the application has no provider**
— which only a test build or the seeder's graph can be.

## 17. FINDINGS

**R1 — the internet disagrees with itself and with the market** (§14.3 F1, F2): 121.96 vs 13,007.54 for the same code;
13,007.54 is 13.3% below the owner's 15,000. Found by querying the providers before designing, and answered by explicit
scales, the 20% guard, the proposal rule and the manual-rate hold.

**R2 — two drills survived first, each exposing a test that could not fail:**
- the store contract's "newest place wins whatever the clock says" put the newest place at the latest timestamp too, so
  ordering by time also passed — the newest rate now carries the oldest timestamp;
- no test read the valuation's pound **total**, so a total converted from the rounded dollar figure passed — a two-line
  test now shows 43,290 against the 43,420 that conversion gives.

**R3 — one drill did not compile** (removing the guard left a variable unused). Refused as a result and redone as a
compiling mutation, per R2 of DESIGN §13.2.

**R4 — `encoding/json` reads a quoted number into `json.Number`.** A provider writing `"13007.5"` would be accepted —
exactly, as text; a quoted non-number is refused. The test was corrected to what the decoder does.

**R5 — the "Arabic catalog is actually Arabic" gate refused a provider's name in Latin letters** in the Arabic file; the
Arabic entry now says *مزوّد Currency API*.

**R6 — golangci-lint v2: 2 issues** (shadowed `err`), both fixed (D-L3.i3); 0 issues.

## 18. EVIDENCE

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN except as below |
| Go tests (race) | **268** Lite test functions, **499** with subtests, 0 failures (L2: 220 / 415); kernel conversions property-tested at 0, 2 and 3 decimals |
| Live providers | `TestTheRealProvidersAnswer` is **NOT RUN** in `lite-ci`. Run by hand with `LITE_FX_LIVE=1` on 2026-09-14: **pass** — jsDelivr 13,007.5355, pages.dev 13,007.5355, ExchangeRate-API 12,195.7862 (×100) |
| Frontend | **256** tests in 21 files, 0 console warnings; bundle gate 4 |
| golangci-lint v2 | 0 issues |
| archlint | clean; **30** rules seen failing |
| Windows | every Lite package and its test binary cross-compiled; `Mizan Lite.exe` (GUI) and `lite-demoseed.exe` (console) built |
| **Mizan after L3's kernel and archlint changes** | `scripts/check.sh` green: 91 Go packages, 337 frontend tests, 0 lint issues |

**The packaged app, with the real internet (2026-09-14):**

- a shop seeded by L3's seeder (`l3 seeded محمد #3`): ready; the launch job fetched **13,007.5355** from
  `currency-api-jsdelivr` and recorded **`held_today`** — the owner's 15,000 set today stayed in force (D-L3.18); rates 1–3
  as seeded (first run 14,800, 15,200, correction 15,000); quit clean;
- a shop from L2 with no rate (`l2 seeded محمد #2`): migration 4 applied; the launch fetch recorded **`proposed`** and **no rate**
  — the first rate is a person's (D-L3.17); quit clean.

## 19. MUTATION DRILLS — 33, ALL CAUGHT

| # | Planted defect | Caught by |
|---|---|---|
| G1 | `LineExtension` then `MulRate` instead of one expression | `TestHalfALitreAt333At13000Is21645NotTheTwiceRounded21710`, `TestLineExtensionMulRateRoundsOnce` |
| G2 | `DivRate` inverting the rate first | `TestMoneyDivRateRoundTripsWithinTheStatedBound` |
| G3 | the rate in force chosen by `recorded_at` | the store contract, on SQLite — **survived first** (R2) |
| G4 | `SetRate` without the guard | `TestSettingTheRateNeedsTheOwner`, `TestRatesReachTheRealOwnerAndSettings` — **did not compile first** (R3) |
| G5 | a large typed change not refused | `TestALargeTypedChangeIsRefusedUntilConfirmedAndBeforeThePIN`, `TestTheRateThroughTheBindings` |
| G6 | a fetch beyond 20% applied | `TestTheFetchDecision`, `TestALargeFetchedChangeIsAProposalThatGoesStale`, `TestAFetchedProposalThroughTheBindings` |
| G7 | the first fetched rate applied | `TestTheFirstFetchedRateIsOnlyAProposal` |
| G8 | a manual rate today not holding | `TestRefreshAppliesWhatTheDecisionAllows` |
| G9 | manual mode applying fetches | `TestRefreshInManualModeOnlyShowsTheInternetRate` |
| G10 | age reported only when stale (Mizan 0.9 §9.1) | `TestEveryReadCarriesItsAge` |
| G11 | the four-decimal limit removed | `TestARateTakesAtMostFourDecimals` |
| G12 | stock value in pounds from the dollar total | `TestStockValueInPoundsIsSummedPerLineNotConverted` — **survived first** (R2) |
| G13 | the first-run rate outside its transaction | `TestARefusedRateLeavesNoShopBehind` |
| G14 | a failure code flattened | `TestAFailedFetchIsLoggedWithACodeAndChangesNothing` |
| G15 | the refresh throttle removed | `TestRefreshIsThrottledToOneAttemptAMinute` |
| G16 | a stale proposal accepted | `TestTheFirstFetchedRateIsOnlyAProposal`, `TestALargeFetchedChangeIsAProposalThatGoesStale` |
| G17 | the new pound not scaled | `TestTheNewPoundIsScaledToTheOldOne` |
| G18 | a provider registered by default | `TestRatesReachTheRealOwnerAndSettings` and the fresh-installation tests |
| F1 | a large change confirmed without asking | "sets the rate manually through the PIN, and a change beyond 20% asks again" |
| F2 | a proposal accepted without `withOwner` | "offers a proposal for the owner's approval, through the PIN" |
| F3 | first run not sending the rate | "blocks the shell until the shop is set up…", "fetches a rate from the internet into the field…" |
| F4 | the read-back keeping trailing zeros | "reads the typed rate back, so 15.000 is seen to mean fifteen" |
| F5 | the header hiding "not updated today" | "marks a rate not updated today, and says when there is none" |
| F6 | the receipt rate not pre-filled | "a delivery in pounds asks for the rate it was paid at…" |
| F7 | a pound column with no rate | "with no rate shows no pound column at all" |
| A1–A8 | fx importing settings; catalog, owner, settings, stock and setup importing fx; `net/http` in fx; `net` in the seeder | archlint, on every run |

## 20. NOT VERIFIED

1. **The Windows build has not run** (O1). In particular the providers over Windows' certificate store and a shop's proxy
   are proven only by construction (Go's standard client), not by a run.
2. **The rate screen and the header have not been looked at** in either language (O5).
3. **The providers' future.** They are free services with no agreement; either may change its scale, its currency
   coverage or its availability. The 20% guard makes a change safe, not invisible; the live test detects a scale change
   when someone runs it.
4. **Whether the owner will want the internet's official-style rate at all** (§14.3 F2): the design makes the gap visible
   and holds the owner's rate, but in automatic mode a fetched rate within 20% of an old rate is applied the next day.

## 21. DEFINITION OF DONE, AND NEXT

| Criterion (§12.3, with §14) | Status |
|---|---|
| `make lite-ci` green | ✅ |
| Every invariant drilled | ✅ 33 of 33 (two strengthened after surviving) |
| The seeder's rates open in the packaged app and the header shows the corrected rate | ✅ by the log and the database; ⏳ looked at (§20.2) |
| The live providers answer | ✅ by hand, 2026-09-14; NOT RUN in CI by design |
| Windows `.exe` builds | ✅ built — not run |
| Mizan's `scripts/check.sh` green | ✅ |
| PROGRESS, DECISIONS and this document updated | ✅ |

### 21.1 At commit (2026-09-14)

The owner approved D-L3.15–26 and D-L3.i1–i8 and **set manual mode as the default** — answering §20.4 and PROGRESS O10: a
fresh installation prices from the owner's rate, fetches the internet's for reference (`held_mode`), and switches to
automatic with the PIN. `settings.Defaults` changed; the tests that exercise automatic mode now set it explicitly, and
`TestTheDefaultModeIsManual` and the bootstrap job test (`held_mode` by default) hold the default.

**Next:** L4's design note — the till, which snapshots this rate on every sale.

