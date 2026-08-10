# Step 1.13 — Packaging, branding, and distribution (Design + implementation record)

> Status: **IMPLEMENTED.** `make ci` green. Three artefacts produced and verified:
> `Mizan ERP 0.1.0-dev.<sha>.exe`, `… Setup.exe`, `… .dmg`.
> Scope: the application icon; Windows and macOS release pipelines; the version scheme; dropping
> Linux.
> **Out of scope:** code-signing certificates (they need the owner's identity and money — §4);
> auto-update (Phase 10).

---

## 1. ANALYSIS

### 1.1 Why this comes before Phase 2

Release infrastructure built at the end always reveals problems that would have been cheap to
fix earlier — a driver that needs cgo, a bundle name baked into fifty places, an icon nobody can
regenerate. Building it now, while the application is small, means the next eight phases each
ship into a pipeline that already works.

It is also unblocked: nothing about packaging waits on the tax decision Phase 2 needs.

### 1.2 Linux is dropped — D1

The directive is two platforms, and the technical case agrees with it.

Windows and macOS differ from Linux in one decisive way: **cgo**. Wails on Windows uses a pure-Go
WebView2 loader, and Step 0.3 chose `modernc.org/sqlite` — a pure-Go driver — for the offline
constraint. Nothing in the Windows path needs a C compiler, so it cross-builds from a Mac in
about six seconds. macOS builds natively.

Linux needs cgo and `webkit2gtk` headers, and the Wails CLI says so outright:

```
WARNING  Crosscompiling to Linux not currently supported.
```

Supporting it would mean a second build machine or a container in the release path, permanently,
for a platform nobody has asked for. Dropped.

**One machine now produces every shippable artefact**, which is worth more than the third
platform.

### 1.3 Risks

| Risk | Consequence | Answer |
|---|---|---|
| An icon nobody can regenerate | A 1024px PNG blob rots; nobody dares touch it | The icon is a *script* (§2) |
| A version that is a commit hash | `Mizan ERP 2050917 Setup.exe`, and a file-version field Explorer cannot parse | A real version scheme (§3) — this actually happened |
| Unsigned binaries shipped unknowingly | Users hit SmartScreen/Gatekeeper and assume malware | Every script prints the warning; hooks are ready (§4) |
| The bundle named `mizan.app` | Finder shows a different product name than the app does | Fixed, and caught only by looking (§5.2) |
| A signed app inside an unsigned image | The signature is on a bundle nobody ships | Signing ordered before packaging (§5.3) |

---

## 2. DESIGN — the icon

### 2.1 The mark

**Mizan** (ميزان) is Arabic for *balance* — a scale. The name is the mark: a beam, a column, a
pivot, two pans, in white on a gradient that runs from the product's own `--color-primary`
(blue-600) into blue-950, so the icon and the application are visibly the same thing.

It carries no currency symbol, deliberately. The product assumes no country (Addendum §C), and a
`$` or `﷼` in the icon would contradict that on the very first thing a customer sees. The pans
read as containers, which is the inventory half of the product.

### 2.2 The icon is a script, not an asset — D2

`scripts/icon.py` renders the master PNG from ~20 numbers, and `scripts/icons.sh` derives every
platform size from it. Two reasons:

- **A binary blob rots.** A committed 1024px PNG is an asset nobody can adjust in five years
  without the original design file, which is always lost.
- **The machine has no imaging library.** No ImageMagick, no Pillow, no rsvg-convert. Rather
  than add a dependency to a project that vendors for offline builds, the script writes PNG
  directly with `zlib`, draws with signed distance fields for analytic anti-aliasing, and leans
  on macOS's own `sips` for resampling.

The `.ico` is packed by the same script, because macOS ships nothing that writes one. ICO has
accepted embedded PNG since Vista, which makes the container a header, one 16-byte directory
entry per image, and the PNG bytes verbatim.

An SVG is emitted from the same constants, for anyone who wants to edit in a real tool — but
generated, never hand-maintained, so the two cannot disagree.

---

## 3. DESIGN — the version scheme

`git describe --tags --always` was used until this step. With no tags in the repository it
returns a bare commit hash, and the first installer built was called
**`Mizan ERP 2050917 Setup.exe`**.

`scripts/version.sh` replaces it:

| State | Version |
|---|---|
| HEAD is exactly a tag | `0.1.0` — a release, and it says so |
| Anything else | `0.1.0-dev.2050917` — a development build, and it says THAT |

`VERSION` at the repository root is the number the product claims. The rule that matters: **a
binary can never be mistaken for a release it is not.**

---

## 4. Signing — designed, not done

Both scripts have signing hooks that are **no-ops until credentials exist**, deliberately: an
unsigned build must remain producible on a clean machine, or nobody can test the packaging
without a paid account.

| Platform | What is needed | Without it |
|---|---|---|
| **Windows** | An Authenticode code-signing certificate (OV ≈ $200–400/yr, EV higher) | SmartScreen warns on first run; users must click through "unrecognised app" |
| **macOS** | An Apple Developer ID ($99/yr), then notarization | Gatekeeper **refuses to open the app**; users must right-click → Open past a warning |

macOS is the harsher of the two: Windows warns, Gatekeeper blocks.

To sign macOS, set `MIZAN_MACOS_IDENTITY` to a `Developer ID Application: …` identity and re-run
`make package-macos`; the script signs the bundle, builds the image, signs the image, and prints
the two `notarytool` commands to finish.

**This is the one part of distribution I cannot do**: both require the owner's legal identity and
payment.

---

## 5. IMPLEMENTATION RECORD

### 5.1 What was built

| File | What |
|---|---|
| `scripts/icon.py` | The mark, the PNG encoder, the ICO packer, the SVG emitter |
| `scripts/icons.sh` | Master → every platform size |
| `scripts/version.sh` | The version scheme |
| `scripts/package-windows.sh` | Cross-build + NSIS installer + signing hook |
| `scripts/package-macos.sh` | Universal build → signed bundle → `.dmg` → signed image |
| `VERSION` | `0.1.0` |
| `Makefile` | `icons`, `build-windows`, `build-macos`, `package-macos`, `release` |
| `wails.json` | Bundle name, company, copyright, product version |

### 5.2 Three defects found by looking at the output

**The first icon had cords.** Two short bars from the beam down to each pan — physically what a
scale has. On screen they were wrong: a cord ends at the *middle* of a bowl, which is its
opening, not its rim, so each one dangled into empty space. Attaching to the rim instead would
have put two thin diagonals into an icon that must survive being 16 pixels wide. Removed; the
eye completes the connection, and the mark is three shapes instead of five.

**The bundle was named `mizan.app`.** `-o "Mizan ERP"` names the *binary inside* the bundle;
Wails names the bundle itself from `wails.json`'s `name`, which was still the project slug. A
user would have seen "mizan" in Finder and in Applications — the one place a product's name is
least negotiable. Only visible by listing `build/bin` after a build.

**The version was a commit hash** (§3).

### 5.3 A bug in the signing order, caught before it could ship

The first draft of `package-macos.sh` created the `.dmg`, then signed the `.app`. That signs a
bundle nobody ships: the image already contains an unsigned copy. Reordered — sign the bundle,
build the image from it, then sign the image.

Worth recording because it would have been invisible until someone with a certificate ran it and
Gatekeeper rejected an apparently-signed build.

### 5.4 Verified, not assumed

Every claim here was checked against a produced artefact:

| Claim | Evidence |
|---|---|
| Windows cross-builds from macOS | `file` → `PE32+ executable (GUI) x86-64, for MS Windows` |
| The NSIS installer builds | `dist/Mizan ERP 0.1.0-dev.2050917 Setup.exe`, 8.4 MB |
| macOS is a genuine universal binary | `lipo -archs` → `x86_64 arm64` |
| The DMG mounts and is drag-to-install | `hdiutil attach` → `Mizan ERP.app` beside `Applications ->` |
| The shipped bundle carries the new icon | extracted `iconfile.icns` from the built `.app` and rendered it |
| Linux genuinely cannot cross-compile | the Wails CLI refuses it by name |

### 5.5 Carried forward

- **Signing certificates** (§4) — the only blocker to a distributable release.
- **Auto-update.** Not designed. Wails has no built-in updater; the realistic options are a
  self-hosted feed or a manual download page. Phase 10, and it should be decided before the
  first customer install, because retrofitting an updater onto deployed copies is painful.
- **Windows 32-bit and ARM** are not built. `windows/amd64` covers effectively every business PC;
  adding `windows/arm64` is one line if it is ever asked for.
- **`make release` runs `ci` first**, so no artefact is ever produced from a tree that does not
  pass its own checks.
