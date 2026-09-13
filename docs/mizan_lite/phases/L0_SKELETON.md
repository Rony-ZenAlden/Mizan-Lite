# Mizan Lite — Phase L0: skeleton and gates

> **Status: COMPLETE — approved and committed 2026-09-13.** golangci-lint was upgraded to v2 on approval
> (F2, resolved). Visual confirmation remains an open item for the owner (§7.2, PROGRESS O2).
> **Branch:** `lite/l0-skeleton` (from `main` at `252e959`).
> **Design:** [`../DESIGN.md`](../DESIGN.md) (approved 2026-09-13), with the amendments in its §13.

---

## 1. What L0 is

The foundation the eight later phases build on, holding no business feature at all. Its purpose is to make
the defects Mizan found late **impossible to build** before anything exists to build them in:
the unconnected binding, the wrong bridge namespace, the silent mock, the unrouted screen, the hardcoded
string, the Windows path nobody tested.

One binding surface — `App.BootStatus`, `App.Health`, `Settings.Get`, `Settings.Update` — is enough for every
gate to have real input, and small enough that each gate could be proven to fail.

---

## 2. What was built

### Go — `internal/lite/` (1,851 lines) and `apps/lite/` (the Wails project)

| Package | What it does |
|---|---|
| `paths` | Data directory per OS. **Windows `%LOCALAPPDATA%`**, macOS `~/Library/Application Support`, override `MIZAN_LITE_DATA_DIR`, file `mizan-lite.db`. Every OS branch takes an `Env`, so Windows is tested on a Mac. |
| `migrations` | `0001_lite_platform.sql`: migration bookkeeping and jobs tables **verbatim from Mizan** (asserted), plus Lite's `settings`. |
| `locales` | Arabic and English catalogs, embedded by Go and imported by Vite from the same files. |
| `settings` | `domain` (values, parsing, `FromStored`, `Apply`), service, `infra/sqlite` (portable UPDATE-then-INSERT), `settingstest` (fake + a contract suite run against fake **and** SQLite), and `PeekLocale`. |
| `bootstrap` | Composition root: open → migrate (with safety snapshot) → catalogs → settings → scheduler + daily backup. Shutdown: stop jobs → close-time backup → close database, idempotent. |
| `api` | Façades constructed before the graph, attached after boot; panic → translatable code; untyped error → logged, never leaked. |
| `logging` | JSON log in the data directory, rotated at launch — before opening, because Windows cannot rename an open file. |
| `litetest` | Opens a real, migrated database file per test. |
| `apps/lite` | `main.go`, window options, **locale middleware**, boot shell, platform build files (`com.mizanerp.lite`, macOS ≥ 13). |

### Frontend — `apps/lite/frontend/` (811 lines)

Vite 5 + React 18 + react-router 6 + Tailwind, exact versions pinned to what Mizan already has installed so
the offline npm cache serves them. `src/api/client.ts` is the only importer of the generated bindings;
`Boot` → `Shell` → `HomeScreen` (the status screen); `LocaleProvider` switches direction in the same frame
and reverts if the choice cannot be saved; three primitives. **There is no mock anywhere.**

### Tooling

`scripts/lite-check.sh` (`make lite-ci`), `scripts/lite-arch-drill.sh`, `make lite-dev`,
`make lite-build-macos`, `make lite-build-windows`, 5 archlint rules added and 4 existing rules extended to
Lite, `.gitattributes` pinning `*.sql` / `*.json` / `*.sh` to LF.

---

## 3. Decisions made while building, and why

**D-L0.1 — the language is set before the first paint, on the Go side.** The window opens before the graph
exists, so the settings service cannot answer when `index.html` is served. `PeekLocale` reads the stored
language from the database file before `wails.Run` (never creating a file, never failing a launch), and an
asset-server middleware rewrites `<html lang="ar" dir="rtl">` to the stored language as the document is
served. Setting `dir` from JavaScript would paint one wrong-direction frame on every English launch.
`TestIndexHTMLCarriesTheMarker` holds both `index.html` files to the exact tag, because a reformatted tag
would make the middleware a silent no-op.

**D-L0.2 — one migration per phase** (design §13 A3).

**D-L0.3 — "DB mock tests" are fakes held honest by a contract.** Your requirement asked for them. A mock
that returns what a test hoped for while the SQL is wrong is the classic false-green, so the settings store
has an in-memory fake **and** a `StoreContract` suite that runs against both the fake and SQLite. Services are
unit-tested over the fake (fast, failure-injectable); atomicity is tested only against the real database,
because a fake transaction cannot prove a rollback.

**D-L0.4 — any console error or warning fails a frontend test.** React reports real defects through the
console. `expectConsoleErrors()` is the explicit opt-out, used once (the error-boundary test).

**D-L0.5 — no boot events; the boot screen polls every 150 ms.** Events would be a second channel with a
caller on only one side until something subscribed.

**D-L0.6 — a native window frame on both platforms, and a single-instance lock.** A frameless window must
redraw and mirror its own caption buttons for right-to-left. A second launch focuses the running window,
so two processes can never contend for one SQLite writer.

**D-L0.7 — Lite's UI primitives start small.** Three primitives (Button, Alert, Spinner) instead of the
eleven design D7 considered copying: a primitive with no caller is the defect in its smallest form.

---

## 4. Findings — defects and gaps found by building L0

Numbered by severity. Each is fixed, or recorded with a recommendation.

**F1 — `platform/database` opened the wrong file for paths containing `#` or `%`. FIXED (shared with Mizan).**
The DSN is a `file:` URI and only spaces were escaped. With `#` in the path, SQLite **silently created the
database at a truncated path**, while the migration runner's restore and the backup service used the real
one. With `%` followed by two hex digits, the database would not open at all. Windows user names can contain
both characters, and the data directory sits under the user's profile. Found by probing, fixed with a
one-pass `strings.Replacer`, pinned by `TestTheDatabaseOpensAtExactlyThePathGiven` (10 path cases,
reopen-and-read), and drilled. *Consequence for Mizan:* an installation whose data path contains `#` has its
database at the truncated path and would open a fresh one after this fix. Nobody has installed Mizan, so no
migration is needed — but this belongs in its changelog.

**F2 — Go 1.27 broke golangci-lint for BOTH editions. RESOLVED on approval: upgraded to v2.13.2.**
golangci-lint v1.64.8 bundles an export-data reader that stops at version 2; Go 1.27 writes version 4. Every
package fails to type-check — even the standard library's `errors` — so every finding it reports is false.
**Mizan's `make ci` has failed at this step since the Go upgrade.** The fix is golangci-lint v2, which
changes the configuration format (`.golangci.yml`) for Mizan too. `lite-check.sh` reported it as
**NOT RUN**, never as passed, until the owner approved the upgrade.

*Resolution (2026-09-13).* golangci-lint v2.13.2, built by Go 1.27.1, pinned in `make tools`. The config was
converted with `golangci-lint migrate` and re-commented by hand; the linter set is unchanged, v1's implicit
exclusions are kept as explicit presets, and the one new style quickfix that would have obscured intent
(`QF1001`, De Morgan rewrites) is disabled with its reason. The first honest run in months found **34
issues**, none of them defects:

| Where | Count | What | Done |
|---|---|---|---|
| Lite | 15 | `contextcheck` on `Store.Close()` — which takes no context by design | reasoned `nolint` on the two production sites, as Mizan's composition root has; excluded in tests |
| Lite | 7 | `err` shadowing | fixed |
| Lite | 4 | a nil-map write the analyser sees inline, two `slog` style points, a map key with deliberate whitespace | fixed |
| Mizan | 1 | `wastedassign` in `identity.SeedRoles` — an existence check whose result was named and never read. **Not a bug**; behaviour identical | `_, err :=` |
| Mizan | 2 | De Morgan quickfixes | `QF1001` disabled, reasoned |
| Mizan | 2 | `reflect.Ptr` → `reflect.Pointer`; an inferable type in a declaration | fixed |

Mizan's full `scripts/check.sh` is green on v2, and `lite-check.sh` now runs the linter instead of reporting it.

**F3 — a Wails bridge rule keyed on `window` is evadable. FIXED in Lite; Mizan has the same hole.**
Drill 21 planted `(window as unknown as Record<string, unknown>)["go"]` and it passed both the AST gate and the
ESLint rule. An alias (`const w = window; w.go`) would also pass. Lite's rule now forbids any property named
`go` or `runtime` on anything. **Mizan's `noWailsGlobals` matches `object.name='window'` and has the same
gap.** Recommendation: apply the property-based rule to Mizan.

**F4 — Mizan's `Info.plist` declares macOS 10.13; Go 1.27 builds for macOS 13.** The linker says so on every
build. A Mizan binary rebuilt with Go 1.27 would launch on macOS 11 or 12 and crash, instead of being refused
with an explanation. Lite declares 13.0, asserted by a test that reads the built value. Recommendation: the
same one-line change for Mizan.

**F5 — Mizan stores its database in Roaming AppData on Windows.** On a domain-joined machine Roaming is
synced at sign-out and restored at sign-in: a live SQLite file restored from an older copy silently rolls a
shop's books back. Lite uses Local AppData. Recommendation for Mizan: move to Local, which needs a data move
for existing installations.

**F6 — Mizan's error-code gate scanned all of `internal/`. FIXED.** Once Lite existed it demanded Lite's codes
in Mizan's catalog, which would have made Mizan depend on Lite. It now skips `internal/lite`, which has its
own gate. Drill M1 proves an untranslated Mizan code is still caught.

**F7 — Lite's coverage gate found two untranslated codes on its first run** (`i18n.translation_load_failed`,
`i18n.translation_write_failed`). Not reachable in Lite today; translated anyway, because narrowing the gate
to individual files is fragile.

**F8 — the pre-migration snapshot's manifest records an empty app version.** Visible in a test's output:
`platform/migrate` writes its own manifest without the build version that Mizan 10.4 threaded into
`platform/backup`. A support conversation about a failed upgrade is exactly when the version matters.
Recorded for Mizan; not changed in L0.

**F9 — a second launch does a little work before the single-instance lock.** Wails checks the lock inside
`wails.Run`, after `main` has resolved paths, opened the log (which may rotate it) and peeked the language.
On macOS, a second launch while the log is over 5 MB would rotate the file the running instance is writing
to. Low severity; recorded.

**F10 — my own comment claimed Wails cancels the shutdown context. It does not.** Verified in
`internal/app/app_production.go`; the comment was corrected, the defensive `WithoutCancel` kept, and a test
now pins it (drill S3).

---

## 5. Evidence

| Check | Result |
|---|---|
| `make lite-ci` (end to end, 35 s) | **pass**, including golangci-lint v2 — **0 issues** (F2 resolved) |
| Go tests, Lite + `apps/lite` | 72 top-level, 114 including subtests, **0 failures**, race detector on |
| Frontend tests and gates | 13 files, **74 tests**; bundle gate 4 tests; **0 console warnings** |
| Cross-compile | `GOOS=windows` vet + test binaries; `GOARCH=amd64` vet (Intel Macs) |
| archlint | clean; **all 8 Lite rules seen failing** on every run |
| **Mizan, after L0's shared changes** | Go: **68 packages pass, 0 failures** (race). Frontend: **337 tests pass**. archlint clean. |
| macOS `.app` | universal (`x86_64 arm64`); built plist: `com.mizanerp.lite`, `LSMinimumSystemVersion 13.0.0`, `0.1.0` |
| Windows `.exe` | `PE32+ x86-64 GUI`, `CGO_ENABLED=0`, version resource `Mizan Lite 0.1.0`, manifest `com.mizanerp.lite`, per-monitor DPI |

### The packaged macOS app, run for real

Launched with a data directory named **`محمد #1 data`** — Arabic, a space, and the `#` of F1:

- the database was created at exactly that path, migrated, and snapshotted before migrating;
- **the scheduled backup ran within a second of launch**, not a day later;
- the log records `"health requested by the frontend"` — **the packaged webview reached Go through
  `window.go.api`**, the property Mizan's 10.17 shipped without;
- a **second launch exited by itself** and one process remained (single-instance lock);
- a **quit removed the `-wal` and `-shm` files**, which SQLite does only when every connection closed cleanly.

---

## 6. Mutation drills — 37, every one caught

A drill plants the defect a test exists to catch and requires the test to fail. **A mutation that did not
compile was refused, not counted** — the drill script was hardened for that after the first round.

| # | Planted defect | Caught by |
|---|---|---|
| 1 | DSN escapes only spaces | `TestTheDatabaseOpensAtExactlyThePathGiven` (4 of 10 cases) |
| 2 | a column default changed in Lite's copy of `jobs` | `TestPlatformTablesMatchMizan` |
| 3 | a migration file with CRLF | `TestMigrationFilesUseLF` |
| 4 | `PeekLocale` ignores the stored value | `TestUpdatePersistsAcrossReopening` |
| 5 | `Save` always inserts | the store contract, on SQLite |
| 6 | the fake returns its internal map | the store contract, on the fake |
| 7 | `Apply` writes an unchanged value | `TestUpdateWritesOnlyWhatChanged` |
| 8 | no close-time backup | `TestShutdownTakesAClosingSnapshotOnlyWhenNoneIsRecent` |
| 9 | the database never closed | `TestShutdownIsSafeToCallTwiceAndReleasesTheFile` |
| 10 | the error wrapper drops the backup path | `TestAWrappedFailureKeepsTheCausesParameters` |
| 11 | the backup job not declared | `TestAFreshInstallationStartsMigratedAndUsable` |
| 12 | settings not read at launch | **survived first** → `TestAnUnreadableSettingsTableIsFoundAtLaunch` written |
| 13 | a graph finished after close is attached | `TestAWindowClosedDuringBootStillReleasesTheDatabase` |
| 14 | the middleware never rewrites | `TestTheDocumentIsServedInTheStoredLanguage` (2 invalid mutations refused first) |
| 15 | shutdown inherits a cancellation | **survived first** → `TestClosingWithACancelledContextStillTakesTheClosingBackup` written |
| 16 | G0: a façade left out of `Bindings()` | `TestEveryFacadeIsBound` |
| 17 | G2: a bound method the client never references | `TestEveryBoundMethodHasAClientFunction` |
| 18 | a Lite error code with no translation | `TestEveryReachableErrorCodeIsTranslated` |
| 19 | a **Mizan** code with no translation, after F6's skip | Mizan's `TestEveryErrorCodeHasATranslation` |
| 20 | G3: a client function with no caller | G3 |
| 21 | G1: `(window as …)["go"]` | **survived first** → rule rewritten on the property (F3) |
| 22–24 | G1: an alias `w.go` (gate and lint); a `wailsjs` import outside the client (lint) | G1 / ESLint |
| 25 | G4: a screen nobody routed | G4 |
| 26 | literal text in a component | the no-hardcoded-text gate |
| 27 | a key missing from English | parity |
| 28 | a test that ends before its effects settle | the console gate |
| 29 | G5: the fake client wired into `main.tsx`, then built | G5 on the bundle — **after** the sentinel was moved into live data; as an unused constant, tree-shaking would have removed it and the gate could never fire |
| 30–37 | one violation per Lite archlint rule | archlint, on every `lite-ci` (the `time.Now` plant was first a reference, not a call — see the script) |

---

## 7. What is NOT verified

Stated plainly, as every Mizan phase review did.

1. **The Windows build has never run.** It builds, and its type, architecture, version resource, manifest and
   pure-Go status were read from the binary. There is no Windows machine, VM or Wine here. Specifically
   unproven on Windows: Local AppData resolution on a real profile, WebView2 behaviour, the single-instance
   lock, and file release on close. **WebView2:** Windows 11 includes it; a Windows 10 machine without it
   would try to download it — which fails offline. Embedding the runtime in the installer is L8's job
   (as Mizan's 1.0.0 did).
2. **Nobody has looked at the app.** macOS refused screenshots because this terminal has no Screen Recording
   or Automation permission, and I did not change your privacy settings. The log proves the screen reached
   Go; it does not prove the screen looks right.
3. **The English first frame has not been observed in a real webview.** The middleware and the marker are
   tested; the packaged app was launched in Arabic only.
4. ~~golangci-lint did not run~~ — resolved: v2 runs, 0 issues (F2).
5. **No demo seeder in L0.** There is nothing to seed yet but a language; `cmd/lite-demoseed` starts in L1 with
   the first product.

---

## 8. Definition of Done

| Criterion (design §10, L0) | Status |
|---|---|
| Spike: two Wails projects, one Go module, both platforms | ✅ |
| Composition root, `0001`, own data directory | ✅ — verified in the packaged run |
| archlint rules, each seen failing | ✅ 8 of 8, on every run |
| G0–G5 built against a one-method surface, each seen failing | ✅ |
| Catalogs and parity gates | ✅ |
| Direction before first paint | ✅ tested — **not observed** (§7.3) |
| Backups wired | ✅ scheduled + pre-migration + close-time, verified |
| `make lite-ci` green | ✅ — including golangci-lint v2 (F2 resolved on approval) |
| Packaged macOS app shows `Health` from Go, in Arabic, RTL | ✅ from Go (log) — **the Arabic RTL screen not yet seen** |
| Every invariant mutation-drilled | ✅ 37 of 37 |
| Opened and looked at, in Arabic and English | ⏳ open item O2 — the owner approved L0 with this outstanding |

**L0 is complete.** The owner approved it on 2026-09-13 with golangci-lint upgraded; visual confirmation is carried
as open item O2 in [../PROGRESS.md](../PROGRESS.md), not silently dropped.
