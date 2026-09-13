package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
