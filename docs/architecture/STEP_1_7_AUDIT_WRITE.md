# Step 1.7 — The Audit Write Path (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green; both mutation drills confirmed (§7.3).
> This step realises **phase D7**, which amends §12's pipeline: the audit record is written
> **inside** the business transaction, not after commit.
> Scope: `modules/audit/contract` (the `Auditable` event), the in-transaction subscriber, the
> actor snapshot, and wiring real events from identity and org.
> **Out of scope:** the audit viewer screen (1.11); retention (Phase 9); hash chaining
> (excluded from v1, decision 8).

---

## 1. ANALYSIS

### 1.1 The contradiction this step resolves

The phase design (§AUD.2, D7) found §15.1 and §12 disagreeing:

- **§15.1**: the audit record is *"written from event subscribers"*.
- **§12**: the pipeline places `Audit` **after** `commit`.

After commit, a process that dies in the gap leaves a business change with **no audit record**
— and completeness is the audit trail's entire value. "The invoice exists but its audit entry is
still pending" is a sentence nobody wants to say to an auditor.

D7 chose atomicity. This step builds it, and §12's diagram is superseded on that one point.

### 1.2 Atomicity is inherited, not implemented — D2

Step 0.6 already built everything needed:

- `Publish` runs subscribers **synchronously, in the caller's goroutine and transaction**.
- The subscriber reaches the database through `db.Writer(ctx)`, which resolves to the **live
  transaction** when the context is inside a Unit of Work (0.3 §3.2).
- A handler error **aborts the publish and propagates**, so the enclosing `Do` rolls back — and
  a handler *panic* is recovered into an error rather than swallowed, because "a recovered panic
  that allowed the commit to proceed would be strictly worse than the crash" (0.6 §3.3).

So this step writes almost no atomicity machinery. It publishes an event inside the transaction
and lets three existing guarantees compose. That is what a foundation is for, and it is worth
noticing that the pieces fit without adjustment.

### 1.3 The cost, restated plainly

**If the audit write fails, the business operation fails.**

A disk error, a constraint violation, a bug in the subscriber — any of them abort the sale, the
password change, the role grant. That is the direction D7 chose, and it is the right one for a
system whose §1.3 principle is that financial records are append-only and whose audit log is
meant to be legally defensible: *a shop that cannot record what it is doing should stop, not
continue silently.*

It is still a real cost, and it is the thing to change first if it ever proves wrong in the
field. The alternative — the outbox (§AUD.2 option B) — is designed and rejected, not
un-considered.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| The subscriber opens its own connection | The audit row commits even when the change rolls back | It uses `db.Writer(ctx)`; a drill asserts a rolled-back change leaves **no** entry (§5) |
| Auditing a read | Every list screen writes a row; the table becomes unusable | Only state changes publish (§4.3) |
| The actor is lost | An entry that cannot answer "who" | Snapshotted from `appctx` at publish time (§3.3) |
| A module forgets to publish | A silent gap in the trail | Not solvable by machinery in this step — stated honestly in §6 |
| Audit failure blocks login forever | A shop cannot open | Login failures audit inside the attempt's own transaction, which has no business change to lose (§4.4) |

---

## 2. DESIGN — `Auditable`

```go
// modules/audit/contract
type Auditable struct {
    Action      string   // "identity.user.password_changed"
    EntityType  string
    EntityID    id.ID
    EntityLabel string   // snapshot
    Before      any      // marshalled to JSON; nil for a creation
    After       any      // nil for a deletion
    Changed     []string
    Source      string   // "" → derived from the context
}
```

It implements `event.Event`, so `eventbus.Subscribe[Auditable]` routes it like any other domain
event.

### 2.1 Where it lives — D1

In **audit's `contract` package**, which is the only legal cross-module channel (`module-isolation`,
1.1). Identity and org import it to publish; nothing imports audit's internals.

Note the direction: publishers depend on the *event type*, never on the audit *service*. A module
announces that something happened; whether anyone records it is not its business — which is the
§3.2 "downstream modules react to events" preference, and what keeps audit removable.

### 2.2 `Before`/`After` are `any`, marshalled by the subscriber

A publisher passes the domain values it already holds; the subscriber marshals them once. The
alternative — every publisher building its own JSON — would mean every module choosing a shape,
and the payload would stop being comparable across modules.

`nil` marshals to SQL NULL rather than the string `"null"`, so "there was no before state" is
representable and distinguishable from a payload that is literally null.

---

## 3. DESIGN — the subscriber

```go
func (m *Module) Subscribe(bus *eventbus.Bus, _ *outbox.Subscribers) error {
    return eventbus.Subscribe(bus, "audit.record", m.svc.record)
}
```

### 3.1 One subscription, on the synchronous bus

Not the outbox. §23.1 reserves the outbox for cross-module *integration* events delivered after
commit — which is precisely the property D7 rejects here.

Audit subscribing to other modules' events looks like it violates §23.1's "domain events are
within a module". It does not, and the distinction is worth stating: **audit is cross-cutting
infrastructure**, like logging. It participates in no business decision and never calls back
into a module. Modules publish `Auditable`; audit is its only subscriber; no module knows audit
exists.

### 3.2 The write

`db.Writer(ctx)` — the executor from the context (0.3 §3.2). Inside a Unit of Work that is the
live transaction, so the entry commits with the change or not at all. There is no `Do` in the
subscriber: opening one would **join** the caller's transaction (0.3 D5) and add nothing, and
opening a *separate* connection would break the guarantee entirely.

### 3.3 The actor snapshot

From `appctx.Actor` (1.3/1.5). The entry stores `actor_name_snapshot` and
`entity_label_snapshot` as **values**, not references, so a five-year-old record stays readable
after the user is renamed or deactivated (§15.1) — the same reasoning that kept a foreign key
off `created_by` in 1.1 D4.

`Actor` gains `DisplayName` and `SessionID` for this, populated by the 1.5 guard. `correlation_id`
comes from `event.CorrelationID(ctx)`, stamped per call since 0.10 — which is what ties every
record produced by one user action together.

With no actor — the setup wizard, a job — the entry records the source as `system` or `job` and
leaves the actor null. An unattributed entry is honest; a fabricated one is not.

---

## 4. DESIGN — what gets audited

### 4.1 In this step

Per §15.3, the Phase-1 subset:

| Module | Actions |
|---|---|
| identity | user created, password changed, user activated/deactivated, role assigned/unassigned, grant added/revoked |
| identity (auth) | login succeeded, login failed, logout, session revoked |
| org | company provisioned, branch/warehouse activated/deactivated |

### 4.2 Not audited

Reads. Auditing a list screen would write a row per page view and make the table unusable for
the question it exists to answer. §15.3 says "all writes to financial and master data" — writes.
Report *exports* containing cost or profit data are audited, and arrive with Phase 8's exports.

### 4.3 Publishing is opt-in, and that is a real gap

There is no mechanism forcing a module to publish. A new write path that forgets leaves a silent
hole, and unlike the 1.5 policy check this cannot be caught by enumerating a surface — "did you
publish?" is not a property of a method signature.

Stated rather than papered over. The mitigation that would work is a review checklist item and,
eventually, a test per module asserting its state-changing operations publish. Recorded in §6.

### 4.4 Failed logins

A failed login has no business change to abort, so "atomic with the change" is vacuous. It is
audited inside the same transaction as its `login_attempts` row, so the attempt and its audit
entry commit together — the same guarantee applied to the only write there is.

---

## 5. TESTING PLAN

| Level | Tests |
|---|---|
| **Atomicity — commit** | A password change writes exactly one entry, with actor, correlation id, and payload. |
| **Atomicity — rollback (the drill)** | A business operation that fails **after** publishing leaves **zero** audit entries. |
| **Abort (the drill)** | A subscriber that fails **aborts the business change** — the password is unchanged and the old one still authenticates. |
| **Panic** | A panicking subscriber also aborts, rather than being swallowed (0.6 §3.3). |
| **Actor** | The snapshot records the display name; correlation ties two entries from one action; no actor → `system` with a null actor. |
| **Reads** | Listing users writes no entry. |
| **Payload** | `nil` before/after is SQL NULL, not the string "null". |

**Mutation-verified**: *atomicity* (publish outside the Unit of Work → the rollback drill must
fail) and *abort* (swallow the subscriber error → the abort drill must fail).

---

## 6. DECISIONS TAKEN

1. **D1 — `Auditable` lives in audit's `contract`**, the §3.2 channel. Publishers depend on the
   event type, never on the audit service. *(§2.1.)*
2. **D2 — Atomicity is inherited from 0.3 + 0.6**, not re-implemented: the subscriber writes
   through the context's executor and a handler error rolls the transaction back. *(§1.2.)*
3. **D3 — A failed audit write aborts the business operation.** The cost of D7, restated.
   *(§1.3.)*
4. **D4 — `appctx.Actor` gains `DisplayName` and `SessionID`** for the snapshot columns.
   *(§3.3.)*
5. **D5 — `audit.DependsOn` corrected to none.** 1.6 declared `["identity"]`, but `audit_log`
   carries **no foreign key** to `users` — deliberately, so history does not depend on a live
   identity row (1.1 D4). The declaration was wrong.
6. **D6 — Reads are not audited**; only state changes. *(§4.2.)*

### Known gap, carried

**Nothing forces a module to publish.** A new write path that forgets leaves a silent hole in
the trail, and it is not catchable by the enumeration trick that made 1.5's guarantee
structural. The honest mitigations are a review item now and a per-module "state changes
publish" test later; neither is built in this step.

---

## 7. IMPLEMENTATION RECORD

### 7.1 What was built

| File | What |
|---|---|
| `kernel/event/event.go` | `Publisher` — the one-method port modules depend on instead of `*eventbus.Bus` |
| `modules/audit/contract/auditable.go` | The `Auditable` event and the four `Source` constants |
| `modules/audit/record.go` | The subscriber, `Actor`, `ActorResolver`, payload marshalling |
| `modules/audit/repo.go` | `insert` — through `db.Writer(ctx)`, still no update, still no delete |
| `modules/audit/audit.go` | `Subscribe` registers the handler; `DependsOn` corrected to none (D5) |
| `modules/identity/audit.go` | Action constants, the `audit` helper, the user snapshot |
| `modules/identity/{service,sessions,rbac}.go` | Twelve publishers, each inside a Unit of Work |
| `modules/org/service.go` | Three publishers |
| `bootstrap/actors.go` | The `appctx` → `audit.ActorResolver` adapter |
| `api/appctx` + `api/bindings/guard.go` | `Actor` gains `DisplayName` and `SessionID` (D4) |

### 7.2 Three decisions taken during implementation

**A service with no publisher fails at the first audited write.** The tempting shape is a
no-op publisher, so a partly-wired graph still works. It would mean a deployment that records
*nothing at all* and never says so — an empty trail is not a degraded trail, it is a missing
one. `identity.publisher_missing` and `org.publisher_missing` turn a wiring mistake into a
caught bug.

**Every identity and org test now wires the audit subscriber**, not just the audit tests. The
write happens inside every one of those transactions, so if it can break an ordinary operation,
the ordinary tests must be the ones that notice. Forty tests exercise the D7 path instead of
two. This is the §1.2 lesson applied in advance: an unused seam is an untested seam.

**Payloads are hand-written projections, never the domain type.** `snapshotUser` exists so that
what enters the trail is decided once, deliberately, in one place. Marshalling `domain.User`
directly would mean every field added to it over the next ten years silently joins a long-lived
plaintext record — and `TestAuditNeverRecordsACredential` asserts the password and its hash
never appear.

Two payloads that would have leaked and do not: the argon2 encoding is in scope at
`CreateUser`'s publish site, and the session token is in scope at `Login`'s. Both are asserted
absent.

### 7.3 The mutation drills

**Drill 1 — write outside the transaction.** `db.Writer(ctx)` → `db.Writer(context.Background())`
in `repo.insert`, so the entry takes a connection of its own.

*Result:* `TestAuditEntryRollsBackWithTheChange` **deadlocked and timed out**. Not merely a
failed assertion — SQLite has exactly one writer (0.3), so a subscriber that opens its own
connection while the caller holds the write lock waits for a lock the caller will not release
until the subscriber returns. This class of mistake cannot be shipped: it hangs on the first
run, on the developer's machine.

**Drill 2 — swallow the subscriber error.** `return s.bus.Publish(...)` → `_ = s.bus.Publish(...); return nil`
in `identity.audit`.

*Result:* both abort drills failed as required — `SetPassword succeeded although the audit write
failed` and `CreateUser succeeded although a subscriber panicked`. D3 is watched.

### 7.4 What the tests found

`TestReadsAreNotAudited` failed on its first run, reporting one entry: `org.company.provisioned`.
The fixture's own provisioning had audited itself — the write path working, discovered by a test
asserting something else. It is also the fourth time a **count-based** assertion has broken in
this project (migrations 0.6, modules 1.1, bindings 1.5 and 1.6). The audit trail is a shared,
growing surface, and the bindings tests were rewritten to **filter** by a fixture-owned entity
type rather than assume the table holds only their own rows.

Those same bindings fixtures moved off raw SQL and onto the real write path, which they could
not use in 1.6 because it did not exist. They now exercise the subscriber, the actor snapshot,
and the composition root's `ActorResolver` adapter — the redaction assertions are unchanged and
still pass, which is the useful part: the read path was right about a payload it had never
actually been given.

### 7.5 Carried forward

- **Nothing forces a module to publish** (§6). Unchanged, and still the honest gap in this step.
- `recordFailure` is the ONE place an audit error is swallowed, with its reasoning at the call
  site: a database hiccup while recording a bad password must not turn "wrong password" into
  "internal error", and the login already failed, so nothing incorrect can commit on the
  strength of the missing record.
- Login records `source: system` and no actor, because no session has been validated at that
  point — correct, but it means "who signed in" lives in the payload rather than the actor
  column. Worth revisiting when the audit viewer (1.11) shows what that reads like.
