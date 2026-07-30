# Step 0.6 — Event Bus & Transactional Outbox (Design + implementation record)

> Status: **APPROVED and IMPLEMENTED.** Design approved 2026-07-30.
> **D3 was rejected in favour of Option A** — per-handler delivery tracking with a real
> table. That reversal and everything it changed is recorded in **§10**; §5.6 below is the
> superseded reasoning, kept so the trade-off stays visible.
> (Permanent Development Protocol, Steps 2–6.)
> Scope: `internal/kernel/event`, `internal/platform/eventbus`, `internal/platform/outbox`.
> **Out of scope:** the durable job scheduler that *runs* the dispatcher on a timer (0.7 —
> see §1.4 for exactly where the line falls), business events (they arrive with their
> modules), the UI for dead-lettered events (Phase 9 diagnostics).

This step builds the atomicity guarantee sitting under every cross-module side effect in the
product. ARCHITECTURE_v1 §23.3 states the stake plainly: without it, "an app crash between
'invoice saved' and 'journal entry created' would leave the books wrong."

Every module will depend on this from its first commit, which is why it is Phase 0 rather
than later.

---

## 1. ANALYSIS

### 1.1 Two kinds of event, and why conflating them is the classic bug

ARCHITECTURE_v1 §23.1 separates them deliberately:

| | **Domain events** | **Integration events** |
|---|---|---|
| Scope | Within a module | Across modules |
| Delivery | Synchronous, same transaction | Asynchronous, after commit, via outbox |
| Failure | Rolls back the whole operation | Retried independently |
| Example | Invoice recalculates its totals | Accounting posts the journal entry |

Conflating them produces one of two failures, both of which are hard to diagnose months
later: either a side effect is silently lost because it was fired outside the transaction
that was rolled back, or an entire sale fails because an unrelated subscriber — a dashboard
counter, a notification — returned an error.

The separation is therefore two distinct mechanisms in this step, not one bus with a flag.

### 1.2 Why the outbox, and not "just publish after commit"

The tempting simplification is to commit the business transaction, then publish. It is wrong
in a way that only shows up in production: the process can die in the gap. The invoice is
saved and the journal entry never happens, and nothing anywhere records that it should have.
For an accounting system this is unrecoverable without manual forensics.

Writing the event **inside** the same transaction makes the gap impossible: either both the
business row and the intent-to-notify commit, or neither does.

The second reason is in ARCHITECTURE_v1 §23.3 and matters more over time: the outbox is
already "an ordered, durable, causally-linked change log", which is exactly the shape a
future cloud-sync protocol consumes. Building sync later without it would mean inventing
change tracking from scratch, or reaching for triggers, which the portability contract (§8.2)
forbids.

### 1.3 What already exists

Unusually for a step, most of the schema is in place — `outbox_events` was created in
`0001_platform.sql` (Step 0.4, decision D4), including the `dead` status and the dispatch
index:

```
id, occurred_at, event_type, aggregate_type, aggregate_id, payload_json,
correlation_id, causation_id, branch_id,
status ∈ (pending|processing|done|failed|dead),
attempts, next_attempt_at, last_error, processed_at
ix_outbox_events_dispatch (status, next_attempt_at, occurred_at)
ix_outbox_events_aggregate (aggregate_type, aggregate_id)
```

`config.ChangeNotifier` (Step 0.5) is a port waiting for exactly this bus, and
`internal/kernel/event` is still a marker package. **No migration is needed for this step**
— see §5.3 for the one place that was nearly untrue.

### 1.4 The 0.6 / 0.7 boundary

PHASE_0_FOUNDATION §BUS.2 says the dispatcher runs "as a durable job (§JOB)", and §JOB is
Step 0.7. The line this design draws:

- **0.6 builds the dispatcher as a callable component** — `Dispatcher.DispatchOnce(ctx)`,
  driven explicitly by tests and, temporarily, by the bootstrap.
- **0.7 schedules it**, turning periodic invocation into a durable job with a heartbeat.

Drawn this way, 0.6 is fully testable and provable on its own, and 0.7 adds scheduling to a
dispatcher whose correctness is already established. The alternative — deferring the
dispatcher to 0.7 — would leave this step delivering a table writer nobody can prove works.

### 1.5 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Event published outside the business transaction | Books silently wrong after a crash | Publish uses the caller's transaction; rollback discards the event (§4.1) |
| A subscriber's failure rolls back the sale | A dashboard counter can abort a customer's purchase | Integration handlers run after commit, retried independently (§5) |
| Handler panics | One bad subscriber crashes the app | Recovered at the dispatch boundary, converted to a typed error (§3.3, §5.4) |
| Events for one aggregate processed out of order | A journal entry posts before the invoice it derives from | Per-aggregate ordering with head-of-line blocking (§5.2) |
| Dispatcher crashes mid-batch | Rows stuck in `processing` forever, events never delivered | Visibility timeout + reclaim, using existing columns (§5.3) |
| A failing handler retried forever | Log spam, a queue that never drains | Capped exponential backoff + jitter → `dead` (§5.5) |
| Retry re-runs handlers that already succeeded | Double journal entries | Handlers must be idempotent; a stable `DeliveryID` makes it cheap (§5.6, **D3**) |
| Subscriptions discovered by reflection | Nobody can grep who reacts to an event | Explicit registration at bootstrap (§3.1, §23.2) |

---

## 2. DESIGN — `kernel/event`

The envelope both mechanisms share, so a domain event promoted to an integration event later
does not change shape.

```go
// internal/kernel/event
type Event interface {
    EventType() string      // "sales.invoice_posted" — namespaced, stable, part of the contract
    AggregateType() string  // "sales_document"
    AggregateID() id.ID
}

type Envelope struct {
    ID            id.ID      // UUIDv7: the event's own identity and its natural ordering
    Type          string
    OccurredAt    time.Time
    AggregateType string
    AggregateID   id.ID
    CorrelationID id.ID      // the user action that started this chain
    CausationID   id.ID      // the event that directly caused this one
    BranchID      id.ID
    Payload       []byte     // JSON
}
```

`CorrelationID` and `CausationID` are carried in the context and stamped automatically, so a
handler that publishes a follow-up event links it without having to remember. Without that,
causal chains are only as good as the discipline of whoever wrote the handler — which is to
say, absent by the third module.

`kernel/event` stays dependency-free (stdlib + kernel), per the `kernel-purity` archlint rule.

---

## 3. DESIGN — the in-process domain bus

### 3.1 Explicit, typed registration

```go
// internal/platform/eventbus
type Bus struct{ /* type → []handler, guarded */ }

func New(opts Options) *Bus
func Subscribe[T event.Event](b *Bus, name string, h func(context.Context, T) error) error
func (b *Bus) Publish(ctx context.Context, e event.Event) error
```

Generic `Subscribe` gives handlers a concrete typed argument rather than an `any` plus a type
assertion. Registration is explicit at bootstrap — **no reflection-based discovery** (§23.2),
so "who reacts to invoice posted?" is answerable with grep, which is the only thing that
still works when the person who wrote it has left.

`name` identifies the subscription in logs and errors. Duplicate `(event type, name)` pairs
are an error, for the same reason as the strategy registry: silent last-one-wins resolved by
init order.

### 3.2 Synchronous, in the caller's transaction

`Publish` runs every subscriber **synchronously, in the caller's goroutine and transaction**.
A handler error aborts the publish and propagates, so the enclosing Unit of Work rolls back —
which is the defined semantic for domain events (§23.1) and the reason they exist: "invoice
recalculates its totals" must not be able to half-happen.

### 3.3 Panic isolation that does not swallow

A panicking handler is recovered at the publish boundary and converted to a typed
`errs.Internal` naming the subscription — then **returned**, so the transaction still rolls
back.

The distinction matters: "isolate panics" must not mean "swallow them". A recovered panic in
a domain handler that let the transaction commit would be strictly worse than the crash,
because the operation would appear to have succeeded.

### 3.4 Ordering

Handlers for one event run in registration order — deterministic, and the order is visible in
the bootstrap. There is no priority mechanism: priorities are a way of encoding a dependency
between handlers that should have been made explicit.

---

## 4. DESIGN — the outbox writer

### 4.1 Publishing inside the transaction

```go
// internal/platform/outbox
func (s *Store) Publish(ctx context.Context, e event.Event) error
```

`Publish` inserts one `outbox_events` row using `db.Writer(ctx)` — the executor resolved from
the context, so inside a Unit of Work it is the live transaction. No separate connection, no
"after commit" hook.

This single fact is the entire atomicity guarantee, and §9 asserts it directly: a rolled-back
business transaction must leave **no** outbox row.

`status` starts `pending`, `attempts` 0, `next_attempt_at` = now (immediately eligible).

### 4.2 Payload

JSON, produced by `encoding/json`. Two rules carried from Step 0.5's codecs, for the same
reason: **money is never encoded as a float**, and payloads decode into concrete typed
targets, never `any` — an untyped decode turns every number into `float64` and corrupts
anything past 2^53.

Payload shape is part of an event's public contract. An event type whose payload changes
meaning gets a **new event type**, never a redefinition — historical rows in a customer's
outbox must remain interpretable.

---

## 5. DESIGN — the dispatcher

### 5.1 The loop

```go
func (d *Dispatcher) DispatchOnce(ctx context.Context) (Report, error)
```

One pass: claim a batch, dispatch each, record the outcome. Returns counts (dispatched,
failed, dead-lettered, skipped) so the caller — a test today, the scheduler in 0.7 — can see
what happened.

Claim is `status = 'pending' AND next_attempt_at <= now`, ordered by `occurred_at, id`, which
is exactly the `ix_outbox_events_dispatch` index built in 0.4.

### 5.2 Per-aggregate ordering, and deliberate head-of-line blocking

§BUS.1 requires ordering per aggregate. That needs more than an `ORDER BY`: if event 1 for
invoice A fails and is scheduled for retry, event 2 for invoice A **must not** be dispatched
first — a "invoice voided" landing before "invoice posted" produces a wrong ledger that
balances, which is the worst kind of wrong.

So the claim skips any aggregate that has an earlier event not yet `done`. One stuck event
blocks its own aggregate and nothing else. That is head-of-line blocking, and it is the
intended behaviour: for a single aggregate, order is a correctness property, not a
performance preference.

### 5.3 Crash recovery — the `processing` state

A dispatcher that dies mid-batch leaves rows in `processing`. Without recovery those events
are never delivered and nothing reports it.

Rows are claimed by setting `status = 'processing'` **and** `next_attempt_at = now +
visibilityTimeout`. A `processing` row whose `next_attempt_at` has passed is presumed
abandoned and reclaimed to `pending`, with a logged warning — the same stale-lock reasoning as
the migration lock (0.4 §3.4).

This deliberately reuses existing columns: the obvious alternative, adding
`processing_started_at`, would mean a migration, and `next_attempt_at` already means "the
earliest time something should happen to this row". **No migration is required for this step.**

### 5.4 Handler failure and panics

An integration handler that returns an error, or panics, marks the event `failed` with
`attempts+1`, `last_error`, and a computed `next_attempt_at`. Panics are recovered per
handler: a bad subscriber must not take down a desktop app that is currently ringing up a
sale.

### 5.5 Backoff and the dead letter

Exponential with jitter, capped:

```
delay = min(base × 2^(attempts-1), cap) ± jitter
```

Jitter is not ceremony — without it, a batch of events that all failed for the same reason
(the printer is off) retries in lockstep forever.

After `MaxAttempts` the row becomes `dead` and is never retried automatically. `dead` is a
state a human must look at; the schema already has it, and the Phase 9 diagnostics screen is
where it surfaces. Nothing is ever deleted.

### 5.6 Idempotency — D3, the decision I most want a second opinion on

The dispatcher marks an event `done` when **all** its handlers have succeeded. If one of
three handlers fails, the retry re-runs all three — so **handlers must be idempotent**, as
§BUS.2 requires ("handlers declare it").

I considered the stronger alternative — an `outbox_deliveries (event_id, handler)` table
giving per-handler tracking, so a retry only re-runs what failed. I am **recommending against
it**, on balance:

- It needs a new table and therefore a migration, which §1.3's "no migration needed" would
  otherwise avoid.
- It makes adding a subscriber semantically murky: when v1.4 adds a handler, does every
  already-`done` historical event get delivered to it? Both answers are defensible, which is
  a sign the model is wrong. With per-event status, the question does not arise.
- The single-process desktop dispatcher makes the duplicate window small and observable.

The cost is real and I want it recorded rather than glossed: **a non-idempotent handler will
double-apply on retry**, and the most dangerous consumer — accounting posting — is exactly
where that is unacceptable. Two mitigations:

1. The dispatcher passes a stable `DeliveryID` (the event's `id`), so a handler dedupes with
   one indexed lookup rather than inventing a scheme.
2. Accounting's posting handler will key journal entries by source event id, making it
   idempotent by construction — recorded here so Phase 2 does not have to rediscover it.

If a handler ever appears that genuinely cannot be idempotent, the deliveries table is the
designed upgrade path and this decision should be revisited.

### 5.7 `config.ChangeNotifier` gets its real implementation

Step 0.5 shipped `NoNotifier`. This step supplies the version that publishes `SettingChanged`
on the **domain bus** — in-process and immediate, not the outbox.

That choice is worth stating: a settings change is not a durable cross-module business fact
needing at-least-once delivery. It is in-process reactivity ("the language menu changed;
re-render"). Routing it through the outbox would add persistence and retry to something whose
value expires in milliseconds.

---

## 6. DESIGN — package layout & dependencies

```
internal/kernel/event/           # Event, Envelope, correlation/causation context carriers
internal/platform/eventbus/      # in-process typed bus (domain events)
internal/platform/outbox/
├── store.go                     # Publish (in-transaction), claim, mark done/failed/dead
├── dispatcher.go                # DispatchOnce, ordering, backoff, reclaim
└── subscriber.go                # integration subscriber registration
```

`eventbus` imports `kernel/{event,errs}`. `outbox` imports those plus `platform/database`.
Neither imports `api` or any module — enforced by the `platform-independent-of-modules` rule
added in 0.5.

---

## 7. DESIGN — the end-to-end proof

§BUS.2 requires Phase 0 to prove the whole path with a trivial event before any module
depends on it. Since no business module exists, the proof is a test-only event published
inside a Unit of Work, dispatched to a recording subscriber, asserting: the row exists after
commit, is absent after rollback, reaches the subscriber exactly once, and ends `done`.

Deliberately **not** doing what §BUS.2 sketches literally ("currency-created → a no-op
logging subscriber"): the currency module does not exist until 0.9, and inventing a fake
currency event now would leave a fixture pretending to be a business event.

---

## 8. TESTING PLAN (Protocol Step 4)

| Level | Tests |
|---|---|
| **Bus** | Typed delivery to the right handler; registration order preserved; duplicate `(type, name)` rejected; a handler error propagates; a handler **panic** becomes a typed error and still aborts; no subscribers is not an error. |
| **Atomicity — the drill** | Publish inside a Unit of Work that **commits** → exactly one row. Publish inside one that **rolls back** → **zero** rows. This is the guarantee the whole step exists for. |
| **Payload** | Round-trip through concrete types; a money field survives exactly (property test, as in 0.5); an unknown event type in the table is skipped, not fatal. |
| **Dispatcher — happy path** | Pending → subscriber invoked once → `done`, `processed_at` set, `attempts` = 1; a second pass dispatches nothing. |
| **Ordering** | Two events for one aggregate dispatch in `occurred_at` order; when the first fails, the second is **not** dispatched (head-of-line blocking); an unrelated aggregate is unaffected. |
| **Retry & backoff** | A failing handler sets `failed`, increments `attempts`, records `last_error`, and schedules `next_attempt_at` in the future; the event is not re-claimed before that time; delay grows and is capped. |
| **Dead letter** | After `MaxAttempts` the row is `dead`, is never re-claimed, and is not deleted. |
| **Crash recovery** | A row left `processing` past its visibility timeout is reclaimed to `pending` and delivered; one inside the timeout is left alone. |
| **Panic isolation** | A panicking integration handler fails that event only; sibling events in the same batch still dispatch. |
| **Concurrency** | Two dispatchers over one database deliver each event once (`-race`). |
| **Notifier** | A settings write reaches a domain-bus subscriber, closing the 0.5 `ChangeNotifier` port. |

**Mutation-verified**, as in 0.4 and 0.5, for the two load-bearing guarantees: *transactional
atomicity* (publish on its own connection instead of the context executor → the rollback test
must fail) and *per-aggregate ordering* (drop the head-of-line check → the ordering test must
fail).

---

## 9. DECISIONS REQUESTED

1. **D1 — Two mechanisms, not one.** A synchronous in-process bus for domain events and the
   durable outbox for integration events, per §23.1. *(§3, §4.)*
2. **D2 — 0.6 builds the dispatcher; 0.7 schedules it.** `DispatchOnce` is callable and fully
   tested here; the durable job wraps it next step. *(Recommended; §1.4.)*
3. **D3 — Per-event delivery with mandatory handler idempotency**, rather than an
   `outbox_deliveries` per-handler table. A stable `DeliveryID` is passed to make dedupe
   cheap; accounting will key journal entries by source event id. *(Recommended, with the
   cost stated; §5.6 — **this is the decision I'd most like you to push back on**.)*
4. **D4 — Per-aggregate ordering with head-of-line blocking.** A stuck event blocks its own
   aggregate and nothing else. *(§5.2.)*
5. **D5 — Visibility timeout via `next_attempt_at`**, reclaiming abandoned `processing` rows,
   so **no migration is needed** in this step. *(§5.3.)*
6. **D6 — Capped exponential backoff with jitter; `dead` after `MaxAttempts`; never
   auto-deleted.** *(§5.5.)*
7. **D7 — `SettingChanged` goes on the domain bus, not the outbox** — it is in-process
   reactivity, not a durable business fact. *(§5.7.)*
8. **D8 — Panics recovered but never swallowed**: converted to typed errors that still roll
   back a domain publish, and still fail an integration delivery. *(§3.3, §5.4.)*

On approval I'll implement in this order, each independently reviewable: `kernel/event` →
`eventbus` + panic isolation → `outbox.Publish` + the atomicity drill → claim/ordering →
dispatch + backoff/dead-letter → reclaim → the `ChangeNotifier` implementation → concurrency
and mutation drills, tests alongside each, then the self-review and improvement notes
(Protocol Steps 5–6).

---

## 10. AMENDED DECISION — D3 rejected: per-handler delivery tracking

**Approved outcome: Option A.** The recommendation in §5.6 was overruled, and on reflection
that is the right call for this project: the stated priority order is *Correctness >
Maintainability > Scalability > Readability > Simplicity > Performance*, and §5.6 traded
correctness for simplicity — the wrong direction here. "Handlers must be idempotent" is an
unenforceable convention, and the first real consumer is accounting posting, where one
violation is a wrong ledger that balances.

### 10.1 The schema — migration `0002_outbox_deliveries.sql`

```sql
outbox_deliveries (
  event_id, handler,                 -- composite PRIMARY KEY
  status ∈ (pending|processing|done|failed|dead),
  attempts, next_attempt_at, last_error, processed_at,
  created_at, updated_at
)
```

A composite `(event_id, handler)` primary key rather than the usual surrogate `id CHAR(36)`:
it is the natural key, and it makes a duplicate delivery row impossible by construction
rather than by convention. Platform bookkeeping tables already do this (`schema_migrations`
keys on `version`). A foreign key to `outbox_events(id)` keeps the two in step — foreign keys
are on per connection from Step 0.3.

**Currency's migration therefore becomes `0003` in Step 0.9.** Version numbers are globally
sequenced, so this is a renumbering, not a conflict.

### 10.2 Deliveries are materialised at publish time, not at claim time

Delivery rows are written **inside the same transaction as the event**, one per registered
handler for that event type.

The alternative — creating them lazily when the dispatcher first claims the event — was
rejected because it makes "what happens when v1.4 adds a subscriber?" ambiguous: a lazily
materialised handler would silently receive every event still in flight, including ones
published long before it existed. Materialising at publish makes the rule exact and
greppable:

> **A subscriber receives only events published after it was registered.**

Retroactive processing, when it is genuinely wanted, is then a deliberate backfill operation
rather than an accident of deployment timing.

Consequence worth stating: an event published while **no** handler is registered for its type
gets zero delivery rows and completes immediately. That is correct — nobody was listening —
but it is also exactly what a bootstrap ordering bug looks like, so it is logged at WARN.

### 10.3 D4 is refined: ordering is per `(aggregate, handler)`

Per-handler tracking makes the ordering guarantee finer-grained, which is a genuine
improvement over the approved design rather than merely a consequence:

- **Before (per-event):** if any handler failed on invoice A's first event, *every* handler
  was blocked on A's second event.
- **Now:** handler X being stuck on A blocks only X's view of A. Handler Y, which succeeded,
  proceeds. Each handler still sees a given aggregate's events in `occurred_at` order, which
  is the property that actually matters.

**A `dead` delivery blocks its `(aggregate, handler)` stream.** If accounting dead-letters
"invoice posted", delivering the subsequent "invoice voided" to accounting would post a
reversal for an entry that was never made. The stream halts until a human resolves it, which
is what `dead` is for.

### 10.4 What Option A costs

Honest accounting of the trade, since §5.6 argued the other way:

- One migration and one table (~40 lines of SQL).
- `Publish` writes 1 + N rows instead of 1. On a single-user desktop database with a handful
  of subscribers per event, this is not measurable.
- The dispatcher operates on deliveries joined to events, rather than on events alone — more
  SQL, but the retry semantics get *simpler*, because "re-run only what failed" needs no
  reasoning about which handlers already ran.

What it buys: a non-idempotent handler can no longer double-apply on retry. Handlers should
still be idempotent — belt and braces — but correctness no longer *depends* on it.

---

## 11. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 11.1 A real concurrency bug, found by the test that was written to find it

The first implementation claimed work in two steps: a `SELECT` of due deliveries, then an
`UPDATE … SET status='processing'` inside `deliver`. Between those two statements a second
dispatcher could select the same rows.

`TestConcurrentDispatchersDeliverEachDeliveryOnce` caught it immediately: three dispatchers
over ten events produced **30 handler invocations instead of 10**. The `done` count still read
10, so every row-level assertion passed — only counting the actual handler calls exposed it.

The fix is a **compare-and-swap claim**: the `UPDATE` carries `AND status IN
('pending','failed')`, and only the dispatcher whose statement affects exactly one row
proceeds. SQLite's single-writer pool serialises the statements, so exactly one wins. A lost
race is reported as `Report.Contended` rather than being silently swallowed.

This is worth recording because it is precisely the bug the outbox exists to prevent, and it
survived a design review, a careful implementation, and every per-row assertion.

### 11.2 A brittle Step 0.4 test, corrected

`TestUpAppliesRealPlatformSchema` asserted `Applied == 1 && ToVersion == 1` — it hardcoded
"the product ships exactly one migration". Adding `0002` broke it, and it would have broken
again on every future migration.

It now derives the expectation from the embedded FS (`Load(migrations.SQLite())`) and asserts
the shipped count and highest version, plus the presence of `outbox_deliveries`. A test that
must be edited every time the system legitimately grows is a maintenance tax, not a safety net.

### 11.3 `SettingChangedEvent` lives in `config`, not `eventbus`

The notifier needed to publish, but `platform/config` must not depend on the bus
implementation merely to announce a change — and `eventbus` must not learn about
configuration types. So `config` declares a minimal `Publisher` interface
(`Publish(ctx, event.Event) error`) and depends only on `kernel/event`. `NewBusNotifier`
satisfies the 0.5 `ChangeNotifier` port against it.

`SettingChanged` deliberately returns nothing: a UI component failing to refresh must not roll
back a valid settings change. The failure is logged and the stored value stands — asserted by
`TestSubscriberFailureDoesNotUndoTheWrite`.

### 11.4 Deliveries for unregistered handlers are skipped, not retried

A renamed or removed subscriber leaves orphan delivery rows whose handler no longer exists.
Retrying them to exhaustion would be pure noise; leaving them outstanding would block their
aggregate stream **forever**, because the head-of-line rule counts any non-`done` delivery.
They are therefore skipped with a warning and reported as `Report.Skipped`.

### 11.5 Verification

**54 tests across three packages, `-race` clean, `make ci` green** (build · archlint · vet ·
tests · golangci-lint · frontend).

| Package | Tests | Coverage |
|---|---|---|
| `platform/eventbus` | 9 | 92.7% |
| `platform/outbox` | 18 | 86.7% |
| `platform/config` (incl. notifier) | 27 | 79.4% |

The outbox tests run against the **real schema** — `0001` and `0002` applied through the Step
0.4 migration runner — so the composite primary key, the foreign key, and the status CHECK are
genuinely exercised.

**Mutation-verified**, as promised in §8:

- *Atomicity*: making `database.Do` commit instead of roll back on error → the rollback drill
  fails with `events = 1 after rollback, want 0 — the event escaped its transaction`.
- *Ordering*: removing the head-of-line `NOT EXISTS` → the ordering test fails with
  `handler saw [voided]` (the void delivered before the post), and the dead-letter blocking
  test fails alongside it.

### 11.6 Carried forward

- **Step 0.7** wraps `DispatchOnce` in a durable job with a schedule and heartbeat. Until
  then nothing calls it outside tests.
- **Currency's migration is now `0003`** in Step 0.9, since `0002` is taken.
- The **dead-letter UI** (listing `dead` deliveries with their `last_error` so a human can
  intervene) belongs to Phase 9 diagnostics. Everything it needs is already recorded.
- A dead delivery halts its `(aggregate, handler)` stream by design. Once a resolution
  workflow exists, "retry this dead delivery" is the operation that unblocks it.

---

*End of Step 0.6. Design approved (D3 amended to Option A), implemented, self-reviewed.*
