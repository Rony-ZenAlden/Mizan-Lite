# Step 0.12 — Phase 0 Definition-of-Done Review

> Status: **COMPLETE. Phase 0 is DONE, with two documented gaps that do not block Phase 1.**
> Reviewed 2026-08-04 against `PHASE_0_FOUNDATION.md` §DOD (ten criteria) and §REPO.3 (the
> seven enforced lint rules).
> This is a **review, not a build**: no design gate, and no production code was written for it.
> The only repository changes are `.gitignore` entries for artifacts `wails build` generates.

Phase 0 set out to build "the reusable application kernel future modules plug into". This
review asks one question per criterion: **is it true, and what is the evidence?**

Evidence is either a **live run** of the packaged desktop application (new in this step — the
`wails` CLI was not previously installed) or a **named test**. Nothing below is asserted from
reading the code alone.

---

## 1. The end-to-end verification that was missing

Step 0.11 closed with an honest flag: the Go and frontend halves were each proven, but had
**never been run together in a window**. That is now done.

```
make tools          → wails v2.13.0 installed; `wails doctor` reports the system ready
make build          → build/bin/mizan.app  (darwin/arm64, packaged, self-signed, 13s)
MIZAN_DATA_DIR=<empty tmp dir> ./Mizan\ ERP
```

First launch, against a genuinely empty directory:

```
INFO mizan window ready version=76449ea            ← the window exists BEFORE any migration
INFO pre-migration backup created path=…/backups/pre-migration-…-v0-to-v3.db
INFO migration applied version=1 name=platform
INFO migration applied version=2 name=outbox_deliveries
INFO migration applied version=3 name=currency
INFO database updated applied=3 to_version=3
INFO mizan started data_dir=/tmp/mizan-dod modules=1
INFO mizan ready
```

**`mizan window ready` precedes every migration line.** That single ordering is Step 0.11's D2
working on real hardware: the window opens first, the graph builds behind it, and the migration
has somewhere to report progress. It was previously provable only by unit test.

Process state confirmed a real GUI application with a live renderer — registered as a
`Foreground` app (`bundleID=com.wails.mizan`) with a `WebKit.WebContent` process spawned, so
the webview loaded and is rendering. Resulting data directory:

| Check | Result |
|---|---|
| Tables created | 13 — the 9 platform tables, `outbox_deliveries`, and currency's 3 |
| `schema_migrations` | 3 rows, each with a distinct SHA-256 checksum |
| Seeded currencies | `EUR`, `SYP`, `TRY`, `USD` — all `is_system=1` |
| Seeded rate types | `custom`, `manual`, `market`, `official` — all `is_system=1` |
| Arabic names | present in `translations` (`ليرة سورية`, `دولار أمريكي`, …) |
| Backup | one verified pre-migration snapshot |

**Second launch** on the same directory logged **no migration lines at all** — the fast path,
and the idempotent re-boot 0.10 D4 promised.

### What could NOT be verified, and why

**A screenshot of the rendered shell.** macOS Screen Recording and Automation permissions are
not granted to this terminal, and the user declined to grant them. So "the shell renders
correctly" rests on the WebKit content process being live plus the 121 component tests
(including both-direction RTL renders), not on a human or machine having *looked* at it.

That is a real, if narrow, gap and it is stated rather than glossed: **nobody has visually
confirmed the pixels.** It costs one manual launch to close, and is the single thing worth
doing by eye before Phase 1 UI work begins.

---

## 2. The ten criteria

| # | Criterion | Verdict | Evidence |
|---|---|---|---|
| 1 | `wails dev` launches an app that migrates a fresh database and renders the themed, translated, RTL-capable shell | ✅ **with a caveat** | §1 — built, launched, migrated 3 migrations on an empty dir, webview live. Rendering proven by test, **not by eye** |
| 2 | Kernel numeric types pass unit + property suites; **the seven lint rules are enforced in CI and green** | ⚠️ **partial** | Kernel: ✅ (round 100%, money 92.4%, quantity 89.0%, fixed 93.8%, property tests). Lint rules: **5 of 7 enforced** — see §3 |
| 3 | Migrations run transactionally with checksum verification; backup and auto-restore-on-failure tested and works | ✅ | `TestDrillInjectedFailureRestoresData`, `TestDrillChecksumTamperIsHardFailure`, `TestDrillCorruptDatabaseAbortsPreflight`, `TestStartFailsFatallyOnACorruptDatabase`, `TestPruneBackupsKeepsNewestAndSparesFailedArtifacts` + the live backup in §1 |
| 4 | The outbox delivers an event end-to-end atomically; a forced handler failure retries and dead-letters visibly | ✅ | `TestDeadLetterAfterMaxAttempts`, `TestFailureSchedulesABackoffAndIsNotReclaimedEarly`, `TestOrderingPerAggregateBlocksBehindAFailure`, `TestConcurrentDispatchersDeliverEachDeliveryOnce`, `TestCrashRecoveryReclaimsAbandonedDeliveries` |
| 5 | A durable job runs on schedule, survives an app restart with correct catch-up, and reports status in the UI panel | ✅ **live-verified** | §4 below |
| 6 | Settings resolve by scope through typed accessors; a change emits `SettingChanged` and a subscriber reacts live | ✅ | `TestScopeResolutionPrecedence`, `TestSessionOverrideWinsOverEveryPersistedScope`, `TestSettingChangedCarriesScopeID`, `TestSubscriberFailureDoesNotUndoTheWrite` |
| 7 | The currency module converts across rate types and resolves a redenomination chain correctly, with tests | ✅ | `TestContextsResolveTheirOwnRateTypeIndependently`, `TestRedenominationChainWalksToTheEnd`, `TestRedenominationLoopIsDetected`, `TestCorrectionOnTheSameDateIsRecordedAndWins`, `TestPivotRoundsOnceNotTwice`, `TestStaleRateIsUsedAndFlagged` |
| 8 | Locale switches at runtime with no reload; RTL snapshot tests pass both directions; i18n key coverage complete | ✅ | `switches language with no reload, updating lang and dir`; `describe.each(BOTH_DIRECTIONS)` over every primitive; `TestEveryErrorCodeHasATranslation`, `TestNoOrphanErrorTranslations`, `TestEveryLocaleDefinesTheSameKeys` |
| 9 | The composition root builds the full graph and shuts down gracefully (drain → flush → checkpoint → close) with no data loss on mid-write close | ✅ **live-verified** | §4 below |
| 10 | The module contract, both config registries, and the strategy registry exist, are documented, and are exercised by the currency module | ✅ | `TestMigrationsAndSeedsAreApplied`, `TestMergeRejectsDuplicateVersionsAcrossModules`, `TestDuplicateKeyIsReportedNotLastOneWins`, `TestDuplicateRegistrationIsAnError`, `TestRateTypesAreSeededAsSystemRows`; currency implements `Module` and seeds through the 0.5 seeder |

**Eight of ten fully satisfied. One (1) satisfied with a visual-confirmation caveat. One (2)
partially satisfied** — the kernel half is complete, the lint half is not.

---

## 3. The one genuine shortfall — REPO.3's "seven lint rules"

DoD item 2 says the seven rules are "enforced in CI and green". **Five are. Two are not.**

| # | REPO.3 rule | Enforced? | Reality |
|---|---|---|---|
| 1 | kernel & `modules/*/domain` import only stdlib + kernel | ✅ | `kernel-purity`, `domain-purity` |
| 2 | **No package imports another module's non-`contract` package** | ❌ **not enforced** | `module-isolation` is listed in `arch-rules.yml` under "Planned rule kinds" — it needs a whole-program package-graph pass. **Vacuously true today: currency is the only module.** |
| 3 | No `float64/32` in domain or money-named types | ⚠️ narrower than written | `no-float` covers `kernel/**` and `modules/*/domain/**`, not "any type named `*Money\|*Price\|…`" anywhere. **Zero violations exist**: the only `float` in production Go is a `case float32, float64:` arm in a `typeName()` helper that exists to *report* a wrong type in an error |
| 4 | No `time.Now()` outside `platform/clock` | ⚠️ narrower than written | Enforced only for `modules/*/domain/**` and `kernel/money/**`. **Zero violations exist** — a repo-wide grep finds no `time.Now()` outside `kernel/clock` in non-test code |
| 5 | No `panic` in library code | ✅ | `forbid-call: panic` over `self/**` with documented exemptions |
| 6 | Frontend: no physical-direction Tailwind classes | ✅ | ESLint `no-restricted-syntax`; 0.11 added the `window.go` boundary beside it, both verified by planting |
| 7 | **No raw SQL string outside `infra` packages** | ❌ **not enforced** | No such rule exists. **The intent holds**: no SQL in `modules/*/domain` or `internal/api`. SQL does live in eight `platform/*` packages (outbox, config, jobs, i18n, metadata) — which *are* infrastructure and own their own tables, so the rule as worded is simply too narrow |

**Assessment.** No rule is being *violated*. Two are unenforced, and both are unenforced for
defensible reasons: rule 2 needs machinery that has no subject yet (one module), and rule 7 was
written before `platform/*` packages owned tables and needs restating rather than implementing.

This is nonetheless the honest answer to DoD 2: **"the seven lint rules are enforced" is not
true today, and the claim should not be carried forward as though it were.** Both are recorded
in §5 as Phase 1 work — rule 2 becomes enforceable and *necessary* the moment a second module
exists, which is exactly when Phase 1 starts.

---

## 4. Live verification of criteria 5 and 9

Both were previously covered by tests that drive `Tick` directly. This is the first time
either has been observed in the packaged application.

**DoD 5 — schedule, restart, catch-up.** With the app running, `job_runs` accumulated 43 rows:
`outbox.dispatch` every 5 s and `platform.heartbeat` every 60 s, all `succeeded`. The app was
then stopped, left down for ~3 minutes, and restarted:

```
platform.heartbeat runs in the catch-up window: 1
```

Three occurrences were missed; `run_once` collapsed them into **exactly one**, which is §24.1's
"three missed rate fetches should collapse to one" behaving correctly against wall time rather
than a fake clock. The panel that displays this is `JobStatusPanel`, over the same
`JobStates`/`RecentRuns` API, with its four states tested.

**DoD 9 — graceful shutdown.** After the stop:

| Check | Result |
|---|---|
| `job_runs` left in `running` | **0** |
| Run outcomes | 47 `succeeded`, nothing `failed`/`cancelled`/`timeout` |
| `mizan.db-wal` size | **0 bytes** — the WAL checkpoint ran |
| Reopen | second boot clean, no recovery, no migrations |

Drain → flush → checkpoint → close, observed end to end. Worth restating what this does **not**
claim: per 0.10 D6, shutdown is politeness, not durability. Correctness under a power cut comes
from the outbox, job reclaim, and the WAL — not from this sequence.

---

## 5. Carried into Phase 1

Nothing here blocks Phase 1. All are recorded so they are not rediscovered.

1. **Enforce REPO.3 rule 2 (`module-isolation`)** — becomes both possible and necessary with
   the second module. This is the rule that stops an ERP's modules quietly fusing, and Phase 1
   adds org/identity/settings/audit, so it should land *early* in Phase 1, not late.
2. **Restate and enforce REPO.3 rule 7** — the honest rule is "no SQL outside `infra` **and
   `platform`**", i.e. no SQL in `domain`, `app`, or `api`. Cheap to express once stated
   correctly.
3. **Visually confirm the shell** — one manual launch, before Phase 1 UI work.
4. **Widen `no-float` and `time.Now` scopes** to match REPO.3's wording. Zero violations today,
   so this is closing the barn door while the horse is still in it — the cheapest moment.
5. **Dev-tooling advisories** — `npm audit` reports 6 issues in vite/vitest/esbuild/
   brace-expansion, all dev-only, none shipped in the desktop binary. Deserves a deliberate
   toolchain bump in its own change.
6. **App icon and `Info.plist`** — currently stock Wails scaffolding, git-ignored with a note.
   They become tracked assets in Phase 10 (polish/installers); the `.gitignore` comment says so
   and says to remove the lines then.
7. **From earlier steps, unchanged:** `Module.Bindings()` reconciliation and `Permissions()`
   (Phase 1); TanStack Query / Zustand / router when a real screen needs them; `RunNow` once it
   can be permission-gated; the subscribe-before-start ordering becomes mutation-testable in
   Phase 2 when accounting subscribes.

---

## 6. Verdict

**Phase 0 is complete.** The application boots from nothing, migrates a fresh database behind a
visible window, seeds reference data in two languages, runs durable background work across a
restart with correct catch-up, shuts down clean, and presents it through a translated,
RTL-capable, themed shell with one enforced error path.

**312 Go tests + 121 frontend tests**, `-race` clean, `make ci` green across gofmt · build ·
archlint · vet · tests · golangci-lint · frontend typecheck/lint/test.

Two things are **not** true that a careless reading of the DoD would suggest, and both are
written down rather than smoothed over: **two of the seven lint rules are not enforced**, and
**no human has looked at the rendered shell**.

Phase 1 (org, identity + RBAC, settings, audit) can begin.

---

*End of Step 0.12. Phase 0 reviewed and closed.*
