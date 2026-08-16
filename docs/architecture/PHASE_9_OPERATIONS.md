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
