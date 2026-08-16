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
