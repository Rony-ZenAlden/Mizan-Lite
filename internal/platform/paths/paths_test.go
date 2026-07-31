package paths_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

func TestOverrideWinsAndCreatesTheTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	t.Setenv(paths.EnvOverride, dir)

	p, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Data != dir {
		t.Errorf("Data = %q, want the override %q", p.Data, dir)
	}
	for _, want := range []string{p.Data, p.Backups, p.Logs} {
		info, statErr := os.Stat(want)
		if statErr != nil {
			t.Errorf("%q was not created: %v", want, statErr)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", want)
		}
	}
	if filepath.Dir(p.DBFile) != p.Data {
		t.Errorf("DBFile %q is not inside Data %q", p.DBFile, p.Data)
	}
}

func TestPathsAreAbsolute(t *testing.T) {
	// A relative data directory would resolve against the working directory, which for a
	// double-clicked desktop app is wherever the OS happens to start it.
	t.Setenv(paths.EnvOverride, "./relative-data")
	p, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p.Data) })

	if !filepath.IsAbs(p.Data) {
		t.Errorf("Data = %q, want an absolute path", p.Data)
	}
}

func TestDirectoriesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	t.Setenv(paths.EnvOverride, filepath.Join(t.TempDir(), "private"))
	p, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.Data)
	if err != nil {
		t.Fatal(err)
	}
	// A business's ledger is not other users' business on a shared machine.
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("data directory mode = %o, want 700", perm)
	}
}

func TestUnwritableDirectoryIsATypedError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	t.Setenv(paths.EnvOverride, filepath.Join(locked, "data"))
	_, err := paths.Resolve("Mizan")
	if err == nil {
		t.Fatal("an unwritable location was accepted")
	}
	if code := errs.CodeOf(err); code != paths.CodeNotWritable {
		t.Errorf("code = %q, want %q", code, paths.CodeNotWritable)
	}
}

func TestWriteProbeLeavesNothingBehind(t *testing.T) {
	// The check must not litter the customer's data directory.
	dir := filepath.Join(t.TempDir(), "clean")
	t.Setenv(paths.EnvOverride, dir)
	p, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(p.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".write-probe") {
			t.Errorf("the write probe was left behind: %s", e.Name())
		}
	}
}

func TestDefaultLocationIsPlatformAppropriate(t *testing.T) {
	// No override: the OS convention applies. The directory is not created here — this only
	// checks where it would go, so the test does not pollute a developer's real profile.
	t.Setenv(paths.EnvOverride, "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	p, err := paths.Resolve("MizanPathTest")
	if err != nil {
		t.Skipf("cannot resolve the default location here: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p.Data) })

	if !strings.HasPrefix(p.Data, home) {
		t.Errorf("default data dir %q is not under the home directory %q", p.Data, home)
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(p.Data, "Application Support") {
			t.Errorf("macOS data dir = %q, want it under Application Support", p.Data)
		}
	case "linux":
		if !strings.Contains(p.Data, ".local/share") && os.Getenv("XDG_DATA_HOME") == "" {
			t.Errorf("linux data dir = %q, want ~/.local/share (not ~/.config, which is for "+
				"configuration rather than data)", p.Data)
		}
	}
}

func TestLinuxHonoursXDGDataHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG applies to Linux")
	}
	xdg := t.TempDir()
	t.Setenv(paths.EnvOverride, "")
	t.Setenv("XDG_DATA_HOME", xdg)

	p, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Data, xdg) {
		t.Errorf("Data = %q, want it under XDG_DATA_HOME %q", p.Data, xdg)
	}
}
