# Step 1.2 — Identity: Credentials & Users (Design + implementation record)

> Status: **DESIGN — implemented in the same turn under the reviewer's standing approval.**
> Decisions are therefore flagged rather than silently taken; **D2 and D3 deviate from the
> phase design** and are the two to review hardest.
> Scope: `internal/platform/crypto` (Argon2id), `internal/modules/identity` — schema `0005`,
> users, credentials, password history, the password policy, and the `Authenticator` port.
> **Out of scope:** sessions, lockout, and `login_attempts` (1.3); RBAC (1.4); the login screen
> (1.10). **PIN credentials** are reserved in the schema and unimplemented (phase D2, Phase 5).

This is the first step whose defects are *security* defects. The bar is different: a wrong
answer here is not a bug report, it is a breach, and several of the mistakes are invisible in
testing because the system behaves correctly right up until someone attacks it.

---

## 1. ANALYSIS

### 1.1 The dependency question, settled — D1

The phase design (§IDN.1, D1) required confirming `golang.org/x/crypto/argon2` before this step
began. Confirmed, and better than expected:

- `golang.org/x/crypto v0.54.0` is **already in `go.mod` as an indirect dependency** and pinned
  in `go.sum`. Promoting it to direct **adds no dependency** — precisely the `google/uuid`
  situation in 0.5.
- It is present in the module cache at that exact version, and `GOPROXY=off go list
  golang.org/x/crypto/argon2` resolves. **The build stays fully offline.**

Hand-writing Argon2id was never on the table.

### 1.2 What actually needs to be right

| Property | Why it is not optional |
|---|---|
| A per-password random salt | Without it, two users with the same password share a hash, and one rainbow table breaks both |
| Memory-hard KDF | A fast hash (SHA-256, bcrypt at low cost) is GPU-crackable at scale |
| Parameters stored **per hash** | §13.1: raising the cost later must not invalidate existing passwords |
| Constant-work verification | Otherwise response time tells an attacker which usernames exist |
| Constant-time comparison | A byte-by-byte `==` on the digest leaks it one byte at a time |
| Hashes never in a DTO | The single worst leak in the system |

### 1.3 Risks and how the design answers each

| Risk | Consequence if unhandled | Design answer |
|---|---|---|
| Password hash reaches the frontend | Catastrophic, and silent | Credentials are a **separate table**, never joined into a user read (§3.1) |
| Timing reveals whether a username exists | A username oracle, which turns a password spray into a targeted attack | A dummy verification runs for unknown users (§4.4) |
| Parameters frozen at install | The cost factor is stuck at 2026 hardware for a decade | PHC-encoded params + **rehash on login** (§4.5) |
| Two copies of the parameters | They drift, and the wrong one is trusted | One PHC string, `params_json` dropped (**D2**) |
| Forced rotation | Users increment a digit; NIST advises against it | Policy defaults follow NIST (**D3**) |
| The last user is deactivated | Nobody can ever log in again; unrecoverable without a database editor | Domain invariant (§4.6) |
| A weak default that ships | Everyone inherits it | No default password exists; setup requires one (Phase §WIZ.1) |

---

## 2. DESIGN — `platform/crypto`

§4 places crypto in `platform/`. The surface is deliberately tiny:

```go
type Hasher interface {
    Hash(password string) (string, error)     // → PHC-encoded string
    Verify(encoded, password string) (ok bool, needsRehash bool, err error)
}

func NewArgon2id(p Params) Hasher
func DefaultParams() Params
```

`Verify` returns `needsRehash` alongside the verdict rather than exposing the parameters,
because the only legitimate reason to ask about them is to decide whether to upgrade — and
that decision is the same everywhere.

### 2.1 Parameters

```
argon2id, m = 19456 KiB (19 MiB), t = 2, p = 1, salt 16 B, key 32 B
```

From OWASP's current recommended set. The 19 MiB/2-pass option rather than the 46 MiB/1-pass
one because **a POS switches users constantly** (§13.1) and this runs on a shop's hardware, not
a server: a login that allocates 46 MiB and stalls a cashier is a real operational cost, while
19 MiB with two passes gives comparable resistance at a fraction of the footprint.

### 2.2 The encoded form — D2

**One PHC-format string**, the de-facto standard produced and consumed by every Argon2
implementation:

```
$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 hash>
```

The phase design (§IDN.1) specified `hash`, `algorithm`, **and `params_json`**. `params_json`
is **dropped**: the PHC string already carries the algorithm, version, and every parameter, so
a separate copy is a second source of truth for one fact — which is exactly what Step 0.4 §11.1
rejected `goose` over ("two version tables"), and what 0.7 D2 refused when it declined to add
`jobs.is_running` beside `job_runs.status`.

`algorithm` is **kept** as a plain discriminator, because migrating away from Argon2 someday
means selecting rows by algorithm, and parsing a PHC prefix in SQL is not portable.

---

## 3. DESIGN — schema `0005_identity.sql`

```sql
users (
  id CHAR(36) PK,
  company_id CHAR(36) NOT NULL REFERENCES companies(id),
  username VARCHAR(60) NOT NULL,
  display_name VARCHAR(120) NOT NULL,
  email VARCHAR(160),
  locale VARCHAR(20),                       -- per-user override of ui.locale
  is_system SMALLINT NOT NULL DEFAULT 0,    -- the setup administrator; never deletable
  is_active SMALLINT NOT NULL DEFAULT 1,
  + universal columns,
  CONSTRAINT ux_users_username UNIQUE (company_id, username)
);

user_credentials (
  id, user_id NOT NULL REFERENCES users(id),
  credential_type VARCHAR(12) NOT NULL,     -- 'password' | 'pin'  (pin reserved, phase D2)
  algorithm VARCHAR(20) NOT NULL,           -- 'argon2id'
  encoded TEXT NOT NULL,                    -- the PHC string (D2)
  must_change SMALLINT NOT NULL DEFAULT 0,
  expires_at CHAR(24),
  + universal columns,
  CONSTRAINT ux_user_credentials UNIQUE (user_id, credential_type)
);

password_history (
  id, user_id NOT NULL REFERENCES users(id),
  encoded TEXT NOT NULL,
  created_at CHAR(24) NOT NULL
);
```

### 3.1 Why credentials are a separate table

So that **no ordinary user query can return a hash**. A `SELECT * FROM users` — in a repository,
a report, a future export, a support session — cannot leak a credential if credentials are not
in `users`. That is a structural guarantee rather than a review convention, and it costs one
join on the one code path that needs it.

`UNIQUE (user_id, credential_type)` means one password and (later) one PIN per user, enforced
by the database rather than by the code that happens to write it.

---

## 4. DESIGN — the identity domain and service

### 4.1 Password policy as settings — D3

§13.1 makes the policy configuration. The **defaults** follow NIST SP 800-63B, which is a
deliberate and slightly unusual choice worth defending:

| Setting | Default | Reasoning |
|---|---|---|
| `identity.password.min_length` | **12** | Length dominates every other factor |
| `identity.password.expiry_days` | **0 = never** | NIST advises **against** periodic rotation: forced rotation produces `Summer2026!` → `Summer2027!`, which is weaker than a stable strong password and trains users to write them down |
| `identity.password.history` | **5** | Cheap, and stops the trivial "change it and change it straight back" |
| Composition rules (symbols, mixed case) | **none** | NIST advises against them for the same reason — they push users toward predictable substitutions while barely raising entropy |

A shop owner who *must* have rotation (a contract, an auditor) sets `expiry_days` and gets it.
The default is what a well-run system does, not what an out-of-date checklist demands. Shipping
the checklist default and calling it security would be the easy choice and the wrong one.

**No maximum length and no character-set restriction**: both are historical artefacts of
storing passwords badly, and a hash has neither problem. A passphrase must work.

### 4.2 The `Authenticator` port

```go
// internal/modules/identity/contract
type Principal struct {
    UserID      id.ID
    CompanyID   id.ID
    Username    string
    DisplayName string
    MustChange  bool
}

type Authenticator interface {
    Authenticate(ctx context.Context, username, password string) (Principal, error)
}
```

Local password authentication is the only implementation. Windows Hello, Touch ID, LDAP and SSO
(§13.2) plug in behind this with no schema change.

This is also identity's **`contract` package** — the first one in the codebase, and the channel
`module-isolation` (1.1) made mandatory. It contains an interface and a DTO and no aggregate,
exactly as §3.2 requires.

### 4.3 A single failure code

`Authenticate` returns **one** error for "no such user", "wrong password", and "inactive user":

```go
errs.Permission(CodeInvalidCredentials, "…")
```

Distinguishable failures are a username oracle. The *audit* trail records which case it was
(1.7); the caller never learns.

### 4.4 Constant work for an unknown user — the subtle one

Returning early when the username does not exist makes "no such user" measurably faster than
"wrong password", and that difference is enough to enumerate accounts.

So when no user (or no credential) is found, `Authenticate` **still performs a full Argon2id
verification** against a fixed dummy PHC hash, then fails. The work is the same either way.

This is the kind of correctness that no functional test would ever catch — the system returns
the right answer both ways — which is exactly why it is designed in rather than noticed later.

### 4.5 Rehash on login — D5

§13.1's promise that parameters "can be increased over time without invalidating existing
passwords" needs a mechanism, or it is just a hope. On a **successful** verification, if the
stored parameters are weaker than the current policy, the password is rehashed and stored.

The plaintext is available at exactly that moment and never again, so this is the only point
where the upgrade can happen. Raising `DefaultParams` in a future release then upgrades the
whole user base as people log in.

### 4.6 The last active user — D6

Deactivating the only active user locks everyone out of the installation permanently, with no
in-application recovery. Refused in the domain, the same shape as 1.1's last-active-branch
guard and for the same reason: an unrecoverable state should be unrepresentable, not
documented.

`is_system` additionally marks the setup administrator as **never deletable** (deactivation is
still possible while another active user exists).

### 4.7 No org `contract` package yet

Identity needs a company id for `users.company_id`, and the setup wizard already has one from
`Provision`. So it is **passed in** rather than fetched — no cross-module call, no contract.

`DependsOn: ["org"]` still holds, because the foreign key makes migration order real.

---

## 5. TESTING PLAN

| Level | Tests |
|---|---|
| **Hashing** | A hash verifies against its password and fails against a wrong one; the same password hashed twice yields **different** encodings (salt); the PHC string round-trips; a malformed/truncated/garbage encoding is a typed error, never a silent `true`. |
| **Parameters** | `Verify` reports `needsRehash` when stored params are below policy and not when they match; a hash made with old params still verifies. |
| **Policy** | Below-minimum length rejected; a long passphrase accepted; reuse of a password in history rejected; reuse outside the history window allowed; policy read from settings, not constants. |
| **Authentication** | Correct credentials return a Principal; wrong password, unknown user, and inactive user return the **identical** code; a successful login with stale params rehashes and the new encoding verifies. |
| **Leak drills** | A user read returns **no** credential material — asserted on the marshalled JSON, not on struct fields; `password_history` never reaches a DTO. |
| **Invariants** | The last active user cannot be deactivated; a system user cannot be deleted; duplicate usernames in one company are refused by the database. |
| **Integration** | Against the real merged schema (0001–0005) so the foreign keys and unique constraints are genuinely exercised. |

**Mutation-verified**: *constant-work verification* (return early for an unknown user → the
timing-shape test must fail) and *history enforcement* (skip the history check → the reuse test
must fail).

---

## 6. DECISIONS TAKEN

1. **D1 — `golang.org/x/crypto/argon2`, promoted from indirect to direct.** No new dependency;
   resolves offline at the pinned version. *(§1.1.)*
2. **D2 — One PHC-encoded string; `params_json` dropped** from the phase design's shape, as a
   second copy of one fact. `algorithm` kept as a discriminator. *(§2.2 — deviation.)*
3. **D3 — Password policy defaults follow NIST SP 800-63B**: 12 characters, **no** forced
   rotation, **no** composition rules, 5-password history. *(§4.1 — deviation from the
   conventional default, argued.)*
4. **D4 — One failure code for every authentication failure**, with constant work for unknown
   users. *(§4.3, §4.4.)*
5. **D5 — Rehash on login** when stored parameters fall below policy. *(§4.5.)*
6. **D6 — The last active user cannot be deactivated**; `is_system` users cannot be deleted.
   *(§4.6.)*
7. **D7 — Identity ships the first `contract` package** (`Authenticator`, `Principal`); org
   still needs none. *(§4.2, §4.7.)*

---

## 7. IMPLEMENTATION RECORD (Protocol Steps 3–6)

### 7.1 The timing drill, and why it is the test that matters

`TestUnknownUserStillDoesTheHashingWork` measures the unknown-user path against the known-user
path and requires them to be the same order of magnitude. Removing the dummy verification:

```
an unknown username returned in 27.5µs against 15.028917ms for a known one —
the constant-work verification is missing, and usernames can be enumerated
```

**A 545× difference.** That is not a subtle signal an attacker has to work for; it is a reliable
oracle over a network or a shared machine. And the system returns the *correct answer* in both
cases, so nothing functional would ever have caught it.

The assertion is a **ratio**, not an absolute time, and takes the **best** of several attempts
so that scheduler noise inflates the fast path rather than deflating it — noise can therefore
only cause a false failure, never a false pass.

### 7.2 Deviations from the design, both deliberate

- **`params_json` dropped (D2).** The PHC string already carries the algorithm, version, and
  every parameter. A second copy is a second source of truth, which is what 0.4 rejected
  `goose` over and 0.7 D2 refused for job state. `algorithm` survives as a discriminator.
- **The PHC parser rejects trailing garbage.** `fmt.Sscanf("p=1XYZ", "p=%d")` succeeds and
  returns 1. The parser now re-renders the parameter segment and compares, because *a parser
  that silently accepts a value it did not fully understand is how a tampered credential gets
  treated as a valid one.* `TestMalformedEncodingIsATypedErrorNotATrue` covers nine malformed
  inputs, and its first assertion is always "this must not VERIFY".

### 7.3 A one-method port, declared by the consumer

Identity needs one fact from org — the acting company's id. Rather than importing org's
package (or inventing a `contract` package for org to own), identity declares:

```go
type Organisation interface{ CurrentCompanyID(ctx) (id.ID, error) }
```

at its own point of use. Go's convention, and it means **org owes identity nothing**: the
dependency is a single method named by the consumer, and the composition root supplies org's
service, which happens to satisfy it. `DependsOn: ["org"]` still holds for migration ordering.

Identity *does* ship the codebase's first `contract` package — `Authenticator` and `Principal`
— because those are genuinely for other modules to consume.

### 7.4 The leak drill asserts on bytes, not fields

`TestNoCredentialMaterialReachesADTO` marshals the result of a user read and greps the JSON for
`argon2`, `$`, `hash`, `encoded`, `password`, `credential`. Asserting on struct fields would
pass while a future embedded type or a `SELECT *` leaked one. The structural defence is that
credentials live in their own table and `userColumns` names every column read; this is the
end-to-end check that the defence holds.

### 7.5 Two sloppy first drafts, corrected before they compiled

Recorded because both were the kind of thing that reads fine and is wrong:

- A hand-rolled `itoa` in the domain "to avoid importing strconv" — unnecessary cleverness in
  security-adjacent code, replaced with `strconv.Itoa`.
- `normaliseUsername` implemented by constructing a throwaway `User` with dummy ids. Replaced
  with an exported `domain.NormaliseUsername`, which matters for a real reason: **login and
  creation must normalise identically**, or an account can be created and never signed into.
  `TestUsernameIsCaseInsensitive` pins it.

### 7.6 Verification

**`make ci` green**: gofmt · build · archlint · vet · tests (race) · golangci-lint · frontend.
25 new Go tests across `platform/crypto` and `modules/identity`. Eleven new error codes,
translated in both locales; the 0.8 gate enforced it.

**Mutation-verified**, as promised in §5:

- *Constant-work verification* — removing the dummy hash fails the timing drill (§7.1).
- *History enforcement* — skipping `rejectReuse` fails `TestPasswordReuseIsRejected` with
  "a recently-used password was accepted".

### 7.7 Carried forward

- **Sessions, lockout, `login_attempts`** are 1.3, with migration `0006`. `Authenticate`
  returns a `Principal`; nothing yet issues a session from it.
- **`must_change` is stored and returned** on the Principal but nothing enforces it — the login
  flow that must redirect to a password change is 1.10.
- **`expires_at` on credentials** is written nowhere: with `expiry_days` defaulting to 0
  (never), there is nothing to write. The column is ready for an installation that turns it on.
- **PIN credentials** remain reserved and unimplemented (phase D2, Phase 5).
- **Audit events** from identity — created, password changed, deactivated, and *which* of the
  indistinguishable failures actually occurred — land in 1.7.

---

*End of Step 1.2. Implemented and self-reviewed.*
