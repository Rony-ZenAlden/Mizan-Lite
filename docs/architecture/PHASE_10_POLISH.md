# Phase 10 — Polish

> Performance, accessibility, RTL, installers, documentation.
>
> The phase where a system that works becomes one somebody can use, install, and be handed.

---

## 1. ANALYSIS

### "Polish" is the wrong word for most of what is left

The §32 heading suggests rounding corners. Three of the items under it are not cosmetic:

- A screen that takes four seconds to open is a screen a till operator works around.
- An application that cannot be installed is not an application.
- Software nobody can hand over is software with one maintainer forever.

Only the RTL and accessibility work is polish in the ordinary sense, and even that is not
decoration here: this is built for Arabic-first markets, so a layout that breaks in RTL is a
layout that is broken for the primary user.

### The gap that is not polish at all

Phase 6 built purchasing's full document chain — orders, receipts, bills, landed costs, returns,
payments — and Phase 6.9 shipped three screens: orders, order detail, bills. **Receipts, returns,
landed costs and supplier payments have no user interface.**

That was recorded as backlog and never closed. It is application work, not polish, and it is the
difference between "purchasing is built" and "purchasing can be used". Leaving it in a phase named
Polish is how it stays unbuilt, so it gets its own step and is named honestly.

### What "performance" can honestly mean here

There is no production install to profile and no user complaining about a slow screen. Optimising
against a guess is how a codebase acquires an index nobody needs and a cache that goes stale.

What CAN be done is bound the risk:

1. **Measure the queries that grow.** A report over three years of a busy shop is the one that
   degrades, and the only honest way to know is to generate that much data and time it.
2. **Fix what a measurement finds**, and nothing else. 8.1 already recorded the order: an index
   first, then a projection with a verifier, driven by a measurement rather than an expectation.
3. **Pin the result**, so a later change that makes a report ten times slower fails rather than
   ships.

A benchmark that runs in CI and asserts a BOUND is the shape. Not a stopwatch printed into a log
nobody reads.

### Accessibility and RTL are one problem seen twice

Both are about a layout that assumes one reader. The checks are mechanical and worth automating,
because a manual audit is a snapshot and the next screen breaks it:

- Every interactive element has an accessible name.
- Nothing conveys meaning by colour alone.
- No physical-direction CSS (`margin-left`, `text-align: right`) where a logical property exists.
- Every screen renders in `dir="rtl"` without overflowing.

The third is the one that will find real bugs, and it is a grep. The first is a test.

### Installers, and what I can honestly claim

Wails builds an NSIS `.exe` on Windows and a `.dmg` on macOS. The configuration is written here;
the BUILD cannot run in this environment, so nothing in this phase may claim "the installer
works". It can claim the configuration is complete, the scripts are correct, and the parts that
can be checked without a build — file presence, manifest validity, version consistency — are.

Saying so plainly in the DoD is the honest version. A criterion that reads "installers build" and
is ticked without a build is worse than one that reads "installer configuration is complete and
verified as far as this environment allows".

Code signing stays a no-op, as decided in Phase 0.

### Documentation is for two readers

- **The shopkeeper**, who needs to install it, set it up, and find the one screen they came for.
- **The next maintainer**, who needs the architecture, the phase records, and the rules this
  codebase has accumulated — most of which are already written, in `docs/architecture/` and in
  the code itself.

The second reader is largely served. What is missing is the ENTRY POINT: a reader arriving at this
repository has thirty-odd design documents and no order to read them in.

---

## 2. DESIGN

### D1 — the purchasing screens are a step, not a footnote

Four screens: receipts, returns, landed costs, supplier payments. They complete a chain that is
otherwise unreachable from the interface, and they are built the way Phases 5–9 built theirs.

### D2 — a benchmark asserts a BOUND, and the bound is generous

A CI benchmark that fails on a 10% regression fails on a busy build machine. One that fails when a
report takes ten times longer catches the change that matters — an accidental N+1, a dropped
index, a projection replaced by a live scan.

The dataset is generated, large enough to be honest (a year of a busy shop), and the assertion is
in seconds rather than milliseconds.

### D3 — the RTL and accessibility checks are STRUCTURAL, not visual

No screenshot comparison, no headless browser audit. Those need a browser this environment cannot
drive, and a snapshot test of a layout fails on every legitimate change.

Instead:
- a source scan for physical-direction CSS, which is a rule with no exceptions in this codebase;
- a test that renders every screen in RTL and asserts it mounts;
- a test that every interactive element in the shared UI has an accessible name.

Each is blunt, cheap, and survives the next screen.

### D4 — the installer configuration is complete; the DoD says what was verified

`wails.json`, the NSIS template, the `Info.plist`, the icons, and the version stamped in one
place. A test asserts the files exist and agree with each other. The DoD criterion says
"configuration complete and internally consistent", not "installer builds", because this
environment cannot build it.

### D5 — documentation is a README that ORDERS what exists

Not a rewrite. A reader arriving needs: what this is, how to run it, how to build it, and the
order to read the architecture documents in. The phase records are the architecture documentation
and they are already written.

A user guide is a separate artefact from a maintainer guide, and conflating them serves neither.

---

## 3. WHAT THIS PHASE DOES NOT DO

- **No code signing.** Phase 0 decided it; the hooks stay no-ops.
- **No Linux packaging.** Dropped in Phase 0.
- **No auto-update.** It needs a server, a signing key, and a rollback story — three mechanisms
  for a feature nobody has asked for, in an application built for shops with unreliable internet.
- **No visual regression testing.** See D3.
- **No speculative optimisation.** Only what a measurement finds.
- **No translation beyond `en` and `ar`.** A third locale is a data change, and the machinery
  already supports it.

---

## 4. STEP SEQUENCE

| Step | What |
|------|------|
| 10.1 | The purchasing screens Phase 6 did not ship |
| 10.2 | Performance — measure, fix what is found, pin the bound |
| 10.3 | RTL and accessibility — structural checks |
| 10.4 | Installer configuration, Windows and macOS |
| 10.5 | Documentation and the reading order |
| 10.6 | Phase 10 Definition-of-Done review, and the project's |

---

## 5. DEFINITION OF DONE

1. Every purchasing document a user can create has a screen to create it on.
2. A benchmark generates a year of a busy shop's data and asserts every report completes within a
   stated bound; the bound is in CI.
3. Any performance change is justified by a measurement recorded in this document, and nothing is
   optimised that was not measured.
4. No source file uses a physical-direction CSS property where a logical one exists.
5. Every screen mounts in `dir="rtl"`.
6. Every interactive element in the shared UI has an accessible name, asserted by test.
7. The installer configuration is complete and internally consistent for Windows and macOS, and a
   test asserts the files exist and their versions agree. **Not** "the installer builds" — this
   environment cannot build it, and the criterion says so.
8. Code signing remains a no-op, and nothing claims otherwise.
9. A reader arriving at the repository has an ordered path through the architecture documents.
10. A shopkeeper has an installation and first-run guide, in English and Arabic.
11. Every earlier phase's DoD result is restated with what has changed since, and anything
    recorded as NOT MET is either fixed or restated as a known gap.
12. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation.

---

## Step 10.1 — the purchasing screens Phase 6 did not ship

Four screens, and the two bindings that were missing under them.

### The finding: two documents had no binding at all

Receipts and payments had bindings and no screens. **Returns and landed costs had neither** — so
a document a user could create in the domain, with its own permissions, its own posting rules and
its own tests, was unreachable from the interface for four phases.

That is 7.6's number series in a smaller shape: a mechanism built, tested, and never connected to
a caller. The difference is that the series failed loudly the first time anybody tried; this
failed silently, because nobody could try.

### D1 — the GRNI total is on the screen, not in the reader's head

A delivery nobody has invoiced sits in goods-received-not-invoiced, and that balance is what an
accountant reconciles at every month end. The screen totals it above the list. Making somebody add
a column to find the figure they came for is how a screen fails a person already under time
pressure.

### D2 — a return shows the CREDIT and the COST side by side

They are different numbers and the difference is real: the supplier credits what they charged, the
stock leaves at what the original delivery cost us (§D.3). A screen showing only the credit leaves
somebody unable to explain why the inventory account moved by a different figure.

### D3 — the payment method is a column, because 6.7's defect was invisible

One posting rule credited cash whatever the method, so a bank transfer would have reduced the
till — and **the till would have been short at every close with no transaction to explain it.**
Showing the method on every row is what lets somebody reconciling see immediately that the money
left the bank.

### D4 — recording a charge and applying it stay two acts

Recording that freight was charged is bookkeeping. Deciding it belongs in the cost of these
particular goods — and therefore in the margin every future sale is measured against — is a
judgement somebody makes. 6.6 made them two service calls; the screen keeps them two actions.

The BASIS is shown rather than hidden behind the total, because two charges of the same amount on
one delivery can cost two products very differently depending on how they are spread.

### The drills

D241–D244, four, all failed first time.

---

## Step 10.2 — performance

Measured first. **Nothing needed optimising**, and that is the step's result rather than a step
skipped.

### The measurement

A generated year of a busy shop: 20 products, 50 customers, 40 sales a day for 365 days —
**14,600 posted documents and their lines**.

| Report | Uninstrumented |
|--------|----------------|
| Profit and loss | ~0ms |
| Balance sheet | ~0ms |
| Sales by day | 47ms |
| Sales by product | 45ms |
| Sales by partner | 46ms |
| Stock valuation | ~0ms |
| Search | 6ms |
| Dashboard | 46ms |

Phase 8's D1 said the lines come back raw and the arithmetic happens in Go, and that *if it is
ever too slow the answer is an index, then a projection with a verifier, in that order, driven by
a measurement.* The measurement says it is not too slow. **DoD criterion 3 is satisfied by nothing
having been optimised**, which is the honest outcome of measuring first.

### The fixture writes rows directly, and says so

Every other fixture goes through the services, because a row inserted around them skips the
invariants they keep — 9.4's entire design. This one does not. It exists to make the DATABASE
big, and posting fifteen thousand documents through the full pipeline would take minutes and
measure the pipeline rather than the reports.

The rows are shaped exactly as the services shape them. That is the trade, stated rather than
hidden.

### D1 — a stopwatch cannot stop the thing it is timing

The first version timed each report and compared afterwards. The drill — making the sales analysis
re-query per row — did not fail it. It **HUNG**, past a ten-minute timeout.

That is the worst possible way to report the exact case this test exists for: the run is killed,
the output truncated, and nobody learns which report was slow. Each report now runs under a
context DEADLINE, which turns a pathological regression into a named failure in bounded time — the
re-drill failed in five seconds with the report named and the cause quoted.

### D2 — the ceiling has to clear the INSTRUMENTED figure

A ceiling of one second — twenty times the clean 46ms — failed under `make ci`, which runs
`-race -covermode=atomic`. The instrumentation costs more than twenty-fold.

This is the design's own warning arriving in practice: *a test that fails on a small regression
fails on a busy build machine.* Fifteen seconds clears the instrumented figure by roughly ten
times, and the N+1 drill still breaches it in seconds.

**The known limit, recorded rather than papered over:** a report that drifts from 46ms to five
seconds is a hundred times worse and still passes. The elapsed times are logged for that reason —
a person reading a CI run can watch a report climb long before the ceiling notices.

### The drills

D245, one, and it took three attempts to become a drill at all: the first hung, the second was
rolled back before the test ran, and the third failed correctly. Counting the mutation's effect
before believing its result — the habit this project acquired in Phase 8 — is what caught the
second.

---

## Step 10.3 — RTL and accessibility

Three gates. All three passed on the day they were written, which is the point: they exist so the
NEXT screen cannot be the first to break.

### D1 — the direction gate is a source scan, and the codebase already passed it

`margin-left` is always the left, in every language. `margin-inline-start` is the side the reader
begins on. The second is correct in both directions and costs nothing, so the rule has no
exceptions — which is what makes a blunt scan the right shape.

The scan found **nine logical properties and zero physical ones**, because every earlier phase used
them without being told to. A gate that finds nothing on the day it is written is not a wasted
gate; it is a rule that was being followed by convention and is now enforced.

It asserts it walked more than fifty files, for the reason this project has now hit three times:
a walk that matches nothing passes while checking nothing.

### D2 — the RTL gate proves a screen MOUNTS, not that it looks right

Eleven screens, rendered under an Arabic catalogue with `dir="rtl"`. It does not prove a layout is
correct — that needs a browser this environment cannot drive, and a screenshot comparison fails on
every legitimate change until nobody reads it.

What it proves is the class of failure that takes a screen from "mirrored oddly" to **blank**: a
missing translation throwing, a component assuming a direction at render, a key that only exists in
English. The direction gate next door covers the layout half at the source.

The first version asserted the direction SYNCHRONOUSLY and read `ltr` every time — the direction
comes from the preferences the provider loads, not from the render helper's initial attribute.
Awaiting it turned a test of `renderApp` into a test of the application applying a locale.

### D3 — accessible names, because the failure is invisible to sighted review

An element with no accessible name is announced as "button", and the user cannot find out which.
The label is right there on screen, drawn by an icon or a sibling the accessibility tree never
sees.

`getByLabelText` resolves through the accessibility tree, so the input test passes only if the
label is BOUND — a label sitting next to an input looks identical and is nameless. A table with no
caption is a grid of numbers with no announced purpose, and forty rows in, a screen-reader user has
no way to know what they are reading.

### The drills

D246–D250, five. One did not apply on the first attempt — a regex that removed more than the
caption and broke the file, which reports as "no tests" rather than as a failure. **Fourth
non-mutation this project has caught**, and the reason the effect is counted before the result is
believed.

---

## Step 10.4 — installer configuration

Less new work than expected, and one real defect.

### What already existed

Phase 1.13 built the packaging: `scripts/package-windows.sh` cross-builds the NSIS `.exe` from
macOS (possible only because 0.3 chose a pure-Go SQLite driver), `scripts/package-macos.sh` builds
the `.dmg`, `scripts/version.sh` derives the version from `VERSION`, and the Wails manifests
template from `wails.json`.

### The defect: a field added in Phase 9 that nothing set

9.1 D2 put `AppVersion` in the backup manifest "for a support conversation", and
`bootstrap.Options` gained the field. **Nothing ever assigned it.** Every manifest carried an empty
string where a build number belongs — precisely the value it was added to avoid.

This is the shape 7.6 found at its worst and 10.1 found again: a mechanism built, wired
half-way, and never connected to its caller. It is now set from `buildinfo.Version` in `shell.go`,
which the Makefile and both packaging scripts already stamp — one source, four consumers.

### D1 — the test says what it verified, and what it could not

The configuration tests assert completeness and internal CONSISTENCY: every file a packaged build
needs is present, `wails.json` agrees with `VERSION`, and both manifests still READ their version
from the config rather than hardcoding it.

> **Corrected in 10.7.** This step originally claimed the environment could not run a Wails build
> and recorded the criterion as "configuration complete" for that reason. **That claim was never
> checked.** `wails`, Go 1.26, Node 22 and `makensis` were all present, and both installers built
> on the first attempt. See Step 10.7.

That last one matters more than it looks. If somebody replaces a template placeholder with a
literal, the file still builds and Explorer shows a number nobody updated.

**The gap is named rather than hidden: an installer that builds and then fails to run is invisible
to everything above.**

### D2 — the signing check was wrong before the script was

The first version asserted the scripts must not mention `codesign`, and failed —
`package-macos.sh` invokes it, guarded by `MIZAN_MACOS_IDENTITY`.

That guard is what makes it a no-op: unset, the build produces an unsigned `.dmg` and says so;
set, somebody with a certificate opted in. Phase 0's decision was that signing must not be
REQUIRED, not that the word must not appear. **The script was right and the test was wrong**, and
the corrected test asserts every signing invocation sits inside a guard.

### The drills

D251–D255, five, all failed first time.

---

## Step 10.5 — documentation

A README that orders what already exists, and a guide for the person who is not a programmer.

### The finding: the entry point was ten phases stale

The README said **"Phase 0 — Foundation (application kernel). No business features yet."**

A reader arriving would have concluded the project was a kernel with nothing on top of it — while
the repository held ten phases of modules, thirty-seven design documents, and a working till.
**A stale entry point is worse than none**, because none prompts you to look and a wrong one
answers your question.

### D1 — order, not rewrite

The phase documents ARE the architecture documentation and they are already written. What was
missing was the order to read them in, and the instruction to **read a phase's DoD review first if
short of time** — it is the most honest section, listing what was proven, by which test, and what
was found wrong while proving it.

The README also now carries the rules this codebase accumulated, each with where it was learned.
They are the things worth knowing before changing anything, and they were scattered across
thirty-seven documents.

### D2 — known gaps are in the README, not implied

Four, stated where a reader will see them: the installers are configured but never built here;
signing is a no-op; the performance ceiling catches structural regressions and not drift;
accounting is read-only by design. The alternative is a reader discovering them.

### D3 — the user guide is a separate artefact

A shopkeeper needs installing, first-run setup, and what to do when something looks wrong. A
maintainer needs the architecture. Conflating them serves neither, so the guide is its own
document in both English and Arabic — and the README says so where the two part.

The guide's most important section is the one Mizan cannot do for the user: **copy backups off
this computer.** A backup on the same disk does not survive that disk failing, and it is the only
step automation cannot take.

### The drills

D256–D259, four. One did not apply: it renamed a document that the README mentions in a code span
rather than a link, so the link walk had nothing to find. Re-aimed at a real link, it fails.

---

## Step 10.6 — Phase 10 Definition-of-Done review, and the project's

### Phase 10: 12/12 met

| # | Proven by |
|---|-----------|
| 1 | 10.1's four screens, and the two bindings that were missing under them |
| 2 | `TestEveryReportCompletesWithinItsBoundOverAYearOfHistory`, in CI |
| 3 | Nothing was optimised, because the measurement found nothing to optimise |
| 4 | `direction.test.ts` — nine logical properties, zero physical |
| 5 | `rtl.test.tsx` — eleven screens under an Arabic catalogue |
| 6 | `accessible-names.test.tsx` |
| 7 | `packaging_test.go` — presence, one version source, templates still templated |
| 8 | `TestSigningIsOptInAndTheBuildDoesNotNeedIt` |
| 9 | `TestTheReadingOrderNamesDocumentsThatExist`, `TestEveryPhaseHasADesignDocument` |
| 10 | `docs/guide/GETTING_STARTED.md` and its Arabic twin, linked and asserted |
| 11 | Below |
| 12 | `make ci` green; drills D241–D259 |

### Criterion 11 — every phase's result, restated

| Phase | Recorded verdict | Now |
|-------|------------------|-----|
| 0 | Reviewed in `STEP_0_12` | Unchanged |
| 1 | Reviewed in 1.12 | Unchanged |
| 2 | 12 of 12 | Unchanged |
| 3 | 11 of 12 in full, 1 with a documented exception | **Still an exception.** See below |
| 4 | 13 of 13, after the review FOUND criterion 3 unmet and fixed it | Unchanged |
| 5 | 14 of 14 | Unchanged |
| 6 | 13 of 14 — **criterion 13 NOT met** | **Permanently false.** See below |
| 7 | 12 of 12, one by a mechanism the criterion did not describe | Unchanged |
| 8 | 12 of 12 | One check has since EXPIRED — see below |
| 9 | 12 of 12 | Unchanged |
| 10 | 12 of 12 | This review |

#### Phase 3, criterion 2 — still an exception, and still the right one

`price_list_items.variant_id` is nullable, where §1.2 asks for `NOT NULL` everywhere.

The criterion exists to prevent a nullable `variant_id` meaning "the product itself", forcing every
query to handle two cases forever. What is actually true is narrower: a CHECK constraint makes any
row that is not exactly one of the two kinds unrepresentable, and exactly one function —
`domain.Resolve`, whose job IS choosing between granularities — reads both cases.

Four phases have passed and no second reader of that column has appeared. The exception stands.

#### Phase 6, criterion 13 — false, and it cannot become true

*"Every seam Phase 4 left has a caller and none needed reshaping."*

Three of four served their first caller unchanged. `Revaluation` did not: it was structurally
unwritable, guarded by a positive-quantity check in both the domain and the schema, and 6.6 had to
rebuild the table to let its own feature exist.

**This is a statement about what happened, and no later work can make it true.** It was recorded
rather than reworded, and restating it here is the honest close: the seam was guessed, the guess
was wrong, and the cost was a migration.

#### Phase 8 — one check has expired

`TestPhaseEightAddedNoSchema` asserted no migration numbered above Phase 7's last. It passed when
Phase 8 closed, which is what it was for. Phase 9 then added `0037` legitimately, and no version
of the test survives — the window between the two is empty, and **a check nobody can make fail is
a claim nobody has verified**. It was deleted with a note in its place.

The criterion it proved remains true of Phase 8. Nothing enforces it going forward, and that is
recorded rather than implied.

---

## The project, closed

Eleven phases. `make ci` green.

### What was built

An offline-first desktop ERP and point of sale: double-entry books with table-driven posting
rules, a till with shifts and printing, stock with moving-average costing and lots, purchasing
from order to payment, expenses and debts, financial statements and margin analysis, verified
backup and restore, CSV import through the real services, and notifications computed from rules
rather than stored.

Fifteen modules, thirty-seven migrations, seventeen binding façades, two locales.

### What the discipline cost and returned

**255 mutation drills.** Roughly one in seven passed, and every pass produced a change: a
strengthened test, a deleted redundancy, a re-aimed mutation, or a rule narrowed to what it
actually meant.

Six defects would have shipped without them, and each was invisible to review:

- A costing conversion **wrong by a factor of a hundred** for two phases, hidden because every
  consuming test used a zero-decimal currency.
- **Thirteen number series nothing ever created** — a freshly installed company could not post a
  single document.
- A `Revaluation` movement that was **structurally unwritable**.
- A whole module wired into the application and **absent from its declaration list**, making its
  permissions ungrantable.
- Seventeen message keys using a placeholder syntax the catalogue **renders literally**.
- Two documents with **no binding at all**, unreachable from the interface for four phases.

### What is not done

Stated plainly, because a project that ends by listing its strengths has not ended honestly:

1. **The installers have never been built.** The configuration is complete and consistent; no
   `.exe` or `.dmg` has been produced or run.
2. **Nobody has used this.** Every guarantee is proven by a test. No shopkeeper has opened it,
   and the first hour of real use will find things no test looked for.
3. **The performance ceiling catches structural regressions, not drift.**
4. **`runEveryReport` lists the phase's reads by hand** (9.7), so a report added later and left
   out is one nobody proves is read-only.
5. **Accounting is read-only**, by §20.6's release plan. Manual journal entries are a later tier.
6. **Two locales.** A third is a data change, and the machinery supports it.

### The rule that held throughout

*Correctness over convenience, and say plainly what is not true.* Every phase document records
what it declined to build and why; every DoD review names a criterion it failed rather than
rewording it; and the two that stayed failed — Phase 3's exception and Phase 6's criterion 13 —
are restated here rather than quietly dropped at the end.

---

## Step 10.7 — the installers, actually built

### The correction that made this step necessary

Step 10.4 recorded criterion 7 as *"configuration complete and internally consistent"* rather than
*"the installers build"*, on the stated grounds that **this environment cannot run a Wails build**.

That was asserted and never checked. `wails`, Go 1.26, Node 22 and `makensis` were all present the
whole time, and both installers built on the first attempt.

The DoD wording was defensible; **the reason given for it was false**, and a limitation claimed
without testing is worse than one discovered, because it stops anybody looking. The criterion is
now met in its stronger form.

### What was built and what was verified

| Artefact | Verified |
|----------|----------|
| `Mizan ERP 0.1.0-dev.d17a9c1.dmg` (14M) | Mounts; carries the `.app` and an `/Applications` symlink; `lipo` reports **`x86_64 arm64`** |
| `Mizan ERP 0.1.0-dev.d17a9c1 Setup.exe` (9.4M) | `PE32 … Nullsoft Installer self-extracting archive`; names the product and version in its UTF-16 strings |
| `Mizan ERP 0.1.0-dev.d17a9c1.exe` (20M) | `PE32+ … x86-64` |

`AppVersion` is confirmed populated end to end: the ldflags value `0.1.0-dev.d17a9c1` appears in
the shipped macOS binary, and the commit `d17a9c1` is stamped eight times. 10.4 wired it into
`bootstrap.Options`; this is the artefact carrying it.

### The defect the build found that no test had

The shipped `.dmg` declared **`CFBundleIdentifier = com.wails.mizan-erp`** — the Wails scaffold's
namespace, still in place because nothing had ever read the built bundle.

It is not cosmetic. macOS keys preferences, keychain entries, TCC permissions and Gatekeeper
records against the bundle identifier, so `com.wails.*` puts this application's state in a
framework's namespace, where a second Wails application with the same product name would collide
with it. **And it can never change after a release** — everything keyed to it would be orphaned.

Now `com.mizanerp.desktop`, in both plists, asserted by test, and confirmed on the rebuilt image.

This is the class of defect 10.4's own DoD wording predicted: *"an installer that builds and then
fails to run is invisible to everything above."* One level down from that — an installer that
builds, runs, and quietly claims the wrong identity — was invisible too, until somebody mounted
the image.

### D1 — the identifier check reads the VALUE, not the file

The first version matched the whole plist and failed on the comment explaining why `com.wails.` is
wrong. **The same mistake 9.4's no-SQL check made against its own doc comment**, in a second
place, three weeks later — which suggests the lesson is not "watch for that" but *a scan that can
match its own explanation should read structure rather than text.*

### The second defect: the fix was in a file nobody tracked

`git status` did not list `build/darwin/Info.plist` after the identifier was corrected. Phase 0 had
ignored `/build/darwin/` as stock Wails scaffolding, with a note in the ignore file itself:

> *"Phase 10 is where the real icon and a customised Info.plist become tracked assets — REMOVE
> THESE TWO LINES THEN, or the customisation will be silently ignored."*

The note was exactly right about the consequence. The fix lived in a file `wails build`
regenerates: correct on this machine, absent from the repository, **gone on the next clean
checkout — while every test kept passing, because they read the working copy.**

Both paths are tracked now, and `TestTheCustomisedBuildAssetsAreTracked` refuses to let them be
ignored again. A note left for a future phase is only as good as somebody reading it; this one was
found by a `git status` that did not show a file which had definitely been edited.

### Signing, unchanged

Both artefacts are UNSIGNED, by Phase 0's decision. The hooks are present and opt-in:
`MIZAN_MACOS_IDENTITY` activates `codesign` and prints the notarization commands; Windows has no
`signtool` invocation at all. `TestSigningIsOptInAndTheBuildDoesNotNeedIt` asserts the guard, and
a build on a clean machine with no certificate still produces a working artefact — which is what
makes the packaging testable.

### Criterion 7, restated

**Met in full.** Both installers build, and each was inspected rather than trusted: the image
mounted, the binary's architectures read, the version resource confirmed, the bundle identity
corrected.

What remains untested is what no build can prove from here: **neither installer has been run on a
clean Windows or macOS machine.** Structure and metadata are verified; first-run behaviour on
another person's computer is not.

### The drills for this step

D260–D261, two, both failed first time — one for the ignore rule, one for the identifier itself.

### Criterion 12, restated for the phase

**261 drills across eleven phases.** `make ci` green, and both installers built from a clean
`build/bin` and `dist`.

---

## Step 10.8 — first run, and the limits of signing

Three items were asked for. **One was completed, one was completed as far as it can be here, and
one cannot be done from a build machine at all** — and saying which is which is the whole content
of this step.

### macOS first run — done, on real hardware

The `.dmg` was mounted, the `.app` copied out, and launched **against a clean data directory**.
The application had never run on this machine; only been built.

```
INFO mizan window ready version=0.1.0-dev.d17a9c1
INFO pre-migration backup created path=…/backups/before_migration-….db
INFO migration applied version=1 name=platform
…37 migrations…
INFO database updated applied=37 to_version=37
INFO permissions synced declared=65
INFO number series synced declared=13 created=13
INFO mizan started modules=14
INFO mizan ready
```

**Zero errors.** The resulting database holds 96 tables, 13 number series, 65 permissions.

Two earlier fixes are confirmed in a shipped artefact rather than in a test:

- **13 number series created on first run** — 7.6's defect, where a freshly installed company
  could not post a single document.
- **`"appVersion": "0.1.0-dev.d17a9c1"` in the scheduled backup's manifest** — 10.4's defect,
  where a field added in Phase 9 was never assigned.

The scheduled backup fired on first run and wrote both a snapshot and its manifest, which is
`CatchUp: RunOnce` behaving as 9.1 D5 designed.

### The signing hook — exercised, and the result is the interesting part

A self-signed certificate was created, found not to be trusted for code signing, and **the
temporary keychain was deleted rather than adding trust settings to the machine.** Modifying a
developer's system trust store to make a test pass is not a trade worth making.

Instead the hook was run with macOS's native **ad-hoc identity**:

```
MIZAN_MACOS_IDENTITY=- ./scripts/package-macos.sh
```

Every step worked: `codesign --force --deep --options runtime --timestamp` succeeded,
`--verify --strict` reported *valid on disk, satisfies its Designated Requirement*, the image
built, the image was signed, and the notarization commands printed. The signature carries
`Identifier=com.mizanerp.desktop`.

**And `spctl --assess --type execute` returns `rejected`** — `Signature=adhoc`,
`TeamIdentifier=not set`.

That is the finding: **signing is necessary and not sufficient.** A Developer ID signature *and*
notarization are both required, and a release that signs but skips notarization ships an artefact
that still warns. `docs/RELEASE.md §4` says so with the commands.

The hook is therefore proven; only the certificate is missing, and a certificate is tied to a
legal identity and a payment that cannot be supplied by a build machine.

### Windows first run — NOT done, and not doable here

No Windows machine, no VM tooling, no Wine. The `.exe` is a PE binary and this is macOS.

What is verified is structural: `PE32 … Nullsoft Installer self-extracting archive`, `PE32+
x86-64`, product and version present in the installer's UTF-16 strings. **The installer has never
been executed.**

`docs/RELEASE.md §3` is the checklist for whoever has a Windows machine, and its most important
line is the one nothing here can test: **the WebView2 runtime**, which is the single most likely
first-run failure on a fresh Windows and which no static check can reach.

### D1 — the release document states what was verified and how

Not "installers work". The document separates *verified on real hardware*, *verified
structurally*, and *not verified*, and puts the manual checks in a checklist with the reason each
one exists — including that a `.dmg` copied locally never carries `com.apple.quarantine`, so the
build machine's own copy **never faces Gatekeeper at all**.

### Release artefacts

Rebuilt clean at the committed version, with checksums:

```
363a736b…  Mizan ERP 0.1.0-dev.0d9100b Setup.exe
c352ccb8…  Mizan ERP 0.1.0-dev.0d9100b.dmg
f8a98975…  Mizan ERP 0.1.0-dev.0d9100b.exe
```

They are marked `-dev` because no tag exists. That is `scripts/version.sh` working as Step 1.13
designed: **a binary must never be mistaken for a release it is not**, and tagging is the act that
makes one.

---

## Step 10.9 — the v1.0.0 release

Three things were asked for. The first uncovered a blocker neither of us had named.

### The blocker: eighteen routes for thirty-nine screens

Before any of this could be called shippable, navigation had **18 routes** against **39 built
screens**. Everything from Phases 8, 9 and 10.1 was unreachable from the menu:

- the dashboard, financial statements, sales and spend analysis, stock valuation
- the notice centre, backups and restore, import
- deliveries, supplier returns, supplier payments

Worse than absent: **the landing route rendered `SystemPanel`** — job status and outbox depth —
because it was written in Phase 1 before any business figures existed. A shopkeeper opening Mizan
would have been shown the queue depth of an internal message bus.

This is the same defect class 10.1 found and the same one 7.6 found at its worst: **built, tested,
and never connected to a caller.** Three phases in a row, in three different layers — bindings,
screens, and now routing. The pattern is not carelessness in any one place; it is that *the
connection is a separate act from the construction*, and nothing was checking the connection.

Thirty routes now, and the landing screen is the dashboard.

### The in-app guide

`/help`, reachable by anybody signed in — **no permission**, because the guide is where a user who
can do nothing else learns what they are looking at.

Four chapters, thirteen sections: getting started and a day in the shop; how the modules work and
why they cannot see each other; where data goes, one sale traced end to end through six modules;
and how the build pipeline turns source into an installer.

#### D1 — the prose lives beside the code, not in the translation catalogue

Every other string is a key in `locales/*.json`, and should be: a label belongs with the thousand
other labels.

A guide is not labels. It is prose — paragraphs referencing each other, ordered steps whose
numbering is the content, passages where English and Arabic must say the same thing and will not
say it the same way. Split across a flat key file you get `help.architecture.para3` and no way to
see whether the section still reads.

Both languages sit in one structure, side by side, where a change to one is visibly a change the
other needs. `it("says everything in both languages")` walks every block and fails on an empty
half.

#### D2 — the guide has its own language toggle, and its own direction

The reader is often not the person the application is set up for: an Arabic-speaking shopkeeper
hands the laptop to a bilingual relative; an English-speaking accountant is shown a screen in
Arabic. Making them change the application's language to read a paragraph — and change it back —
is worse than one button.

`dir` is set on the guide's **subtree**, not the document. Reading in Arabic while the application
runs in English must not flip the menu and the toolbar around the reader.

#### The bug the tests found

Seeding the language from `locale` in a `useState` initialiser looked right and was wrong: the
initialiser runs at mount, before the provider has loaded preferences, so **an application set to
Arabic opened the guide in English every time.**

The fix separates "the reader chose" from "the reader has not touched it": until they choose, the
guide follows the application; once they do, it stops. A deliberate choice overwritten by a
background load is the more annoying of the two bugs.

### WebView2, and a caveat that matters for this product

The installer checks **both** registry scopes — machine-wide and per-user — and falls back to the
bundled installer. `TestTheWindowsInstallerChecksForWebView2` asserts all of it, because a Wails
upgrade regenerating `wails_tools.nsh` could drop it silently, and the symptom is an application
that starts and shows nothing.

The bundled file is the **bootstrapper** (1.7MB), which downloads the runtime. So **installing on
a Windows machine that lacks WebView2 needs an internet connection once** — which sits awkwardly
with an offline-first product for shops with unreliable connections.

The alternative is Microsoft's Evergreen Standalone installer at ~130MB. That is a trade about
what the product promises, not a bug to fix quietly, so it is documented in `docs/RELEASE.md §3`
with the steps — and the test asserts the bundled file's SIZE, so swapping it cannot change the
offline story without the change being visible.

### Version 1.0.0

`VERSION` and `wails.json` both say `1.0.0`; the packaging test asserts they agree. A tag makes
`scripts/version.sh` stop marking builds `-dev`.

### The drills

D262–D265, four, all failed first time.

---

## Step 10.10 — the workflows that were not there, and the guide

### "Simplify registering products" — there was nothing to simplify

The catalogue screen was READ-ONLY, and so was the whole catalogue façade: `Categories`,
`Products`, `Product`, `Units`, `Scan`, `Price`. **No `CreateProduct` binding existed.**

`catalog.CreateProduct` has existed since Phase 3 and the CSV importer has used it since 9.4. So
registering a product was possible — by opening a text editor, writing a CSV, and importing it.

That is the **fourth** appearance of one pattern, in a fourth layer:

| Phase | Built | Missing |
|-------|-------|---------|
| 7.6 | thirteen number series | anything that created them |
| 10.1 | returns, landed costs | bindings and screens |
| 10.9 | eleven screens | routes |
| 10.10 | product creation, stock counting | bindings and forms |

The lesson is not carelessness in any one place. **Connecting is a separate act from building, and
nothing in this project was checking connections** — each was found by somebody going looking, and
three of the four by a request to do something else.

### D1 — the form asks two questions

The service takes eleven fields. The form asks for a code and a name; three more are behind *More
options*, already answered.

What is hidden has an answer right for most shops most of the time: goods rather than a service,
the company's own unit list, no category. **A product that cannot be saved until it is filed is a
product somebody keys into a notebook instead.**

The form stays open after saving and clears its fields, because somebody adding products is
usually adding several.

### D2 — the count asks what is THERE, not the difference

`Inventory.Adjust` takes a delta, which is what the ledger stores and the right shape for a
movement.

It is the wrong question to ask a person holding a shelf. They know there are eleven; making them
work out that eleven is two fewer than the thirteen on screen is arithmetic the computer should
do, and **the subtraction a tired person gets wrong at the end of a long day.**

`CountStock` converts. The movement is still a delta, split back into a magnitude and a direction
flag — because 4.2 put the direction in a flag deliberately, and a signed quantity puts the
direction inside the number.

A count that AGREES records nothing and returns success. A movement of zero is a ledger row saying
nothing happened, which 4.3 already refuses — and "the count agreed" is not an error.

### Two defects my own layer introduced, both caught by tests

- **The empty unit was passed straight through** and every creation failed with
  `catalog.invalid_unit`. The service was right to refuse; the form's job is to not ask, and
  *answering the question it skipped* is the binding's job. It now resolves the company's own
  default.
- **The movement was missing its product.** A shelf row knows a variant — that is what a shelf row
  is — and the domain refused a movement it could not place. `ProductOfVariant` was exposed for
  it: the repository had answered that since Phase 3 and nothing had asked from outside.

### The guide, expanded to a walkthrough

`/help` is now **How to Use Mizan ERP** / **دليل استخدام نظام ميزان** — five chapters, eighteen
sections, and a per-module walkthrough covering products, stock, selling, buying, people,
spending, reports, operations and settings.

Two tests hold it to that: one requires every module id to be present, and one requires each
walkthrough section to carry at least one ordered list of **three or more** steps with the **same
count in both languages** — because a missing step in one language is a reader following
instructions that skip something.

The module list is written out rather than derived from `ROUTES`. Deriving it would keep it in
step automatically and would also let a route be added with a section that says nothing, since the
id would match and the prose would be empty. **Adding a module should mean deciding what the guide
says about it.**

### The drills

D266–D271, six, all failed once each was a compilable mutation. Two needed re-aiming: deleting a
field left an unused variable rather than a behaviour change, which is a build error and not a
drill.
