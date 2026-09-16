package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// plistString reads the VALUE of a key from an Info.plist, skipping a comment between the key and
// its value. Mizan 10.7 D1: an identifier check that greps the file for the expected string passes
// when the string appears in a comment and the real value is still the framework's.
func plistString(t *testing.T, file, key string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	re := regexp.MustCompile(`(?s)<key>` + regexp.QuoteMeta(key) + `</key>\s*(?:<!--.*?-->\s*)?<string>([^<]*)</string>`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("%s has no string value for %s", file, key)
	}
	return m[1]
}

func TestTheMacIdentityIsLitesOwn(t *testing.T) {
	for _, file := range []string{
		filepath.Join("build", "darwin", "Info.plist"),
		filepath.Join("build", "darwin", "Info.dev.plist"),
	} {
		t.Run(file, func(t *testing.T) {
			if got := plistString(t, file, "CFBundleIdentifier"); got != bundleID {
				t.Fatalf("CFBundleIdentifier = %q, want %q", got, bundleID)
			}
			mizan := plistString(t, filepath.Join("..", "..", "build", "darwin", "Info.plist"), "CFBundleIdentifier")
			if mizan == bundleID {
				t.Fatalf("Lite and Mizan share the bundle identifier %q", mizan)
			}
		})
	}
}

func TestTheMacMinimumMatchesWhatGoBuilds(t *testing.T) {
	// Go 1.27's object code targets macOS 13. A lower declared minimum lets an older Mac launch a
	// binary that then crashes, instead of refusing it with an explanation.
	for _, file := range []string{
		filepath.Join("build", "darwin", "Info.plist"),
		filepath.Join("build", "darwin", "Info.dev.plist"),
	} {
		got := plistString(t, file, "LSMinimumSystemVersion")
		major, err := strconv.Atoi(strings.SplitN(got, ".", 2)[0])
		if err != nil || major < 13 {
			t.Errorf("%s: LSMinimumSystemVersion = %q, want 13 or later", file, got)
		}
	}
}

func TestTheWindowsManifestIsNotTheFrameworksIdentity(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("build", "windows", "wails.exe.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`assemblyIdentity type="win32" name="([^"]+)"`).FindStringSubmatch(string(raw))
	if m == nil || m[1] != bundleID {
		t.Fatalf("assembly identity = %v, want %q", m, bundleID)
	}
	if !strings.Contains(string(raw), "permonitorv2") {
		t.Error("the manifest lost per-monitor DPI awareness; text would blur on scaled Windows displays")
	}
}

func TestWailsConfigNamesTheProduct(t *testing.T) {
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName string `json:"productName"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Mizan Lite" || cfg.OutputFilename != "Mizan Lite" || cfg.Info.ProductName != "Mizan Lite" {
		t.Fatalf("wails.json = %+v", cfg)
	}
	for _, required := range []string{
		filepath.Join("build", "windows", "icon.ico"),
		filepath.Join("build", "windows", "info.json"),
		filepath.Join("build", "appicon.png"),
	} {
		if _, err := os.Stat(required); err != nil {
			t.Errorf("%s is missing; Wails would scaffold a default and the build would carry it: %v", required, err)
		}
	}
}

// ── L8: the packaging (§5) ────────────────────────────────────────────────────────────────────────

func liteRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatalf("reading %v: %v", parts, err)
	}
	return string(raw)
}

// TestVersionsAgree: one version source (D-L8.10). The installer, the .app and the About screen all take
// apps/lite/wails.json's productVersion — through templates and ldflags, never typed twice.
func TestVersionsAgree(t *testing.T) {
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(cfg.Info.ProductVersion) {
		t.Fatalf("productVersion = %q, want three numbers", cfg.Info.ProductVersion)
	}
	for _, key := range []string{"CFBundleVersion", "CFBundleShortVersionString"} {
		if got := plistString(t, filepath.Join("build", "darwin", "Info.plist"), key); got != "{{.Info.ProductVersion}}" {
			t.Errorf("%s = %q — it must come from wails.json, not be typed", key, got)
		}
	}
	version := liteRepoFile(t, "scripts", "lite-version.sh")
	if !strings.Contains(version, `apps/lite/wails.json`) || !strings.Contains(version, "lite-v") {
		t.Error("scripts/lite-version.sh must read wails.json and require a lite-v tag for a release")
	}
	for _, script := range []string{"lite-package-macos.sh", "lite-package-windows.sh"} {
		if body := liteRepoFile(t, "scripts", script); !strings.Contains(body, "scripts/lite-version.sh") {
			t.Errorf("scripts/%s does not take its version from lite-version.sh", script)
		}
	}
}

// TestTheInstallerKeepsTheShopsData and its neighbours hold what the Windows installer must do (D-L8.11) — on a machine that
// cannot run it, the script itself is the evidence.
func TestTheWindowsInstallerIsTheOneL8Describes(t *testing.T) {
	nsi := liteRepoFile(t, "apps", "lite", "build", "windows", "installer", "project.nsi")
	for _, must := range []struct{ what, contains string }{
		{"install WebView2 from the bundled runtime", `File "..\..\..\..\..\build\windows\webview2\MicrosoftEdgeWebView2RuntimeInstallerX64.exe"`},
		{"fail loudly when WebView2 cannot be installed", "Abort \"WebView2 runtime installation failed"},
		{"refuse to install over a running application", `com.mizanerp.litesim`},
		{"refuse to uninstall while it runs", "!insertmacro lite.refuseWhileRunning"},
		{"offer Arabic", `!insertmacro MUI_LANGUAGE "Arabic"`},
		{"say where the data stayed", "$LOCALAPPDATA\\Mizan Lite"},
	} {
		if !strings.Contains(nsi, must.contains) {
			t.Errorf("the installer does not %s", must.what)
		}
	}
	// The one line that would be unrecoverable: an uninstaller that deletes the shop's books.
	for _, forbidden := range []string{`RMDir /r "$LOCALAPPDATA\Mizan Lite"`, `RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"`, `RMDir /r "$LOCALAPPDATA"`} {
		if strings.Contains(nsi, forbidden) {
			t.Errorf("the uninstaller deletes the shop's data: %s", forbidden)
		}
	}
	build := liteRepoFile(t, "scripts", "lite-package-windows.sh")
	if !strings.Contains(build, "100000000") || !strings.Contains(build, "fetch-webview2.sh") {
		t.Error("scripts/lite-package-windows.sh must refuse a bootstrapper-sized WebView2 runtime")
	}
}

// TestSigningIsOptIn: a build never needs a certificate (D-L8.13). Mizan proved the opposite costs a day: a packaging script
// that signs unconditionally cannot be run by anyone without the identity.
func TestSigningIsOptIn(t *testing.T) {
	mac := liteRepoFile(t, "scripts", "lite-package-macos.sh")
	if !strings.Contains(mac, `if [ -n "${MIZAN_LITE_MACOS_IDENTITY:-}" ]`) {
		t.Error("the macOS packaging script must sign only when an identity is set")
	}
	if strings.Contains(mac, "notarytool submit \"$DMG\"") {
		t.Error("the script must PRINT the notarization commands, not run them")
	}
	win := liteRepoFile(t, "scripts", "lite-package-windows.sh")
	if strings.Contains(win, "signtool") {
		t.Error("signtool must not be in the Windows build: it fails on every machine without a certificate")
	}
}

// TestEveryModuleHasALicence: the notices shipped in the About screen name every module this build is made from (D-L8.15).
// The OFL requires the font's licence to travel with every copy.
func TestEveryModuleHasALicence(t *testing.T) {
	text := liteRepoFile(t, "internal", "lite", "notices", "THIRD_PARTY_NOTICES.txt")
	if !strings.Contains(text, "SIL Open Font License") {
		t.Error("the font's licence is not in the notices")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info")
	}
	var missing []string
	for _, dep := range info.Deps {
		if dep.Replace != nil {
			dep = dep.Replace
		}
		if !strings.Contains(text, "== "+dep.Path+" ") {
			missing = append(missing, dep.Path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("no licence for %v — run scripts/lite-notices.sh", missing)
	}
}
