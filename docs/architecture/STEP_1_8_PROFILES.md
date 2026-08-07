# Step 1.8 — Country & business profiles, and seed-file discovery (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green; both mutation drills confirmed (§7.3).
> Scope: `platform/seeds` (discovery, ordering, layering); the `profile` module (country
> profiles as files, business profiles as a catalogue + bundle); the seed files themselves.
> **Out of scope:** the setup wizard (1.9) — nothing here is *called* by a wizard yet; tax
> profiles and charts of accounts (Phase 2, and see §2.4); Hijri date *rendering* (§6, D7).

---

## 1. ANALYSIS

### 1.1 What this step is really for

§16.4 calls business profiles *"the concrete mechanism that satisfies 'generic ERP through
configuration, not code'"*. That sentence is the reason this project exists in the shape it
does, and this is the step that builds the mechanism.

The country side is the same idea pointed at jurisdiction rather than trade: Addendum §C says
**no country is hardcoded anywhere in Go code — country is data**, and *"adding a country is
dropping in a JSON file — no code, no release."*

"No release" is load-bearing. It means the loader cannot read only from the embedded binary.

### 1.2 The Phase 0 item this closes

0.5 D7 deferred *"JSON seed-file discovery and ordering across modules"* to 0.9, where the
first real seed would exist. 0.9 seeded currencies from **Go literals** instead and carried the
item forward again. 0.12 carried it a third time.

Country and business profiles are the first seeds that are genuinely file-shaped: numerous,
edited by non-programmers, and expected to grow without a release. This is the first real
consumer, so this is where the mechanism gets built — and, per the pattern this project keeps
hitting, **built at the moment it has a consumer, not before**.

### 1.3 The risk this step actually carries

Not complexity. **Asserting facts I cannot verify.**

A country profile encodes claims about the world: which currency, which week-start, which date
format, which tax regime. Addendum §C.3 is explicit that Syrian tax rates and charts of
accounts must **not** be encoded from memory — they are jurisdictional facts that change, to be
supplied by the customer or their accountant.

That constraint is honoured here and extended: every field in a shipped profile is either
(a) an ISO/ITU registry value, or (b) a **default the user sees and can change in the wizard**.
Nothing encodes a legal or fiscal rule. `tax_profile` and `chart_of_accounts_template` are
present in the schema and **null in every shipped file**.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A malformed user file bricks the install | The customer cannot open their shop | Shipped files are fatal, **user files are reported and skipped** (D4) |
| A profile becomes a second source of truth | `date_format` in a profile row disagrees with the setting | Country profiles are **not stored** (D1); the wizard copies values into settings and the profile is never read again |
| A bundle names a setting that does not exist | The wizard half-applies and fails at the tenth key | Bundles are validated against the config registry **at load**, so a bad shipped bundle fails at boot (D5) |
| A user file silently overrides a shipped one | Support cannot reproduce the customer's behaviour | Overrides are whole-file, by name, and reported in the load report |
| Shipping empty bundles looks like a stub | Reviewer assumes the mechanism is unfinished | Stated plainly: no business module has declared settings yet (§4.3) |

---

## 2. DESIGN — `platform/seeds`

### 2.1 What belongs in the platform, and what does not — D3

The platform does **discovery, ordering, layering, size limits, and problem reporting**.
It does *not* decode profiles: `platform` must not import a module (`platform-independent-of-modules`),
and a country profile is a module's vocabulary.

```go
type Layer struct { FS fs.FS; Origin Origin }   // OriginShipped | OriginUser

func Discover(dir string, layers ...Layer) (Set, error)

type Set struct { Files []File; Problems []Problem }
type File struct { Path, Name string; Data []byte; Origin Origin }
```

`Name` is the base filename without `.json`, and it is the **identity**: a later layer's
`sy.json` replaces an earlier layer's entirely.

### 2.2 Replace, never merge

A user file replaces a shipped file **whole**. Merging would produce a document that is half
ours and half theirs, and no one — least of all a support engineer three years from now — could
say which half produced a given behaviour. Replacement is legible: *this file, from this layer.*

### 2.3 Shipped is fatal, user is skipped — D4

```go
func Decode[T any](set Set, decode func([]byte, *T) error) (Result[T], error)
```

Returns an error **only** when a *shipped* file fails. A user file that fails is appended to
`Problems`, and the load continues.

The asymmetry is the whole rule, and it is centralised here so that no module can get it wrong:

- A broken **shipped** file is a bug in our build. It must fail loudly, on a developer's
  machine, in CI — never silently on a customer's.
- A broken **user** file is a typo by an administrator at 11pm. It must not stop the business
  from opening tomorrow. It is reported, and the shipped file it was replacing stays in force.

### 2.4 Strict JSON

Decoding uses `DisallowUnknownFields`. A profile with `"defualt_locale"` must be reported, not
silently ignored — a silently-ignored key is a setting the customer believes they configured.

Each file is capped (1 MiB). A seed document is a page of JSON; anything larger is a mistake or
a hostile file, and neither should be read into memory unbounded.

---

## 3. DESIGN — country profiles

### 3.1 They are files, not a table — D1

There is no `country_profiles` table. This is a deliberate departure from the shape of every
other reference data set in the system, and the reason is §C.2:

> *The wizard writes settings and seeds; it contains no logic that cannot be reproduced by
> editing configuration later — otherwise the wizard becomes a hidden source of truth.*

A country profile is read **once**, at setup, to supply defaults. Every value it offers is then
copied into a setting or a company column, which is from that moment the only source of truth.
A stored profile row would be a *second* answer to "what is this company's date format?", one
that never changes and that some future query would eventually read. That is a bug waiting for
its first author.

So: the registry is loaded at startup, held in memory, and consulted by the wizard. Nothing
persists.

### 3.2 The document

Addendum §C.1's shape, decoded strictly:

```json
{
  "country_code": "SY",
  "name_key": "country.sy",
  "default_locale": "ar",
  "supported_locales": ["ar", "en"],
  "functional_currency_default": "SYP",
  "pricing_currency_default": "USD",
  "chart_of_accounts_template": null,
  "tax_profile": null,
  "fiscal_year_start_month": 1,
  "date_format": "dd/MM/yyyy",
  "time_format": "HH:mm",
  "first_day_of_week": 6,
  "number_format": { "decimal_separator": ".", "thousand_separator": ",",
                     "digit_grouping": [3], "numeral_system": "western" },
  "calendar": { "primary": "gregorian", "secondary": "hijri" },
  "address_format": ["street", "district", "city", "governorate", "country"],
  "phone_country_code": "+963",
  "rounding_rule": { "mode": "nearest", "increment_minor": 10000, "stage": "grand_total" }
}
```

Validated on load: the code is two upper-case letters **and matches the filename**; currencies
are three letters; the month is 1–12; the week-start is 0–6; `supported_locales` is non-empty
and contains `default_locale`; `numeral_system` and `calendar.primary` are from closed sets.

`name_key` rather than a name: the country's name is translated like everything else (§22),
and a profile that carried an English string would make English the source language of the
country list.

### 3.3 What ships — D6

`sy.json` is the development seed, per §C.3. `sa`, `ae`, and `eg` ship alongside it because
the first market is regional and a one-entry country list is not a testable wizard.

Every shipped file contains **only** ISO/ITU registry values (currency, phone code) and
**defaults the user sees and changes** (locale, date format, week start). `tax_profile` and
`chart_of_accounts_template` are `null` in all four. No file asserts a tax rate, a legal
requirement, or an accounting structure.

> **For the reviewer:** the non-registry defaults — date format, first day of week, pricing
> currency — are my best reading, not verified facts. Each is one line in one file and is shown
> to the user during setup before it takes effect. If any is wrong for the first customer, say
> so and it changes with no code.

### 3.4 Calendar — D7 (open question 1)

`calendar.secondary` is carried as **data** and nothing renders it. Storage is Gregorian
regardless (§G.4), so building the secondary display later changes a formatter and no schema.

Deciding "do we build Hijri display now?" needs the first customer's answer, and answering it
wrong in either direction is expensive: building it unused is waste, and assuming Gregorian-only
in the *storage* layer would be a rewrite. The profile carries the preference so the question
stays answerable, and the expensive half is already correct.

---

## 4. DESIGN — business profiles

### 4.1 A catalogue table plus a bundle file — D2

Two halves, deliberately:

| Half | Where | Why |
|---|---|---|
| **Catalogue** — code, name, description, active | `business_profiles` table (0009) | It is a list the UI shows and an administrator may deactivate; that is metadata, and metadata has a table (§CFG.3) |
| **Bundle** — the settings and flags it applies | the JSON file | It is a script run once, not a record. Storing it would invite editing the stored copy, which would then disagree with the file |

The catalogue rows are seeded **from** the bundle files through the existing idempotent
`code`-keyed seeder, so `Module.Metadata()` needed no contract change to carry file-sourced
seeds.

### 4.2 Validated at load, not at apply — D5

Every key a bundle names is checked against the config registry when the module is built:
`settings` keys must be declared settings, `flags` keys must be declared flags.

`Settings.Set` already rejects an undeclared key — but it rejects it *at apply time*, halfway
through the wizard, on the customer's machine. Checking at load turns the same defect into a
boot failure on a developer's machine for a shipped bundle, and a reported-and-skipped file for
a user's.

### 4.3 What ships, and an honest note — D8 (open question 4)

`general_retail.json` (the neutral default) and `furniture.json` (the known first customer).

**Both bundles are nearly empty, and that is correct rather than unfinished.** A bundle can only
set settings that some module has declared, and today those are language, theme, password
policy, session policy, and currency roles — none of which differ between a furniture shop and a
pharmacy. The settings that *will* differ (lot/expiry tracking, receipt layout, default units,
terminology) belong to modules that do not exist until Phase 3–5.

The mechanism is what ships now. Filling the bundles is a data task that happens as each module
declares its settings, which is exactly the property that makes this "configuration, not code".

### 4.4 `Apply` — D9

```go
func (s *Service) Apply(ctx context.Context, companyID id.ID, code string) error
```

Writes every setting and flag in the bundle at **company scope**, in one transaction, and
publishes `Auditable` — so applying a profile is a recorded act with an actor, like every other
change since 1.7.

Built now although only 1.9 will call it: it is the mechanism §16.4 names, its contract is
already fixed by the bundle format, and building it with the bundle keeps the format honest.
The seam is one step from its consumer, not three phases.

---

## 5. TESTING PLAN

| Level | Tests |
|---|---|
| **Discovery** | Deterministic order; a user file replaces a shipped one by name; both origins reported |
| **The asymmetry (D4)** | A malformed **shipped** file fails the load; a malformed **user** file is reported and skipped, and the shipped file it shadowed stays in force |
| **Strictness** | An unknown key is an error, not a silent ignore |
| **Country validation** | Filename/code mismatch; bad month; a `default_locale` missing from `supported_locales` |
| **Shipped files** | Every shipped file parses and validates — the test that makes D4's "fatal" claim true |
| **§C.3** | **No shipped country profile carries a tax profile or a chart of accounts** |
| **Catalogue** | Business profiles seed idempotently; a second run reports zero changes |
| **Bundle validation (D5)** | A bundle naming an undeclared setting or flag is rejected at load |
| **Apply** | Writes at company scope; is atomic; is audited |

**Mutation-verified**: (1) make a user-file failure fatal → the "a bad user file must not stop
the shop" test must fail; (2) drop `DisallowUnknownFields` → the strictness test must fail.

---

## 6. DECISIONS TAKEN

1. **D1 — Country profiles are files, never a stored table.** Read once at setup; a stored copy
   would be a second source of truth for values the wizard has already written into settings.
   *(§3.1.)*
2. **D2 — Business profiles split into a catalogue table and a bundle file.** The list is
   metadata; the bundle is a script. *(§4.1.)*
3. **D3 — `platform/seeds` discovers, orders, layers, and reports; modules decode.** Platform
   must not learn a module's vocabulary. *(§2.1.)*
4. **D4 — A broken shipped file is fatal; a broken user file is reported and skipped.** A bug in
   our build must fail on our machine; a typo in a customer's file must not close their shop.
   *(§2.3.)*
5. **D5 — Bundles are validated against the config registry at load**, not at apply. *(§4.2.)*
6. **D6 — `sy` (per §C.3) plus `sa`, `ae`, `eg` ship.** ISO/ITU values and user-visible defaults
   only; no tax rate and no chart of accounts in any file. *(§3.3.)*
7. **D7 — `calendar.secondary` is carried as data; no Hijri rendering is built.** Storage is
   Gregorian either way, so the expensive half is already right. *(§3.4 — open question 1.)*
8. **D8 — `general_retail` and `furniture` ship**, both nearly empty, because no module has yet
   declared a setting that differs by trade. *(§4.3 — open question 4.)*
9. **D9 — `Apply` is built now**, one step from its consumer, and is audited. *(§4.4.)*

### Known gaps, carried

- **The non-registry defaults in the shipped country files are unverified** (§3.3). One line
  each, shown to the user before they take effect.
- **`Apply` runs with no actor during setup**, so its audit entry will read `system`. Correct
  today; revisit when the wizard creates the administrator *before* applying the profile (1.9).

---

## 7. IMPLEMENTATION RECORD

### 7.1 What was built

| File | What |
|---|---|
| `platform/seeds/seeds.go` | Discovery, deterministic ordering, layering, the 1 MiB bound, `Decode`'s shipped-is-fatal rule, `StrictJSON` |
| `modules/profile/country.go` | The `Country` document, its validation, the loader |
| `modules/profile/business.go` | The `Business` bundle, registry validation, the catalogue seed, `Apply` |
| `modules/profile/profile.go` | The service, the two-layer load, `UserFS`, the module surface |
| `modules/profile/migrations/0009_profiles.sql` | `business_profiles` — the catalogue only |
| `seeds/country_profiles/{sy,sa,ae,eg}.json` | Four profiles, no tax rate and no chart of accounts in any |
| `seeds/business_profiles/{general_retail,furniture}.json` | Two bundles, honestly near-empty |
| `bootstrap` | The module wired, with the data-directory overlay |

### 7.2 Three decisions taken during implementation

**`Module.Metadata()` needed no contract change.** The catalogue rows come from files rather
than Go literals, and `Metadata() []SeedSpec` never said where a spec came from. The 0.5 D7
item closed without touching the module interface — the seam was already the right shape,
which is worth recording because it is the first Phase-0 seam to fit at first use rather than
needing adjustment.

**Validation moved out of the decode callback.** `seeds.Decode` treats any error on a shipped
file as fatal, so validating inside the callback would have been correct — but it would have
tied every module's validation to the decode step and lost the file's `Origin` at the point of
the check. Validation now runs over the decoded documents, where the origin is in hand and the
same rule is applied explicitly. Slightly more code, and the rule is visible instead of implied.

**`param()` renders error arguments.** `errs` parameters are strings because they cross the
i18n boundary (§22.2), where a message is a template and its arguments are text. One helper
rather than each call site reaching for `fmt` differently.

### 7.3 The mutation drills

**Drill 1 — a customer's broken file is fatal too.** In `seeds.Decode`, `if file.Origin ==
OriginShipped` → `if true`.

*Result:* `TestABrokenUserFileIsSkippedNotFatal` and `TestABrokenUserFileDoesNotStopTheModule`
both failed — *"a customer's malformed file stopped the load"*. D4 is watched at both the
platform and the module level.

**Drill 2 — drop `DisallowUnknownFields`.**

*Result:* `TestAnUnknownKeyIsRejected` failed — *"an unknown key was accepted; a typo would be
silently ignored"*.

### 7.4 What the tests found

`TestApplyWritesAtCompanyScopeAndIsAudited` failed on its first run with
`profile.undeclared_key` for `ui.theme` — a key that plainly exists. The cause is real and
worth writing down: **the settings registry is populated by package `init`**, so a key is
declared only if something imported the package declaring it. Bootstrap imports every module,
so production is complete; a focused test is not.

Two consequences. The test now blank-imports `platform/ui`, with the reason at the import. And
`TestABundleWithAWrongTypedValueIsRejected` had been **passing for the wrong reason** — the
bundle was rejected as undeclared, never reaching the type check it claimed to exercise. That
is the third time in this project a passing test has turned out to be testing nothing; the
pattern each time is a test whose failure mode and success mode are reached by different paths.

### 7.5 Carried forward

- **The non-registry defaults in the four shipped country files are unverified** — date format,
  first day of week, pricing currency. One line each, shown to the user during setup before they
  take effect, and changeable with no code.
- **Both business bundles are empty.** They fill as modules declare settings that differ by
  trade, which is Phase 3–5. The mechanism, its validation, and `Apply` are complete and tested.
- **`Apply` has no caller yet.** Step 1.9's wizard is the consumer, one step away.
- **Country name keys are translation keys**, and `country.{sy,sa,ae,eg}` were added to both
  catalogues. A fifth country dropped in by a customer will render its key until someone
  translates it — acceptable, and the alternative (an English name in the file) would make
  English the source language of the country list.
