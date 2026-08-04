# Step 1.3 — Sessions, Lockout & the Real AppContext (Design + implementation record)

> Status: **DESIGN — implemented in the same turn under the reviewer's standing approval.**
> **D1 revises a value I proposed to you last turn** (idle timeout 30 → 60 minutes) and **D2
> is a product-shaped security decision** I want read carefully. Both are argued below rather
> than applied quietly.
> Scope: migration `0006`, `sessions` and `login_attempts`, the login/logout flow, throttling,
> the session-sweep job, and `appctx` finally reading a real session.
> **Out of scope:** RBAC and permission checks (1.4/1.5); the login screen and the
> must-change-password redirect (1.10); PIN unlock (Phase 5).

Step 1.2 answered "is this a real person, and who?". This step answers "and are they *still*
that person, five minutes later?" — plus the question 0.10 and 0.11 both deferred: **who is
acting, so settings can resolve at user scope.**

---

## 1. ANALYSIS

### 1.1 A desktop ERP has no password-reset email

This single fact should shape the lockout design, and it is easy to miss when copying a
server-side playbook.

There is no email service, no SMS, no help desk, and no second administrator on a fresh
install. If the sole administrator's account is **permanently locked**, the business is dead:
the only recovery is editing the database by hand, which no shop owner can do.

An attacker does not need to guess a password to cause that. They need only to fail five times
against a known username — and the username is often on the shop's own invoices.

So a hard lock is not a security feature here; it is a **denial-of-service vector aimed at the
customer**. §13.1 already says "exponential backoff", and this design commits to that reading
explicitly (**D2**).

### 1.2 Writing on every request

A session must be checked on every call. The obvious implementation updates `last_seen_at`
each time — which on SQLite's **single writer** (0.3) means every read-only binding call queues
behind a write.

That is a real cost on the hot path, and the fix is cheap: only persist `last_seen_at` when it
is already stale by more than a coarse granularity. Idle timeout is measured in tens of
minutes; recording it to the minute loses nothing (§4.4).

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Permanent lockout of the sole admin | The business cannot open, with no in-app recovery | **Throttle, never lock** (§4.5, D2) |
| Session id guessable | Session hijacking | 256 bits from `crypto/rand`, and the id is **not** a UUIDv7 (§3.2) |
| Session token stored in plaintext | A database read is a login | The token is **hashed** at rest (§3.2, D3) |
| A write per request | The single writer becomes the bottleneck | `last_seen_at` persisted at minute granularity (§4.4) |
| Idle timeout fires mid-queue | Users disable it, or pick shorter passwords | 60 minutes, and Phase 5 gets PIN unlock rather than a shorter timeout (§4.1, D1) |
| Revoked session keeps working | An administrator's "sign this device out" is a lie | Every check reads the row; there is no in-memory cache (§4.4) |
| Attempts against unknown users discarded | The shape of an attack is invisible | `login_attempts.user_id` is nullable and unknown usernames are recorded (§3.3) |
| Sessions accumulate forever | An unbounded table on a 10-year install | A durable sweep job (§5) |

---

## 2. DESIGN — settings

| Setting | Default | Note |
|---|---|---|
| `identity.session.idle_minutes` | **60** | See D1 |
| `identity.session.absolute_hours` | **12** | Covers a full shift; forces re-auth daily |
| `identity.session.remember_days` | **30** | Absolute expiry when "stay signed in" is chosen |
| `identity.lockout.threshold` | **5** | Failures before throttling begins |
| `identity.lockout.base_seconds` | **30** | First delay |
| `identity.lockout.max_seconds` | **900** | Cap — 15 minutes |

### 2.1 D1 — idle timeout is 60 minutes, not the 30 I proposed

Last turn I proposed 30 minutes. On reflection that is the wrong number for **this** product,
and shipping it because I said it would be worse than changing it.

A shop is quiet for stretches. A 30-minute idle timeout means the owner re-types a twelve-
character passphrase after every lull — and the predictable outcome is a shorter password, a
written-down password, or the timeout switched off entirely. That is the same failure mode the
1.2 password policy was designed to avoid (D3 there): **friction that drives users toward
weaker security is not security.**

60 minutes with a 12-hour absolute cap keeps the protection that matters — an unattended
machine overnight, a stolen laptop — without the daily friction.

The genuinely right answer for a till is not a shorter timeout at all: it is a **screen lock
resumed with a PIN**, which is exactly what §13.1's PIN credential is for and what Phase 5
will build. Recorded here so Phase 5 does not reach for a shorter timeout instead.

Both values are settings. An installation that must have 30 minutes types 30.

---

## 3. DESIGN — schema `0006_sessions.sql`

### 3.1 `sessions`

```sql
sessions (
  id              CHAR(36) PK,
  user_id         CHAR(36) NOT NULL REFERENCES users(id),
  branch_id       CHAR(36) NOT NULL REFERENCES branches(id),
  token_hash      CHAR(64) NOT NULL,       -- SHA-256 of the token; never the token
  created_at      CHAR(24) NOT NULL,
  last_seen_at    CHAR(24) NOT NULL,
  idle_expires_at CHAR(24) NOT NULL,       -- rolls forward on use
  absolute_expires_at CHAR(24) NOT NULL,   -- fixed at creation
  ended_at        CHAR(24),
  end_reason      VARCHAR(12),             -- logout | idle | absolute | revoked
  device_info     VARCHAR(200),
  CONSTRAINT ux_sessions_token UNIQUE (token_hash)
);
```

The session carries a **branch** (§26.1: "the AppContext always carries a current branch"), so
a future branch switch is a session update rather than a new concept.

### 3.2 The token is hashed at rest — D3

The row id is not the credential. A separate **256-bit token** from `crypto/rand` is returned
to the caller, and only its **SHA-256** is stored.

Why hash it at all, when the database is on the user's own machine? Because a database that
travels — a backup, a support copy, the `.mizanbak` export of Phase 9 — would otherwise contain
live credentials. A backup should never be a set of working logins.

Why SHA-256 and not Argon2id, when 1.2 insisted on a memory-hard KDF for passwords? Because the
threat is different: a session token is 256 bits of uniform randomness, so there is no
dictionary to attack and nothing to slow down. Argon2 protects **low-entropy** secrets. Using
it here would cost 19 MiB on every request to defend against an attack that cannot work.

A UUIDv7 would be the wrong choice for the token for the same reason it is the right choice for
a primary key: it is **time-ordered and partly predictable**, which is exactly what a bearer
credential must not be.

### 3.3 `login_attempts`

```sql
login_attempts (
  id, company_id CHAR(36) NOT NULL,
  username    VARCHAR(60) NOT NULL,     -- as normalised, even if no such user
  user_id     CHAR(36),                 -- NULL when the username does not exist
  succeeded   SMALLINT NOT NULL,
  reason      VARCHAR(24),              -- unknown_user | bad_password | inactive | throttled
  device_info VARCHAR(200),
  attempted_at CHAR(24) NOT NULL
);
```

`user_id` is nullable **and unknown usernames are recorded** (§IDN.3): an attempt against a
username that does not exist is the *shape of an attack*, and discarding it discards the
evidence. `reason` is the one place the distinction 1.2 refuses to tell the caller is written
down — the audit trail knows, the attacker does not.

---

## 4. DESIGN — the session service

### 4.1 Login

```go
func (s *Service) Login(ctx, LoginInput) (Session, error)
```

1. Check the throttle for `(company, username)` — refuse early if locked out (§4.5).
2. `Authenticate` (1.2), which is constant-work and returns one error for every failure.
3. Record the attempt, success or failure, with its real `reason`.
4. On success: mint a token, insert the session, return it.

The throttle check comes **first**, before the Argon2 work, which is what makes throttling
actually cheap under attack — the point is to stop doing expensive work for someone who is
guessing.

### 4.2 Validate

```go
func (s *Service) Validate(ctx, token string) (contract.Principal, Session, error)
```

Looks the session up by token hash, rejects it if ended, idle-expired, or absolutely expired
(marking the row with the reason), then rolls the idle expiry forward.

**No in-memory session cache.** An administrator's "sign this device out" must take effect on
the next call, and a cache would make revocation a lie for as long as the entry lives. The
lookup is one indexed read.

### 4.3 Logout and revoke

`Logout` ends the caller's own session; `Revoke` ends someone else's (an administrator action,
permission-gated in 1.5). Both set `ended_at` and `end_reason` — sessions are **never deleted**
on logout, because "who was signed in when this happened" is an audit question.

### 4.4 The write-per-request problem

`last_seen_at` and `idle_expires_at` are persisted only when `last_seen_at` is more than
**60 seconds** stale. Idle timeout is measured in tens of minutes, so minute-granularity costs
nothing, and it turns a write on every binding call into a write at most once a minute per
session — which matters because SQLite has exactly one writer (0.3).

The expiry *check* still happens on every call, in memory, against the stored value. Only the
**persistence** is coarse.

### 4.5 Throttling, not locking — D2

After `threshold` consecutive failures for a `(company, username)`:

```
delay = min(base × 2^(failures − threshold), max)
```

The account is refused until `lastFailure + delay`. A **successful** login clears the counter.

There is **no state that a human must clear**, and no permanent lock — see §1.1. The worst an
attacker achieves by hammering a username is 15 minutes of delay, which they also inflict on
themselves; what they cannot do is take the shop offline until someone edits a database.

The refusal returns a **distinct** code (`identity.too_many_attempts`) with a `retryAfter`
parameter, deliberately breaking 1.2's one-error rule. It reveals nothing: the attacker already
knows they have been trying this username, because they were the one trying. The legitimate
user who fat-fingered their password five times genuinely needs to be told to wait rather than
being left to conclude the system is broken.

---

## 5. DESIGN — the session sweep job

`identity.session_sweep`, `@every 1h`, `CatchUp: Skip`, singleton.

Marks expired sessions ended and deletes ended rows older than 90 days. The expiry *check* does
not depend on it — a session is validated against its own timestamps — so a missed sweep is
housekeeping, not correctness, which is exactly what `Skip` means (0.7 §5.2).

This is the **first module-declared job**; the two before it were the platform's own.

---

## 6. DESIGN — `appctx` reads a real session

This closes the item 0.10 §6.2 and 0.11 D4 both carried:

```go
func WithPrincipal(ctx, contract.Principal, branchID id.ID) context.Context
func PrincipalFrom(ctx) (contract.Principal, bool)
```

`appctx.Scopes` — which has returned "no company, no branch, no user" since Phase 0 — now
reports them from the session. Two consequences arrive for free, because the machinery was
built to expect them:

- **Settings resolve at user scope.** 0.11 D4 wrote `ui.locale` and `ui.theme` at *system*
  scope because no user existed. The scope chain from 0.5 now resolves per-user with no
  frontend change.
- **The locale a session sees is its user's**, so two people sharing a machine get their own
  language.

`appctx` keeps a nil-safe fallback: with no session, scopes are empty and settings resolve at
system scope, exactly as today. That is the state the login screen and the setup wizard run in.

---

## 7. TESTING PLAN

| Level | Tests |
|---|---|
| **Token** | Two sessions never share a token; the stored value is a hash, and the raw token appears **nowhere** in the row; a token that does not exist is rejected. |
| **Expiry** | A session past its idle window is rejected and marked `idle`; one past its absolute window is rejected as `absolute` **even if recently used**; a live session is accepted. |
| **Roll-forward** | Use extends the idle window but never the absolute one. |
| **Write coalescing** | Repeated validation within the granularity performs **no** write (asserted by row state, not by timing). |
| **Revocation** | A revoked session stops working on the very next call — no cache. |
| **Throttle** | Failures below the threshold do not throttle; at the threshold the next attempt is refused with `retryAfter`; the delay grows and is **capped**; a success **clears** the counter; the throttle is per username, so one account's failures do not lock another. |
| **No permanent lock** | After many failures, waiting out the capped delay lets a correct password in. **This is the D2 guarantee.** |
| **Attempts** | Success and failure are both recorded; an unknown username produces a row with a NULL `user_id`; `reason` distinguishes cases the caller cannot. |
| **appctx** | Scopes report the session's company/branch/user; with no session they are empty; a user-scoped setting resolves per user. |
| **Integration** | Against the real merged schema (0001–0006). |

**Mutation-verified**: *absolute expiry* (roll the absolute window forward on use → the
"absolute expiry survives activity" test must fail) and *throttle clearing* (skip the reset on
success → the counter must wrongly persist).

---

## 8. DECISIONS TAKEN

1. **D1 — Idle 60 minutes** (revising the 30 I proposed), absolute 12 hours, remember-me 30
   days. Friction that drives users toward weaker security is not security; a till wants PIN
   unlock (Phase 5), not a shorter timeout. *(§2.1.)*
2. **D2 — Throttle, never permanently lock.** A desktop ERP has no password-reset email, so a
   permanent lock on the sole administrator is an attacker-triggered outage with no in-app
   recovery. *(§1.1, §4.5.)*
3. **D3 — Session tokens are 256-bit random, stored as SHA-256**, not as the row id and not
   Argon2-hashed. A backup must not be a set of working logins; a high-entropy token needs no
   memory-hard KDF. *(§3.2.)*
4. **D4 — `last_seen_at` persisted at 60-second granularity**, so session upkeep does not
   serialise behind SQLite's single writer. *(§4.4.)*
5. **D5 — Sessions are ended, never deleted, on logout**; the sweep prunes only long-dead rows.
   *(§4.3, §5.)*
6. **D6 — Throttling returns a distinct code with `retryAfter`**, deliberately breaking 1.2's
   one-error rule, because it reveals nothing the attacker does not already know and the
   legitimate user needs it. *(§4.5.)*

---

## 9. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 9.1 The `Module.Jobs()` path had never worked

Identity's session sweep is the **first job any module has declared**, and declaring it exposed
a latent defect in the Module contract dating from 0.7/0.10:

- `Module.Jobs()` returned `[]jobs.Def` — a declaration with no handler.
- The composition root registered them as `reg.Register(def, nil)`.
- `Registry.Register` **rejects a nil handler**, correctly: *"a job needs a handler"*.

So any module that declared a job would have failed at startup. It went unnoticed because the
only two jobs in existence are the platform's own, registered through helpers that pair a
handler at the call site, and no module had declared one in three phases.

Fixed with a `jobs.Registration{Def, Handler}` pair: `Def` stays a pure declaration (key,
schedule, timeouts) and the handler is behaviour, which keeps the 0.7 split while making a
module's job actually runnable. Currency and org return nil and were unaffected.

Worth recording as a pattern: **an unused seam is an untested seam.** This is the third time
Phase 0 machinery has turned out to be subtly wrong at its first real use (0.11 §1.2's envelope
drift, 0.12's lint rules, this) — and each time the cost of finding out was small precisely
because the first consumer arrived while the code was still fresh.

### 9.2 D1 revisited a value I had proposed

Last turn I proposed a 30-minute idle timeout. Implementing it, the reasoning did not survive
contact with the product: a shop is quiet for stretches, and re-typing a twelve-character
passphrase after every lull drives users to shorter passwords, sticky notes, or switching the
timeout off — the same failure the 1.2 password policy was designed to avoid.

Changed to 60 minutes with the 12-hour absolute cap unchanged, and recorded rather than applied
quietly. The right answer for a till is a PIN-resumed screen lock (Phase 5), not a shorter
timeout, which is noted so Phase 5 does not reach for the wrong lever.

### 9.3 The two tests that carry the step

**`TestAbsoluteExpirySurvivesActivity`** validates a session every 30 minutes for 13 hours —
always inside the idle window, well past the absolute cap. Removing the absolute check:

```
a session survived past its absolute expiry because activity kept extending it
```

That is the difference between a session that ends and one that lives forever, and it is
invisible to any test that does not deliberately keep the session warm.

**`TestTheThrottleAlwaysElapses`** is the D2 guarantee. Twenty-five failures, then wait out the
cap, then a correct password must work. Without the cap, an attacker takes the shop offline
permanently with five wrong guesses against a username printed on its own invoices — and there
is no reset email to recover with.

### 9.4 `ExpiryReason` checks absolute before idle

Deliberate ordering: a session used constantly for thirteen hours is over its absolute limit,
and checking idle first would let activity mask that. The two checks are not commutative and
the code says so.

### 9.5 Verification

**`make ci` green.** 21 new Go tests. Four new error codes, translated in both locales.

**Mutation-verified**, as promised in §7:

- *Absolute expiry* — removing the check fails `TestAbsoluteExpirySurvivesActivity`.
- *Throttle clearing* — making a success not end the failure streak fails
  `TestASuccessClearsTheCounter` with "throttled after 2 failures following a success".

### 9.6 Carried forward

- **Nothing calls `Validate` yet.** The binding decorator that resolves a session and stamps
  `appctx.WithActor` is Step 1.5; the login screen is 1.10. Sessions are fully built and
  exercised by tests, and no production code path issues one.
- **`must_change` is still unenforced** — carried from 1.2, and belongs to the 1.10 login flow.
- **Settings now resolve at user scope** wherever an actor is stamped, which closes 0.10 §6.2
  and 0.11 D4 — but until 1.5 stamps one, resolution is unchanged in practice.
- **Sessions are not yet audited.** Login, logout, revocation, and the throttle are exactly the
  events §15.3 requires; they land with the audit module in 1.7, which is also where the
  `reason` column stops being write-only.

---

*End of Step 1.3. Implemented and self-reviewed.*
