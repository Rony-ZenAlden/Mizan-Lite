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

### Step 3.2 — Categories, products, the default variant, attributes

**Delivered.** `0016_catalog.sql` (seven tables), the product/category/variant/attribute domain,
the repositories, and the service: `CreateCategory`, `CreateProduct`, `ChangeStockUnit`,
`CreateAttribute`, `LinkAttribute`, `MarkVariantHistory`.

**D1 — §A.1 is a constructor guarantee, then a transaction guarantee.** `NewProduct` returns
`(Product, Variant, error)`: there is no code path in the domain that produces a product without
its default variant. `InsertProduct` then takes both and writes both inside the caller's
transaction, because the one place the invariant could still break is between two INSERTs.

**D2 — `is_variant_defining` lives on the LINK.** Material defines variants for a sofa and
describes a screwdriver. Putting the flag on the attribute would force a business selling both
to define "material" twice, and the whole point of a shared dictionary is that it is shared.

**D3 — a variant-defining attribute must be a `list`.** Generation is a Cartesian product and
needs a finite set to multiply; "warranty in months" has none. Refused by name, rather than
producing zero variants and leaving the user to work out why.

**D4 — `combination` is sorted.** `COLOUR:RED|SIZE:L` regardless of the order the attributes came
back in. Nothing guarantees a stable order from the database, and without the sort generation
would create a duplicate variant every time it changed.

**D5 — `MarkVariantHistory` exists before anything calls it.** Phase 4 sets the flag on the first
movement and Phase 5 on the first document line. It is written now because it is what the two
strongest rules READ — a variant that cannot be deleted, a stock unit that cannot change — and a
rule whose trigger does not exist yet is a rule no test can exercise. The same reason identity
declared its `Organisation` port before org existed (1.2).

**Mutation drills — 7 run, all fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | `InsertProduct` stops writing the default variant | 4 tests fail, incl. the whole-table property |
| 2 | Units-comparable check removed | Domain **and** service tests fail |
| 3 | Stock-unit lock removed | `…IsLockedOnceStockHasMoved` fails at both levels |
| 4 | `ux_variant_one_default` weakened to a plain index | `…CannotHaveTwoDefaultVariants` fails |
| 5 | Default variant becomes deletable | `TestTheDefaultVariantCannotBeDeleted` fails |
| 6 | Enumerability check removed | Domain and service tests fail |
| 7 | `Combination` stops sorting | `…IsIndependentOfAttributeOrder` fails |

Drill 1 is the one that matters: it fails `TestNoProductIsEverWithoutAVariant`, which asserts the
property over the whole table rather than per-product, because the path that breaks §A.1 will be
one nobody thought to write a specific test for.

### Step 3.3 — Variant generation, exclusions, barcodes, packagings

**Delivered.** `0017_variants.sql` (`variant_exclusions`, `product_packagings`, `barcodes`), the
generation domain (`GeneratePlan`, `NewExclusion`, `NewPackaging`, `NewBarcode`, `Resolve`), the
repositories, and the service: `PlanVariants`, `ApplyVariantPlan`, `Exclude`, `CreatePackaging`,
`CreateBarcode`, `ScanBarcode`.

**D1 — the plan is re-computed inside the transaction, not carried in from the preview.** A plan
is a snapshot of the catalog at the moment it was made. Between the preview and the click,
somebody may have sold one of the variants it intended to delete — applying the stale plan would
then delete a variant that now has history, which is the exact thing §A.4 forbids. Re-planning
under the write lock is the only safe version, and the preview stays honest because it calls the
same function.

**D2 — history decides delete-versus-deactivate, in the domain.** Not in the caller, not on the
screen. A variant that appears on a two-year-old invoice is retired; one that never appeared on
anything is removed. `ApplyVariantPlan` calls `RequireDeletable()` again before each delete, so
the decision cannot be bypassed by calling the repository directly.

**D3 — a retired variant that is wanted again is REVIVED, not re-created.** Reusing the row keeps
its history, its barcodes, and its SKU. A new row would orphan every label already printed.

**D4 — an exclusion is a subset test, not equality.** `COLOUR:RED` removes every red variant;
`COLOUR:RED|SIZE:XXL` removes exactly one. A manufacturer who drops a colour says so once rather
than listing every size it came in.

**D5 — a cap of 1000 variants per generation.** Not a technical limit. Five attributes with six
values each is 7,776 variants, and the person who added the fifth was thinking about the fifth.
The cap turns an unusable catalog back into an error naming the number, before anything is
written.

**D6 — a barcode is trimmed but never upper-cased**, unlike every other code in this module. A
barcode is scanned, not typed; the scanner sends exactly what is printed, and folding case would
make two distinct printed codes collide — the one thing a barcode must never do.

**Mutation drills — 8 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | History stops protecting a variant | 4 tests fail across both levels |
| 2 | A retired wanted variant is re-created | `…ReactivatedNotRecreated` + service test fail |
| 3 | Exclusions ignored | 5 tests fail |
| 4 | Partial exclusion becomes exact match | `…RemovesEveryCombinationContainingIt` fails |
| 5 | `ux_barcodes_code` scoped per variant | **Passed at first — the test was wrong.** See below. |
| 6 | A retired barcode still scans | `TestARetiredBarcodeIsNotRecognised` fails |
| 7 | A box barcode resolves to 1 | `…ResolvesToTwelve` fails at both levels |
| 8 | The explosion cap removed | `…RefusedBeforeAnythingIsWritten` fails |

**Drill 5 is the one worth reading.** `CreateBarcode` looks a code up and refuses a duplicate
*before* the index is ever consulted, so scoping the index per-variant changed nothing any test
could observe — the test was passing on the service guard while claiming to pin the schema. But
the service guard only protects writes that go through the service, and the migration comment
claims more: that a bad import cannot produce two. A new test now writes straight to the table,
the way 3.2's default-variant test does. **The sixth drill to correct a test rather than the
code**, and the second in this phase to catch a redundant guard masking the one that matters.

### Step 3.4 — Partners

**Delivered.** A new `partner` module: `0018_partners.sql` (`partners`, `partner_addresses`,
`partner_contacts`, `partner_role_usage`), the domain, the repositories, the service, and the
composition-root wiring.

**D1 — one table, two role flags (decision 9).** A workshop that buys steel from a merchant and
sells them finished brackets is ordinary. Two tables would make that either a duplicate identity
nobody keeps in step or a join nobody remembers to write — and the statement of account for such
a company would be two queries a developer has to think to combine. The cost is columns only one
role uses; NULL says "not applicable" perfectly well.

**D2 — `partner_role_usage` is separate from `has_history`, and the split is the interesting
part.** They drive different rules:

- `has_history` refuses **deletion** — the partner appears on some document.
- Per-role usage refuses removing **that one role** — a supplier who has been billed has a
  payable in the accounts, and clearing the flag would hide them from the supplier list while the
  balance remains: a payable that becomes invisible without becoming settled.

One flag could not express both, and the case it would get wrong — a partner who bought once and
supplies constantly, wanting to stop being a customer — is not a rare shape. Drill 4 exists
precisely to keep the two apart.

**D3 — a credit limit of zero means NO LIMIT, not "no credit".** The two readings differ by every
sale the business makes, and a limit of zero refusing everything is never what leaving the field
alone was meant to say. The rule lives in the domain so that what zero means has exactly one
home; a check re-written at the point of sale would be a second place to forget it.

**D4 — permissions split by role as well as by action.** A salesperson maintains customers and
has no business editing supplier payment terms; a buyer the reverse. One `partner.manage` would
force a business to choose between granting too much and granting nothing.

**D5 — addresses are free-form lines.** Street/number/district columns assume one country's
shape and force every other into the wrong boxes, which §1 says this cannot do.

**Mutation drills — 8 run, all fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | A partner may have no role (domain) | `TestAPartnerMustHaveAtLeastOneRole` fails |
| 2 | The schema CHECK on roles dropped | `TestTheDatabaseRefusesARolelessPartner` fails |
| 3 | A used role can be removed | `…CannotBeRemoved` + the audit test fail |
| 4 | Role usage collapsed to one flag | `…UnusedRoleCanStillBeRemoved…` fails |
| 5 | Zero credit limit means no credit | `TestAZeroCreditLimitMeansNoLimit` fails |
| 6 | A partner with history becomes deletable | `…DeactivatedNotDeleted` fails |
| 7 | One-default-address index weakened | `TestOnlyOneDefaultAddressPerPurpose` fails |
| 8 | The role filter ignores `is_supplier` | `…SeparatesPureCustomersFromPureSuppliers` fails |

Drills 1 and 2 are deliberately a pair: the domain and the schema each keep the same rule, and
each is watched to fail on its own — the lesson 3.3's fifth drill taught, where a service guard
was silently standing in for the index the test claimed to pin.

### Step 3.5 — Price lists and resolution

**Delivered.** A new `pricing` module: `0019_pricing.sql` (`price_lists`, `price_list_items`,
`partner_price_lists`, `branch_price_lists`), the pure resolution domain, the repositories, the
service, and the composition-root wiring. This realises **D1** — multiple price lists with
resolution — decided in §1.5.

**D1 — §2.6's five levels became three lists × two granularities, and the reason is module
ownership.** Levels 4 and 5 were "the variant's own price" and "the product's price": columns on
`product_variants` and `products`, tables the **catalog** module owns. A module adding a column
to another module's table breaks the ownership rule that makes a modular monolith survive ten
years, and the alternative — catalog carrying a price column for a feature it knows nothing
about — is worse.

So a `price_list_item` targets **either** a product or a variant, and a variant-level item
overrides a product-level one *within the same list*. Same expressive power, one mechanism
instead of two, and no column crossing a boundary. The partner's assigned list moved from a
column on `partners` to a table here for exactly the same reason, correcting what 0018's comment
promised.

**D2 — list priority outranks granularity.** A partner's blanket product price beats the default
list's price for that exact variant. That is what a negotiated rate means, and getting it
backwards would quietly ignore every agreement the business has made.

**D3 — the highest qualifying quantity break wins.** Taking the first match instead would charge
the base price on an order of ten and lose the sale the discount existed to win.

**D4 — no price is an ERROR, but a price of zero is a price.** A variant nobody has priced is a
configuration gap; returning zero would sell it for nothing, silently, on a receipt that looks
perfectly ordinary. A free sample priced at zero is a different thing entirely, and the two have
their own tests sitting next to each other.

**D5 — a missing default list is a distinct error from an unpriced item.** One is a setup step
nobody completed; the other is a product nobody priced. Different problems, different answers.

**D6 — sale and purchase are one concept with a direction.** A supplier's agreed rates and a
customer's wholesale list are both price lists; separating them by table would duplicate every
column and both resolution functions.

**Mutation drills — 9 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | Product price checked before variant price | Domain and service tests fail |
| 2 | The LOWEST qualifying break wins | 3 tests fail |
| 3 | An unpriced variant resolves to zero | 3 tests fail |
| 4 | List validity dates ignored | `…OutOfDateIsSkipped`, `…RetiredListIsSkipped` fail |
| 5 | One-default-list index weakened | `TestOnlyOneDefaultListPerDirection` fails |
| 6 | `ck_price_item_targets_one` dropped | **Passed at first — the test was wrong.** See below. |
| 7 | The domain accepts both/neither targets | `TestAPriceTargetsExactlyOneThing` fails |
| 8 | The branch list consulted before the partner's | 2 service tests fail |
| 9 | Direction ignored on the default list | `TestSaleAndPurchaseListsAreSeparate` fails |

**Drill 6, and the pattern now has a name.** `NewItem` refuses a price targeting both or neither
before the CHECK is ever reached, so dropping the constraint changed nothing any test could see.
This is the **third** time in Phase 3 that a higher layer silently stood in for a lower one the
test claimed to pin — after 3.1's cross-category check (the kernel covered it) and 3.3's barcode
index (the service covered it). The rule that came out of it:

> **When two layers keep one rule, each needs a test that can only fail if THAT layer is the one
> enforcing it.** In practice: write straight to the table.

Drills 6 and 7 are now that pair, as drills 1 and 2 were in 3.4.

### Step 3.6 — Screens: catalog browse, product detail, partners

**Delivered.** Two bindings (`Catalog`, `Partners`), four screens (catalog browse, product
detail, partner list, partner detail), a `Badge` primitive, three routes, and 22 frontend tests.

**D1 — read-only, deliberately.** The same reasoning §20.6 applies to accounting: Phase 3 builds
the master data and the screens that READ it. A create form belongs with the release that has
designed the workflow around it; shipping one now would put a control behind a permission nobody
has a process for.

**D2 — §A.1 is made invisible on screen.** Every product has a default variant, and the browse
list shows a **dash** rather than "1" for a simple product, while the detail hides the variants
section entirely. `isSimple` is computed in the binding, not the screen, so the rule has one
home. A shop selling bags of cement never encounters the word "variant".

**D3 — the partner detail method is split into `Customer` and `Supplier`.** A single method would
need "either permission", and the policy model deliberately holds **one** permission per method
so that what a method requires is a fact of its declaration rather than something evaluated at
call time. Widening the model for one caller would be inventing a seam; two methods say the same
thing without it, and the screens are two screens anyway.

**D4 — the catalog filters in the browser, the partner list on the server.** A category tree is
bounded and already loaded; a partner list is not, and searching by **tax number** matters at a
counter where the customer hands over a card with a number on it and nothing else the operator
can type.

**D5 — locks are explained, never rendered as controls that refuse.** A stock unit fixed by
movement and a role fixed by trade both produce a short explanation rather than a disabled
toggle, because a control that always fails teaches people the software is broken.

**D6 — a credit limit of "0" reads as "No limit".** Printing the zero would say the opposite of
the truth, and the two readings differ by every sale the business makes. It is also hidden
entirely for a pure supplier, where it is a field nothing reads.

**Mutation drills — 8 run, all now fail as required.**

| # | Mutation | Result |
|---|---|---|
| 1 | Simple product's variant count prints "1" | `…dash rather than 1` fails |
| 2 | Variants section shows for a simple product | **Passed at first — the test was wrong.** See below. |
| 3 | Zero credit limit prints as "0" | `…reads a zero credit limit as no limit` fails |
| 4 | Raw combination string shown | `…renders the combination readably` fails |
| 5 | Stock-unit lock explanation dropped | `…explains why a stock unit cannot be changed` fails |
| 6 | Role-lock explanation dropped | `…explains that a traded role cannot be removed` fails |
| 7 | Credit limit shown for a pure supplier | `…does not show a credit limit…` fails |
| 8 | Category filter ignored | `filters the list by category` fails |

**Drill 2, and the pattern crosses to the frontend.** The test asserted the absence of a table
named "Variants" — but the row filter empties the list anyway, and `Table` renders an
`EmptyState` rather than a `<table>` when it has no rows. The test was passing on the filter
while claiming to pin the `isSimple` guard. It now asserts on the **heading**, which renders
unconditionally inside the guarded block and can therefore only be removed by the guard.

**Fourth occurrence in Phase 3** — after 3.1 (the kernel covered the domain check), 3.3 (the
service covered the index), and 3.5 (the domain covered the CHECK). The rule 3.5 wrote down holds
on the frontend unchanged: *when two mechanisms produce the same visible outcome, the test must
name the one it is about.*

---

## Step 3.7 — Phase 3 Definition-of-Done review

**Result: 11 of 12 met in full; 1 met with a deliberate, documented exception that needs a
decision.** Every criterion below names the test that proves it, and every one of those tests was
watched to FAIL under a mutation before being counted.

| # | Criterion | Verdict | Proof |
|---|---|---|---|
| 1 | Every product has at least one variant; a simple product's is automatic and never shown | ✅ | `NewProduct` returns `(Product, Variant, error)` — no domain path yields a product alone. `TestNoProductIsEverWithoutAVariant` asserts it over the whole table; the browse screen shows a dash, not "1" |
| 2 | `variant_id` is `NOT NULL` everywhere it appears | ⚠️ **exception** | See below |
| 3 | Conversion within a category is exact and round-trips; across it is a typed error | ✅ | `TestConversionRoundTrips`, `TestCrossCategoryConversionIsRefused` |
| 4 | A fractional quantity of a non-fractional unit is refused | ✅ | `TestAFractionalCountIsRefused`, `…IsNotSilentlyRounded` — refused, never rounded |
| 5 | `stock_uom` cannot change once stock has moved | ✅ | `TestTheStockUnitCannotChangeOnceStockHasMoved` (domain) and `…IsLockedOnceStockHasMoved` (through storage) |
| 6 | Generation produces a previewable plan and refuses to delete a variant with history | ✅ | `TestPlanningChangesNothing`, `TestAVariantThatHasBeenSoldIsDeactivatedAndOneThatHasNotIsDeleted` |
| 7 | An attribute is variant-defining for one product and descriptive for another | ✅ | `TestAnAttributeIsVariantDefiningForOneProductAndDescriptiveForAnother` — one dictionary row, two meanings |
| 8 | A barcode resolves to exactly one variant; a packaging barcode to a quantity | ✅ | `TestABarcodeResolvesToExactlyOneVariant`, `TestTheDatabaseRefusesADuplicateBarcode…`, `TestScanningABoxBarcodeResolvesToTwelve` |
| 9 | One `partners` table serves both roles, including a partner who is both | ✅ | `TestAPartnerCanBeBothCustomerAndSupplier`, `TestTheDatabaseRefusesARolelessPartner` |
| 10 | Price resolution follows the documented order and records which list answered | ✅ | `TestResolutionFollowsTheDocumentedOrder` (6 cases), `TestAPartnersListBeatsTheBranchAndTheDefault` |
| 11 | Every new binding has a declared policy; every state change is audited in-transaction | ✅ | The startup policy check covers `Catalog` and `Partners`; `TestARefusedCreateIsNotAudited`, `TestARefusedChangeIsNotAudited`, `TestARefusedPriceIsNotAudited` prove the rollback takes the entry with it |
| 12 | `make ci` green, with the mutation drills each step declares | ✅ | Green. **40 drills** across 3.1–3.6 |

### Criterion 2 — the exception, stated plainly

`price_list_items.variant_id` is **nullable**, and §1.2 names `price_list_items` among the tables
that must carry `NOT NULL`. This is a direct consequence of the 3.5 decision to let a price
target either a product or a variant, and it is my deviation, not an oversight.

**What the criterion exists to prevent** (§1.2): a nullable `variant_id` meaning *"the product
itself"*, which forces every query, stock lookup, price resolution, and report to handle two
cases forever.

**What is actually true here:**

- The NULL does not mean "the product itself" ambiguously — `ck_price_item_targets_one` makes
  the database refuse any row that is not **exactly one** of the two kinds. The state the
  criterion fears is unrepresentable, not merely discouraged.
- Exactly **one** function reads both cases: `domain.Resolve`, whose entire job is choosing
  between granularities. No stock lookup, no document line, no report touches it.
- Every *transactional* `variant_id` — `product_variant_values`, `barcodes`, and everything
  Phases 4–6 will add — is and must stay `NOT NULL`.

**The alternative**, if the criterion is to be met literally: split the table into
`price_list_items` (`variant_id NOT NULL`) and `price_list_product_items`
(`product_id NOT NULL`). The repositories already load the two granularities as two separate
queries, so the change is mostly mechanical. The cost is a second table duplicating
`price_minor`, `min_quantity_micro`, and `is_active`, plus a second insert path.

**This was escalated rather than settled, and has now been decided: the design stands as
implemented.** The `ck_price_item_targets_one` CHECK carries the invariant, exactly-one-of-two
remains unrepresentable, and criterion 2 is read as governing *transactional* `variant_id`
columns — which every one of them, present and future, still satisfies. Recorded here so the
deviation stays visible to whoever reads §1.2 next.

### What Phase 3 leaves for later

Carried deliberately, not forgotten:

- **No create/edit forms.** The screens read; the workflows that write belong with the releases
  that design them (§20.6's staging, applied here).
- **Variant generation has no screen.** The plan/apply pair exists and is tested, but nothing
  shows the preview yet — 3.3 built the mechanism, and the screen belongs with the release that
  introduces variant editing.
- **`MarkVariantHistory` and `MarkHistory` have no production caller.** Phase 4 and Phase 5 are
  their triggers. They exist now because they are what the strongest rules READ, and a rule whose
  trigger does not exist is a rule no test can exercise.
- **Packagings have no screen**, and `Catalog.Scan` / `Catalog.Price` have no caller — Phase 5's
  till is the consumer.
- **`branch_price_lists` cannot be set from a screen**, only through the service.

### The lesson Phase 3 taught

Four of the 40 mutation drills **passed**, and each time the code was right and the test was
wrong — a second layer was silently standing in for the one the test claimed to pin:

| Step | The test thought it pinned | What actually answered |
|---|---|---|
| 3.1 | the domain's cross-category check | the kernel's own refusal |
| 3.3 | `ux_barcodes_code` | `CreateBarcode`'s duplicate lookup |
| 3.5 | `ck_price_item_targets_one` | `NewItem`'s validation |
| 3.6 | the `isSimple` guard | the empty-row filter |

**The rule, now written down:** *when two layers or two mechanisms produce the same observable
outcome, a test must name the one it is about* — assert on something only that layer controls
(no wrapped cause; a direct table write; the heading rather than the table). Defence in depth is
right; a test that cannot tell which defence fired is not.
