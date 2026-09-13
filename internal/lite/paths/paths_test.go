package paths_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/paths"
)

// fakeEnv builds an Env for any operating system, on whichever machine runs the test.
func fakeEnv(goos string, vars map[string]string, home string, homeErr error) paths.Env {
	return paths.Env{
		GOOS:   goos,
		Getenv: func(key string) string { return vars[key] },
		UserHomeDir: func() (string, error) {
			return home, homeErr
		},
	}
}

// TestRootChoosesThePlatformDataDirectory pins the DECISION per platform. The separators come from
// filepath.Join on the machine running the test, so the expectation is built the same way: what
// is under test is which base directory is chosen and in what order, not Go's path joining.
func TestRootChoosesThePlatformDataDirectory(t *testing.T) {
	cases := []struct {
		name string
		env  paths.Env
		want string
	}{
		{
			name: "windows uses LOCALAPPDATA, never Roaming",
			env: fakeEnv("windows", map[string]string{
				"LOCALAPPDATA": `C:\Users\محمد\AppData\Local`,
				"APPDATA":      `C:\Users\محمد\AppData\Roaming`,
			}, `C:\Users\محمد`, nil),
			want: filepath.Join(`C:\Users\محمد\AppData\Local`, "Mizan Lite"),
		},
		{
			name: "windows without LOCALAPPDATA falls back to the profile's AppData\\Local",
			env:  fakeEnv("windows", map[string]string{"APPDATA": `C:\Users\x\AppData\Roaming`}, `C:\Users\x`, nil),
			want: filepath.Join(`C:\Users\x`, "AppData", "Local", "Mizan Lite"),
		},
		{
			name: "windows ignores a LOCALAPPDATA that is only whitespace",
			env:  fakeEnv("windows", map[string]string{"LOCALAPPDATA": "   "}, `C:\Users\x`, nil),
			want: filepath.Join(`C:\Users\x`, "AppData", "Local", "Mizan Lite"),
		},
		{
			name: "macOS uses Application Support",
			env:  fakeEnv("darwin", nil, "/Users/rony", nil),
			want: filepath.Join("/Users/rony", "Library", "Application Support", "Mizan Lite"),
		},
		{
			name: "other platforms honour XDG_DATA_HOME",
			env:  fakeEnv("linux", map[string]string{"XDG_DATA_HOME": "/data"}, "/home/x", nil),
			want: filepath.Join("/data", "mizan-lite"),
		},
		{
			name: "other platforms fall back to ~/.local/share",
			env:  fakeEnv("linux", nil, "/home/x", nil),
			want: filepath.Join("/home/x", ".local", "share", "mizan-lite"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := paths.Root(tc.env)
			if err != nil {
				t.Fatalf("Root: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Root = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheOverrideWinsOnEveryPlatformAndIsMadeAbsolute(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "portable")
	for _, goos := range []string{"windows", "darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			env := fakeEnv(goos, map[string]string{
				paths.EnvOverride: "  " + absolute + "  ",
				"LOCALAPPDATA":    `C:\ignored`,
				"XDG_DATA_HOME":   "/ignored",
			}, "/ignored-home", nil)
			got, err := paths.Root(env)
			if err != nil {
				t.Fatalf("Root: %v", err)
			}
			if got != absolute {
				t.Fatalf("Root = %q, want the trimmed override %q", got, absolute)
			}
		})
	}

	t.Run("a relative override is resolved against the working directory", func(t *testing.T) {
		env := fakeEnv("darwin", map[string]string{paths.EnvOverride: "relative-data"}, "/h", nil)
		got, err := paths.Root(env)
		if err != nil {
			t.Fatalf("Root: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Fatalf("Root = %q, want an absolute path", got)
		}
	})
}

func TestTheOverrideIsNotMizansVariable(t *testing.T) {
	// A machine configured for Mizan must not relocate Lite. If these were ever equal, the two
	// editions would share a data directory the moment either was overridden.
	if paths.EnvOverride == "MIZAN_DATA_DIR" {
		t.Fatal("Lite's override must differ from Mizan's MIZAN_DATA_DIR")
	}
	env := fakeEnv("darwin", map[string]string{"MIZAN_DATA_DIR": "/mizan"}, "/Users/x", nil)
	got, err := paths.Root(env)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if got == "/mizan" {
		t.Fatal("Lite followed Mizan's override")
	}
	if paths.DBFileName == "mizan.db" {
		t.Fatal("Lite's database file name must differ from Mizan's mizan.db")
	}
}

func TestAMissingHomeDirectoryIsATypedError(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  paths.Env
	}{
		{"home lookup fails", fakeEnv("darwin", nil, "", errors.New("no home"))},
		{"home is empty", fakeEnv("darwin", nil, "  ", nil)},
		{"windows with no LOCALAPPDATA and no home", fakeEnv("windows", nil, "", errors.New("no home"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := paths.Root(tc.env)
			if errs.CodeOf(err) != paths.CodeUnresolvable {
				t.Fatalf("code = %q (err %v), want %q", errs.CodeOf(err), err, paths.CodeUnresolvable)
			}
		})
	}
}

func TestResolveCreatesTheTreeAndLeavesNoProbe(t *testing.T) {
	// A data root with every character that has broken a path somewhere in this project: an
	// Arabic profile name, a space, and the `#` that truncated the SQLite DSN (platform/database).
	root := filepath.Join(t.TempDir(), "محمد #1", "Mizan Lite")
	env := fakeEnv(runtime.GOOS, map[string]string{paths.EnvOverride: root}, "/unused", nil)

	got, err := paths.ResolveIn(env)
	if err != nil {
		t.Fatalf("ResolveIn: %v", err)
	}
	if got.DBFile != filepath.Join(root, paths.DBFileName) {
		t.Fatalf("DBFile = %q", got.DBFile)
	}
	for _, dir := range []string{got.Data, got.Backups, got.Logs, got.WebView} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%q was not created: %v", dir, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Errorf("%q has mode %v, want 0700", dir, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".write-probe")); !os.IsNotExist(err) {
		t.Fatalf("the write probe was left behind: %v", err)
	}
}

func TestResolveIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	env := fakeEnv(runtime.GOOS, map[string]string{paths.EnvOverride: root}, "/unused", nil)
	first, err := paths.ResolveIn(env)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := paths.ResolveIn(env)
	if err != nil {
		t.Fatalf("second resolve over an existing tree: %v", err)
	}
	if first != second {
		t.Fatalf("resolves differ: %+v vs %+v", first, second)
	}
}

func TestAnUnwritableDataDirectoryIsReportedAtLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not make a directory read-only on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	root := filepath.Join(t.TempDir(), "locked")
	for _, dir := range []string{"", "backups", "logs", "webview"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	env := fakeEnv(runtime.GOOS, map[string]string{paths.EnvOverride: root}, "/unused", nil)
	_, err := paths.ResolveIn(env)
	if errs.CodeOf(err) != paths.CodeNotWritable {
		t.Fatalf("code = %q (err %v), want %q", errs.CodeOf(err), err, paths.CodeNotWritable)
	}
}
