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
