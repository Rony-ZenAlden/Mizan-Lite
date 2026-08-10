# Phase 3 — Master Data (Design)

> Status: **DESIGN — implemented step by step under the standing autonomous mandate.**
> Scope: units of measure, the catalog (categories, products, variants, attributes, barcodes,
> packagings), partners, and price lists.
> **Out of scope:** stock levels and movements (Phase 4); anything that sells or buys (Phases
> 5–6). This phase builds *what* is traded; the next builds *how much of it there is*.

---

## 1. ANALYSIS

### 1.1 What makes this phase risky

Not correctness — **shape**. Phase 2's danger was books that balance and are wrong; this
phase's is a schema that works and cannot grow.

Addendum §A puts it plainly: variants *"touch inventory, pricing, barcodes, sales lines,
purchasing, and every report"*. A wrong decision here does not fail a test — it multiplies
through five later phases and is only visible when changing it means migrating transaction
history.

So this phase is mostly about honouring decisions already made, and the two that matter most
were made for exactly that reason.

### 1.2 Every product has at least one variant — §A.1

A simple product — a bag of cement, a haircut — gets one automatically-created **default
variant**, invisible in the UI. There is no such thing as a product without variants.

The reason is not elegance. Every downstream table references `variant_id NOT NULL`:
`stock_levels`, `sales_document_lines`, `barcodes`, `price_list_items`, `stock_movements`,
`inventory_layers`. The alternative — a nullable `variant_id` meaning "the product itself" —
forces **every** query, stock lookup, price resolution, and report to handle two cases forever.

The cost is one row per simple product. The benefit is that a product can later gain real
variants **without migrating its transaction history**: the existing default simply becomes the
first variant.

### 1.3 Variant-defining versus descriptive attributes — §A.2

Colour makes variants. Warranty period does not. Without that split, every specification anyone
records becomes a variant dimension, and a product with 4 colours × 3 sizes × 6 specs generates
72 SKUs instead of 12.

The flag lives on the **product-attribute link**, not on the attribute: material defines
variants for a sofa and is a specification for a screwdriver.

### 1.4 Packaging is not a unit of measure — §B.5

A kilogram is a kilogram for every product. A box is 12 for one and 24 for another. Modelling
"box of 12" as a UoM produces hundreds of near-duplicate units and breaks conversion, so
packagings are a separate, product-specific concept.

### 1.5 The open question this phase must answer — D1

§33.1 question 2: *"Does v1 need multiple price lists (wholesale/retail/customer-specific), or
one price with per-line discounts?"*

**Decision: multiple price lists, with resolution.** Taken autonomously, and the reasoning is
the §26 argument in miniature — retrofitting a price-resolution layer onto sales lines that
already exist means touching every document, every report, and every historical price. The
schema cost now is one table and one resolution function; the cost later is a migration.

It also matches how the product already works: resolution with a recorded reason is exactly the
shape tax (§19.3) and settings (0.5) use, so it is a pattern to reuse rather than invent.

**A single-price shop never sees it.** One default list, no picker, no concept.

### 1.6 Risks

| Risk | Consequence | Answer |
|---|---|---|
| Nullable `variant_id` | Two cases in every query, forever | Every product has a default variant (§1.2) |
| Attribute explosion | 72 SKUs where 12 belong | Variant-defining is a per-product flag (§1.3) |
| A variant deleted after it was sold | Historical documents cannot be reprinted | Deactivate, never delete, when history exists (§2.4) |
| `stock_uom` changed after movements | Every historical quantity and cost silently restated | Immutable once stock has moved (§2.2) |
| Fractional chairs | "0.5 of a chair" sold | `allows_fractional` per unit, enforced in the domain (§2.2) |
| Cross-category conversion | kg silently becomes metres | Conversion is legal only within a category — the kernel already refuses (§2.1) |
| Price lists retrofitted later | A migration touching every document | Built now (§1.5) |

---

## 2. DESIGN

### 2.1 Units of measure — reusing the kernel

`quantity.Unit` and `quantity.Quantity` already exist (0.2) with category-safe conversion,
`allows_fractional`, and int64 micro scaling. This phase adds the **persistence** and the
category dictionary; the arithmetic is not rewritten.

Conversion goes through the category's reference unit, with a 128-bit intermediate, rounded once
to the target's precision — which the kernel does.

### 2.2 Three units per product — §B.3

`stock_uom`, `sales_uom`, `purchase_uom`, all in one category. This is what makes real trade
work: buy cable in rolls, stock and sell it in metres.

**`stock_uom` is immutable once anything has moved.** Changing the unit stock is held in would
invalidate every historical quantity and cost. Phase 3 has no movements yet, so the rule is
written now and enforced against a port Phase 4 satisfies — declared at the point of use, like
identity's `Organisation` (1.2).

### 2.3 Attributes and specifications — §A.3

The dictionary is global and reusable; the link to a product carries `is_variant_defining`.
Specifications are stored per product, typed by the attribute's `value_type`.

### 2.4 Variant generation — §A.4

A domain service, not UI logic:

1. Cartesian product of the variant-defining attribute values.
2. Remove `variant_exclusions`.
3. Diff against what exists: new combinations are created; removed ones are **deleted only if
   they have no history**, and deactivated otherwise.
4. Return a **plan** the caller previews before applying — the staged-change pattern exchange
   rates use (§18.4). Adding a fifth colour to a product with four sizes silently creating 20
   SKUs is exactly the surprise a careful ERP avoids.

Step 3 is the one that matters: *"deleting a variant that appears on a two-year-old invoice
would break document reprinting and every historical report. This is enforced in the domain."*

### 2.5 Partners — one table — decision 9

Customers and suppliers are one `partners` table discriminated by role flags, not two tables.
A business that both buys from and sells to the same company is ordinary, and two tables make
that either a duplicate or a join nobody remembers to write.

### 2.6 Price lists — D1

```
price_lists       code, name, currency, is_default, valid_from/to, priority
price_list_items  price_list_id, variant_id, price_micro, min_quantity_micro
```

Resolution, highest priority first, mirroring §19.3's shape:

1. a partner's assigned list
2. a branch's default list
3. the company default list
4. the variant's own price
5. the product's price

Quantity breaks live in `min_quantity_micro`, so "10 or more, cheaper" needs no second concept.

---

## 3. STEP SEQUENCE

| Step | Deliverable |
|---|---|
| **3.1** | Units of measure: categories, units, conversion, fractional control |
| **3.2** | Catalog: categories, products, the default variant, attributes |
| **3.3** | Variant generation, exclusions, barcodes, packagings |
| **3.4** | Partners |
| **3.5** | Price lists and resolution |
| **3.6** | Screens: catalog browse, product detail, partners |
| **3.7** | Phase 3 Definition-of-Done review |

---

## 4. DEFINITION OF DONE

1. Every product has at least one variant; a simple product's is created automatically and
   never shown.
2. `variant_id` is `NOT NULL` everywhere it appears.
3. Conversion within a category is exact and round-trips; across categories it is a typed error.
4. A fractional quantity of a non-fractional unit is refused.
5. `stock_uom` cannot change once stock has moved.
6. Variant generation produces a previewable plan, and refuses to delete a variant with history.
7. An attribute is variant-defining for one product and descriptive for another.
8. A barcode resolves to exactly one variant, and a packaging barcode resolves to a quantity.
9. One `partners` table serves customers and suppliers, including a partner who is both.
10. Price resolution follows the documented order and records which list answered.
11. Every new binding has a declared policy; every state change is audited in-transaction.
12. `make ci` green, with the mutation drills each step declares.

---

*Implementation proceeds step by step; each step records its own decisions and drills.*

---

## STEP RECORDS

### Step 3.1 — Units of measure

**Delivered.** `uom_categories` and `units_of_measure` (0015), the pure domain
(`Convert`, `Normalise`, `ValidateCategoryUnits`), the layered `seeds/uom` loader with
`ApplyUnits`, the sqlite repositories, and the catalog module wired into the composition root
and the setup wizard.

**D1 — the domain re-checks what the kernel already checks.** `Convert` refuses a cross-category
conversion itself rather than letting `quantity.ConvertTo` refuse it, because the kernel reports
a mismatch of its own opaque category identifiers and a user needs to read "KG and M measure
different things". The duplication is a *precondition*, not a fallback — see the drill below for
why that distinction had to be made testable.

**D2 — a fractional count is refused, never rounded.** Rounding 0.5 of a chair up invents stock
and rounding it down loses a sale. Neither is a decision code makes on a user's behalf.

**D3 — a re-apply never overwrites.** `ApplyUnits` is idempotent by unit code, so an upgrade
that re-runs it leaves a unit an administrator renamed exactly as they left it (0.5's rule), and
records nothing in the audit trail when it creates nothing — an entry per boot saying "no units
were created" is the noise that trains people to ignore the trail.

**D4 — `App.Catalog` and `setup.Service.catalog` were the i18n catalogue.** Both renamed to
`Messages`/`messages`. Two fields called "catalog" on one struct is a collision waiting for
whoever reads it next.

**Mutation drills — 5 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | Cross-category guard removed | **Passed at first — the test was wrong.** Fixed; see below. |
| 2 | Fractional refusal removed | `TestAFractionalCountIsRefused`, `…IsNotSilentlyRounded` fail |
| 3 | Fractional count silently *rounded* instead of refused | Both fractional tests fail |
| 4 | Reference-factor-must-be-one check removed | `TestAReferenceMustHaveAFactorOfOne` fails |
| 5 | One-reference-per-category check removed | Domain **and** service tests fail |

**Drill 1 is the one worth reading.** Deleting the domain's own cross-category check changed
nothing any test could observe: the kernel refuses the same conversion, and `Convert` wraps that
refusal with the same error code and the same two unit-code params, so both paths were
indistinguishable — the comment claimed a difference that did not exist in observable
behaviour. The test now asserts `errors.Unwrap(err) == nil`, which pins the refusal as having
happened *before* the arithmetic was attempted. The fifth time a drill has corrected a test
rather than the code.

**Also caught:** drill 4's first run reported `(cached)` — the mutation had never applied, an
indentation mismatch in the patch. Every later drill asserts the substitution actually changed
the file before running the tests.
