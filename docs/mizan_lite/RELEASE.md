# Releasing Mizan Lite

What one command does, what it cannot check, and the steps that need a person with a machine (L8 §5, §12.4).
For what a release contains, see [phases/L8_RELEASE.md](phases/L8_RELEASE.md); for the shop's own documents, [guide/](guide/SHOP_GUIDE.md).

---

## 1. One command

```bash
make lite-release
```

It runs, in order, and stops at the first failure:

1. `scripts/lite-check.sh` — gofmt, vet, the Windows and Intel-Mac cross-compiles, archlint and its planted drills, the Go tests
   under the race detector, golangci-lint, ESLint, typecheck, the frontend tests and gates, the production bundle, the bundle
   gate, and the end-to-end journeys in a real browser;
2. the shipped guide PDFs are checked against `docs/mizan_lite/guide` — a guide edited and not regenerated fails here;
3. every past schema (1–8) upgrades to this release with every row intact;
4. the macOS `.dmg` and the Windows `Setup.exe` (WebView2 inside);
5. the packaged macOS application is opened on a freshly seeded shop and closed;
6. `SHA256SUMS-<version>.txt` and `MANIFEST-<version>.txt`, which names what was **not** verified here.

Artefacts land in `dist/lite/`.

## 2. Versions

`apps/lite/wails.json` `productVersion` is the number the product claims. A build made exactly on its tag is a release:

```bash
git tag -a lite-v0.9.0 -m "Mizan Lite 0.9.0"
make lite-release          # → dist/lite/Mizan Lite 0.9.0.dmg
```

Anywhere else the version is `<version>-dev.<sha>` and every artefact says so. The About screen, the log's first line and every
backup manifest carry the same string.

## 3. What a release cannot check here

| | Why |
|---|---|
| The Windows installer has never been run | no Windows machine in this build environment — [phases/WINDOWS_PROTOCOL.md](phases/WINDOWS_PROTOCOL.md) is the checklist for the machine that has one |
| Printing on paper | no thermal printer here; the driver and raw paths are exercised against a stand-in queue |
| An export opened in Excel | Excel is not installed; workbooks are read back by the tests and seen in Quick Look |
| Gatekeeper and SmartScreen | both artefacts are unsigned (§4) |

## 4. Signing

Both artefacts ship **unsigned** for the pilot (L8 Q-L8.2). The hooks exist and activate on an identity:

```bash
MIZAN_LITE_MACOS_IDENTITY="Developer ID Application: … (TEAMID)" make lite-package-macos
xcrun notarytool submit "dist/lite/Mizan Lite <version>.dmg" --keychain-profile mizan-lite --wait
xcrun stapler staple "dist/lite/Mizan Lite <version>.dmg"
```

Windows Authenticode is a separate step on a machine holding the certificate; sign both the application and the installer:

```
signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f cert.pfx /p PASSWORD "Mizan Lite <version> Setup.exe"
```

A signed but un-notarized macOS image is still refused by Gatekeeper — Mizan proved that (`docs/RELEASE.md` §2).

## 5. Before handing it to a shop

- [ ] `make lite-release` green, and the manifest read.
- [ ] The visual pack (`build/lite-e2e/screens`) looked through in both languages.
- [ ] [phases/WINDOWS_PROTOCOL.md](phases/WINDOWS_PROTOCOL.md) passed on a real Windows machine.
- [ ] A receipt printed on the shop's printer, and an export opened in Excel.
- [ ] The guides printed: the shop guide and the counter card.
- [ ] For 1.0.0: [phases/PILOT.md](phases/PILOT.md)'s exit criteria met.
