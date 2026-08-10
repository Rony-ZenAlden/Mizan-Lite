# Step 1.12 — Phase 1 Definition-of-Done review

> Status: **REVIEW COMPLETE. 11 of 12 criteria pass; 1 cannot be verified without the
> reviewer's machine.**
> Two criteria failed on first inspection and were closed inside this step (§3). One blind spot
> in the tooling was found and closed (§4).
> `make ci` green: build · archlint · vet · tests+race · golangci-lint · frontend (152 tests).

---

## 1. How this review was run

Each criterion was checked against **evidence** — a named test, a file, a command — not against
memory of having built it. That distinction earned its keep: two criteria I would have called
green from recollection turned out to be false, and one of them had been false for four steps.

Where a criterion failed, it was **closed in this step** rather than recorded as debt. A
Definition of Done that ships with exceptions is a list of intentions.

---

## 2. The twelve criteria

### ✅ 1. A fresh install launches into the setup wizard, and completing it produces a company, branch, warehouse, fiscal year, seeded roles, and an administrator — in one transaction

`Setup.Apply` runs everything inside one `db.Do`; every inner call joins it (0.3 join
semantics), so there is one commit.

| Evidence | |
|---|---|
| `TestSetupIsRequiredOnAFreshInstall` | the wizard is what a fresh install gets |
| `TestTheAdministratorCanSignInAfterSetup` | company, branch, and a usable administrator |
| `TestEveryShippedCountryCanCompleteSetup` | runs the whole wizard once **per shipped country** |
| `TestARefusedSetupWritesNothing` | a failure leaves no partial company |
| `SetupGate` + `gates.test.tsx` | the frontend routes to the wizard and mounts nothing below it |

### ✅ 2. That administrator can log in; a wrong password is indistinguishable from an unknown user; repeated failures lock the account and every attempt is recorded

| Evidence | |
|---|---|
| `TestEveryFailureIsIndistinguishable` | one code for unknown user, wrong password, inactive, no credential |
| `TestUnknownUserStillDoesTheHashingWork` | constant work — the timing channel, which no functional test would catch |
| `TestThrottleEngagesAtTheThreshold`, `TestTheThrottleAlwaysElapses` | §13.1's "delay, never lock" |
| `TestAttemptsAreRecordedIncludingUnknownUsernames` | every attempt, including ones for users who do not exist |
| `TestThrottleIsPerUsername` | one person's mistakes do not lock out the shop |

Note the wording: the criterion says "lock the account", and the implementation deliberately
does **not** lock — it delays with backoff (§13.1). A lockable account is a denial-of-service
against a shop that needs to open. The spirit is met; the letter is corrected here, on purpose.

### ✅ 3. A session expires on both idle and absolute timeout, survives a restart when "stay signed in" is set, and can be revoked by an administrator

**This failed on inspection, and had been failing since Step 1.5.** See §3.1. Now:

| Evidence | |
|---|---|
| `TestIdleExpiryEndsAQuietSession`, `TestAbsoluteExpirySurvivesActivity` | both windows |
| `TestStaySignedInSurvivesARestart` | **new** — a second graph over the same data directory |
| `TestSigningOutForgetsTheRememberedSession` | **new** |
| `TestARevokedSessionIsNotRestored` | **new** — revocation beats the stored token |
| `TestRevocationTakesEffectImmediately`, `TestRevokingASessionEndsIt` | administrator revocation |

### ✅ 4. Every binding method has a declared policy, and removing one makes the application fail to start with a message naming the method

`Set.ValidatePolicies` runs in `shell.go` **before** `Attach`, and its failure is fatal. It
catches three rots, not one: a method with no policy, a policy for a method that no longer
exists, and a policy naming a permission no module declares.

The message is `Identity.Users has no declared policy` — the method by name, in a startup log.

| Evidence | |
|---|---|
| `TestEveryBindingMethodHasAPolicy` | every method on all nine façades |
| `TestPolicyCoverageRejectsAnUndeclaredPermission` | the unreachable-method case |
| `TestPublicMethodsArePinned` | five public methods, each with its reason recorded |
| Step 1.5 mutation drill | deleting one entry fails startup |

### ✅ 5. A role without a permission is actually blocked at the backend, not merely hidden in the UI; the denial renders as a translated permission-denied state

The guard **is** the accessor (1.5 D1): a method that skips its policy has no database, no
services, and no context. Not unprotected — non-functional.

| Evidence | |
|---|---|
| `TestAdministrationRequiresItsPermissions` | six administration methods refused to a permissionless user |
| `TestAPermissionlessUserIsRefused` | the same at the guard |
| `TestForbiddenIsAPermissionCategory` | the denial crosses as a permission category |
| `admin.test.tsx` "explains a screen reached without the permission" | the `denied` `EmptyState`, rendered from a translation key |

### ✅ 6. Scope resolution is correct at global, branch, and warehouse, with tests, even though v1 seeds only global grants

| Evidence | |
|---|---|
| `TestScopeSatisfaction` | the table across kinds × granted/not |
| `TestScopedGrantIsEnforcedEndToEnd` | a branch-scoped grant through the real graph |
| `TestWildcardsAreGrantSideOnly`, `TestAdministratorHasNoBypass` | no superuser shortcut |
| `ValidatePolicies` | a warehouse-scoped policy **fails startup** until Phase 4 implements branch⊃warehouse containment (1.4 D3) — the dangerous reading would be to treat it as global |

### ✅ 7. A restricted field is absent from the serialized JSON for a user lacking its permission

Asserted on the **marshalled bytes**, not on a Go struct field — a struct assertion passes just
as happily for a blanked field, which is the design being rejected.

| Evidence | |
|---|---|
| `TestRestrictedPayloadKeysAreAbsentNotBlank` | the key itself is absent |
| `TestForgettingRedactionHidesRatherThanLeaks` | forgetting the call HIDES rather than leaks |
| `admin.test.tsx` "tells withheld apart from empty apart from present" | the three states reach the screen |

### ✅ 8. Every audited action writes an `audit_log` row in the same transaction, with actor, correlation id, and label snapshots; a failed audit write rolls the change back

| Evidence | |
|---|---|
| `TestAuditRecordsAUserBeingCreated` | actor snapshot, correlation id, label, source |
| `TestAuditEntryRollsBackWithTheChange` | a rolled-back change leaves **zero** entries |
| `TestFailedAuditWriteAbortsTheOperation` | the audit table is dropped; the old password still authenticates |
| `TestPanickingSubscriberAbortsTheOperation` | a recovered panic still rolls back |
| `TestAuditNeverRecordsACredential` | no password, no argon2 encoding, no session token |
| Step 1.7 drill | writing on a separate connection **deadlocks** — SQLite has one writer, so that mistake cannot ship |

### ✅ 9. Country and business profiles apply from seed files, and every value they set remains individually editable afterwards

| Evidence | |
|---|---|
| `TestEveryShippedProfileLoads`, `TestAUserFileAddsACountry` | shipped and data-directory layers |
| `TestNoShippedCountryEncodesTaxOrAccounting` | §C.3 held to |
| `TestApplyWritesAtCompanyScopeAndIsAudited` | a bundle writes ordinary settings rows |
| `TestTheWizardsChoiceBeatsTheProfilesDefault` | the person overrides the profile |
| `TestEveryShippedCountrysCurrenciesAreSeeded` | the cross-module invariant 1.8 broke and 1.9 closed |

### ✅ 10. `module-isolation` is enforced and green; no module imports another module's non-`contract` package

`go run ./tools/archlint ./...` — clean. Six modules; every cross-module import is a `contract`
package (`identity → audit/contract`, `org → audit/contract`, `profile → audit/contract`).

The rule caught a **design** error rather than a typo during Phase 1: `platform/modules` could
not import `identity/contract`, which is what moved the auth types to `internal/platform/auth`
(1.4).

### ✅ 11. Settings resolve at user scope for a signed-in user, and locale/theme follow the user

| Evidence | |
|---|---|
| `TestPreferencesNowResolveAtUserScope` | closes 0.11 D4's system-scope limitation |
| `TestScopesReportTheActor` | `appctx.Scopes` reads the session |
| `PreferencesProvider.test.tsx` | the frontend holds them as server state, not component state |

### ⚠️ 12. The Phase 0 carried-forward list in §DEBT is empty

Eleven of twelve items are closed. **One cannot be closed by me.**

| Item | Origin | Status |
|---|---|---|
| `Module.Permissions()` joins the contract | 0.9 D5 | ✅ 1.4 |
| `config.Authorizer` real implementation | 0.5 D8 | ✅ 1.4 |
| `appctx.Scopes` reads a real session | 0.10 | ✅ 1.3 |
| Settings write at user scope | 0.11 D4 | ✅ 1.3 |
| **`Module.Bindings()` reconciled with static façades** | 0.11 D8 | ✅ **1.12 — removed** (§3.2) |
| Permission-denied `EmptyState` gets a producer | 0.11 D6 | ✅ 1.11 |
| TanStack Query / Zustand / router | 0.11 D1 | ✅ 1.10 |
| JSON seed-file discovery | 0.5 D7, 0.9 | ✅ 1.8 |
| `module-isolation` | 0.12 | ✅ 1.1 |
| SQL rule restated | 0.12 | ✅ 1.1 |
| **Visual confirmation of the shell** | 0.12 | ❌ **not done** |

**Visual confirmation has never happened.** Sixteen screens now exist — boot, failure, wizard
(seven steps), login, forced password change, dashboard, users, roles, sessions, audit — and not
one has been rendered on a display. Every one is covered by tests; none has been looked at.

This is not a criterion tests can close. It needs one command on the reviewer's machine:

```
make dev        # or: wails dev
```

What it would establish that nothing else can: that the RTL layout is actually mirrored rather
than merely asserted, that Arabic text renders in the shipped font, that nothing overflows at a
shop-counter window size, and that the seven-step wizard reads like a sequence rather than a
form split into pieces.

**Recorded as the one open item of Phase 1.** It blocks nothing structural, and it should
happen before the first customer sees the product.

---

## 3. What this review changed

### 3.1 "Stay signed in" was a promise the code could not keep

Step 1.3 gave a remembered session a longer **absolute** window. Step 1.5 (D3) put the token in
the Go process, out of reach of JavaScript. Both decisions are right. Together they made §13.1's
"stay signed in" do nothing observable: the session row survived a restart, and the only thing
that could present it — a token in process memory — died with the window.

Four steps and no test caught it, because **no test had ever restarted the application.**

Fixed with `internal/api/bindings/remember.go`: the token is written to
`<dataDir>/session.token` (0600) when the user opts in, restored and **validated** at `Attach`,
and removed on sign-out or when the stored token turns out to be dead.

The trade is stated at the file: the token sits beside `mizan.db`, which holds every price,
customer, balance, and audit row in plain SQLite — anyone who can read the file can already read
all of that directly. What it adds is convenience for an attacker already on the machine, which
is why it is opt-in, removed on sign-out, and still subject to idle expiry, absolute expiry, and
an administrator's revocation. The OS keychain is stronger and was not taken: it means cgo and a
per-platform dependency in a project whose hard constraint is a pure-Go offline build.

Mutation-verified: removing the write makes `TestStaySignedInSurvivesARestart` fail with *"a
user who asked to stay signed in was shown the login screen after a restart"*.

### 3.2 `Module.Bindings()` was removed, not reconciled

0.9 (D5) added it, expecting each module to return its Wails binding struct. 0.11 (D2) then
inverted boot so the window opens **before** the object graph exists — which means Wails is
handed its fixed `[]any` while no module has been constructed.

The method could never work after that, and for two phases every one of six modules returned
`nil` while the real surface was assembled statically in `internal/api/bindings`.

The 0.11 D8 debt item said "reconcile". The honest reconciliation is deletion: a contract method
with no possible implementor is a promise the architecture cannot keep, and leaving it invites
someone to try. `modules.Module`, five implementations, the bootstrap collector, and
`App.Bindings` all went with it.

---

## 4. A blind spot in the tooling

`scripts/check.sh` ran `archlint ./internal/...`. The repository root — `main.go` and
`shell.go` — was **never scanned**, and since 0.11 moved the boot inversion and the
policy-coverage check into `shell.go`, that is real logic sitting outside the architecture rules.

Widened to `./...`. It immediately fired on `main.go`'s `os.Exit`, which is the one place a
non-zero exit code is correct — the `forbid-call` rule already said "outside the application
bootstrap" and its exclusion list simply never named the entrypoint. Added `**/main.go`.

Verified the widening is real rather than nominal: removing the exclusion makes the scan fail on
`main.go`, which it could not have done before this change.

---

## 5. Where Phase 1 stands

**Test totals:** 152 frontend tests, 28 Go packages reporting coverage, mean **81.1%**.

Packages showing 0% report it because their tests live in another package — `internal/api/setup`
is exercised entirely through `bindings_test`, `internal/modules/audit` through
`identity_test`, `internal/api/redact` through the binding tests. Coverage-by-package
under-reports where a package is tested through its consumer, which is where these are best
tested.

| Package | Coverage |
|---|---|
| `platform/strategy` | 97.1% |
| `platform/modules` | 95.6% |
| `kernel/locale` | 93.9% |
| `platform/eventbus` | 92.7% |
| `kernel/money` | 92.4% |
| `platform/crypto` | 90.9% |
| `platform/database` | 88.9% |
| `platform/i18n` | 87.4% |
| `platform/seeds` | 86.2% |
| `platform/migrate` | 85.5% |
| `api/bindings` | 80.1% |
| `platform/config` | 79.4% |
| `modules/currency` | 75.0% |
| `modules/profile` | 67.9% |
| `bootstrap` | 67.7% |
| `modules/identity` | 66.6% |
| `modules/org` | 61.5% |
| `kernel/clock` | 53.3% |
| `modules/org/domain` | 42.1% |

`modules/org/domain` at 42% is the weakest number and it is honest: most of that package is
fiscal-period generation, which Phase 2 is the first thing to actually use. Raising it now would
mean testing shapes rather than behaviour.

### 5.1 Carried into Phase 2

- **Visual confirmation** (§2, criterion 12) — the one open Phase 1 item.
- **Audit paging.** The viewer reads a 200-row window. §FE.2 called the audit log "the first
  genuinely paginated read"; it is not paginated, and will need to be before a shop has a year
  of history.
- **Nothing forces a module to publish `Auditable`** (1.7 §6). A new write path that forgets
  leaves a silent hole, and it is not catchable by the enumeration trick that made 1.5's
  guarantee structural.
- **The shipped country defaults are unverified** (1.8 §3.3) — date format, first day of week,
  pricing currency for `sy`/`sa`/`ae`/`eg`. One line each, shown to the user during setup.
- **Branch⊃warehouse containment** (1.4 D3) — Phase 4, and `ValidatePolicies` fails startup on
  any warehouse-scoped policy until then.
- **Tax profiles and charts of accounts** — Phase 2 needs the first customer's real figures, or
  confirmation that an empty user-configured tax profile is acceptable (§C.3).

---

## 6. The pattern this phase confirmed

Four Phase-0 seams were subtly wrong at their **first real use**, and only then:

| Seam | Built | Wrong until |
|---|---|---|
| `envelope.Result.Data` omitempty | 0.11 | 0.11's own §1.2 |
| `Module.Jobs()` returned defs with no handler | 0.7 | 1.3 |
| `policy.Public()` never stamped an actor | 1.5 | 1.11 |
| "Stay signed in" had nothing to present | 1.3/1.5 | **1.12** |

Plus four tests that **passed while proving nothing**, each found by a mutation drill rather
than by review: the 1.4 cascade claim, the 1.8 registry-population gap, the 1.9 empty-bundle
ordering test, and the 1.11 neighbouring-rule refusal.

The rule earned: **a seam with no consumer is drafted, not built** — and a drill that passes is
telling you the test is wrong.

---

*Phase 1 is complete but for visual confirmation. Awaiting approval to begin Phase 2.*
