# Step 0.8 — Internationalization Platform & Seed Locales (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30 — all seven decisions
> (D1–D7) accepted as recommended, including D3's upfront cost of writing every error string
> in both languages. Implementation record at **§10**.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `locales/{en,ar}/*.json` (new, at module root), `internal/kernel/locale`,
> `internal/platform/i18n`, and replacing the frontend's duplicate dictionary.
> **Out of scope:** the React `useTranslation` hook, provider, and language switcher UI
> (Step 0.11), locale-aware document printing and Hijri calendars (§22.5, Phase 9), and
> business entity translations (they arrive with their entities).

Two different problems share the word "translation", and ARCHITECTURE_v1 §22.4 is explicit
that they **must not share a mechanism**:

| | **UI strings** | **User content** |
|---|---|---|
| Examples | "Save", "Invoice total", error messages | Product names, category names, account names |
| Authored by | Developers, at build time | The customer, at runtime |
| Storage | `locales/*.json`, shipped in the binary | `translations` table |
| Adding a language | Add a JSON file | Insert rows |

Conflating them produces either user data trapped in source control or UI strings that a
customer can accidentally delete. This step builds both, separately.

---

## 1. ANALYSIS

### 1.1 The rule that makes instant language switching possible

§22.2 states it plainly: **the backend never produces user-facing prose.** It returns error
codes and parameters, and the frontend renders them.

This is not stylistic. If translated strings came from the server, switching language would
leave every already-rendered error in the old language — a partial switch, which looks broken
in a way a full one never does. Every step so far has honoured it: `errs.Error` carries
`Code` + `Params`, and every `Description` field in the settings and jobs registries is
documented as "an i18n key, never prose".

**Step 0.8 is where those codes finally acquire meaning.** It also means the Go message
resolver is a small component with a narrow purpose (§1.3), not the centre of this step.

### 1.2 A drift already in the tree

§22.1 requires **one** source of truth: `locales/{en,ar}/*.json`, consumed by Go via
`go:embed` and by React at build time — "one set of files, one key namespace, no drift
between backend error messages and frontend labels".

What actually exists after Step 0.1:

- `locales/` **does not exist**.
- `frontend/src/i18n/locales.ts` hardcodes a `MESSAGES` dictionary in TypeScript, with a
  comment conceding it is a placeholder.

So today there is one catalog, in the wrong place, invisible to Go. That is not yet drift —
but it becomes drift the moment the backend needs a string, and every step that passes makes
it more expensive to unify. **This step creates `locales/` and deletes the duplicate**, rather
than leaving two catalogs to diverge until 0.11.

### 1.3 What the backend actually needs strings for

Since the backend returns codes, the Go resolver serves only the few artifacts the backend
renders itself:

- **Printed documents** (Phase 9) — a PDF is generated server-side and has no React to render it.
- **Generated file names** — `backup-2026-07-30.mizanbak`, export filenames.
- **Operator-facing log lines**, where an English-only message is correct.

Small, but real. Building it now costs little and settles the key namespace before eight
modules start inventing their own.

### 1.4 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Two catalogs drift | Backend and frontend disagree about what a key means | One `locales/` tree, consumed by both; a test asserts Go and TS see identical keys (§6.2) |
| An error code has no catalog entry | The user sees `migrate.checksum_mismatch` on screen | **Coverage test over every declared `Code*` constant** (§6.3, **D3**) |
| A locale is missing keys another has | Blank labels in Arabic only, found by a customer | Key-set parity test across locales (§6.1) |
| Locale change needs a restart | The §22.3 promise ("no reload, no restart") quietly fails | Locale is a **setting**; the existing `SettingChanged` bus event drives it (§3.3, **D2**) |
| A second locale-change mechanism | Two ways to observe the same fact, drifting | Reuse `config.SettingChangedEvent`; no `LocaleChanged` type (**D2**) |
| Arabic pluralisation done naively | Wrong grammar in front of every Arabic customer | **No pluralisation in this step**, and the reason is recorded (§4.3, **D4**) |
| User-content cache unbounded | A shop with 40,000 products holds every name in memory | Lazy per-`(entity_type, locale)` load, invalidated on write (§5.2, **D5**) |

---

## 2. DESIGN — `locales/` at the module root

```
locales/
├── en/
│   ├── common.json     # shared UI vocabulary
│   └── errors.json     # one entry per stable error Code
└── ar/
    ├── common.json
    └── errors.json
```

At the module root, not under `internal/`, for the same reason `migrations/` is: **`go:embed`
cannot reach above its own directory**, and burying the catalogs inside a Go package would
hide them from translators and from the frontend build.

Keys are namespaced and flat — `"errors.migrate.checksum_mismatch"`, `"common.action.save"`.
Flat rather than nested because the error codes that dominate this catalog are already
dotted strings, and nesting would mean splitting them on every lookup.

**JSON, not Go or TS**, because a translator must be able to edit a catalog without a compiler
and without touching source.

---

## 3. DESIGN — `kernel/locale`

```go
// internal/kernel/locale
type Locale string          // "en", "ar"
type Direction string       // "ltr", "rtl"

func Parse(s string) (Locale, bool)
func (l Locale) Direction() Direction
func (l Locale) IsRTL() bool
func (l Locale) FallbackChain() []Locale   // ar → en; en → en

const Default = Locale("en")

// carried in context, like correlation/causation in kernel/event
func WithLocale(ctx, l) context.Context
func FromContext(ctx) Locale               // Default when unset
```

Pure kernel — stdlib only, per the `kernel-purity` archlint rule.

### 3.1 The fallback chain

§I18N specifies `ar → en → key`. Resolution takes the first locale that defines the key; if
none do, **the key itself is returned**.

Returning the key rather than an empty string is deliberate: a missing translation shows
`common.action.save` on screen, which is ugly, obviously wrong, and instantly greppable. A
blank button is none of those things.

### 3.2 Locale is not hardcoded to two values

`Parse` accepts any well-formed tag and the catalog loader discovers locales from the
directory listing. Adding Kurdish is dropping in `locales/ku/` — the same "data, not code"
mandate that governs everything else. `en` and `ar` are the seed set, not the definition.

### 3.3 Locale changes reuse the settings mechanism — D2

§I18N mentions "a `LocaleChanged` event". Steps 0.5 and 0.6 already deliver exactly that
capability, and I recommend **not** adding a second one.

The active locale is a **setting** — `ui.locale`, declared in `platform/i18n`, scoped
system/company/user (a cashier may prefer Arabic on a shop configured in English). Writing it
already publishes `config.SettingChangedEvent` on the domain bus (0.6, D7), which is precisely
"subscribers react without a restart".

A distinct `LocaleChanged` event would be a second way to observe one fact, and the two would
eventually disagree about ordering. Instead `i18n` exposes:

```go
func OnLocaleChanged(bus *eventbus.Bus, name string, fn func(context.Context, locale.Locale) error) error
```

which subscribes to `SettingChangedEvent` and filters. **One mechanism, typed ergonomics.**

---

## 4. DESIGN — the message catalog and resolver

### 4.1 Loading

`locales/` is embedded with `go:embed all:locales`. At startup the catalog is parsed once into
`map[Locale]map[string]string`, and **parse failure is fatal**: a malformed catalog is a build
defect that must never reach a customer, and unlike a stale settings row it is not inert — it
silently blanks the UI.

### 4.2 Resolution

```go
type Catalog struct{ /* locale → key → template */ }

func (c *Catalog) T(l locale.Locale, key string, params map[string]string) string
func (c *Catalog) Has(l locale.Locale, key string) bool
func (c *Catalog) Locales() []locale.Locale
func (c *Catalog) Keys(l locale.Locale) []string
```

Parameters interpolate as `{name}`:

```json
{ "errors.migrate.database_too_new": "This database was created by a newer version ({version})." }
```

Simple brace substitution rather than `text/template`: catalogs are edited by translators, and
a template syntax error would be a runtime failure in a string a translator wrote. An unknown
placeholder is left as-is rather than erroring — a visible `{version}` is a bug report;
a dropped one is a mystery.

### 4.3 No pluralisation — D4

Arabic has **six** plural forms (zero, one, two, few, many, other). Anything less than full
ICU plural rules produces text that is subtly wrong to every Arabic-speaking customer, and
"looks fine to the English-speaking developer" is exactly how that ships.

Phase 0 needs none: no seed string is quantity-dependent. So this step deliberately implements
**no** pluralisation rather than a naive `count == 1` fallback that would be silently wrong.

When the first plural string appears, the choice is ICU MessageFormat (a dependency — weighed
against the offline constraint, as with cron in 0.7) or per-locale plural-rule functions.
Recorded here so it is a decision, not a discovery.

---

## 5. DESIGN — user-content translations

The `translations` table already exists from `0001_platform.sql` (§9.5), including
`UNIQUE (entity_type, entity_id, field_name, locale)` and the lookup index. **No migration is
needed.**

### 5.1 The resolver

```go
type Translations struct{ /* db, cache */ }

// Resolve returns the translated field, falling back to the entity's base column value.
func (t *Translations) Resolve(ctx, entityType string, entityID id.ID, field, fallback string) string
func (t *Translations) ResolveMany(ctx, entityType string, ids []id.ID, field string, fallbacks map[id.ID]string) map[id.ID]string
func (t *Translations) Set(ctx, entityType string, entityID id.ID, field string, l locale.Locale, value string) error
```

`ResolveMany` exists because the alternative is a query per row while rendering a product
list — the classic N+1 that turns a 500-product grid into 500 round trips.

The `fallback` parameter is the entity's own `name` column (§9.5's "base `name` column
(fallback)"), so an untranslated entity shows its original name rather than a blank cell.

### 5.2 Caching — D5

**Lazy, per `(entity_type, locale)`, invalidated on write to that entity type.**

Not load-everything-at-startup: a shop with 40,000 products would hold every name in memory
before the first screen renders, on a machine also running the UI and SQLite. Not per-key
caching either: rendering a grid would miss on every row.

Loading one entity type's translations for the active locale on first use is the shape that
matches how the data is actually read — a screen shows one kind of thing at a time.

If a customer's catalog grows past what this comfortably holds, the fix is a bounded LRU
behind the same interface. Recorded so it is a known evolution, not a surprise.

---

## 6. DESIGN — the completeness gates

These are the part of this step most likely to prevent a real customer-visible defect.

### 6.1 Locale parity

A Go test asserts every locale defines **exactly** the same key set — no missing keys, no
orphans. This already exists for the TS placeholder dictionary (`locales.test.ts`, Step 0.1)
and moves to the real catalogs.

### 6.2 Go and TypeScript see the same catalog

The frontend imports `locales/{en,ar}/*.json` directly (Vite resolves JSON in the module
graph; it needs `server.fs.allow` for dev since the files sit above the Vite root). The
hardcoded `MESSAGES` dictionary is **deleted**.

Typed keys come free: `keyof typeof enCommon` gives the union, so a mistyped key is a
TypeScript error — the same structural guarantee as the typed setting handles in 0.5, with no
code generation.

A test asserts the key set Go loads equals the key set TypeScript imports, so §22.1's "no
drift" is enforced rather than asserted.

### 6.3 Every error code has a translation — D3

The backend returns codes; the frontend renders them. A code with no catalog entry therefore
reaches the user as raw text like `outbox.handler_panicked`.

Proposal: a Go test that walks `internal/**` with `go/ast`, collects every `Code* = "..."`
constant declaration, and asserts each has an entry in `errors.json`. The codebase already
declares these consistently — `CodeChecksumMismatch = "migrate.checksum_mismatch"` and about
forty others — and every step's doc comment calls them "stable codes that double as i18n
keys". This test is what makes that claim true.

We already have AST-walking machinery in `tools/archlint`, but a plain test is the simpler
home: this is a completeness check on data, not an architecture rule.

Expected initial cost: writing ~40 error strings in two languages. That is the actual work
this gate forces, and it is work that would otherwise surface one missing string at a time, in
front of customers.

---

## 7. DESIGN — package layout & dependencies

```
locales/{en,ar}/{common,errors}.json     # single source (§22.1)
internal/kernel/locale/                  # Locale, Direction, fallback, context carriers
internal/platform/i18n/
├── catalog.go        # go:embed, parse, T(), Has(), Locales()
├── setting.go        # the ui.locale setting + OnLocaleChanged helper
└── translations.go   # user-content resolver, cache, Set
```

`kernel/locale` imports stdlib only. `platform/i18n` imports `kernel/{locale,errs,id}`,
`platform/database`, `platform/config`, and `platform/eventbus`. Nothing imports `i18n`
except the bootstrap and, later, the printing layer.

---

## 8. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **Locale** | `Parse` accepts known and unknown-but-wellformed tags, rejects garbage; `ar` is RTL and `en` LTR; fallback chain is `ar→en` and `en→en`; context carrier round-trips and defaults to `en` when unset. |
| **Catalog** | A key present in the requested locale resolves; a key missing in `ar` falls back to `en`; **a key missing everywhere returns the key itself**; parameters interpolate; an unknown placeholder is left visible; a malformed catalog fails loudly at load. |
| **Parity — the gate** | Every locale defines exactly the same keys (no missing, no orphans). |
| **Cross-language parity** | The key set Go embeds equals the key set the frontend imports. |
| **Error-code coverage — the gate** | Every `Code* = "..."` constant declared under `internal/**` has an entry in `errors.json`, in every locale. |
| **Translations** | Resolve returns the translation when present and the fallback when absent; `ResolveMany` is one query for N entities (asserted by count, not by timing); `Set` upserts on the unique key; the cache is invalidated on write; a different locale is cached separately. |
| **Locale setting** | Writing `ui.locale` publishes `SettingChangedEvent`; `OnLocaleChanged` fires with the new locale and **ignores unrelated setting changes**; user scope beats company scope (the cashier case). |
| **Integration** | Against the real `translations` table through a Unit of Work, so the UNIQUE constraint is genuinely exercised. |

**Mutation-verified**, per the pattern: *fallback chain* (make `ar` resolution skip the `en`
fallback → the fallback test must fail) and *error-code coverage* (delete one entry from
`errors.json` → the gate must fail naming that code).

---

## 9. DECISIONS REQUESTED

1. **D1 — Create `locales/` at the module root and delete the frontend's hardcoded
   dictionary**, so §22.1's single source is real from this step rather than after 0.11.
   *(Recommended; §1.2, §6.2.)*
2. **D2 — Locale is a setting (`ui.locale`); no separate `LocaleChanged` event.** Reuse the
   0.6 bus event with a typed `OnLocaleChanged` helper. *(Recommended; §3.3.)*
3. **D3 — An error-code coverage gate**: every declared `Code*` constant must have an entry in
   `errors.json` in every locale. Costs ~40 strings × 2 languages now.
   *(Recommended; §6.3 — this is the decision with real upfront work attached.)*
4. **D4 — No pluralisation in this step**, rather than a naive one that would be wrong in
   Arabic. The choice is recorded for when the first plural string appears. *(§4.3.)*
5. **D5 — Lazy per-`(entity_type, locale)` translation cache**, invalidated on write, rather
   than load-all or per-key. *(§5.2.)*
6. **D6 — The backend still returns codes, never prose.** The Go resolver exists only for
   backend-rendered artifacts (printed documents, generated filenames, operator logs).
   *(§1.1, §1.3.)*
7. **D7 — No migration.** `translations` already exists from `0001_platform.sql`. *(§5.)*

On approval I'll implement in this order, each independently reviewable: `kernel/locale` →
`locales/` seed catalogs → `catalog.go` + resolver → the parity and error-code gates (writing
the ~40 error strings) → `ui.locale` setting + `OnLocaleChanged` → user-content translations +
cache → the frontend switch to the shared catalog → the mutation drills, tests alongside each,
then the self-review and improvement notes (Protocol Steps 5–6).

---

## 10. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 10.1 The coverage gate paid for itself before it was finished

§6.3 justified D3 as preventing raw codes reaching users. It caught its first four the moment
it ran — `i18n.catalog_invalid`, `i18n.invalid_locale`, `i18n.translation_load_failed`, and
`i18n.translation_write_failed`, all codes **introduced by this very step**.

That is the failure mode in miniature: a developer adds an error, ships it, and the string
only becomes visible when a customer hits that path. The gate turns it into a failing test on
the machine that wrote the code. Final catalogue: **65 keys per locale, 57 of them error
codes**, all present in both languages.

### 10.2 A design correction: `locales/` needs its own Go package

§4.1 said the catalogs would be embedded "with `go:embed all:locales`" from
`platform/i18n`. That is impossible for the same reason `migrations/` has its own package:
**`go:embed` cannot reach above its own directory.**

So `locales/locales.go` exists purely to embed the tree it sits in, exactly like
`migrations/migrations.go`. Second occurrence of this constraint — worth noting so the next
root-level asset directory is designed with it in mind rather than discovering it again.

### 10.3 User content deliberately does NOT fall back across languages

The design said user-content resolution walks the locale chain. Implementing it exposed that
this is wrong, and the code now walks a regional tag down to its language (`ar-SY` → `ar`) and
**stops**.

The asymmetry with UI strings is the point. English is guaranteed to define every UI key, so
falling back to it always yields a real string. User content has no such guarantee: the base
`name` column holds whatever the shop owner typed. Showing an Arabic shop an English product
name — because somebody once entered an English translation — is worse than showing them
their own original text. `TestTranslationsNeverCrossLanguages` pins this.

### 10.4 Two frontend checks that a reviewer could not do by eye

- **No English left in the Arabic catalog.** A copy-paste that leaves English text in
  `ar/errors.json` is invisible to a reviewer who does not read Arabic. `messages.test.ts`
  asserts no non-trivial value is byte-identical across the two catalogs.
- **The duplicate cannot come back.** A Go test fails if `frontend/src/i18n/locales.ts`
  reappears, so §22.1's single source is enforced rather than remembered.

`frontend/src/i18n/locales.ts` was deleted and `messages.ts` now imports
`../../../locales/{en,ar}/*.json`. Typed keys came free — `keyof typeof enCommon` gives the
union, so a mistyped key is a TypeScript error with no code generation. `vite.config.ts` gained
`server.fs.allow: [".."]` because the shared catalogs sit above the Vite root; production
builds inline the JSON and need no allowance.

### 10.5 Verification

**30 Go tests + 9 frontend tests, `-race` clean, `make ci` green.**
`kernel/locale` 93.9%, `platform/i18n` 87.4%.

**Mutation-verified**, per §8:

- *Fallback chain*: stopping the chain before `Default` → `TestFallbackChain` fails with
  `chain for "ar" = [ar], want [ar en]`, and `TestFallbackChainAlwaysEndsAtDefault` fails
  alongside it.
- *Coverage gate*: deleting `migrate.checksum_mismatch` from `en/errors.json` → the gate fails
  naming the missing code, and the locale-parity test fails independently.

### 10.6 Carried forward

- **Step 0.11** builds the React `useTranslation` hook, provider, and language switcher over
  `messages.ts`. `App.tsx` currently calls `translate()` directly, which is adequate for a
  shell but not for a real UI.
- **Step 0.10** stamps `locale.WithLocale` onto the request context from the resolved
  `ui.locale` setting. Until then `Active()` reads the setting directly.
- **Pluralisation** (D4) arrives with the first quantity-dependent string, as ICU rules or
  per-locale plural functions.
- **§22.5 printing concerns** — Arabic-Indic digit selection, Hijri calendars, RTL document
  layout — belong to the printing layer in Phase 9. `locale.Direction()` is the piece of it
  that exists now.

---

*End of Step 0.8. Design approved, implemented, self-reviewed.*
