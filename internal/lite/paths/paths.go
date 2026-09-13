// Package paths resolves where Mizan Lite keeps a shop's data, on Windows and on macOS.
//
// # Why Lite does not reuse platform/paths
//
// Mizan's resolver hardcodes three things Lite must not share:
//
//   - the override variable MIZAN_DATA_DIR, so a machine that set it for Mizan would point Lite at
//     the same directory;
//   - the file name mizan.db, so both editions would then open the SAME database file;
//   - %AppData% (Roaming) on Windows — see Root for why Lite uses Local instead.
//
// The two editions must be installable side by side without either being able to touch the
// other's data (design D10). A resolver is sixty lines; sharing one would mean parameterising
// every one of those decisions for a second caller.
//
// # Why every OS branch takes an Env
//
// Lite ships on Windows and macOS and is developed on one of them. A resolver that read
// runtime.GOOS and os.Getenv directly could only ever be tested on the platform running the test,
// so the Windows branch would be proven by nobody until a shop installed it. Env makes both
// branches ordinary table tests on either machine.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys.
const (
	CodeUnresolvable = "lite.paths.unresolvable"
	CodeNotWritable  = "lite.paths.not_writable"
)

const (
	// EnvOverride relocates the whole data directory: a portable install, a training copy, a
	// throwaway database for development. Distinct from Mizan's MIZAN_DATA_DIR on purpose.
	EnvOverride = "MIZAN_LITE_DATA_DIR"
	// AppDirName is the directory created under the OS data root.
	AppDirName = "Mizan Lite"
	// DBFileName is the live database. Distinct from Mizan's mizan.db, so even a shared override
	// directory could not make the two editions open one file.
	DBFileName = "mizan-lite.db"
)

// Paths are the locations Lite reads and writes.
type Paths struct {
	// Data is the root of everything below.
	Data string
	// DBFile is the SQLite database.
	DBFile string
	// Backups holds verified snapshots. Separate from Data so a sync client can exclude it.
	Backups string
	// Logs holds the application log.
	Logs string
	// WebView is the Windows WebView2 user-data folder (cookies, cache, local storage). Kept under
	// the data root so uninstalling and reinstalling cannot leave it orphaned beside the program,
	// and so it is never inside Program Files, which a normal user cannot write to.
	WebView string
}

// Env is everything the resolver needs from the operating system.
type Env struct {
	GOOS        string
	Getenv      func(string) string
	UserHomeDir func() (string, error)
}

// SystemEnv is the real operating system.
func SystemEnv() Env {
	return Env{GOOS: runtime.GOOS, Getenv: os.Getenv, UserHomeDir: os.UserHomeDir}
}

// Resolve determines the data directory for this machine and creates the tree.
func Resolve() (Paths, error) { return ResolveIn(SystemEnv()) }

// ResolveIn determines the data directory for env and creates the tree.
func ResolveIn(env Env) (Paths, error) {
	root, err := Root(env)
	if err != nil {
		return Paths{}, err
	}
	p := Layout(root)

	// 0o700: a shop's takings and its customers' debts are not other users' business. On Windows
	// the mode is ignored and the profile directory's ACL does the same job.
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, errs.Wrap(err, errs.CategoryInternal, CodeNotWritable,
				"creating the data directory").WithParam("path", dir)
		}
	}
	// Proven now, at launch, where "cannot write to the data directory" is a fixable message —
	// rather than at the first sale, where it is a lost one.
	if err := checkWritable(p.Data); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// Layout names the files and directories under a data root. It touches nothing.
func Layout(root string) Paths {
	return Paths{
		Data:    root,
		DBFile:  filepath.Join(root, DBFileName),
		Backups: filepath.Join(root, "backups"),
		Logs:    filepath.Join(root, "logs"),
		WebView: filepath.Join(root, "webview"),
	}
}

// Root chooses the data root without touching the filesystem.
//
//	override  $MIZAN_LITE_DATA_DIR, made absolute
//	Windows   %LOCALAPPDATA%\Mizan Lite
//	macOS     ~/Library/Application Support/Mizan Lite
//	other     $XDG_DATA_HOME/mizan-lite, else ~/.local/share/mizan-lite   (development only)
//
// # Why Local and not Roaming AppData on Windows
//
// Roaming AppData is copied to a server at sign-out and back at sign-in on a domain-joined
// machine. A live SQLite database is the worst possible thing to roam: the copy happens while
// the write-ahead log may hold committed transactions not yet in the main file, and a roaming
// profile that restores an older copy silently rolls a shop's books back. Local AppData is never
// roamed. Mizan's resolver uses Roaming; this is recorded as a finding for Mizan, not changed
// there, because moving an existing installation's data is a migration of its own.
func Root(env Env) (string, error) {
	if override := strings.TrimSpace(env.Getenv(EnvOverride)); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", errs.Wrap(err, errs.CategoryValidation, CodeUnresolvable,
				"resolving "+EnvOverride).WithParam("value", override)
		}
		return abs, nil
	}

	switch env.GOOS {
	case "windows":
		if local := strings.TrimSpace(env.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, AppDirName), nil
		}
		// LOCALAPPDATA is set by Windows for every interactive session; it is missing only under
		// an unusual service account. The profile's own AppData\Local is the same place.
		home, err := homeDir(env)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", AppDirName), nil

	case "darwin":
		home, err := homeDir(env)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", AppDirName), nil

	default:
		if xdg := strings.TrimSpace(env.Getenv("XDG_DATA_HOME")); xdg != "" {
			return filepath.Join(xdg, "mizan-lite"), nil
		}
		home, err := homeDir(env)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "mizan-lite"), nil
	}
}

func homeDir(env Env) (string, error) {
	home, err := env.UserHomeDir()
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeUnresolvable,
			"locating the home directory")
	}
	if strings.TrimSpace(home) == "" {
		return "", errs.Internal(CodeUnresolvable, "the home directory is empty")
	}
	return home, nil
}

// checkWritable proves the directory accepts a create, a write and a delete. MkdirAll succeeding
// on an existing directory proves none of those: a read-only mount, a full disk and a locked-down
// profile all pass it.
func checkWritable(dir string) error {
	probe := filepath.Join(dir, ".write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeNotWritable,
			"the data directory is not writable").WithParam("path", dir)
	}
	_, writeErr := f.WriteString("ok")
	// Closed BEFORE removing: Windows refuses to delete a file that is still open, where macOS
	// would allow it — a probe that removed first would pass here and fail on every Windows
	// machine.
	closeErr := f.Close()
	removeErr := os.Remove(probe)
	for _, err := range []error{writeErr, closeErr, removeErr} {
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeNotWritable,
				"the data directory is not writable").WithParam("path", dir)
		}
	}
	return nil
}
