# Mizan ERP — Architecture Addendum v1.1

> Companion to [`ARCHITECTURE_v1.md`](./ARCHITECTURE_v1.md). Read that first.
> Status: **DRAFT — awaiting approval.** No production code yet.

This addendum contains the detailed designs that your nine decisions now require, plus four
new recommendations that arise specifically from them.

| Section | Contents |
|---|---|
| **A** | Product variant architecture (decision 6) |
| **B** | Units of measure & fractional quantities (decision 5) |
| **C** | Country profiles & the setup wizard (decision 2) |
| **D** | Costing abstraction — WAC now, FIFO later (decision 3) |
| **E** | Numeric precision contract (arises from B) |
| **F** | Refinements to decisions 4 and 8 |
| **G** | New recommendations from the Syria default market |
| **H** | Updated build order |

---

# A. Product variant architecture

You asked for a flexible variant model rather than product-specific fields. This is the
design, and it is one of the higher-leverage schema decisions in the whole system — variants
touch inventory, pricing, barcodes, sales lines, purchasing, and every report.

## A.1 The governing decision **[RECOMMENDATION — important]**

**Every product has at least one variant, including simple products.**

A simple product (a bag of cement, a service) gets one automatically-created **default
variant**, invisible in the UI. There is no such thing as a product without variants.

Why this matters more than it looks:

- Every downstream table references `variant_id NOT NULL` — `stock_levels`,
  `sales_document_lines`, `purchase_document_lines`, `barcodes`, `price_list_items`,
  `stock_movements`, `inventory_layers`. One column, one join, one meaning.
- The alternative — a nullable `variant_id` meaning "the product itself" — forces **every**
  query, every stock lookup, every price resolution and every report to handle two cases
  forever. That branching is how catalog code becomes unmaintainable, and it multiplies
  through the system rather than staying local.
- A product can later gain variants without migrating its transaction history: the existing
  default variant simply becomes the first variant.

The cost is one extra row per simple product and a UI that hides the concept when a product
has exactly one variant. That is a very good trade.

## A.2 Attributes: variant-defining vs descriptive **[RECOMMENDATION]**

A second distinction prevents attribute explosion. Attributes come in two kinds:

| Kind | Creates variants? | Example | Stored on |
|---|---|---|---|
| **Variant-defining** | Yes — part of the SKU matrix | Colour, Size, Storage capacity | `variant_attribute_values` |
| **Descriptive (specification)** | No — informational only | Warranty period, Country of origin, Wood finish, Voltage | `product_specifications` |

Without this split, every specification anyone wants to record becomes a variant dimension,
and a product with 4 colours × 3 sizes × 6 recorded specs generates 72 SKUs instead of 12.
The same attribute may be variant-defining for one product and descriptive for another
(Material defines variants for a sofa; it is a spec for a screwdriver), so the flag lives on
the **product-attribute link**, not on the attribute itself.

## A.3 Schema

```sql
-- ─── Attribute dictionary (global, reusable, translatable) ────────────────────

attributes (
  id                 CHAR(36)    NOT NULL PRIMARY KEY,
  code               VARCHAR(50) NOT NULL,        -- 'COLOR', 'SIZE', 'CAPACITY'
  name               VARCHAR(120) NOT NULL,       -- fallback; translated via `translations`
  value_type         VARCHAR(20) NOT NULL,        -- 'select'|'text'|'number'|'boolean'
  display_type       VARCHAR(20) NOT NULL,        -- 'dropdown'|'radio'|'swatch'|'button'
  uom_id             CHAR(36),                    -- for numeric attributes (e.g. Length in m)
  sequence           INTEGER     NOT NULL DEFAULT 0,
  is_active          SMALLINT    NOT NULL DEFAULT 1,
  -- + universal columns
  CONSTRAINT ux_attributes_code UNIQUE (code),
  CONSTRAINT ck_attributes_value_type CHECK (value_type IN ('select','text','number','boolean'))
);

attribute_values (
  id                 CHAR(36)    NOT NULL PRIMARY KEY,
  attribute_id       CHAR(36)    NOT NULL,
  code               VARCHAR(50) NOT NULL,        -- 'RED'
  value              VARCHAR(120) NOT NULL,       -- 'Red'  (translated via `translations`)
  swatch_hex         VARCHAR(9),                  -- '#C81E1E' for colour swatches
  image_ref          VARCHAR(255),                -- material/pattern thumbnails
  sequence           INTEGER     NOT NULL DEFAULT 0,
  is_active          SMALLINT    NOT NULL DEFAULT 1,
  CONSTRAINT ux_attribute_values UNIQUE (attribute_id, code)
);

-- ─── Product template ─────────────────────────────────────────────────────────

products (
  id                 CHAR(36)    NOT NULL PRIMARY KEY,
  code               VARCHAR(50) NOT NULL,        -- root SKU / item code
  name               VARCHAR(200) NOT NULL,       -- translated via `translations`
  category_id        CHAR(36),
  product_type       VARCHAR(20) NOT NULL,        -- 'stockable'|'service'|'consumable'|'kit'
  stock_uom_id       CHAR(36)    NOT NULL,        -- the unit stock is held in  (§B)
  sales_uom_id       CHAR(36)    NOT NULL,        -- default selling unit
  purchase_uom_id    CHAR(36)    NOT NULL,        -- default buying unit
  tax_group_id       CHAR(36),                    -- NULL → inherit category/branch/company
  price_mode         VARCHAR(20) NOT NULL,        -- 'reference_currency'|'fixed_local'  (§18.5)
  pricing_currency   CHAR(3),                     -- NULL → company pricing currency
  tracking           VARCHAR(10) NOT NULL DEFAULT 'none',   -- 'none'|'lot'|'serial'
  has_variants       SMALLINT    NOT NULL DEFAULT 0,        -- UI hint only, never logic
  is_active          SMALLINT    NOT NULL DEFAULT 1,
  CONSTRAINT ux_products_code UNIQUE (code)
);

product_attributes (              -- which attributes apply to this product
  id                 CHAR(36)  NOT NULL PRIMARY KEY,
  product_id         CHAR(36)  NOT NULL,
  attribute_id       CHAR(36)  NOT NULL,
  is_variant_defining SMALLINT NOT NULL DEFAULT 1,   -- §A.2 — the key flag
  sequence           INTEGER   NOT NULL DEFAULT 0,
  CONSTRAINT ux_product_attributes UNIQUE (product_id, attribute_id)
);

product_attribute_values (        -- which VALUES are in play for this product
  id                    CHAR(36) NOT NULL PRIMARY KEY,
  product_attribute_id  CHAR(36) NOT NULL,
  attribute_value_id    CHAR(36) NOT NULL,
  price_extra_micro     BIGINT   NOT NULL DEFAULT 0,  -- authoring convenience, §A.5
  CONSTRAINT ux_prod_attr_values UNIQUE (product_attribute_id, attribute_value_id)
);

product_specifications (          -- descriptive attributes (non-variant-defining)
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  product_id         CHAR(36) NOT NULL,
  attribute_id       CHAR(36) NOT NULL,
  attribute_value_id CHAR(36),                  -- for 'select'
  value_text         VARCHAR(255),              -- for 'text'
  value_number       BIGINT,                    -- for 'number' (×10⁶)
  value_bool         SMALLINT,
  CONSTRAINT ux_product_specs UNIQUE (product_id, attribute_id)
);

-- ─── Variant: the sellable, stockable, barcoded unit ──────────────────────────

product_variants (
  id                 CHAR(36)     NOT NULL PRIMARY KEY,
  product_id         CHAR(36)     NOT NULL,
  sku                VARCHAR(60)  NOT NULL,
  name_suffix        VARCHAR(200),               -- 'Red / XL'  (generated, translatable)
  price_micro        BIGINT,                     -- NULL → inherit product price  (§A.5)
  cost_micro         BIGINT       NOT NULL DEFAULT 0,   -- WAC, maintained by inventory (§D)
  weight_micro       BIGINT,
  volume_micro       BIGINT,
  reorder_point      BIGINT,                     -- quantity ×10⁶
  reorder_quantity   BIGINT,
  is_default         SMALLINT     NOT NULL DEFAULT 0,   -- the auto variant of a simple product
  is_active          SMALLINT     NOT NULL DEFAULT 1,
  CONSTRAINT ux_product_variants_sku UNIQUE (sku)
);

variant_attribute_values (        -- the coordinates of this variant in the matrix
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  variant_id         CHAR(36) NOT NULL,
  attribute_id       CHAR(36) NOT NULL,
  attribute_value_id CHAR(36) NOT NULL,
  CONSTRAINT ux_variant_attr_values UNIQUE (variant_id, attribute_id)
);

variant_exclusions (              -- combinations that must not be generated
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  product_id         CHAR(36) NOT NULL,
  attribute_value_a  CHAR(36) NOT NULL,
  attribute_value_b  CHAR(36) NOT NULL,
  reason             VARCHAR(200)
);

barcodes (
  id                 CHAR(36)    NOT NULL PRIMARY KEY,
  variant_id         CHAR(36)    NOT NULL,       -- always the variant, never the product
  barcode            VARCHAR(64) NOT NULL,
  symbology          VARCHAR(20) NOT NULL,       -- 'EAN13'|'UPC'|'CODE128'|'QR'|'INTERNAL'
  packaging_id       CHAR(36),                   -- §B.5 — scanning a case vs a unit
  is_primary         SMALLINT    NOT NULL DEFAULT 0,
  CONSTRAINT ux_barcodes UNIQUE (barcode)
);
```

Indexes: `ix_product_variants_product`, `ix_variant_attr_values_variant`,
`ix_variant_attr_values_value` (for "show me everything in Red"),
`ux_barcodes` (exact-match scan path), `ix_products_category`.

## A.4 Variant generation

A domain service, not ad-hoc UI logic:

1. Take the cartesian product of `attribute_values` across all `is_variant_defining` attributes.
2. Remove combinations matching `variant_exclusions`.
3. Diff against existing variants:
   - **New combinations** → create variant, generate SKU from the template pattern
     (`{product.code}-{value.code}-{value.code}`, configurable), compute `price_micro`
     from base + `price_extra_micro` sum, generate `name_suffix`.
   - **Removed combinations with no transaction history** → hard delete.
   - **Removed combinations with transaction history** → `is_active = 0`, **never deleted.**
     Deleting a variant that appears on a two-year-old invoice would break document reprinting
     and every historical report. This is enforced in the domain, not left to the UI.
4. Present the whole plan as a **preview** (N created, N deactivated, N unchanged) before it is
   applied — the same staged-change pattern used for exchange rates (§18.4). Adding a fifth
   colour to a product with four sizes silently creating 20 SKUs is exactly the kind of
   surprise a careful ERP avoids.

## A.5 Variant pricing **[RECOMMENDATION]**

Two mechanisms, deliberately separated by role:

- **Authoring convenience:** `product_attribute_values.price_extra_micro` — "XL costs 2 more."
  Used *only at generation time* to compute a price.
- **Storage truth:** `product_variants.price_micro`, an **absolute** price. `NULL` means
  "inherit the product's price."

Storing the resolved absolute price rather than a live delta is the important half. A live
delta chain must be re-evaluated at every price read, and it interacts badly with price lists,
currency conversion, tax-inclusive pricing, and rounding — the effective price becomes the
output of a four-stage computation that nobody can predict from looking at the record. An
absolute stored price is one number you can read, audit, and index, while the delta still
gives you the fast authoring path.

Price resolution order at sale time:
```
price list line (customer/branch specific)
  → variant.price_micro
    → product base price
      → error: product has no price
```

## A.6 Why not EAV or a JSON blob

Both were considered and rejected:

- **Pure EAV** (one generic `entity_attribute_value` table for everything) makes every product
  query a multi-way self-join, makes indexing nearly useless, and defeats the portability
  contract's goal of predictable SQL.
- **JSON attribute blobs** are unqueryable in a portable way — SQLite, MySQL, PostgreSQL and
  SQL Server have four different JSON query dialects, which directly violates §8. They also
  cannot carry foreign keys, so a renamed colour doesn't propagate.

The design above is **normalized where it must be queried** (variant coordinates, barcodes)
and flexible where it must be extensible (the attribute dictionary is data). That is the
combination that stays fast and stays configurable.

---

# B. Units of measure & fractional quantities

Confirmed for day one: kilograms, litres, metres, fabric, timber, cable.

## B.1 Quantity representation

`int64` scaled by **10⁶** (six decimal places), per §7.2. This covers every case you listed
with large headroom: 0.001 kg, 0.005 m, 0.25 litre. The int64 range at that scale reaches
~9.2 × 10¹² units — far beyond any physical inventory.

## B.2 Schema

```sql
uom_categories (
  id, code, name, is_system, ...              -- Weight, Volume, Length, Area, Count, Time
);

units_of_measure (
  id                    CHAR(36)    NOT NULL PRIMARY KEY,
  uom_category_id       CHAR(36)    NOT NULL,
  code                  VARCHAR(20) NOT NULL,   -- 'KG', 'G', 'M', 'CM', 'L', 'PCS'
  name                  VARCHAR(80) NOT NULL,   -- translated via `translations`
  symbol                VARCHAR(12) NOT NULL,   -- 'kg' — locale-aware display
  factor_to_reference   BIGINT      NOT NULL,   -- ×10⁹ relative to the category reference
  is_reference          SMALLINT    NOT NULL DEFAULT 0,  -- exactly one per category
  allows_fractional     SMALLINT    NOT NULL DEFAULT 1,  -- §B.4
  rounding_precision    BIGINT      NOT NULL,   -- ×10⁶, e.g. 1000 = 0.001
  display_decimals      INTEGER     NOT NULL DEFAULT 3,
  is_active             SMALLINT    NOT NULL DEFAULT 1,
  CONSTRAINT ux_uom_code UNIQUE (code)
);
```

**Conversion is only legal within a category.** `kg → g` is valid; `kg → m` is a domain error,
not a silent zero. Conversion goes through the category reference unit:

```
qty_target = qty_source × factor_source / factor_target
```
evaluated with a 128-bit intermediate and rounded once to the target's `rounding_precision`.

## B.3 Three units per product

`stock_uom_id`, `sales_uom_id`, `purchase_uom_id` — all in the same category. This is what
makes real trade work: **buy cable in rolls, stock it in metres, sell it in metres**; buy
sugar in 50 kg sacks, stock and sell in kg.

**`stock_uom_id` is immutable once the product has any stock movement.** Changing the unit
stock is held in would invalidate every historical quantity and every cost figure. The domain
rejects it; the correct operation is a new product plus a transfer.

Document lines store **both**: the entered quantity in the document's unit, and the converted
quantity in the stock unit.

```sql
quantity_micro        BIGINT NOT NULL,   -- as entered, in uom_id
uom_id                CHAR(36) NOT NULL,
quantity_stock_micro  BIGINT NOT NULL,   -- converted, in the product's stock UoM
```

Storing both means a reprinted invoice still says "2 rolls" while inventory correctly moved
200 metres — and neither figure depends on a conversion factor that might be edited later.

## B.4 Fractional control

`allows_fractional = 0` on `PCS`, `EACH`, `BOX` and similar count units. The domain rejects a
fractional quantity for those units with a clear, translatable error. You can sell 1.5 kg of
timber; you cannot sell 0.5 of a chair. This is a per-unit property rather than a global
setting because a single business legitimately has both.

`rounding_precision` is applied on entry, so a scale reading of 1.4372381 kg becomes 1.437 kg
once, at the boundary — not repeatedly and inconsististently in later calculations.

## B.5 Packaging **[RECOMMENDATION]**

Do **not** model "box of 12" as a unit of measure. It is product-specific, whereas a UoM is
universal — a kilogram is a kilogram for every product, but a box is 12 for one product and 24
for another. Putting product-specific factors into the global UoM table produces hundreds of
near-duplicate units and breaks conversion.

Separate concept:

```sql
product_packagings (
  id, variant_id, name,              -- 'Case of 24'
  quantity_micro,                    -- 24.000000 in the product's stock UoM
  barcode, is_purchase_default, is_sales_default, ...
);
```

Scanning a case barcode then adds 24 units in one action — which is the actual operational
requirement behind the request, achieved without polluting the UoM dictionary.

---

# C. Country profiles & the setup wizard

Confirmed: no country is hardcoded anywhere in Go code. Country is data.

## C.1 Country profile seeds

`seeds/country_profiles/<iso2>.json` — versioned, translatable, user-extendable:

```json
{
  "country_code": "SY",
  "name_key": "country.sy",
  "default_locale": "ar",
  "supported_locales": ["ar", "en"],
  "default_currency": "SYP",
  "functional_currency_default": "SYP",
  "pricing_currency_default": "USD",
  "chart_of_accounts_template": "coa_generic_trading_ar",
  "tax_profile": "tax_sy",
  "fiscal_year_start_month": 1,
  "date_format": "dd/MM/yyyy",
  "time_format": "HH:mm",
  "first_day_of_week": 6,
  "number_format": {
    "decimal_separator": ".",
    "thousand_separator": ",",
    "digit_grouping": [3],
    "numeral_system": "western"
  },
  "calendar": { "primary": "gregorian", "secondary": "hijri" },
  "address_format": ["street", "district", "city", "governorate", "country"],
  "phone_country_code": "+963",
  "rounding_rule": { "mode": "nearest", "increment_minor": 10000, "stage": "grand_total" }
}
```

Everything in this file is editable after setup. The profile only supplies **defaults**; it
never constrains later configuration. Adding a country is dropping in a JSON file — no code,
no release.

Note `numeral_system` and the `calendar.secondary` field: Arabic-Indic digits (٠١٢٣) versus
Western digits, and Hijri date display, vary by country *and* by customer preference within a
country. Both are settings, not assumptions.

## C.2 Setup wizard flow

Runs once, on first launch, and is itself driven by the profile registry:

```
1. Language                 → ar / en             (affects the wizard immediately)
2. Country                  → loads the country profile as defaults
3. Company                  → name, legal name, tax number, logo, address
4. Currency                 → functional currency (default from profile)
                            → pricing currency (optional; default USD or = functional)
5. Business profile         → Furniture / Pharmacy / Grocery / Restaurant / …
                              applies feature flags + settings + seed data (§16.4)
6. Fiscal setup             → fiscal year start, first period
7. Branch & warehouse       → one default of each, named by the user
8. Tax                      → enable/disable; if enabled, seed the country tax profile
9. Administrator account    → username, strong password (no default password ships)
10. Optional starting data  → import products/customers, or start empty
```

Every step is revisitable in Settings afterwards. The wizard writes settings and seeds; it
contains no logic that cannot be reproduced by editing configuration later — otherwise the
wizard becomes a hidden source of truth, which is a maintenance trap.

## C.3 Syria as the development seed

`sy.json` plus `coa_generic_trading_ar` and `tax_sy` become the fixtures for development and
demos. Two implications worth being explicit about, because they shape the code rather than
just the data:

- **Arabic is the primary development locale**, not a translation added at the end. RTL is
  verified continuously (§22.3), and the Arabic search normalization in §28 is a v1 feature,
  not a polish item.
- The tax profile ships **with the ability to be entirely empty or disabled.** Where VAT
  applicability is uncertain or varies, the correct default is tax disabled, with the customer
  enabling and configuring it — which the §19.5 design already supports cleanly.

I have deliberately **not** encoded specific Syrian tax rates or a specific chart of accounts
into this design. Those are jurisdictional facts that change, that I should not assert from
memory, and that the architecture is explicitly built to accept as data. Before Phase 2 I'll
need either the actual figures from you or your accountant, or confirmation that shipping an
empty, user-configured tax profile is acceptable for the first customer.

---

# D. Costing abstraction — WAC now, FIFO later

Confirmed: Weighted Average Cost by default, with FIFO addable without redesign.

## D.1 The strategy port

Costing is an **extension point** (§25.2), registered like any other:

```go
// internal/modules/inventory/domain/costing.go
type CostingStrategy interface {
    Key() string                                  // "wac" | "fifo" | "standard"

    // Called inside the same transaction as the stock movement.
    OnReceipt(ctx context.Context, s *StockState, m Movement) (CostResult, error)
    OnIssue(ctx context.Context, s *StockState, m Movement) (CostResult, error)
    OnAdjustment(ctx context.Context, s *StockState, m Movement) (CostResult, error)
    OnReturn(ctx context.Context, s *StockState, m Movement) (CostResult, error)
    OnRevaluation(ctx context.Context, s *StockState, m Movement) (CostResult, error)
}

type CostResult struct {
    UnitCost      UnitAmount   // cost applied to this movement
    NewAverage    UnitAmount   // resulting average (WAC)
    LayersUsed    []LayerUse   // consumed layers (FIFO) — empty for WAC
    ValueDelta    Money        // inventory value change → drives the GL posting (§20.3)
}
```

Nothing outside this interface knows which method is in use. Sales asks for the cost of an
issue; it does not know or care whether the answer came from an average or a layer.

## D.2 Layers are maintained in both modes **[the key decision]**

`inventory_layers` is written on **every receipt regardless of costing method**:

```sql
inventory_layers (
  id, variant_id, warehouse_id, lot_id,
  received_at, source_movement_id,
  quantity_micro,            -- original received quantity
  remaining_micro,           -- decremented on issue
  unit_cost_micro,           -- cost at receipt
  is_exhausted, ...
);
```

Under WAC these rows are recorded but not used for valuation. That costs one insert per
receipt and buys you the thing you asked for: **switching a company to FIFO is a configuration
change plus a recompute, not a migration.** If layers were only created when FIFO was enabled,
the switch would require reconstructing purchase history from movements — which is
approximate at best and impossible once opening balances are involved.

## D.3 WAC arithmetic

```
new_average = (qty_on_hand × current_average + received_qty × received_unit_cost)
              ÷ (qty_on_hand + received_qty)
```

Computed with a 128-bit intermediate, rounded once to the `UnitAmount` scale (§E). Explicit
handling for the cases that produce wrong numbers in naive implementations:

- **Zero or negative on-hand at receipt** → the new cost is simply the receipt cost; no
  division by a non-positive quantity.
- **Issue with insufficient stock** (where negative stock is permitted) → issues at the
  current average and records a `cost_variance` movement, so the variance is visible and
  postable rather than silently absorbed.
- **Returns** are costed at the **original issue's** cost, not the current average. Returning
  an item bought at last year's price must not create phantom profit. This requires the
  return document line to carry `source_line_id` — designed in from the start.
- **Landed costs** (freight, customs, clearing) allocated across a receipt's lines by value,
  weight, volume, or quantity, using largest-remainder allocation so the total ties exactly.

## D.4 Switching methods

Guarded, because it changes reported profit:

- Requires an explicit permission and is fully audited.
- **Only permitted at a fiscal period boundary** with all prior periods closed. Mid-period
  switching produces a period whose COGS is computed two different ways, which is
  indefensible to an auditor.
- Runs a revaluation job that recomputes valuation under the new method and posts the
  difference to an inventory revaluation account as a normal journal entry.
- The old method's figures remain intact in history; nothing is retroactively rewritten.

`costing_method` is a **company setting** in v1, with the schema allowing per-category override
later (`categories.costing_method_override`) — some businesses need FIFO for perishables and
average for hardware in the same company.

---

# E. Numeric precision contract

Fractional quantities force this to be settled precisely: with integer quantities, unit price
× quantity is exact; with 0.375 kg it is not. There are now **four** distinct numeric types,
and mixing them is a bug class worth designing out entirely.

| Type | Scale | Stored as | Used for |
|---|---|---|---|
| `Money` | currency minor units | `BIGINT` | Totals, GL amounts, payments, balances |
| `UnitAmount` | 10⁻⁶ of the **major** currency unit | `BIGINT` | Unit prices, unit costs, WAC |
| `Quantity` | 10⁻⁶ | `BIGINT` | All quantities |
| `Rate` | 10⁻⁹ | `BIGINT` | Exchange rates, UoM conversion factors |
| `Percent` | 10⁻⁶ | `BIGINT` | Tax rates, discount rates |

**Why `UnitAmount` is separate from `Money`:** the average cost of a metre of cable or a gram
of a compound is routinely a fraction of a minor unit. Rounding unit cost to whole cents and
then multiplying by 3,000 metres produces a materially wrong inventory valuation. Unit prices
and unit costs need more precision than the totals derived from them — but the totals must
land exactly on minor units, because you cannot pay a fraction of a cent.

**The rounding rule, stated once and enforced everywhere:**

> Multiply at full precision through a 128-bit intermediate. **Round exactly once**, at the
> point where a value becomes a `Money` figure — the line extension.

```
line_extension : Money = round( unit_price(UnitAmount) × quantity(Quantity) )
                         ↑ 128-bit intermediate, single rounding, explicit mode
```

Never round the unit price first. Never round twice. Discount and tax then operate on the
rounded line extension, and largest-remainder allocation guarantees the parts sum exactly to
the document total.

All of this lives in `internal/kernel/money` with property-based tests (§30) asserting the
invariants — no arithmetic of this kind is ever written inline in a use case.

---

# F. Refinements to decisions 4 and 8

Both of your decisions stand. Each needs one structural detail preserved now so the deferred
option remains genuinely available later — these are the two places where "we can add it
later" is only true if something small is done today.

**Numbering (decision 4).** Non-gapless is right for v1. Two rules apply anyway: allocate the
number at **posting** rather than draft creation, and allocate **transactionally**. Without
the first, abandoned drafts burn numbers and the sequence looks broken to the user even though
gaps are legal. Without the second, two terminals can produce the same number. Detailed in
§9.4 of the main document.

**Audit hash chaining (decision 8).** Excluded from v1 as you decided. The one thing that must
happen now is reserving `prev_hash` and `row_hash` as nullable columns in migration 0001,
because **a hash chain cannot be backfilled** — retroactive hashes over unprotected rows prove
nothing. When the enterprise edition enables chaining it writes a genesis record and protects
forward from that instant, reporting the protected range honestly. Detailed in §15.2.

---

# G. New recommendations arising from your decisions

Four items that your answers bring into scope. The first is the significant one.

## G.1 Multiple rate types per currency pair **[RECOMMENDATION — matters for Syria]**

The current `exchange_rates` design assumes one rate per currency pair per date. In the
markets Mizan is aimed at first, that assumption is wrong. Businesses in economies with
currency controls routinely track **more than one rate simultaneously** — an official/central
bank rate and a market rate — and legitimately use different ones for different purposes:
the official rate for tax filings and statutory accounts, the market rate for pricing and
purchasing decisions.

Without this, users are forced to pick one rate and manually adjust everywhere else, which
destroys the accuracy the whole currency subsystem exists to provide.

The change is one column plus one setting:

```sql
exchange_rates (
  ...,
  rate_type  VARCHAR(20) NOT NULL DEFAULT 'default',   -- 'official'|'market'|'custom'
  CONSTRAINT ux_exchange_rates UNIQUE (from_currency, to_currency, rate_type, valid_from)
);
```

Plus settings for which rate type each context uses: `currency.rate_type.pricing`,
`currency.rate_type.accounting`, `currency.rate_type.reporting`. A company with a single rate
sets all three to `default` and never sees the concept.

**Cost now: one column and three settings. Cost later: a schema migration touching every
historical rate row, plus reworking every rate lookup.** I recommend including it.

## G.2 Currency redenomination support **[RECOMMENDATION]**

High-inflation economies periodically redenominate — removing zeros and issuing a new
currency unit. Historical documents must keep their original amounts (an invoice for 500,000
old units must reprint as 500,000), while reports spanning the changeover need consistent
figures.

The good news is that **no new mechanism is required**, provided one assumption is avoided:
that currency codes are restricted to the ISO-4217 list. Model a redenomination as a new
currency record with a fixed conversion rate to its predecessor:

```sql
currencies (
  ...,
  succeeded_by_code   CHAR(3),      -- old → new
  redenomination_factor BIGINT,     -- ×10⁹, fixed, never expires
  is_historical       SMALLINT NOT NULL DEFAULT 0
);
```

Existing documents keep their original code untouched; the reporting layer converts through
the fixed factor when a range crosses the boundary. Requirements: allow non-ISO and custom
currency codes, never assume exactly three characters is ISO-valid, and never hard-delete a
currency that appears in any document.

Cost now: three columns and one rule ("currency codes are data, not an enum"). Cost later:
either falsified history or a painful data migration under time pressure.

## G.3 Price update workflows for inflation

Given USD-referenced pricing in a high-inflation local currency, the exchange-rate preview
(§18.4) will be used **frequently** — possibly daily — not as a rare administrative event.
That changes its design priorities:

- It must be **fast and low-friction**: a keyboard-driven review screen, not a wizard.
- It needs **bulk actions**: apply to a category, exclude a supplier's products, apply only
  where the change exceeds a threshold.
- It needs **psychological price rounding** — the per-currency rounding rules in §18.6 applied
  as part of the preview, so prices land on sensible figures rather than 47,382.
- Rate staleness must be **visible in the POS itself**, not buried in settings. Selling at a
  three-day-old rate during rapid movement is a real loss.

This is a UX priority note for Phase 5, not a schema change — the tables already support it.

## G.4 Dual calendar display

Listed as open question §33.1(5). If required: store Gregorian always (never store a Hijri
date as the source of truth — the conversion has regional variance), display a secondary
calendar per the country profile, and allow Hijri entry with immediate Gregorian conversion
shown. Confirm whether the first customer needs it; the `calendar.secondary` field is already
in the profile schema either way.

---

# H. Updated build order

Unchanged in shape; the resolved decisions add specific work to phases 0, 1, and 3.

| Phase | Additions from these decisions |
|---|---|
| **0. Foundation** | Four-type numeric kernel with the single-rounding rule (§E) + property-based tests |
| **1. Core data** | Country profile registry, setup wizard, functional/pricing currency split, dual-calendar setting |
| **2. Financial spine** | `rate_type` on rates (§G.1), redenomination columns (§G.2), country-driven tax profile seeding |
| **3. Master data** | Full UoM system (§B), variant architecture (§A), packagings, specifications |
| **4. Inventory** | Costing strategy port with WAC implementation; layers written from day one (§D) |
| **5. Sales & POS** | Fractional quantity entry, packaging/case scanning, fast rate-update workflow (§G.3) |

Phases 0–3 carry almost all of the irreversible decisions. Everything after them is
comparatively mechanical if these are right.

---

# I. Confirmations — RESOLVED

Both new recommendations are **APPROVED**, with scope expanded per your instructions.

1. **Rate types (§G.1) — APPROVED and expanded.** The currency subsystem supports multiple
   rate types from the start (`official`, `market`, `custom`, `manual`, extensible). Rate
   types are **data, not an enum** — new types are added without a schema change. Which rate
   type applies is configurable **per context**: pricing, purchasing, sales, accounting,
   reporting, and tax each resolve their own rate type through settings. v1 may expose only
   one in the UI while the backend already carries all six context bindings. Full design in
   the Phase 0 document, §CUR.
2. **Redenomination (§G.2) — APPROVED.** Currencies are database entities with
   successor/predecessor relationships and fixed conversion factors. Historical documents are
   never rewritten; cross-period reports convert through fixed factors. Currency codes are
   data, never a hardcoded enum, and non-ISO codes are permitted.
3. **Tax profiles — CONFIRMED country-independent.** No Syrian or country-specific tax or
   accounting rule is encoded in Go. Tax is disabled by default for the first customer,
   fully user-configurable, with all country settings stored as data.

**Overarching mandate (recorded):** every configurable business rule — taxes, currencies,
document numbering, invoice/print templates, units of measure, payment methods, exchange-rate
providers, posting rules, report definitions — is stored as configuration or metadata rather
than hardcoded logic wherever reasonable. This is now formalized as an architectural pillar:
see **Phase 0 §CFG, "Configuration & Metadata Architecture."**

---

*End of addendum v1.1. Foundational decisions resolved. Proceed to*
[`PHASE_0_FOUNDATION.md`](./PHASE_0_FOUNDATION.md)*.*
