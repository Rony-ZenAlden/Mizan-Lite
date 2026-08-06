# Step 1.6 — Field-Level Redaction (Design + implementation record)

> Status: **DESIGN — implemented in the same turn under the reviewer's standing approval.**
> **D1 inverts the mechanism** from "populate then remove" to "populate only if permitted",
> which changes what forgetting a step costs. That is the decision to read hardest.
> Scope: `internal/api/redact`, `internal/modules/audit` (schema `0008`, read path, the two
> permissions), and the `Audit` binding.
> **Out of scope:** writing audit entries — the `Auditable` event and its in-transaction
> subscribers are 1.7 (phase D7).

### A note on sequence

The phase §SEQ puts the audit module wholly in 1.7. This step brings its **schema and read
path** forward, because §14.2's redaction has no other consumer in Phase 1: gating a field
needs a response with a field worth gating. Building the mechanism against nothing would be
the speculative machinery 0.9 D1 and 1.1 D5 both refused.

1.7 keeps what it is actually about — recording entries atomically with the change that caused
them.

---

## 1. ANALYSIS

### 1.1 What §14.2 asks for

> "Can see cost price" and "can see profit margin" are permissions, not screens. […] enforced
> at the DTO-mapping layer so restricted fields are **never serialized to the frontend at all**.

Two requirements, and the second is the interesting one. Hiding a field in the UI is not
security — the value already crossed the boundary and sits in the webview's memory, in any IPC
log, and in a devtools inspector.

### 1.2 Absent, not blanked — and the reason that actually matters

The obvious implementation blanks the field: build the DTO, then overwrite the restricted value
with `""` before returning. It is secure — the value never leaves the process — and it is still
the wrong design, for two reasons:

**The contract becomes ambiguous.** A blank `beforeJson` is indistinguishable from an audit
entry that genuinely had no "before" state. The UI shows a dash either way, and the user cannot
tell "there is nothing here" from "there is something here you may not see" — which are very
different facts, and the second one is the one that matters to a person wondering whether they
need to ask someone.

**Forgetting leaks.** With a plain `string` field, the safe state depends on remembering to
overwrite it. Every new code path that builds the DTO — a second query, an export, a report —
is another place to forget, and forgetting is silent.

### 1.3 The inversion — D1

So the mechanism is not "populate then remove". It is **populate only if permitted**:

```go
dto.BeforeJSON = redact.Visible(ctx, authz, audit.PermViewPayload, entry.BeforeJSON)
```

The field is a **pointer, nil by default**, with `json:",omitempty"` — so an unset field is
genuinely absent from the JSON object, not present-and-empty.

The property this buys is worth stating plainly:

> **Forgetting the redaction call hides data rather than leaking it.**

A new code path that neglects to call `Visible` produces a DTO with the field nil, which
serialises as absent. The failure mode of carelessness is a missing field someone reports as a
bug — not a leaked one nobody notices.

That is the inverse of the blanking design, where forgetting is a silent disclosure.

### 1.4 Risks

| Risk | Consequence | Answer |
|---|---|---|
| A restricted field ships as a plain type | Blanking is required and forgettable | Pointer + `omitempty`; a test asserts the JSON key is absent (§5) |
| Redaction applied in one query but not another | Partial disclosure | Population is the only path that sets the field (§1.3) |
| The permission is never granted to anyone | The field is unreachable, not secure — a silent outage | The 1.5 coverage check already rejects undeclared permissions; audit declares both (§3) |
| Absent field breaks the frontend | A crash instead of a graceful "hidden" | `beforeJson?: string` in TypeScript — optional, and the UI renders a distinct hidden state |

---

## 2. DESIGN — `internal/api/redact`

Deliberately tiny:

```go
// Visible returns a pointer to value when the actor holds permission, or nil when they do not.
func Visible[T any](ctx context.Context, authz auth.Authorizer, permission string, value T) *T

// VisibleIn is the same at a non-global scope.
func VisibleIn[T any](ctx, authz, permission string, scope auth.Scope, value T) *T
```

No struct tags, no reflection, no registry. A registry would let a field be declared restricted
and still populated by a path that does not consult the registry — which is the failure this
design exists to remove. Here, the *only* way to set the field is through the function that
checks.

`authz` is `auth.Authorizer` — the platform port from 1.4 — so `redact` depends on the
interface, never on the identity module.

---

## 3. DESIGN — the audit module (read path)

### 3.1 Schema `0008_audit.sql`

Exactly §15.1, including the chain columns decision 8 reserves:

```sql
audit_log (
  id, occurred_at, actor_user_id, actor_name_snapshot,
  branch_id, session_id, correlation_id,
  action, entity_type, entity_id, entity_label_snapshot,
  before_json, after_json, changed_fields,
  source, device_info,
  prev_hash CHAR(64) NULL, row_hash CHAR(64) NULL,   -- reserved, NULL in v1 (§15.2)
  created_at
);
```

**Label snapshots** (`actor_name_snapshot`, `entity_label_snapshot`) so a five-year-old entry
stays readable after a user is renamed or deactivated — the reason §15.1 gives, and the reason
1.1 D4 refused a foreign key on `created_by`.

**No UPDATE or DELETE path exists in the repository.** Append-only is enforced by there being
no code to do otherwise, not by a comment.

### 3.2 The two permissions

| Permission | Grants |
|---|---|
| `audit.entry.view` | the entry list: who, what, when, which entity |
| `audit.entry.view_payload` | additionally `before_json` / `after_json` / `changed_fields` |

The split is real, not a fixture. Audit payloads contain exactly the detail a junior user should
not read — an old salary, an old price, an old credit limit — while the *fact* that a change
happened is ordinary operational information a manager needs.

Seeded roles: Administrator has `*`; **Manager gets `view` but not `view_payload`**, which makes
the distinction live in the default configuration rather than only in a test.

---

## 4. DESIGN — the `Audit` binding

```go
Audit.Entries(filter AuditFilterDTO) → Result[[]AuditEntryDTO]   // audit.entry.view
```

The method-level policy (1.5) gates the *list*; `Visible` gates the payload within each row. The
two mechanisms compose without knowing about each other: the guard decides whether you see
anything, redaction decides how much of it.

```go
type AuditEntryDTO struct {
    ...
    BeforeJSON    *string `json:"beforeJson,omitempty"`
    AfterJSON     *string `json:"afterJson,omitempty"`
    ChangedFields *string `json:"changedFields,omitempty"`
}
```

---

## 5. TESTING PLAN

| Level | Tests |
|---|---|
| **Absence** | Without `view_payload`, the marshalled JSON has **no** `beforeJson` key — asserted on the bytes, not on struct fields, because a struct assertion passes for a blanked field too. |
| **Presence** | With the permission, the payload is present and correct. |
| **List gating** | Without `audit.entry.view`, the method is refused by the 1.5 guard before redaction is reached. |
| **Fail-closed** | A DTO built without calling `Visible` serialises with the field absent — the D1 property, asserted directly. |
| **Redact unit** | `Visible` returns nil with no actor, nil without the permission, and a pointer to the value with it. |
| **Append-only** | The repository exposes no update or delete. |
| **Frontend** | The TypeScript type marks the payload optional, and the panel renders a distinct hidden state rather than an empty one. |

**Mutation-verified**: *redaction* (make `Visible` always return the value → the absence test
must fail) and *fail-closed* (change the DTO field to a non-pointer `string` → the absence test
must fail, because a blank key is still present).

---

## 6. DECISIONS TAKEN

1. **D1 — Populate only if permitted**, rather than populate-then-blank. Pointer fields with
   `omitempty`, so forgetting the check **hides** data instead of leaking it. *(§1.3.)*
2. **D2 — No registry and no struct tags.** The only way to set a restricted field is the
   function that checks; a registry would allow a path that bypasses it. *(§2.)*
3. **D3 — The audit schema and read path land here**, because redaction needs a real consumer.
   Writing entries stays in 1.7. *(Sequence note.)*
4. **D4 — Manager is seeded with `view` but not `view_payload`**, so the distinction is live in
   the default configuration rather than only in a test. *(§3.2.)*

---

## 7. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 7.1 The inversion is what the mutation drills actually measure

Two drills, and the second is the one that justifies D1.

**Redaction removed** — `Visible` always returns the value:

```
a restricted value crossed the boundary ("50000"):
  … "beforeJson":"{\"salary\":50000}","afterJson":"{\"salary\":75000}" …
```

**`omitempty` dropped** — the pointer stays, so this isolates exactly the absent-vs-blank
decision. A nil pointer then serialises as `null`, and both tests fail:

```
the "beforeJson" key is present for a caller without the permission —
it must be ABSENT, not blank: … "beforeJson":null,"afterJson":null …

a DTO built without any redaction call still emits "beforeJson" —
the safe state is not the default, so forgetting the call would LEAK
```

The second message is the point of the whole step. With `omitempty` on a pointer, a code path
that forgets the redaction call produces a **missing field** — a bug someone reports. Without
it, the same forgetfulness produces a **present field**, and if the value had been populated
first it would be a silent disclosure.

The first attempt at that mutation replaced the pointers with plain strings and did not
compile, because other tests dereference them. A mutation that does not build proves nothing;
reshaping it to change only `omitempty` made it prove more, not less.

### 7.2 The two gates compose without knowing about each other

`Audit.Entries` carries a method policy (`audit.entry.view`, from 1.5) *and* field redaction
(`audit.entry.view_payload`, this step). Neither substitutes for the other:

- A method-level permission cannot express *"you may see that this changed, but not what it
  changed from"*.
- Field redaction cannot keep an unauthorised caller out of the list at all.

`TestTheListItselfIsGated` and `TestRestrictedPayloadKeysAreAbsentNotBlank` assert the two
independently, so a regression in one cannot be masked by the other.

### 7.3 Manager holds `view` but not `view_payload`

Seeded that way (D4) so the distinction is live in the **default configuration**, not only in a
test fixture. A manager needs to see that a price changed; the old price is a different
question. If the only place the two permissions differed were a test, the split would be
theatre.

### 7.4 A count-based test broke for the second time

`TestAllReturnsEveryBinding` asserted `len(All()) == 6`. `Auth` broke it in 1.5, `Audit` broke
it here. Rewritten to assert by **presence and non-nilness** — the same fix 0.6 §11.2 applied
to the migration count and 1.1 applied to the module set.

Three occurrences now. The pattern is worth stating once: **a test that counts a growing
collection is a maintenance tax, not a safety net**, and the assertion that carries the meaning
is "the thing I need is there".

### 7.5 Append-only is enforced by absence

The audit repository has no `Update` and no `Delete`. §15.1 requires the trail to be append-only,
and the enforcement is that there is no method to call — stronger than a comment and visible in
one glance. `TestAuditReadPathHasNoWriteOrDelete` asserts the surface stays that way as the
module grows in 1.7.

### 7.6 Verification

**`make ci` green.** 8 new Go tests plus 4 in `redact`, and 2 new frontend tests (123 total).
One new error code, translated in both locales.

The TypeScript type marks the payload **optional**, and a `payloadHidden` helper puts the
"withheld vs empty" distinction in one place rather than an inline `=== undefined` at every
call site.

**Mutation-verified**: redaction removed, and `omitempty` dropped (§7.1).

### 7.7 Carried forward

- **Nothing writes audit entries yet** — that is 1.7, on the synchronous domain bus so an entry
  commits in the same transaction as the change (phase D7). The tests here seed rows directly,
  which is honest: this step is about whether the read path redacts.
- **`redact` has one consumer.** It will get more as cost price and profit margin arrive in
  Phase 3, which are §14.2's own examples.
- **The audit viewer screen** is 1.11; it must render the hidden state distinctly from an empty
  one, which `payloadHidden` exists to make easy.
- **Retention** (§15.1: an administrative operation that itself writes an audit record) is
  Phase 9.

---

*End of Step 1.6. Implemented and self-reviewed.*
