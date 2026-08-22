package buildinfo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// # What this file checks, and what it explicitly cannot
//
// Wails builds an NSIS `.exe` on Windows and a `.dmg` on macOS. **This environment cannot run
// either build**, so nothing here claims the installers work.
//
// What it does claim is that the configuration is complete and internally CONSISTENT: the files
// exist, the version has one source, and the manifests agree with it. That is the difference
// between a criterion that says "installers build" and is ticked without a build, and one that
// states its limit — which is what 10.4's DoD does.
//
// The gap is real and named: an installer that builds and then fails to run is invisible to
// everything below.

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	return root
}

// TestEveryFileAPackagedBuildNeedsIsPresent
//
// A missing icon or manifest fails the build on a release machine, minutes into a job, with an
// error naming a path nobody recognises. Asserting presence here fails it in a second, on the
// developer's machine, naming the file.
func TestEveryFileAPackagedBuildNeedsIsPresent(t *testing.T) {
	root := repoRoot(t)

	for _, required := range []string{
		"wails.json",
		"VERSION",
		"scripts/version.sh",
		"scripts/package-windows.sh",
		"scripts/package-macos.sh",

		// Windows: the icon, the version resource, the manifest, and the NSIS project.
		"build/windows/icon.ico",
		"build/windows/info.json",
		"build/windows/wails.exe.manifest",
		"build/windows/installer/project.nsi",

		// macOS: both plists. The dev one is not optional — `wails dev` uses it, and a missing
		// one breaks the loop everybody works in rather than the release nobody runs yet.
		"build/darwin/Info.plist",
		"build/darwin/Info.dev.plist",
	} {
		if _, err := os.Stat(filepath.Join(root, required)); err != nil {
			t.Errorf("%s is missing, and a packaged build needs it: %v", required, err)
		}
	}
}

// TestTheProductVersionHasOneSource
//
// # DoD criterion 7, and the failure it prevents
//
// `VERSION` is the number the product claims. `scripts/version.sh` derives the build's version
// from it. `wails.json` stamps it into the Windows resource and the macOS bundle.
//
// If `wails.json` drifts from `VERSION`, a Windows user sees one number in Explorer's properties
// and another in the application's own about screen — and a support conversation starts with
// working out which is true.
func TestTheProductVersionHasOneSource(t *testing.T) {
	root := repoRoot(t)

	declared, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatalf("reading VERSION: %v", err)
	}
	version := strings.TrimSpace(string(declared))
	if version == "" {
		t.Fatal("VERSION is empty")
	}

	raw, err := os.ReadFile(filepath.Join(root, "wails.json"))
	if err != nil {
		t.Fatalf("reading wails.json: %v", err)
	}
	var config struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName    string `json:"productName"`
			ProductVersion string `json:"productVersion"`
			CompanyName    string `json:"companyName"`
			Copyright      string `json:"copyright"`
		} `json:"info"`
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parsing wails.json: %v", err)
	}

	if config.Info.ProductVersion != version {
		t.Errorf("wails.json says the product version is %q and VERSION says %q — a Windows "+
			"user would see one number in Explorer and another in the application",
			config.Info.ProductVersion, version)
	}
	for name, value := range map[string]string{
		"name":        config.Name,
		"productName": config.Info.ProductName,
		"companyName": config.Info.CompanyName,
		"copyright":   config.Info.Copyright,
	} {
		if strings.TrimSpace(value) == "" {
			t.Errorf("wails.json has no %s, which becomes an empty field in the installer",
				name)
		}
	}
}

// TestTheWindowsResourceReadsItsVersionFromTheConfig
//
// `build/windows/info.json` is a TEMPLATE. If somebody replaces a placeholder with a literal, the
// resource stops following `wails.json` and the two drift silently — the file still builds, and
// Explorer shows a number nobody updated.
func TestTheWindowsResourceReadsItsVersionFromTheConfig(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "build/windows/info.json"))
	if err != nil {
		t.Fatalf("reading the Windows resource: %v", err)
	}
	body := string(raw)

	for _, placeholder := range []string{
		"{{.Info.ProductVersion}}",
		"{{.Info.CompanyName}}",
		"{{.Info.ProductName}}",
		"{{.Info.Copyright}}",
	} {
		if !strings.Contains(body, placeholder) {
			t.Errorf("the Windows resource no longer reads %s from wails.json, so the two can "+
				"drift with nothing to notice", placeholder)
		}
	}
}

// TestTheMacBundleReadsItsVersionFromTheConfig
func TestTheMacBundleReadsItsVersionFromTheConfig(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "build/darwin/Info.plist"))
	if err != nil {
		t.Fatalf("reading the macOS bundle plist: %v", err)
	}
	body := string(raw)

	for _, key := range []string{
		"CFBundleShortVersionString",
		"CFBundleVersion",
		"CFBundleIdentifier",
		"LSMinimumSystemVersion",
	} {
		if !strings.Contains(body, key) {
			t.Errorf("the macOS bundle declares no %s, which macOS needs to install and update "+
				"the application", key)
		}
	}
	if !strings.Contains(body, "{{.Info.ProductVersion}}") {
		t.Error("the macOS bundle no longer reads its version from wails.json")
	}

	// # The bundle identifier is the product's, not the framework's
	//
	// Wails scaffolds `com.wails.{{safeBundleID .Name}}`, and that is what the FIRST .dmg this
	// project built actually carried — found by inspecting the mounted image rather than by any
	// test, because nothing was checking.
	//
	// It is not cosmetic. macOS keys preferences, keychain entries, TCC permissions and Gatekeeper
	// records against it, so `com.wails.*` puts this application's state in a framework's
	// namespace, where a second Wails application with the same product name collides with it.
	// And it can never change after a release: everything keyed to it would be orphaned.
	// Read the VALUE, not the file. The first version matched the whole body and failed on the
	// plist's own comment, which explains why `com.wails.` is wrong — the same mistake 9.4's
	// no-SQL check made against its own doc comment, in a second place.
	identifier := plistString(t, body, "CFBundleIdentifier")
	if strings.HasPrefix(identifier, "com.wails.") {
		t.Errorf("the bundle identifier is %q — the Wails scaffold's namespace. macOS keys "+
			"preferences, keychain and permissions against it, and it can never change once "+
			"something has shipped", identifier)
	}
	if !strings.HasPrefix(identifier, "com.mizanerp.") {
		t.Errorf("the bundle identifier is %q, not the product's own reverse-DNS namespace",
			identifier)
	}
}

// plistString reads the <string> following a <key>, which is all the structure this needs.
//
// A full plist parser would be a dependency for one lookup in one test, and this file's whole
// point is checking configuration without adding any.
func plistString(t *testing.T, body, key string) string {
	t.Helper()
	at := strings.Index(body, "<key>"+key+"</key>")
	if at < 0 {
		t.Fatalf("the plist has no %s", key)
	}
	open := strings.Index(body[at:], "<string>")
	closed := strings.Index(body[at:], "</string>")
	if open < 0 || closed < 0 || closed < open {
		t.Fatalf("%s has no value", key)
	}
	return body[at+open+len("<string>") : at+closed]
}

// TestSigningIsOptInAndTheBuildDoesNotNeedIt
//
// # What Phase 0 actually decided, and what the first version of this test asserted instead
//
// Phase 0 said signing is out of scope and the hooks stay no-ops. The first version read that as
// "the scripts must not mention codesign" and failed — because `package-macos.sh` DOES invoke it,
// guarded by `MIZAN_MACOS_IDENTITY`.
//
// That guard is what makes it a no-op. An unset variable means the build produces an unsigned
// `.dmg` and says so; a set one means somebody with a certificate opted in. The script was right
// and the test was wrong.
//
// What the criterion actually forbids is signing being REQUIRED: a build that needs a certificate
// works for its author and for nobody else. So this asserts every signing invocation sits inside
// a guard, and that the script does not exit non-zero without one.
func TestSigningIsOptInAndTheBuildDoesNotNeedIt(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts/package-macos.sh"))
	if err != nil {
		t.Fatalf("reading the macOS packaging script: %v", err)
	}

	lines := strings.Split(string(raw), "\n")
	guardDepth := 0
	for number, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// A crude block tracker: enough to know whether a signing line sits inside an
		// identity check, which is the only thing being asserted.
		if strings.HasPrefix(trimmed, "if ") && strings.Contains(trimmed, "MIZAN_MACOS_IDENTITY") {
			guardDepth++
			continue
		}
		if trimmed == "fi" && guardDepth > 0 {
			guardDepth--
			continue
		}
		for _, signing := range []string{"codesign ", "productsign ", "xcrun notarytool"} {
			if strings.HasPrefix(trimmed, signing) && guardDepth == 0 {
				t.Errorf("scripts/package-macos.sh:%d invokes %s outside an identity guard — "+
					"a build that needs a certificate works for its author and nobody else",
					number+1, strings.TrimSpace(signing))
			}
		}
	}

	// And the guard is the one the rest of the project names, so a rename cannot silently make
	// signing mandatory.
	if !strings.Contains(string(raw), "MIZAN_MACOS_IDENTITY") {
		t.Error("the macOS script has no identity guard at all")
	}

	// Windows is unsigned outright: there is no signtool invocation to guard.
	windows, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts/package-windows.sh"))
	if err != nil {
		t.Fatalf("reading the Windows packaging script: %v", err)
	}
	for _, line := range strings.Split(string(windows), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, "signtool") {
			t.Error("scripts/package-windows.sh signs the binary; Phase 0 decided it does not")
		}
	}
}

// TestTheCustomisedBuildAssetsAreTracked
//
// # The fix that would have been silently discarded
//
// Phase 0 ignored `/build/darwin/` as stock Wails scaffolding and left a note: *"Phase 10 is where
// the real icon and a customised Info.plist become tracked assets — REMOVE THESE TWO LINES THEN,
// or the customisation will be silently ignored."*
//
// 10.7 corrected the bundle identifier in that very file. Had the note gone unread, the fix would
// have lived in an untracked file that `wails build` regenerates: present on one machine, absent
// from the repository, and gone on the next clean checkout — while every test above kept passing,
// because they read the working copy.
//
// The note was found by `git status` failing to list a file that had definitely been edited. This
// test is what makes the next one unnecessary.
func TestTheCustomisedBuildAssetsAreTracked(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	for _, line := range strings.Split(string(raw), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		for _, customised := range []string{"/build/darwin/", "/build/appicon.png"} {
			if entry == customised {
				t.Errorf(".gitignore still ignores %s, which now holds customisation — the "+
					"bundle identifier fix would live in a file `wails build` regenerates, "+
					"present on one machine and absent from the repository", customised)
			}
		}
	}
}

// TestTheWindowsInstallerChecksForWebView2
//
// # The single most likely first-run failure on a fresh Windows
//
// A Wails application IS a WebView2 host. Without the runtime it does not start — it shows nothing
// and exits, which reads to a user as "the program is broken" rather than "a component is
// missing".
//
// Windows 11 ships WebView2. Windows 10 usually has it through Edge, and "usually" is not a
// guarantee anybody can act on. So the installer must check and install it, and this asserts the
// check is still there — a Wails upgrade regenerating `wails_tools.nsh` could silently drop it.
func TestTheWindowsInstallerChecksForWebView2(t *testing.T) {
	root := repoRoot(t)

	tools, err := os.ReadFile(filepath.Join(root, "build/windows/installer/wails_tools.nsh"))
	if err != nil {
		t.Fatalf("reading the installer tools: %v", err)
	}
	body := string(tools)

	// BOTH registry scopes. A per-user install of Mizan on a machine where WebView2 was installed
	// per-machine — or the reverse — would otherwise reinstall it, or worse, conclude it is
	// absent and fail.
	for _, key := range []string{
		`SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients`, // machine-wide
		`Software\Microsoft\EdgeUpdate\Clients`,             // per-user
	} {
		if !strings.Contains(body, key) {
			t.Errorf("the installer does not check %s for WebView2", key)
		}
	}
	if !strings.Contains(body, "MicrosoftEdgeWebview2Setup.exe") {
		t.Error("the installer has no WebView2 installer to fall back on")
	}
	if !strings.Contains(body, "/silent /install") {
		t.Error("the WebView2 install is not silent, so it would interrupt the installer")
	}

	// And the file is actually there. A reference to a missing file fails the NSIS build, but it
	// fails it minutes in, naming a path rather than the cause.
	bootstrapper := filepath.Join(root, "build/windows/installer/tmp/MicrosoftEdgeWebview2Setup.exe")
	info, err := os.Stat(bootstrapper)
	if err != nil {
		t.Fatalf("the WebView2 installer is not bundled: %v", err)
	}

	// # Which installer is bundled, and why the size is asserted
	//
	// Microsoft ships two. The BOOTSTRAPPER is ~1.7MB and downloads the runtime at install time;
	// the EVERGREEN STANDALONE installer is ~130MB and needs no network.
	//
	// This bundles the bootstrapper, so **installing Mizan on a Windows machine that lacks
	// WebView2 requires an internet connection once** — which sits awkwardly with an
	// offline-first product, and is documented in docs/RELEASE.md §3 rather than left for a
	// shopkeeper to discover.
	//
	// The size check is what makes a switch to the standalone installer VISIBLE: swapping the
	// file without updating this test and the documentation would change the offline story
	// silently.
	const bootstrapperMax = 8 << 20
	if info.Size() > bootstrapperMax {
		t.Errorf("the bundled WebView2 installer is %d bytes — larger than a bootstrapper, so "+
			"this is probably the offline standalone one. That is a legitimate change and it "+
			"changes what the product promises about installing without a network: update this "+
			"test and docs/RELEASE.md §3 together", info.Size())
	}
}
