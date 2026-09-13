# Mizan Lite — Phase L1: catalogue, units, owner PIN, demo data

> **Status: COMPLETE — approved and committed 2026-09-13.**
> §1–§13 are the approved design note; **§14 onwards is the implementation record.**
> **Date:** 2026-09-13. **Base:** L0 (`c2a1d0e`). **Design:** [../DESIGN.md](../DESIGN.md), §13 amendments included.

---

## 0. How to read this

| § | Content |
|---|---|
| 1 | Analysis — what L1 must make possible, and six things that are harder than they look |
| 2 | Scope — in, out, and why |
| 3 | The schema — `0002_catalogue_owner.sql` |
| 4 | Catalogue: products, units, prices, the quick grid |
| 5 | **Arabic text**: one normalisation for search and for duplicate names |
| 6 | **Numbers typed by a person**: one normalisation, shared by Go and TypeScript |
| 7 | **The owner PIN**: credential, lockout, elevation, recovery, and the guard |
| 8 | First run |
| 9 | The demo data generator |
| 10 | Bindings, and the screen that calls each one |
| 11 | Architecture rules that change |
| 12 | Tests, drills, and the Definition of Done |
| 13 | **Decisions and questions for approval** |

---

## 1. ANALYSIS

### 1.1 What L1 must make possible

A shop owner, on a fresh installation, in Arabic:

1. names the shop, sets an **owner PIN**, and writes down a **recovery code**;
2. adds *زيت زيتون بلدي* sold by the litre at $3.25, *دبس رمان* in jars at 45,000 SYP, and a 16-litre tin;
3. finds *زيت* again by typing `زيت`, `زيـــت`, or scanning its barcode — whatever the keyboard layout;
4. pins the fast sellers to the till's quick grid;
5. corrects a price — **and a cashier cannot do the same without the owner's PIN**.

And a developer runs `lite-demoseed` and gets a shop's worth of believable catalogue in seconds.

### 1.2 Six things harder than they look

**H1 — Arabic text has many spellings of the same word.** *أسطنبولي*, *إسطنبولي* and *اسطنبولي* are one word
to a reader and three strings to a database. So are *زيت* and *زيـــت* (tatweel stretching), *رمانة* and
*رمانه* (ta marbuta), *حلوى* and *حلوي* (alef maqsura). A search that compares bytes finds none of the other
spellings, and a uniqueness check that compares bytes lets a shop create the same product three times — the
duplicate nobody spots until a stock count. §5.

**H2 — the keyboard decides which digits arrive.** Some Arabic layouts (macOS's *Arabic*, for one) type `١٢٣` on
the number row; a Persian layout types `۱۲۳`; the Arabic decimal separator is `٫`. A USB barcode scanner *emulates a keyboard*,
so with an Arabic layout active a scanner can deliver `٦٢٢٣٠٠٠١١٢٣٤٥` for a Latin barcode. An input that parses
only `0-9` rejects the price; a lenient one reads `1` from `١٫٧٥٠` and drops the rest. §6.

**H3 — a 6-digit PIN is not a password, and the threat it answers is at the counter.** Anyone who can open the
database file can change any row in it; no PIN in this application defends against that, and pretending
otherwise would be dishonest. What the PIN *can* do is stop a cashier voiding a sale or cutting a price while
the owner is not looking. That threat is online and physical: the defence is lockout and a short elevation
window, not hash strength alone. §7.

**H4 — a forgotten PIN has no server to reset it.** Lite is offline by construction. If the owner forgets the
PIN, every guarded act — voids, rates, profit — is closed forever, unless a recovery path was set up *before*
it was needed. §7.5.

**H5 — the guard must live where the rule lives.** "Changing a price needs the owner" is a business rule. Put
in the binding layer, it becomes the one thing a future caller (the seeder, an import, a second screen) can
walk around. Put in the catalogue service, the service must know about the owner without importing the owner
module. §7.6.

**H6 — a PIN with nothing to guard is a mechanism with no caller.** Q7 guards voids (L4), rates (L3) and profit
(L6) — none of which exist in L1. Building the PIN now with no guarded act would be the defect this project has
found eight times. So L1 proposes a guarded catalogue act (§13, Q-L1.1), and the PIN's own management —
changing it, recovering it, the history of its use — gives every part of the mechanism a real caller. §7.7.

---

## 2. SCOPE

### 2.1 In L1

| Area | What |
|---|---|
| Units | The nine seeded units (Q-L1.5), names in both catalogs |
| Currencies | SYP (0 decimals) and USD (2 decimals) as rows — needed for a price's currency (DESIGN C8: codes are data) |
| Products | Create, edit names and barcode, set price, deactivate / reactivate, search, quick-grid slot |
| Text | Arabic normalisation for search and duplicate names (§5) |
| Numbers | Digit and separator normalisation for every typed number, Go and TypeScript agreeing (§6) |
| Owner | PIN, lockout, elevation window, recovery code, the guard port, the owner's event history (§7) |
| First run | Shop name, language, owner PIN, recovery code (§8) |
| Demo data | `cmd/lite-demoseed` and `internal/lite/demoseed` — catalogue only in L1 (§9) |

### 2.2 Not in L1, and where it lands

| Not here | Why | Where |
|---|---|---|
| Stock on hand, average cost | No stock movement exists yet; a cost column with no writer is a column nobody can trust | L2 (`ALTER TABLE products ADD …`) |
| Opening a package (tin → loose litres) | An operation on stock, not on the catalogue | L2 (Q8) |
| Price history | A sale line snapshots its price (DESIGN §9.3); the owner's event history records each price change with old and new value (§7.4) | — |
| Exchange rates | A USD-priced product shows its USD price; no conversion until a rate exists | L3 |
| Product images, categories, variants | Not asked for; stripped by DESIGN §3.2 | — |
| Unit change after first movement is refused | The rule reads a movement that cannot exist before L2; written with its trigger | L2 |

---

## 3. THE SCHEMA — `0002_catalogue_owner.sql`

One migration for the phase (DESIGN §13.3 A3).

```sql
-- ─────────────────────────────────────────────────────────────────────────────
-- Currencies — codes are data (DESIGN C8). Seeded by the migration because a product
-- cannot be priced without one, and the set is fixed for v1.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE currencies (
  code            CHAR(3)      NOT NULL PRIMARY KEY,
  decimal_places  SMALLINT     NOT NULL CHECK (decimal_places BETWEEN 0 AND 4),
  sort_order      SMALLINT     NOT NULL
);
INSERT INTO currencies (code, decimal_places, sort_order) VALUES ('SYP', 0, 1);
INSERT INTO currencies (code, decimal_places, sort_order) VALUES ('USD', 2, 2);

-- Units — names live in the catalogs under "uom.<code>".
CREATE TABLE uoms (
  code            VARCHAR(16)  NOT NULL PRIMARY KEY,
  kind            VARCHAR(8)   NOT NULL CHECK (kind IN ('mass', 'volume', 'count')),
  input_decimals  SMALLINT     NOT NULL CHECK (input_decimals BETWEEN 0 AND 6),
  sort_order      SMALLINT     NOT NULL,
  CONSTRAINT ck_uom_count_is_whole CHECK (kind <> 'count' OR input_decimals = 0)
);
INSERT INTO uoms VALUES ('kg', 'mass', 3, 1);
INSERT INTO uoms VALUES ('l', 'volume', 3, 2);
INSERT INTO uoms VALUES ('piece', 'count', 0, 3);
INSERT INTO uoms VALUES ('jar', 'count', 0, 4);
INSERT INTO uoms VALUES ('container', 'count', 0, 5);
INSERT INTO uoms VALUES ('tin', 'count', 0, 6);
INSERT INTO uoms VALUES ('bag', 'count', 0, 7);        -- Q-L1.5
INSERT INTO uoms VALUES ('bottle', 'count', 0, 8);     -- Q-L1.5
INSERT INTO uoms VALUES ('box', 'count', 0, 9);        -- Q-L1.5

CREATE TABLE products (
  id                 CHAR(36)     NOT NULL PRIMARY KEY,
  name_ar            VARCHAR(200) NOT NULL,
  name_en            VARCHAR(200),
  -- §5: the normalised Arabic name. UNIQUE, so one product cannot exist under three spellings.
  name_key           VARCHAR(200) NOT NULL,
  -- §5: normalised Arabic name + English name + barcode, the one column search reads.
  search_text        VARCHAR(500) NOT NULL,
  barcode            VARCHAR(64),
  uom_code           VARCHAR(16)  NOT NULL REFERENCES uoms(code),
  price_currency     CHAR(3)      NOT NULL REFERENCES currencies(code),
  sell_price_micro   BIGINT       NOT NULL CHECK (sell_price_micro >= 0),   -- 10⁻⁶ of the major unit
  quick_slot         SMALLINT     CHECK (quick_slot BETWEEN 1 AND 24),       -- Q-L1.6
  is_active          SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version        BIGINT       NOT NULL DEFAULT 1,
  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  CONSTRAINT ux_products_name_key   UNIQUE (name_key),
  CONSTRAINT ux_products_barcode    UNIQUE (barcode),
  CONSTRAINT ux_products_quick_slot UNIQUE (quick_slot),
  CONSTRAINT ck_products_name_present CHECK (name_ar <> '' AND name_key <> ''),
  -- A deactivated product cannot hold a till slot: it would be a button that sells nothing.
  CONSTRAINT ck_products_slot_needs_active CHECK (quick_slot IS NULL OR is_active = 1)
);
CREATE INDEX ix_products_active_name ON products (is_active, name_key);

-- ─────────────────────────────────────────────────────────────────────────────
-- The owner — exactly one row (§7.2).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE owner_credentials (
  singleton          SMALLINT     NOT NULL PRIMARY KEY CHECK (singleton = 1),
  pin_hash           VARCHAR(200) NOT NULL,     -- PHC-encoded Argon2id (platform/crypto)
  recovery_hash      VARCHAR(200) NOT NULL,
  failed_attempts    SMALLINT     NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
  locked_until       CHAR(24),                  -- NULL = not locked
  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  row_version        BIGINT       NOT NULL DEFAULT 1
);

-- LEDGER. Append-only: what the owner's PIN was used for, and every attempt that failed.
CREATE TABLE owner_events (
  id            CHAR(36)     NOT NULL PRIMARY KEY,
  occurred_at   CHAR(24)     NOT NULL,
  kind          VARCHAR(24)  NOT NULL CHECK (kind IN (
                  'pin_set', 'pin_changed', 'elevated', 'elevation_failed',
                  'locked_out', 'recovered', 'elevation_ended', 'guarded_act')),
  action        VARCHAR(64),      -- the guarded act's code, e.g. 'catalog.price.change'
  subject_id    CHAR(36),         -- what it acted on, e.g. a product id
  before_value  VARCHAR(200),     -- e.g. 'USD 3.250000' — never a credential
  after_value   VARCHAR(200),
  CONSTRAINT ck_owner_events_act_complete CHECK (
    (kind = 'guarded_act') = (action IS NOT NULL))
);
CREATE INDEX ix_owner_events_occurred ON owner_events (occurred_at);
```

**Notes.**

- **`name_key` is UNIQUE across active and inactive products.** A deactivated *زيت زيتون* blocks a new one
  with the same name; the error says so and offers reactivation. A partial unique index (active only) would
  avoid that, but partial indexes are not portable to MySQL, and the portability contract holds (DESIGN §8).
- **No `stock` or `cost` column yet.** L2 adds them with `ALTER TABLE products ADD COLUMN … NOT NULL DEFAULT 0`,
  written in the phase that writes them.
- **`owner_events` stores values, never the PIN, a hash, or the recovery code** — Mizan's rule for its till PIN: an audit trail
  is read by more people than a credential store (Mizan 5.7).

---

## 4. CATALOGUE

### 4.1 The product, as a value

```go
package domain // internal/lite/catalog/domain

type Product struct {
    ID            id.ID
    NameAR        string   // required, trimmed, ≤ 200 runes
    NameEN        string   // optional; "" displays NameAR
    Barcode       string   // optional; digits normalised (§6), trimmed, ≤ 64
    UnitCode      string   // one of the seeded units
    PriceCurrency string   // "SYP" or "USD"
    SellPrice     money.UnitAmount
    QuickSlot     int      // 0 = none; 1–24
    Active        bool
    RowVersion    int64
}
```

`NewProduct` and `Product.Rename`, `Product.Reprice`, `Product.SetBarcode`, `Product.Deactivate` return new
values and typed errors; none does I/O. **Changing the unit is not a method in L1** — it is refused from the
first stock movement onwards (L2), and a method written before that rule exists would have to be taken away.

### 4.2 Prices

- Typed as a decimal string in the price's currency, normalised (§6), parsed with `money.ParseUnitAmount`.
- **Input decimals = the currency's decimals**: USD `3.25`, SYP `45000`. Storage stays 10⁻⁶, so a later
  decision to allow `3.255` needs no migration.
- Crosses the boundary as `{ currency: "USD", micro: "3250000" }` and is displayed from Go's formatted string
  — the frontend does no arithmetic (DESIGN D9).
- A price of **zero is allowed** (a free sample) and shown as zero, not as missing.

### 4.3 The quick grid

24 slots (Q-L1.6), 4 × 6 at the till's minimum width. `SetQuickSlot(productID, slot)` **moves** a product
into a slot; if another product held it, that product is unpinned in the same transaction, so there is never a
moment with two products claiming one button. A deactivated product loses its slot in the same transaction as
its deactivation (enforced by `ck_products_slot_needs_active` as well as by the service).

### 4.4 Optimistic concurrency

Every edit carries `row_version`; a stale one is refused with `database.concurrent_modification` — which
already has a translation. The owner editing a price on one screen while the seeder or a second window changed
it must see a refusal, not a silent overwrite.

---

## 5. ARABIC TEXT — ONE NORMALISATION

`internal/lite/textkey.Normalise(s string) string`, pure, used for `name_key`, `search_text`, and every search
query. **Only Go normalises** — search runs in Go — so there is one implementation.

| Rule | From | To |
|---|---|---|
| Diacritics (harakat, U+064B–U+065F) and the superscript alef (U+0670) | *زَيْت* | *زيت* |
| Tatweel (U+0640) | *زيـــت* | *زيت* |
| Alef with hamza above/below, madda, wasla | أ إ آ ٱ | ا |
| Alef maqsura | ى | ي |
| Ta marbuta | ة | ه |
| Hamza on waw / on ya | ؤ ئ | و ي |
| Persian kaf and yeh (a Persian keyboard, or a pasted name) | ک ی | ك ي |
| Arabic-Indic and Extended Arabic-Indic digits | ١٦ ۱۶ | 16 |
| Latin letters | `Olive` | `olive` |
| Whitespace | runs, tabs, NBSP, zero-width joiners | one space, trimmed |

**What it does not do**, deliberately: remove the definite article *ال*. *الزيت* and *زيت* are the same word to a
reader, but stripping *ال* also mangles words that merely begin with those letters (*الماس* → *ماس*), and
search already finds *زيت* inside *الزيت* because it matches substrings.

**Properties the tests hold:** idempotent (`Normalise(Normalise(s)) == Normalise(s)`); never lengthens its input;
every character in the table is covered by a fixed example; a property test over random mixed Arabic/Latin
strings for idempotence.

**Search.** `LIKE '%' || ? || '%' ESCAPE '\'` over `search_text`, with `%`, `_` and `\` in the query escaped first
— otherwise a product named *50% خصم* turns a search for `50%` into "anything starting with 50". An exact
barcode match is returned first. `LIKE … ESCAPE` is portable; full-text search is not, and a shop's catalogue
is hundreds of rows, not millions.

---

## 6. NUMBERS TYPED BY A PERSON — ONE NORMALISATION, TWO LANGUAGES

DESIGN §8.3 named this *the most likely bug in this product*. L1 is where the first number is typed.

`internal/lite/numinput.Normalise(raw string) (string, error)` in Go, and `normaliseNumber(raw)` in TypeScript.
Go is authoritative; TypeScript exists so an input can show "not a number" as it is typed.

| Input | Result |
|---|---|
| `١٫٧٥٠` | `1.750` |
| `۱.۵` | `1.5` |
| ` 3.25 ` | `3.25` |
| `٤٥٠٠٠` | `45000` |
| `1,750` | **refused** — `,` and `٬` are grouping separators, and guessing turns 1,750 kg into 1.75 kg |
| `1.2.3`, `.`, `-5`, `1e3`, `٣,٥` | **refused** |
| `0.5` for a `jar` | refused by the unit, after normalisation (`quantity.Parse`) |
| `3.255` for a USD price | refused: more decimals than the currency (§4.2) |

**How the two implementations are kept from drifting:** one fixture file,
`internal/lite/numinput/testdata/cases.json`, holding every case above and more. The Go test and the Vitest test
**both read it**. A case added for one is enforced on both; an implementation that disagrees fails its own
suite. (A property test in each language additionally checks that any string of Latin digits with at most one
`.` survives normalisation unchanged.)

**Barcodes** go through the digit half only (no decimal point), so a scanner on an Arabic layout delivers the
same barcode as on a Latin one.

---

## 7. THE OWNER PIN

### 7.1 What it defends, stated plainly

The owner PIN stops **a person at the counter** from doing what only the owner should: voiding a sale (L4),
changing the exchange rate (L3), reading profit (L6) — and, if approved, changing a price (Q-L1.1).

It does **not** defend against someone who can open the database file, run the demo seeder against the data
directory, or administer the computer. Those people can change any row directly. Encrypting the database to
prevent that is a different feature, with key-management questions an offline shop cannot answer, and it is not
in v1. This is written here so no one reads the PIN as more than it is.

### 7.2 The credential

- **6 to 12 digits** (Q-L1.2). Mizan allows 4; a 4-digit PIN watched over a shoulder once is learned, and the
  owner types it rarely, so two more digits cost little.
- **Refused as weak:** all one digit (`111111`), ascending or descending runs (`123456`, `987654`), and the
  shop's own phone-number-shaped patterns are *not* checked (there is no phone number to check against).
- Arabic-Indic digits are accepted (§6) — an owner on an Arabic keyboard types `٢٤٦٨١٣`.
- **Hashed with Argon2id at password strength** through Mizan's `platform/crypto`, whose parameters and PHC
  encoding are already tested (Mizan 1.2). Mizan 5.7's reasoning holds: a PIN's small space is an argument for
  throttling, not for a cheaper hash. `needsRehash` from `Verify` upgrades the hash on the next success.

### 7.3 Lockout

| Failures in a row | Wait before the next attempt |
|---|---|
| 1–4 | none |
| 5 | 30 s |
| 6 | 60 s |
| 7 | 2 min |
| … | doubling, **capped at 15 min** |

- **Persisted** (`failed_attempts`, `locked_until`), so quitting and relaunching the application does not reset
  the counter — otherwise lockout is one restart away from useless.
- A success resets both. Every failure and every lockout is an `owner_events` row, so the owner can see that
  somebody tried.
- The clock is injected (`kernel/clock`); every threshold is a test with a fixed clock, not a sleep.
- **A wait, never a permanent lock.** A shop whose owner mistyped at 8 a.m. must still be able to void a sale at
  9; a permanent lock with no server to lift it is a denial of service against the owner.

### 7.4 Elevation

Entering the PIN **elevates** the application for a short window (Q-L1.3; proposed **2 minutes from entry**,
not sliding).

- Held **in Go memory only** — never a token in JavaScript (Mizan 1.5 D3), never in the database. A restart
  ends it.
- Ended early by the header's **Lock** button, and by the window closing.
- The header shows **"Owner mode — 1:42"** while elevated, so a cashier taking over the counter can see it and
  the owner can see they forgot to lock.
- Not sliding, deliberately: a sliding window kept alive by the owner's last act can stay open for an hour of
  continuous work while they step away between two acts.

### 7.5 Recovery

At first run the owner is shown a **recovery code** once — 16 characters from an unambiguous alphabet
(`23456789ABCDEFGHJKMNPQRSTUVWXYZ`, no `0/O`, `1/I/L`), grouped `XXXX-XXXX-XXXX-XXXX`, generated with
`crypto/rand`, stored only as an Argon2id hash. The screen asks the owner to write it down and confirm they have.

- **Using it** sets a new PIN and issues a **new** code; the old one is spent.
- It is **throttled by the same lockout** as the PIN — otherwise it is a second, unthrottled door.
- 16 characters from 31 symbols is about 79 bits; unlike the PIN, it is not guessable at a counter even without
  lockout.

Without it (Q-L1.4), a forgotten PIN closes voids, rates and profit **permanently**, with no server to reopen
them.

### 7.6 The guard — where the rule lives

The catalogue service declares the port it needs, and the owner module satisfies it in the composition root —
Mizan's port pattern (0.5, 1.2), so `catalog` never imports `owner`:

```go
package catalog

// OwnerGate is what the catalogue needs from the owner: permission for an act only the owner may do.
type OwnerGate interface {
    // Require returns nil when the application is elevated, and records the act — IN the caller's
    // transaction, so an act that rolls back leaves no record that it happened. Otherwise it returns
    // errs.Permission(owner.CodeRequired).
    Require(ctx context.Context, act GuardedAct) error
}

type GuardedAct struct {
    Action    string // "catalog.price.change"
    SubjectID id.ID
    Before    string // "USD 3.250000"
    After     string // "USD 3.500000"
}

func (s *Service) SetPrice(ctx context.Context, in SetPriceInput) (domain.Product, error) {
    var out domain.Product
    err := s.db.Do(ctx, func(ctx context.Context) error {
        current, err := s.repo.Get(ctx, in.ProductID)
        // … reprice in the domain, compare row versions …
        if err := s.owner.Require(ctx, GuardedAct{Action: ActPriceChange, SubjectID: current.ID,
            Before: priceText(current), After: priceText(next)}); err != nil {
            return err
        }
        return s.repo.Update(ctx, next)
    })
    return out, err
}
```

**The seeder and every other caller go through the same service** and so meet the same guard. The seeder
elevates with the PIN it set, exactly as a person would.

**On the frontend, one mechanism for every guarded act:** a call that fails with `lite.owner.required` opens the
PIN dialog; a correct PIN elevates and the call is **retried once**, automatically. The screen code for a
guarded act is the same as for any act — it does not know it is guarded, so a future guarded act cannot forget
to ask. The retry is once: a second refusal is shown, never looped.

### 7.7 Every part has a caller in L1

| Part | L1 caller |
|---|---|
| Set PIN, issue recovery code | First run (§8) |
| Verify, lockout, elevation | The PIN dialog, before a guarded act |
| `Require` | `catalog.SetPrice` — **if Q-L1.1 is approved**; otherwise `catalog.Deactivate` |
| End elevation | The header's Lock button |
| Change PIN (needs the current PIN) | Owner screen |
| Recover with code | "Forgot PIN?" in the PIN dialog |
| Event history | Owner screen |

---

## 8. FIRST RUN

A fresh installation shows one screen before anything else, after boot and before the shell:

1. **Shop name** — required; shown in the header, and on receipts from L7.
2. **Language** — the choice cards already on screen in L0's header, here as the first question.
3. **Owner PIN**, typed twice.
4. **Recovery code** — shown, with "I have written it down" required before finishing.

`CompleteFirstRun` writes the shop name, the language and the credentials **in one transaction**; either the
installation is set up or it is not. `FirstRunStatus` answers from the database (credentials exist **and** a shop
name is set), never from a flag that can disagree with the data.

**The recovery code is generated by Go and returned exactly once** — it is not stored in any form the
application can show again.

---

## 9. THE DEMO DATA GENERATOR

`cmd/lite-demoseed/main.go` (thin) → `internal/lite/demoseed.Run(ctx, app, Options)`.

- **Drives the services, never SQL** — the reasoning of Mizan's own `cmd/demoseed`: every row is written the way
  a shop's would be, so the verifiers of later phases agree with it.
- **Refuses an installation that has completed first run.** Re-seeding means deleting the data directory, a
  decision for whoever runs it (`MIZAN_LITE_DATA_DIR=… lite-demoseed`).
- **Completes first run itself** with a shop name, Arabic, a PIN from `-pin` (default printed on completion), and
  prints the recovery code — so a developer can open the seeded app and use owner mode.
- **L1's data:** ~40 bilingual pantry products from an embedded JSON file (data, not code): loose olive oil by the
  litre and in 16 L tins, pomegranate molasses and grape and carob syrups in jars, labneh and white cheese by the
  kilo, ghee in tins, za'atar and sumac by the kilo, bulgur, freekeh, lentils, chickpeas, makdous and pickles in
  jars — priced in both currencies, 12 of them on the quick grid, one price change made through the guard (so
  the owner history is not empty).
- **Grows with every phase** (DESIGN §9.4): L2 adds opening stock and packages, L3 rates, L4 sales.
- **Tested:** `demoseed.Run` against a temporary directory — first run completed, 40 products, every one with both
  names, 12 slots filled, one `guarded_act` event, a second run refused — and `cmd/lite-demoseed` cross-compiled
  for Windows in `lite-ci`.

---

## 10. BINDINGS AND THEIR CALLERS

Every method, and the screen that calls it — G2 and G3 will hold this table true.

| Façade.Method | Caller |
|---|---|
| `App.BootStatus`, `App.Health` | Boot, HomeScreen (L0) |
| **`App.FirstRunStatus`** | the first-run gate |
| **`App.CompleteFirstRun`** | FirstRunScreen |
| `Settings.Get`, `Settings.Update` | Shell (L0); shop name joins the DTO |
| **`Catalog.Units`**, **`Catalog.Currencies`** | ProductForm |
| **`Catalog.Products`** (query, include inactive) | ProductsScreen |
| **`Catalog.Product`** | ProductForm (edit) |
| **`Catalog.CreateProduct`**, **`Catalog.UpdateProduct`** | ProductForm |
| **`Catalog.SetPrice`** (guarded) | ProductForm |
| **`Catalog.SetActive`** | ProductsScreen |
| **`Catalog.SetQuickSlot`** | ProductsScreen |
| **`Owner.Status`** (PIN set, locked until, elevated until) | Shell header |
| **`Owner.Elevate`** | OwnerPinDialog |
| **`Owner.EndElevation`** | Shell header (Lock) |
| **`Owner.ChangePIN`** | OwnerScreen |
| **`Owner.Recover`** | RecoverDialog |
| **`Owner.Events`** | OwnerScreen |

**21 methods** (4 in L0, 17 new). **Routes:** `/` status (L0), `/products`, `/owner`; FirstRunScreen is a gate,
not a route, so it cannot be navigated to after setup.

---

## 11. ARCHITECTURE RULES THAT CHANGE

| Change | Why |
|---|---|
| `lite-edition-boundary` allows `platform/crypto` | Argon2id for the PIN (§7.2) — reuse of a tested hasher, not a second one |
| **`lite-module-isolation`** (new): `catalog`, `owner`, `settings` import none of each other; only `bootstrap`, `api`, `demoseed` may import several | DESIGN §13.3 A10 — now writable, because L1 has three modules to plant a violation between |
| `mizan-independent-of-lite` applies to `self/cmd/demoseed/**` instead of `self/cmd/**` | `cmd/lite-demoseed` is Lite's and must import it |
| **`lite-cmd-entry`** (new): `cmd/lite-demoseed` imports only std and `internal/lite/**` | Same shape as `lite-app-entry` |
| `no-sql` covers `internal/lite/textkey`, `numinput`, `demoseed` | They are pure or orchestrate; SQL stays in `infra/sqlite` |

Each is added to `scripts/lite-arch-drill.sh` and seen failing.

---

## 12. TESTS, DRILLS, DEFINITION OF DONE

### 12.1 Tests, by layer (names are the contract)

| Layer | Tests |
|---|---|
| **textkey** | one fixed example per table row in §5 · `TestNormaliseIsIdempotent` (property) · `TestNormaliseNeverLengthens` (property) · `TestTheDefiniteArticleIsKept` |
| **numinput** | `TestSharedFixture` over `cases.json` · property: plain Latin decimals unchanged · `TestGroupingSeparatorsAreRefused` |
| **catalog/domain** | name required and bounded · price decimals by currency · zero price allowed · deactivation clears the slot |
| **catalog service (fake store + contract)** | `StoreContract` run on the fake and SQLite · `TestDuplicateSpellingsAreOneProduct` (أسطنبولي / إسطنبولي) · `TestStaleRowVersionIsRefused` · `TestMovingASlotUnpinsTheHolderInOneTransaction` · `TestSearchEscapesLikeWildcards` (*50% خصم*) · `TestBarcodeMatchesAcrossKeyboardLayouts` |
| **owner** | weak PINs refused · `TestLockoutDoublesAndCaps` (fixed clock) · `TestLockoutSurvivesRestart` · `TestSuccessResetsTheCounter` · `TestElevationExpiresAtTwoMinutesNotSliding` · `TestRecoveryCodeIsSingleUse` · `TestRecoveryIsThrottledLikeThePIN` · `TestNoEventEverHoldsACredential` |
| **the guard** | `TestPriceChangeWithoutElevationIsRefusedAndWritesNothing` · **`TestARolledBackActLeavesNoRecordOfIt`** · `TestTheSeederMeetsTheSameGuard` |
| **first run** | `TestFirstRunIsAllOrNothing` (a failure after the PIN leaves no PIN) · `TestFirstRunStatusReadsTheDataNotAFlag` · `TestTheRecoveryCodeIsReturnedOnceAndStoredOnlyHashed` |
| **demoseed** | the §9 assertions · refuses a set-up installation |
| **bindings** | wire shapes of every new DTO · `lite.owner.required` crosses as a code |
| **frontend** | `normaliseNumber` over the SAME `cases.json` · `withOwner` retries once and never loops · PIN dialog shows the lockout wait · first-run gate blocks the shell · product form in both directions · the recovery code needs "written down" before finishing · Latin digits in every price display |
| **gates** | G0–G5 with 21 methods · catalog parity for every new key · error-code coverage for every new code · docs gate |

### 12.2 Drills planned

At least one per invariant above, including: a normalisation that skips hamza (duplicate names get through);
`numinput` accepting `,` (the fixture fails on both sides); lockout counter held in memory (restart test fails);
elevation made sliding (expiry test fails); `Require` recording outside the transaction (rollback test fails);
the retry looping (frontend test fails); the seeder writing a product row directly (the guard test fails);
the TypeScript normaliser diverging from Go (the shared fixture fails in Vitest).

### 12.3 Definition of Done

> `make lite-ci` green, golangci-lint included · every invariant drilled · `lite-demoseed` runs against a clean
> directory and its result opens in the packaged app · the packaged app opened and looked at in Arabic and
> English — and, since that needs the owner (L0 O2), the phase record says so rather than claiming it ·
> Windows `.exe` and `lite-demoseed.exe` built · [../PROGRESS.md](../PROGRESS.md), [../DECISIONS.md](../DECISIONS.md)
> and this document updated.

---

## 13. DECISIONS AND QUESTIONS FOR APPROVAL

### 13.1 Decisions — approve, amend, or reject

| # | Decision | § |
|---|---|---|
| D-L1.1 | One migration `0002_catalogue_owner.sql`; currencies and units seeded by it | 3 |
| D-L1.2 | `name_key` UNIQUE across active and inactive products (portable; reactivation offered) | 3, 5 |
| D-L1.3 | Arabic normalisation per §5, Go only; the definite article kept | 5 |
| D-L1.4 | Number normalisation per §6 in Go and TypeScript, bound by one shared fixture file | 6 |
| D-L1.5 | Price input decimals = the currency's decimals; zero allowed | 4.2 |
| D-L1.6 | PIN hashed with `platform/crypto` Argon2id; `platform/crypto` joins the edition boundary | 7.2, 11 |
| D-L1.7 | Persisted lockout: 5 free attempts, 30 s doubling, 15 min cap, never permanent | 7.3 |
| D-L1.8 | Elevation in Go memory only; fixed window; Lock button; visible countdown | 7.4 |
| D-L1.9 | The guard is a port declared by the module that needs it; records the act in the caller's transaction | 7.6 |
| D-L1.10 | One frontend mechanism: `lite.owner.required` → PIN dialog → retry once | 7.6 |
| D-L1.11 | First run is one transaction; its status is read from the data | 8 |
| D-L1.12 | The seeder drives services, completes first run itself, refuses a set-up installation | 9 |
| D-L1.13 | The PIN defends the counter, not the file — stated in the product documentation | 7.1 |

### 13.2 Questions only you can answer

**Q-L1.1 — Which catalogue acts need the owner PIN?** *Recommended: changing a price, and deactivating a
product.* A cashier who can cut the price of olive oil before ringing up a friend's order has the same power as
one who can void the sale afterwards. Creating a product and correcting a name stay open, because a shop's
catalogue is built during trading hours by whoever is at the counter.

**Q-L1.2 — PIN length?** *Recommended: 6 to 12 digits.* (Mizan allows 4.)

**Q-L1.3 — How long does owner mode last?** *Recommended: 2 minutes from entering the PIN, not extended by
activity, ended early by Lock.* The alternative — asking for the PIN on every single guarded act — is safer and
slower; it suits a shop where the owner is rarely at the counter.

**Q-L1.4 — A recovery code?** *Recommended: yes.* Without one, a forgotten PIN closes voids, rates and profit
permanently, and there is no server to reopen them.

**Q-L1.5 — Are six units enough?** kg, litre, piece, jar, container, tin. Pantry shops also commonly sell by the
**bag** (*كيس*), **bottle** (*قنينة*), and **box** (*علبة*). Adding a unit later is a migration with a seed row, so
it is cheap either way — but a product created as "piece" because "bottle" was missing is a product whose unit
cannot change once it has stock (L2).

**Q-L1.6 — How many quick-grid buttons?** *Recommended: 24* (4 × 6 at the till's minimum width).

**Q-L1.7 — Shop name required at first run?** *Recommended: yes* — it is the header now and the receipt later.

### 13.3 The owner's answers (2026-09-13)

| Q | Answer | Effect on the design |
|---|---|---|
| Q-L1.1 | PIN for changing a price and for deactivating a product | `Catalog.SetPrice` and `Catalog.SetActive(false)` are guarded; reactivation is not. **There is no delete**: products will be referenced by sale lines, so deactivation is the removal — "deleting" in the answer is read as deactivation |
| Q-L1.2 | 6–12 digits | as §7.2 |
| Q-L1.3 | 2 minutes after entering the PIN, plus a Lock button | as §7.4 — counted from entry, not extended by activity |
| Q-L1.4 | Recovery code at setup | as §7.5 |
| Q-L1.5 | **Nine units**: kg, litre, piece, jar, container, tin, **bag, bottle, box** | three count units added to `0002`'s seed (§3) |
| Q-L1.6 | 24 quick-grid buttons | as §4.3 |
| Q-L1.7 | Shop name required at first run | as §8 |

The owner also confirmed the L0 interface lays out and runs correctly on macOS (closes PROGRESS O2).

---

# IMPLEMENTATION RECORD

## 14. What was built

| Layer | Packages | What |
|---|---|---|
| Schema | `migrations/sqlite/0002_catalogue_owner.sql` | currencies (SYP, USD), **nine** units, products, owner credential, owner history |
| Text | `internal/lite/textkey`, `internal/lite/numinput` | Arabic normalisation (§5); typed-number normalisation (§6) with `testdata/cases.json` — the fixture Go and TypeScript both read |
| Catalogue | `catalog`, `catalog/domain`, `catalog/infra/sqlite`, `catalog/catalogtest` | products, prices, till buttons, search; `OwnerGate` port; fake store + contract run on fake **and** SQLite |
| Owner | `owner`, `owner/domain`, `owner/infra/sqlite`, `owner/ownertest` | PIN (6–12), persisted lockout, 2-minute in-memory owner mode, recovery code, `Require` |
| Settings | `settings/domain` | `shop.name` added |
| First run | `setup` | shop name + language + PIN in one transaction; status read from the data |
| Composition | `bootstrap` | wiring, the `ownerGate` adapter, `PINHasher`/`Random` options |
| Bindings | `api` | `App.FirstRunStatus`, `App.CompleteFirstRun`; `Catalog` (9); `Owner` (6) — **21 methods in total** |
| Seeder | `internal/lite/demoseed`, `cmd/lite-demoseed` | 40 bilingual products from embedded JSON, 12 on the grid, one guarded price change |
| Frontend | `api/client.ts`, `owner/OwnerProvider` + `PinDialog`, `app/FirstRun`, `screens/products`, `screens/owner`, `ui/Dialog`, `ui/Field`, `ui/Checkbox`, `i18n/numbers` | first-run gate, `withOwner` (retry once), PIN and recovery dialog, products list and form, owner screen, header owner-mode countdown and Lock |
| Catalogs | `internal/lite/locales` | +25 error codes and +100 interface strings per language (134 and 59 keys now) |
| Rules | `arch-rules.yml`, `scripts/lite-arch-drill.sh` | 7 rules added, 2 extended; **15 rules, each seen failing on every run** |

## 15. Decisions made while building (within the approved design)

**D-L1.i1 — `textkey` and `numinput` are allowed in domain packages, and a new rule keeps them pure.** Archlint refused
`catalog/domain` and `owner/domain` importing them. They are what a domain rule is made of ("a name of marks only is
missing"; "a price has no more decimals than its currency"), so the right answer was to name them in `lite-domain-purity`
and add `lite-pure-text`: they may import only the standard library and `kernel/errs`. The note's §11 had not foreseen it.

**D-L1.i2 — `setup` is an orchestrator.** It reaches `settings` and `owner` through two ports it declares; the isolation
rules forbid it everything else.

**D-L1.i3 — a failed PIN attempt commits.** The attempt's transaction returns nil and carries the refusal out, so the
failure counter survives the error. Proven against real SQLite (`TestAWrongPINIsCountedEvenThoughItFails`).

**D-L1.i4 — owner mode is switched on only after the attempt commits** (§16, R1).

**D-L1.i5 — tests read the expected schema version from the migration set** (`litetest.LatestSchemaVersion`), so a phase's
migration no longer turns every schema assertion into a false failure.

**D-L1.i6 — test hashing is cheap, production hashing is not.** `bootstrap.Options.PINHasher` defaults to Argon2id at
password strength; tests pass `ownertest.FastParams`. Nothing in the owner's logic depends on the cost.

**D-L1.i7 — the product form reads the product fresh before editing**, and sends at most three acts, each only when it
changes something: names/barcode (open), price (through `withOwner`). An unchanged price never asks for a PIN.

**D-L1.i8 — commands may print.** `.golangci.yml` excludes `cmd/` from the `fmt.Print` rule, as it already excluded
`tools/archlint`; archlint's own `fmt.Print` rules stop at `internal/`.

## 16. Findings

**R1 — three defects found reviewing the owner service before its tests existed** (each now pinned by a drill):
owner mode switched on *inside* the attempt's transaction, so a credential write that then failed still left the
application elevated (O8); a weaker stored hash upgraded from the PIN **as typed** — in Arabic-Indic digits it would
stop verifying (O7); "attempts remaining" off by one.

**R2 — an impure React state updater, found by the console gate.** `OwnerProvider` called `refresh()` inside a
`setStatus` updater; React may call an updater twice, so the re-read would fire twice. Moved into its own effect; the
countdown test now asserts Go is re-read **exactly once** when owner mode runs out.

**R3 — a drill found a test that could not fail (C8).** First-run status was tested only with the PIN set before the shop
name, so a status ignoring the PIN passed. `TestAShopNameWithoutAPINIsNotComplete` added.

**R4 — golangci-lint v2 found 23 issues in L1** (17 shadowed variables, `fmt.Printf` in the seeder, invisible Unicode
characters written raw in test strings, a builtin shadowed, a map key with deliberate whitespace). All fixed; 0 issues.

**R5 — `go test -c -o DIR` cannot build several packages sharing a base name** (three `domain`, three `sqlite`). The
Windows cross-compile now writes one test binary per package.

**R6 — my own mistakes the tests caught:** a hamza-on-waw expectation missing a letter (the code was right); a lockout
comment saying "five free" where the code and the approved table say four; four frontend tests that did not match the
real behaviour (the shell adopting the stored language; a fake that forgot a lock).

## 17. Evidence

| Check | Result |
|---|---|
| `make lite-ci` | **pass** — nothing NOT RUN |
| Go tests (race) | **156** test functions, **293** with subtests, 0 failures (L0: 75) |
| Frontend | **173** tests in 18 files, 0 console warnings; bundle gate 4 |
| golangci-lint v2 | 0 issues |
| archlint | clean; 15 rules seen failing |
| Windows | every Lite package and its test binary cross-compiled; `Mizan Lite.exe` (GUI) and `lite-demoseed.exe` (console) built, pure Go |
| **Mizan after L1's shared changes** | `scripts/check.sh` green: 81 Go packages, 337 frontend tests, 0 lint issues |

**The seeder and the packaged app, run for real** (data directory `seeded محمد #1`):

- the seeder built 40 products, 12 on the grid, one owner credential, one guarded price change, schema version 2; a
  second run was refused with `lite.demoseed.already_set_up`;
- the PIN is stored only as `$argon2id$…` — the digits appear **nowhere** in the database file;
- the packaged app on the seeded directory: ready, then `"health requested by the frontend"` — the first-run gate
  opened and the shell reached Go;
- the packaged app on a fresh directory: two migrations applied, ready, and **no** Health call — the gate held the shell
  back until first run;
- both quit cleanly with no `-wal`/`-shm` left.

## 18. Mutation drills — 31, all caught

| # | Planted defect | Caught by |
|---|---|---|
| O1 | a wrong PIN's refusal returned from inside the transaction (counter rolled back) | `TestAWrongPINIsCountedEvenThoughItFails` |
| O2 | owner mode extended by activity | `TestElevationExpiresTwoMinutesAfterEntryNotAfterActivity` |
| O3 | the lockout wait never set | `TestLockoutSurvivesRestart` |
| O4 | a recovery code not replaced on use | `TestRecoveryCodeIsSingleUse` |
| O5 | a PIN in Arabic digits verified as typed | `TestAPINTypedInArabicDigitsVerifies` |
| O6 | the lock check removed | `TestLockoutEngagesOnTheFifthFailureDoublesAndResets` |
| O7 | hash upgraded from the raw input (R1) | `TestAWeakerStoredHashIsUpgradedOnSuccessAndStillVerifies` |
| O8 | owner mode granted inside the transaction (R1) | `TestOwnerModeIsNotGrantedWhenTheAttemptFailsToSave` — first recorded as "covered structurally", which is not a drill; the test was written and then the drill run |
| C1 | normalisation skips hamza on alef | `TestDuplicateSpellingsAreOneProduct` |
| C2 | Go accepts `,` as a decimal point | `TestSharedFixture` |
| C3 | LIKE wildcards not escaped | the store contract, on SQLite |
| C4 | moving a slot does not unpin the holder | `TestMovingASlotUnpinsTheHolder` |
| C5 | deactivation not guarded | `TestDeactivationNeedsTheOwnerAndReactivationDoesNot` |
| C6 | first run without a transaction | `TestAWeakPINLeavesNoShopNameBehind` |
| C7 | the seeder skips the PIN | `TestTheSeederBuildsAShop` |
| C8 | first-run status ignores the owner | **survived first** → `TestAShopNameWithoutAPINIsNotComplete` (R3) |
| C9 | a five-digit PIN allowed | `TestNormalisePIN` |
| F1 | `withOwner` loops on refusal | "never loops" — **the first mutation called a name that did not exist, so its failure proved nothing; re-run with a real loop** |
| F2 | TypeScript accepts `,` — diverging from Go | the shared fixture, in Vitest |
| F3 | an unchanged price is sent | "an unchanged price is not sent, so no PIN is asked for" |
| F4 | the recovery code skippable | "…shows the recovery code until it is written down" |
| F5 | cancelling the PIN shows an error | "cancelling the PIN keeps the form open and shows no error" |
| F6 | a wrong PIN left in the field | "keeps the dialog open on a wrong PIN…" |
| F7 | a client function nothing calls | G3 |
| A1–A7 | one violation per new rule: Mizan cmd importing Lite, impure text packages, the seeder command, catalog/owner/settings isolation, setup's reach | archlint, on every run |

## 19. Not verified

1. **The Windows build has not run** (L0 O1, unchanged).
2. **The L1 screens have not been looked at.** macOS still refuses screenshots in this environment. The log proves the
   first-run gate and the shell behave on real data; it does not prove the product form or PIN dialog look right in either
   direction. **Owner:** run `MIZAN_LITE_DATA_DIR=~/Desktop/lite-demo go run ./cmd/lite-demoseed`, then open the app with the
   same variable (the seeder prints the PIN and recovery code).
3. **The PIN's cost on the slowest machine a shop has.** Argon2id at 19 MiB / 2 iterations is Mizan's measured default; a
   verify holds the single writer for its duration. Not measured on low-end Windows hardware.

## 20. Definition of Done

| Criterion (§12.3) | Status |
|---|---|
| `make lite-ci` green, golangci-lint included | ✅ |
| Every invariant drilled | ✅ 31 of 31 |
| `lite-demoseed` runs against a clean directory and its result opens in the packaged app | ✅ |
| Packaged app looked at in Arabic and English | ⏳ owner (§19.2) |
| Windows `.exe` and `lite-demoseed.exe` built | ✅ built — not run (§19.1) |
| PROGRESS, DECISIONS and this document updated | ✅ |
