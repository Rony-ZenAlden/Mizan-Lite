# Phase 9 — Operations

> Backup and restore, import and export, notifications.
>
> The phase where the application stops being only a thing that records and starts being a thing
> somebody has to look after.

---

## 1. ANALYSIS

### Backup already exists, and that is the phase's first decision

`internal/platform/migrate/safety.go` takes a snapshot before every migration, and it does the
part everybody skips: it opens the snapshot INDEPENDENTLY, integrity-checks it, and confirms the
migration history reads back. Its comment says why — *"an unverified backup is a rumour"*.

What it is not is reusable. `backup` is a method on `migrate.Runner`, private, named
`pre-migration-…`, and reachable only from the migration path.

So the question is not "how do we take a backup" but the one this codebase has asked in every
phase since 4.4: **before declaring a new place for a fact to live, look for the one an earlier
phase already left.** A second snapshot implementation would be a second thing that can be wrong
about whether a backup is trustworthy — and the one that was written first is the one with the
verification in it.

### Restore is the dangerous half, and the danger is not the copy

Copying a file over another file is easy. What makes restore dangerous is everything around it:

- **The application is holding the database.** SQLite will happily let a file be replaced
  underneath an open connection and then behave in ways nobody can reason about.
- **The incoming file may be rubbish** — truncated, from a different schema version, or not a
  database at all. Discovering that AFTER overwriting is unrecoverable.
- **The file being replaced is somebody's data.** A restore that goes wrong and left nothing to
  go back to is worse than no restore feature, because the user chose it believing it was safe.

The order those three imply is not negotiable: **verify the incoming file, back up the outgoing
one, then swap — and do the swap where nothing is holding the database.**

### Import is where an ERP gets corrupted

Export is a read and cannot break anything. Import writes, and the temptation is to write fast:
read a CSV, `INSERT` the rows, done. Every product imported that way skips the code that refuses
a duplicate SKU, resolves the unit, assigns the default variant, and writes the audit entry.

Two phases have already recorded the general form of this. 5.4: *when a test needs a helper that
imitates a production mechanism, that mechanism is untested.* An importer that imitates a service
is worse — the mechanism exists and is being bypassed.

So the rule is decided before any code: **an import calls the same service a screen calls, row by
row, and has no SQL of its own.** It will be slower. A shop importing four thousand products will
wait a few seconds longer, once.

### Notifications: the trap is storing them

The obvious design is a `notifications` table: something detects a condition, writes a row, a
screen reads it, the user dismisses it.

That table is a projection of facts that live elsewhere — stock levels, due dates, job outcomes —
with no rebuild, no verifier, and no drift report. Phase 8 refused exactly that shape for
reporting and gave the reason: *the moment a fact needs a third home, the second home was the
wrong one.* Worse, a stored notification goes STALE: "invoice 4471 is overdue" survives the
invoice being paid, and a user who has been told three times about something they already fixed
stops reading notifications at all.

What genuinely needs storing is the DISMISSAL — "I have seen this and do not want to see it
again" is a fact about the user, and lives nowhere else.

So notifications are computed on demand from rules, and dismissals are stored against a stable key
the rule derives. A condition that goes away takes its notification with it.

### What the phase must not become

An operations phase attracts scope. The refusals are in §3, but one is worth stating here because
it shaped the analysis: **no cloud sync, no remote backup, no telemetry.** This is an offline-first
desktop application for shops that may have no reliable internet, and a feature that silently
requires one is a feature that fails in the field.

---

## 2. DESIGN

### D1 — one backup mechanism, promoted to platform at its second caller

`migrate`'s snapshot-and-verify moves to `internal/platform/backup`. The migration runner becomes
its first caller and the operations service its second.

This is one caller earlier than the threshold `round.Allocate` and `domain.ValueOf` were promoted
at. The difference is that those were arithmetic — cheap to duplicate, obvious when wrong. A
backup that is silently unverified looks identical to one that is verified, and the cost of
finding out is the whole database.

### D2 — every backup records what it is a backup OF

A file called `mizan-20260816.db` tells a user the date and nothing else. A restore needs to know
the SCHEMA VERSION, because restoring a v41 backup into a v36 binary is a downgrade the migration
runner cannot perform and must refuse rather than attempt.

So a backup is a file plus a manifest — version, timestamp, size, and the reason it was taken —
and the manifest is written from the database itself, never from what the caller believed.

### D3 — restore verifies FIRST, backs up SECOND, swaps THIRD, and requires a restart

The order from the analysis, made concrete:

1. Open the incoming file independently and integrity-check it. Refuse a file that is not a
   database, is corrupt, or has no migration history.
2. Compare its schema version with the binary's. Equal or older is restorable — older migrates
   forward on the next start, which is the runner's ordinary job. NEWER is refused: this binary
   does not know how to read it.
3. Take a backup of the CURRENT database, through the same mechanism, labelled as a
   pre-restore snapshot.
4. Stage the incoming file beside the live one and record the intent.
5. Ask the user to restart. The swap happens at STARTUP, before the database is opened.

Step 5 is the part a desktop application can get wrong in a way a server cannot. The alternative
— closing every connection, swapping, reopening — leaves an open window in which a background job
or an in-flight binding call touches a file that no longer exists.

### D4 — export is CSV, and its shape is the report's

Every Phase 8 report exports. The columns are the report's own, the rows are its rows, and no
export re-queries anything — a second query would be a second answer.

CSV rather than XLSX because XLSX needs a library, and a library that writes a binary format is a
dependency for a decade. A shop that wants a spreadsheet opens the CSV in one.

Money exports as it crosses the JS boundary: a string of MINOR units, with the currency named in
its own column. Not formatted, because a formatted number is not a number to a spreadsheet, and
not divided by a hundred here, because that is the presentation layer's job in a locale this
package does not know.

### D5 — import goes through services, one row at a time, and reports every failure

No SQL. Row 3,000 failing does not roll back rows 1 to 2,999 — a partial import that says exactly
what did not go in is more useful than an all-or-nothing one that says only "row 3000 was bad".

A DRY RUN is the default posture of the screen: parse, validate every row through the same checks,
report what would happen, and only then commit. Importing four thousand products into a live shop
is not a thing to do twice.

### D6 — notifications are RULES, and only dismissals are stored

A rule is a function from the current state to zero or more notices. Each notice carries a stable
KEY derived from what it is about — `stock.low:<variantId>`, `sales.overdue:<documentId>` — and a
dismissal is a row against that key.

A condition that resolves takes its notice with it, because the rule stops producing it. Nothing
goes stale, and there is no projection to rebuild or verify.

The one thing this cannot do is tell a user about something that HAPPENED and is now over — "a
backup failed last night". That is what the job history already records, and a notice can be a
rule over it.

### D7 — nothing in this phase runs without a person, except what already did

Backups can be scheduled, because 0.8's scheduler exists and a shop that never takes one is the
common case. Restore, import and export are all initiated by a person and always will be:
each destroys or creates data, and none has a correct default.

---

## 3. WHAT THIS PHASE DOES NOT DO

- **No cloud sync, no remote backup, no telemetry.** Offline-first, for shops with no reliable
  internet. A feature that silently requires one fails in the field.
- **No XLSX, no PDF.** CSV exports and Phase 5's printing cover what a shop needs.
- **No import of TRANSACTIONS.** Products, partners and opening balances only. Importing posted
  invoices means importing their stock movements, their journal entries and their numbering, and
  a mistake there is unwindable only by hand.
- **No email or SMS.** Notifications are in-application. Sending anything needs a network, a
  credential store, and a delivery guarantee — three mechanisms for a feature nobody asked for.
- **No automatic restore.** A restore is always a person's decision.
- **No notification history.** See D6.

---

## 4. STEP SEQUENCE

| Step | What |
|------|------|
| 9.1 | The backup platform — promoted, manifested, scheduled, retained |
| 9.2 | Restore — verify, snapshot, stage, swap at startup |
| 9.3 | Export — CSV, from the reports that already exist |
| 9.4 | Import — through services, dry run first |
| 9.5 | Notifications — rules and dismissals |
| 9.6 | Bindings and screens |
| 9.7 | Phase 9 Definition-of-Done review |

---

## 5. DEFINITION OF DONE

1. There is ONE backup implementation, and the migration runner uses it.
2. Every backup is verified by opening it independently; an unverifiable one is reported as a
   failure, never listed as a backup.
3. A backup carries a manifest read from the database, and a restore refuses a backup NEWER than
   the binary.
4. A restore takes a snapshot of what it is about to replace, before it replaces it.
5. The swap happens where nothing holds the database, and a failed swap leaves the original
   intact.
6. Old backups are pruned by a policy, and the most recent is never pruned.
7. Every export's columns are the report's own, and no export re-queries.
8. An import calls the same service a screen calls and contains no SQL.
9. An import can be dry-run, and a failing row does not discard the rows that succeeded.
10. A notification disappears when its condition does; only dismissals are stored.
11. Every new binding has a declared policy, and every destructive operation is audited.
12. `make ci` green, with the mutation drills each step declares — and a drill that PASSES is
    treated as a defect in the test, the code, or the mutation.

---

## Step 9.1 — the backup platform

`internal/platform/backup`: take, verify, list, find, prune. The migration runner's private
snapshot became its first caller.

### D1 — the promotion kept the reasoning, and one line of it was nearly lost

Moving code is where behaviour quietly changes. This one nearly did.

0.7's `verifyBackup` tolerated a database with no `schema_migrations` table, on one line of
comment: *"a fresh database that has never migrated has no history table yet; that is fine."* The
promoted `Verify` refused it — and the migration suite failed within minutes, because **the
snapshot taken before the very first migration has no history table**, since the migration that
creates it has not run.

The fix drew a line worth keeping: `Verify` reports what it FOUND, including version 0, and the
stricter question — *is this something we can restore from* — belongs to the restore in 9.2, where
the policy is. A verification enforcing a restore's rules could not serve the migration path at
all.

### D2 — an unverifiable snapshot is REMOVED, never left on disk

A file in the backup directory is a promise. One that cannot be opened is a promise that will be
discovered broken at the worst possible moment — which is the moment somebody needs it.

### D3 — retention is per REASON

A nightly scheduled backup would otherwise push out the pre-migration snapshot from the upgrade
that broke something, which is the one a support conversation is about.

### D4 — pruning rides with the backup job

A separate prune job would be a second thing that can fail, and its failure — a disk filling over
months — is silent. And a prune failure never fails the RUN: the snapshot succeeded, and reporting
"backup failed" would send an operator looking for a file that exists.

### D5 — `CatchUp: RunOnce`

A laptop closed for a week gets ONE backup when it opens, not seven. Six would be identical and
the seventh would push the useful older ones out of the retention window.

### The drills

D204–D211, eight. Three passed, and the three resolutions were all different — which is the point
of having five:

- **D204** (*strengthen the test*) — deleting the cleanup of an unverifiable snapshot changed
  nothing, because `Take` removes the destination BEFORE writing, so the rubbish the test
  pre-created was gone before the statement ran and `Verify` failed on a missing file rather than
  an unusable one. The fake now writes the unusable file itself, which is what a truncated copy
  does.
- **D208** (*delete the redundant code*) — `max(s.keep, 1)` in `Prune` read as the floor that stops
  a misconfiguration emptying the directory. It cannot fire: `New` already normalises a
  non-positive `Keep` to the default. **The floor belongs in one place**, and the test now
  exercises the normalisation, where a drill can reach it.
- **D209** (*keep it, document that it saves work not answers, write no test*) — `Find`'s rejection
  of names containing separators or `..` is defensive. `Find` only ever returns entries from
  `List`, whose paths are built by joining this service's directory with basenames from `ReadDir`,
  so a traversal cannot escape a set it is never compared against. The guard stays for the
  validation error and the stated intent; what is now ASSERTED is the mechanism — every listed
  backup lives in the backup directory and names a file rather than a path.

`TestThereIsOnlyOneBackupImplementation` walks the tree and requires every online-backup statement
to be executed from this package. DoD criterion 1 is a claim a comment cannot keep.

---

## Step 9.2 — restore

`Prepare` while running, `Apply` at startup, and a restart between them.

### D1 — the split is the design, not a limitation

The application HOLDS the database. Everything that can fail happens in `Prepare`, while the
application is running and nothing is at risk; the one act that cannot be undone happens in
`Apply`, at the one moment when nothing has the file open.

Closing every pool mid-session and swapping would leave a window in which a background job or an
in-flight binding call touches a file that no longer exists. A desktop application can get this
wrong in a way a server cannot, because a server restarts as a matter of course.

### D2 — the order in `Prepare` is not negotiable

Verify the incoming file → refuse one newer than this build → **snapshot what is about to be
replaced** → stage → record the intent.

The snapshot is the step easiest to leave out and worst to omit. A restore that goes wrong having
left nothing to go back to is worse than no restore feature at all, because the user chose it
believing it was safe.

The version check prevents a specific failure: a shop that upgraded, took backups, then
reinstalled an older build to work around something. Restoring would give them a database whose
schema the code does not match, and the symptom would be a column that does not exist appearing
hours later in an unrelated screen.

### D3 — `Apply` renames the outgoing file before it renames the incoming one

A delete-then-rename has a window in which the live path holds nothing at all, and a crash there
costs the shop everything. Renaming aside first means a failure between the two leaves something
at the live path — and a failed second rename puts the original back, so a failed swap is a
restore that simply did not happen.

The displaced original is KEPT, under `.replaced`. It and the safety snapshot are the two things a
support conversation has to work with, and deleting one to save space is a trade nobody asked for.

The `-wal` and `-shm` sidecars are removed, because they belong to the database that was replaced
and a stale write-ahead log replayed over a restored file is corruption that looks like a restore
that half-worked.

### D4 — staging COPIES, and a failed restore is not a lost backup

An abandoned or cancelled restore leaves the backup exactly where it was.
`TestCancellingAStagedRestoreLeavesNothingBehind` asserts it, and a drill that changed the copy to
a move failed there.

### D5 — a failed `Apply` does not stop the application starting

The swap either happened or it did not, and both outcomes leave a usable database. Refusing to
boot would strand a shop with a working database and no way in.

### The drills

D212–D217, six, all failed once each was a real mutation. D214's first attempt did not compile —
`backup.Intent{}` in a short assignment needs parentheses — which is the same class as 8.5's D190:
**a mutation that does not apply is not a passing drill, it is no drill.** It is the second time
this phase's tooling has caught that, and the reason each drill's effect is now checked before its
result is believed.

`TestARestoreSurvivesARestartAndTakesEffect` is the one that matters. It boots, provisions a
company, backs up, renames the company, prepares a restore, asserts the RUNNING application still
sees the new name, shuts down, restarts, and asks again. The package's own tests could show none
of that.

---

## Step 9.3 — export

`internal/platform/tabular` writes CSV; three binding methods render Phase 8's reports.

### D1 — the export takes the SCREEN's data, and cannot re-query

`ExportAnalysis(analysis AnalysisDTO, …)` receives the DTO the screen already has. It has no
service to call — nothing in its signature can reach a report — so DoD criterion 7 holds by
CONSTRUCTION rather than by discipline.

A second query with the same arguments would be a second answer, and the user would have a
spreadsheet that disagrees with the screen it came from the moment anything changed in between.

### D2 — the byte-order mark is not decoration

Excel on Windows reads a UTF-8 file as the system's legacy code page unless it begins with a BOM.
Without it, an Arabic product name opens as mojibake **for exactly the users this application is
built for**. Every other consumer tolerates the BOM; Excel does not tolerate its absence.

### D3 — a cell that would become a formula is defused

A cell beginning with `=`, `+`, `-` or `@` is a FORMULA to Excel, LibreOffice and Google Sheets.
`=1+1` displays as 2, which is merely wrong.

The values come from a shop's own data — a product named "-- clearance --", a note starting
"+974…" — so this is primarily about an export that silently changes what the data SAYS. That it
also closes an injection route is the second reason, not the first. An ordinary value is left
alone: prefixing everything would be safe and would also put an apostrophe in front of every
product name in the file.

### D4 — a short row is refused

It silently shifts every value after it into the wrong column, and the file still opens. The
reader sees a cost under a quantity heading with no way to know.

### D5 — base64 over the boundary, not a path

Writing the file and returning where it went means this process choosing a directory on the user's
machine — a permission question on macOS, a different directory on Windows, a surprise on both.
Handing the bytes back lets the webview use the platform's own save dialog.

### The drills

D218–D222, five, all failed first time.

### A mistake worth recording

I created `internal/api/bindings/export_test.go` without checking whether the name was taken — and
it was, by a Phase 5 file holding `PublicMethodsForTest` and `DeclaredPoliciesForTest`. The build
broke four tests away from the change, which is the only reason it was caught immediately.

This is the second file-destroying collision in the project. The first was a drill backup keyed on
a BASENAME, where two files called `printing.go` overwrote each other; the rule recorded then was
*backup paths derive from the full path, never the basename*. The rule this one adds is narrower
and should have followed from it: **check whether a filename exists before writing it.** The new
file is `csv_export_test.go`.

---

## Step 9.4 — import

`internal/modules/imports`, over the real catalog and partner services.

### D1 — the adapter is four lines, and every one is the argument

`importCatalog.CreateProduct` calls `catalog.CreateProduct`. That call refuses a duplicate code,
resolves the unit, creates the DEFAULT VARIANT, and writes the audit entry, inside one
transaction. An importer with its own `INSERT` would do none of it, and a shop's first four
thousand products would be the only rows in the database no invariant was ever applied to —
indistinguishable from good ones until somebody tried to sell one.

`TestAnImportGoesThroughTheServiceAndGetsEverythingItDoes` asserts on the default variant, because
nothing in the CSV mentions one. A product that has a variant went through the service; a product
that does not was inserted around it.

### D2 — a structural check, because a comment is not a rule

`TestTheImporterContainsNoSQL` reads the package and refuses any statement keyword in a string
literal, any database import, and any query method.

Its first version matched the file's BYTES and failed on this package's own doc comment — which
says an importer must not "`INSERT` the rows, done". The comment is the argument for the rule;
failing on it would have meant deleting the explanation to satisfy the check. SQL has to be in a
string to be executed, so a literal is the only place worth looking.

### D3 — a failing row does not discard the rows that succeeded

A partial import that says exactly what did not go in beats an all-or-nothing one that says only
"row 3000 was bad". Re-importing a file whose good rows are already in is safe, because the
services refuse duplicates — which is the same property that makes the failure legible.

The row after a failure especially: an importer that stopped at the first error would have left it
out, and the test asserts it is in.

### D4 — `Line` is the SPREADSHEET's line, header included

An index into the parsed rows is off by one and sends the user to the wrong line of a
four-thousand-row file.

I then made exactly that mistake in the test that checks it — indexing `Rows[3]` and getting the
row after the failure. The test now searches for the failure instead. **The off-by-one a field
exists to prevent is one the test writer makes too.**

### D5 — columns are read by NAME

A spreadsheet somebody edited has its columns in whatever order they left them, and an importer
reading by position would put SKUs in the price column without complaining.

The exported BOM is stripped on the way back in: a file this application exported and Excel edited
comes back with the mark still on it, and the first header would otherwise match nothing.

### The drills

D223–D227, five, all failed first time.

---

## Step 9.5 — notifications

`internal/platform/notify` runs rules; `internal/modules/ops` stores dismissals; the rules
themselves live in the composition root.

### D1 — the table holds DISMISSALS, and that is the whole design

A `notifications` table would be a projection of facts living elsewhere, with no rebuild, no
verifier and no drift report — the shape Phase 8 refused for reporting. It would also go stale:
"invoice 4471 is overdue" survives the invoice being paid, and **a user told three times about
something they already fixed stops reading notifications at all**, which costs more than never
having had them.

`TestAFreshInstallIsToldItHasNoBackup` asserts the distinction directly: taking a backup silences
the notice, and `Dismissed` stays at zero. The condition resolved; nothing was suppressed.

### D2 — a key is prefixed by its rule

A dismissal is stored against the key, so two rules both emitting "overdue" would mean dismissing
one silences the other — with no way for the user to tell which, or to get it back.

### D3 — one key per CONDITION, or one per THING, decided per rule

The valuation rule emits ONE notice however many variants are involved: a shop whose books are out
by a hundred does not need four hundred notices, and a key per variant would let somebody dismiss
the difference away one line at a time.

The job rule emits one per JOB, because a failing backup and a failing outbox dispatch are
different problems with different fixes.

### D4 — a broken RULE is reported; an unreadable DISMISSAL STORE fails

Opposite calls, deliberately. A centre that goes quiet because one rule broke is worse than one
that says so — **silence reads as "nothing is wrong"**. But a dismissal store that cannot be read
means every notice reappears including the ones the user explicitly silenced, and nagging somebody
who already said no is how a feature gets turned off entirely.

### D5 — the job rule reads the MOST RECENT outcome

9.5's design named a gap: a rule cannot tell a user about something that happened and is over. The
job history is the answer, because a rule over that history is still derived from current state.

"Most recent" is what makes it a rule rather than a stored message. A job that failed at midnight
and succeeded at one o'clock is working, and reporting it would teach the reader that these
notices do not mean anything.

### The drills

D228–D233, six. One passed and one did not apply:

- **D233** — removing the "most recent outcome" filter changed nothing, because NO test covered
  the job rule at all. `TestAJobIsReportedOnlyWhileItsMostRecentRunFailed` now writes a failed run
  straight to `job_runs`, asserts the notice, writes a later success, and asserts it resolves.
- **D230** — left `strings` unused and did not compile. Third time this phase's tooling has caught
  a non-mutation, and the reason each drill's effect is verified before its result is believed.

### Two findings from CI rather than from a drill

**The foreign key caught a fake user.** `notice_dismissals.user_id` references `users(id)`, and
the first version of the per-user test used made-up ids. The schema refusing to store a preference
for somebody who does not exist is the constraint doing exactly its job.

**A Phase 8 test had expired.** `TestPhaseEightAddedNoSchema` asserted no migration above 0036
existed. Phase 9 added 0037 legitimately, and there is no version of that test that survives — the
window between Phase 8's last migration and Phase 9's first is EMPTY. Weakening it to stay green
would have been worse than deleting it: **a check nobody can make fail is a claim nobody has
verified.** It is recorded in place of the test, with what it did and did not prove.

---

## Step 9.6 — bindings and screens

One `Operations` façade; three screens.

### D1 — three invented permissions, caught by the coverage check

The first version of `operationsPolicies` demanded `system.manage`, `partner.manage`, and an
`ops.notice.view` declared in a package that is not a module. **None of the three exists**, and
7.6's coverage check named all of them: *a permission no module declares can never be granted, so
the method is permanently unreachable.*

The replacements are permissions that already mean the thing:

- Backups and restore use `identity.user.manage`. Whoever may create and delete users administers
  this installation, and a backup contains every user's data — the two powers are the same power.
- Notices use `identity.session.view`, which the Jobs and Runs screens already use. They answer
  the same question: how is this installation running.
- Imports use the catalog's and partner's own manage permissions, because an import CREATES
  records and there is no separate "may import" — that would be a second way to grant one power.

`ops.PermNoticeView` was deleted rather than left as a constant nothing can grant.

### D2 — the restore screen's job is to be understood, not to warn

Three things do that, and none is a warning triangle: the confirmation NAMES the backup and its
date; the result says a RESTART is required; and the safety snapshot is named on screen before it
is needed, so the way back is visible in advance.

A backup from a newer build is LISTED and marked, never hidden — a user hunting for a file they
know they made is worse off than one told why it cannot be used, and "update, then restore" is
something they can act on.

### D3 — the import screen's commit button does not exist until a check has run

A user who has not seen what would happen cannot agree to it, and an import is not something to
discover the shape of afterwards. Every row is shown, not only the failures.

### The drills

D234–D237, four, all failed first time.

### The defect CI could not see

Every interpolated message added in Phases 8 and 9 used `{{name}}`. This catalogue interpolates
`{name}`, and the renderer leaves unknown syntax alone — so "3 hidden" rendered as **"{3} hidden"**
in both locales, across seventeen keys.

`TestEveryErrorCodeHasATranslation` checks that a key EXISTS, which every one of them did. The Go
tests assert on message keys rather than rendered text. The only thing that caught it was one
frontend test that happened to assert a rendered interpolation.

The lesson is narrower than "test rendering": **a message catalogue has a syntax, and nothing was
checking that the messages were written in it.** `TestNoTranslationUsesDoubleBracePlaceholders`
now does, and it is drilled.

A second collision came out of the same fix: `imports.failed` existed in both `common.json` and
`errors.json`, which the catalogue loader refuses as "defined twice in the same locale" — at
STARTUP, so every binding test failed at once. The error code owns the key; the screen's heading
got its own.

---

## Step 9.7 — Phase 9 Definition-of-Done review

**Result: 12/12 met.**

| # | Proven by | Written in this step |
|---|-----------|----------------------|
| 1 | `TestThereIsOnlyOneBackupImplementation` | — |
| 2 | `TestAnUnverifiableSnapshotIsRemovedRatherThanListed` | — |
| 3 | `TestASnapshotIsVerifiedAndCarriesWhatItIsABackupOf`, `TestABackupFromANewerBuildIsRefused` | — |
| 4 | `TestPreparingARestoreTouchesNothingAndSnapshotsWhatItWillReplace` | — |
| 5 | `TestARestoreSurvivesARestartAndTakesEffect` | `TestAFailedSwapLeavesTheOriginalIntact` |
| 6 | `TestPruningKeepsTheNewestOfEachReason…`, `TestKeepingZeroStillKeepsOne` | — |
| 7 | `TestAnExportRendersTheReportItWasGivenAndDoesNotRequery` | — |
| 8 | `TestTheImporterContainsNoSQL`, `TestAnImportGoesThroughTheService…` | — |
| 9 | `TestADryRunWritesNothing`, `TestAFailingRowDoesNotDiscardTheRowsThatSucceeded` | — |
| 10 | `TestANoticeDisappearsWhenItsConditionDoes`, `TestAFreshInstallIsToldItHasNoBackup` | — |
| 11 | 7.6's policy-coverage check | `TestEveryDestructiveOperationLeavesAnAuditEntry`, `TestADryRunIsNotAudited` |
| 12 | `make ci` | drills D204–D240 |

### Criterion 5's second half had no test, and could not have one

*"A failed swap leaves the original intact."*

`Apply` renames the outgoing database aside, then renames the incoming one into place. If the
SECOND rename fails, the live path holds nothing, and putting the original back is the difference
between "the restore did not happen" and "the shop has no database".

There is no way to make that rename fail from outside the process. **A path that cannot be tested
is a path that has never run** — so the package exposes the rename as a replaceable variable,
declared in a test-only file, used by exactly one test.

That is a seam added for a test, which this codebase otherwise avoids. The alternative was leaving
the three most dangerous lines in the phase unexercised behind a comment saying they were careful.

### Criterion 11 was half-met and read as met

*"Every destructive operation is audited."*

Every BINDING had a policy — 7.6's check guarantees that. Nothing was audited. A restore, the
single most destructive act the application offers, left no entry at all.

Three acts are now audited: a prepared restore, a cancelled one, and a completed import. A dry run
is NOT, because it writes nothing and auditing it would fill the trail with entries about decisions
nobody made.

The prepared-restore entry names the SAFETY SNAPSHOT, and the test asserts that specifically —
an auditor reading "a restore was prepared" wants to know what the shop could go back to, and
that name is the answer.

Auditing here happens AFTER the act rather than in-transaction, unlike every document in Phases
5–7. There is no transaction: the act is a file on disk. A failure to write the entry is reported
and does not fail the call, because reporting a staged, correct restore as failed would send an
operator looking for a problem that is not there.

### Phase 9 in total

37 drills, D204–D240. Seven passed and every one produced a change — including three where the
mutation had not applied at all, which is why counting a mutation's effect before believing its
result became part of how these are run.

The two most valuable findings came from neither a drill nor a criterion:

- **`{{name}}` in seventeen message keys.** The catalogue interpolates `{name}`, the existence
  check only checks existence, and the Go tests assert on keys rather than rendered text. One
  frontend test asserting a rendered interpolation caught it. *A message catalogue has a syntax,
  and nothing was checking that the messages were written in it.*
- **Three invented permissions**, caught by 7.6's coverage check the moment the façade was
  registered — the check earning its keep two phases after it was built.
