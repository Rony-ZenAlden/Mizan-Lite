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

This environment cannot run a Wails build, so **nothing here claims the installers work.** What is
asserted is that the configuration is complete and internally CONSISTENT: every file a packaged
build needs is present, `wails.json` agrees with `VERSION`, and both manifests still READ their
version from the config rather than hardcoding it.

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
