# Releasing Mizan

What is verified, what is not, and the steps that need a person with an account and a machine.

---

## 1. What a release build produces

```bash
make ci                       # must be green first
make package-macos            # .app → .dmg
./scripts/package-windows.sh  # .exe → NSIS Setup.exe
cd dist && shasum -a 256 * > SHA256SUMS.txt
```

| Artefact | Size | What it is |
|----------|------|------------|
| `Mizan 1.0.0.dmg` | 15M | macOS disk image, Universal (`x86_64` + `arm64`) |
| `Mizan 1.0.0 Setup.exe` | 211M | Windows NSIS installer, **WebView2 runtime embedded** |
| `Mizan 1.0.0.exe` | 20M | bare Windows binary, portable install |
| `SHA256SUMS.txt` | — | checksums, verified with `shasum -a 256 -c` |

The Windows build cross-compiles from macOS because Step 0.3 chose a pure-Go SQLite driver.

---

## 2. What has been verified, and how

### macOS — first run, verified on real hardware

The `.dmg` was mounted, the `.app` copied out, and launched **against a clean data directory**.
Observed:

- 37 migrations applied, in order, from an empty database
- a pre-migration backup taken and verified before the first migration
- **65 permissions synced**
- **13 number series created** — the Phase 7.6 defect, confirmed fixed in a shipped artefact
- 14 modules started; `mizan ready`
- the scheduled backup job fired, writing a snapshot **and its manifest**
- the manifest recorded `"appVersion": "1.0.0"` — the Phase 10.4 fix, confirmed end to end
- **6 background jobs registered**, including the daily backup
- **zero errors in the log**
- resulting database: 96 tables, 13 number series, 65 permissions

### macOS — the signing hook, verified with an ad-hoc identity

`MIZAN_MACOS_IDENTITY=- ./scripts/package-macos.sh` was run end to end:

1. `codesign --force --deep --options runtime --timestamp --sign` — succeeded
2. `codesign --verify --strict` — *valid on disk, satisfies its Designated Requirement*
3. the `.dmg` built and was itself signed
4. the notarization commands printed

The signature verifies and carries the right identifier (`com.mizanerp.desktop`).

**And Gatekeeper still rejects it.** `spctl --assess --type execute` returns `rejected`, because
the signature is `adhoc` with `TeamIdentifier=not set`.

That is the finding worth carrying: **signing is necessary and not sufficient.** A Developer ID
signature *and* notarization are both required. §4 below is not optional paperwork.

### Windows — NOT verified, and cannot be from here

The artefacts are structurally correct — `PE32 … Nullsoft Installer self-extracting archive` and
`PE32+ x86-64`, both naming the product and version — but **no Windows machine, VM, or Wine
runtime exists in the build environment.** The installer has never been executed.

This includes the offline WebView2 path. That the runtime is *embedded* is verified here — by the
211MB artefact and by `TestTheWindowsInstallerChecksForWebView2` reading the macro — but that it
*installs* is not, and cannot be. The unplugged-cable check in §3 is the only thing that proves
it, and it needs a Windows machine.

§3 is the checklist for the person who has a Windows machine.

---

## 3. Before shipping: the manual checks

Neither can be done from the build environment. Both need a machine that has never had Mizan
installed.

### Windows

- [ ] Copy `Mizan <version> Setup.exe` to a clean Windows 10 or 11 machine.
- [ ] Run it. **SmartScreen will warn** ("Windows protected your PC") until §4 is done — choose
      *More info → Run anyway*.
- [ ] Confirm the installer offers a sensible path, creates a Start-menu entry, and completes.
- [ ] Launch Mizan. **Confirm it opens with the network cable unplugged.** WebView2 is the one
      dependency Mizan cannot ship inside its own binary, and it is the single most likely
      first-run failure on a fresh Windows — so the installer carries the whole runtime:

      `build/windows/webview2/MicrosoftEdgeWebView2RuntimeInstallerX64.exe` (213MB) is Microsoft's
      **Evergreen Standalone Installer**, embedded by the `mizan.webview2offline` macro in
      `project.nsi`. It is what takes the installer from 9.4MB to 211MB, and it is why that trade
      was made: a shop with no reliable connection must still be able to install from a USB stick.

      The macro checks the machine-wide key, then the per-user key, and only runs the embedded
      installer (silently, `/silent /install`) when neither is present. A machine that already has
      the runtime — every Windows 11, and most Windows 10 through Edge — installs in seconds and
      touches nothing.

      **Nothing here reaches the network.** That is the property to test, and unplugging is the
      only way to test it: a build machine with a connection cannot tell a bundled runtime from a
      downloaded one.
- [ ] Confirm the setup wizard appears and a company can be provisioned.
- [ ] Confirm `%APPDATA%\Mizan` holds `mizan.db` and a `backups` folder with a manifest.
- [ ] Uninstall from Add/Remove Programs. Confirm it removes cleanly and **leaves the data
      directory alone** — an uninstaller that deletes a shop's books is unrecoverable.

### macOS, on a machine that is not the build machine

- [ ] Copy the `.dmg` to a clean Mac **via a download or AirDrop**, so it carries the
      `com.apple.quarantine` attribute. Copying it locally does not, and the build machine's own
      copy therefore never faces Gatekeeper.
- [ ] Confirm Gatekeeper blocks it, and that right-click → *Open* → *Open* works as the guide
      says.
- [ ] Confirm the app launches, provisions, and writes to `~/Library/Application Support/Mizan`.
- [ ] Confirm it runs on **Apple Silicon and Intel** — the binary is Universal and only a real
      machine of each proves it.

---

## 4. Signing — the two things only the owner can do

Both artefacts ship **unsigned**. The hooks exist, are tested, and activate on an identity being
present. What is missing is a certificate, and a certificate is tied to a legal identity, a
payment, and an enrolment that cannot be done on someone's behalf.

### macOS: Apple Developer ID + notarization

1. Enrol in the Apple Developer Program (~$99/year) as the legal entity shipping Mizan.
2. Create a **Developer ID Application** certificate and install it in the login keychain.
3. Store an app-specific password as a keychain profile:
   ```bash
   xcrun notarytool store-credentials mizan \
     --apple-id "you@example.com" --team-id "ABCDE12345" --password "app-specific-password"
   ```
4. Build with the identity set:
   ```bash
   MIZAN_MACOS_IDENTITY="Developer ID Application: Your Name (ABCDE12345)" make package-macos
   ```
5. Notarize and staple, using the commands the script prints:
   ```bash
   xcrun notarytool submit "dist/Mizan <version>.dmg" --keychain-profile mizan --wait
   xcrun stapler staple "dist/Mizan <version>.dmg"
   ```
6. Confirm: `spctl --assess --type execute --verbose "dist/Mizan <version>.dmg"` must print
   **accepted**. Anything else means a user still sees a warning.

**Step 5 is not optional.** A signed but un-notarized image is still rejected — proven above.

### Windows: Authenticode

1. Buy a code-signing certificate from a CA (DigiCert, Sectigo, SSL.com). An **EV** certificate
   clears SmartScreen immediately; a standard OV one builds reputation over weeks or months of
   downloads, during which users still see the warning.
2. Sign both the application and the installer — signing only the installer leaves the extracted
   binary unsigned:
   ```bash
   signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 \
     /f cert.pfx /p PASSWORD "Mizan <version>.exe"
   signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 \
     /f cert.pfx /p PASSWORD "Mizan <version> Setup.exe"
   ```
   `/tr` timestamps the signature so it stays valid after the certificate expires. Without it the
   binary stops verifying the day the certificate does.
3. Verify: `signtool verify /pa /v "Mizan <version> Setup.exe"`.

There is deliberately **no `signtool` invocation in `scripts/package-windows.sh`**: it would fail
on every machine without a certificate, and `TestSigningIsOptInAndTheBuildDoesNotNeedIt` asserts
its absence. Sign as a separate step on a Windows machine that holds the certificate.

---

## 5. Versioning

`VERSION` holds the number the product claims. `scripts/version.sh` derives the build's version:

- on an exact tag: `0.1.0` — a release
- anywhere else: `0.1.0-dev.<sha>` — a development build, and it says so

**A release therefore requires a tag.** Without one, every artefact is marked `-dev`, which is
correct and deliberate: a binary must never be mistaken for a release it is not.

```bash
git tag -a v0.1.0 -m "Mizan 0.1.0"
make ci && make release
```

---

## 6. Release checklist

- [ ] `make ci` green
- [ ] `VERSION` bumped and committed
- [ ] tag created, so artefacts are not marked `-dev`
- [ ] `make release` — both platforms from a clean `build/bin` and `dist`
- [ ] `shasum -a 256 * > SHA256SUMS.txt`
- [ ] macOS signed **and notarized**; `spctl --assess` prints *accepted*
- [ ] Windows signed with a timestamp; `signtool verify /pa` passes
- [ ] §3's manual checks done on clean machines of both platforms
- [ ] `docs/architecture/KNOWN_GAPS.md` reviewed — nothing there has become urgent
