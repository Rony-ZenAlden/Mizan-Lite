// Package paths resolves where Mizan keeps a customer's data on their machine.
//
// It exists because `database.Config.Path` is required and, until now, nothing supplied it:
// every test passed a temporary directory and the real application had no answer at all.
//
// The data directory is emphatically NOT inside the installation folder. An installer that
// replaces the application directory would take the customer's database with it, and on
// Windows the program files directory is not writable by a normal user anyway.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeUnresolvable = "paths.unresolvable"
	CodeNotWritable  = "paths.not_writable"
)

// EnvOverride names the environment variable that relocates the whole data directory.
//
// This is what makes a portable install on a USB stick, a second isolated instance for
// training, and a developer's throwaway database possible without a code change.
const EnvOverride = "MIZAN_DATA_DIR"

// Paths are the locations Mizan reads and writes.
type Paths struct {
	// Data is the root of everything below.
	Data string
	// DBFile is the SQLite database.
	DBFile string
	// Backups is where the migration safety layer writes its pre-migration snapshots
	// (Step 0.4). Separate from Data so a customer can exclude it from a sync client
	// without excluding the live database.
	Backups string
	// Logs is where structured logs are written.
	Logs string
}

// Resolve determines the data directory and creates the tree.
//
// Per platform, using stdlib only:
//
//	macOS    ~/Library/Application Support/<app>
//	Windows  %AppData%\<app>
//	Linux    $XDG_DATA_HOME/<app>, else ~/.local/share/<app>
//
// os.UserConfigDir gives the right answer on macOS and Windows. On Linux it returns
// ~/.config, which is for configuration rather than data, so XDG_DATA_HOME is honoured
// explicitly — a database in ~/.config would be backed up by dotfile tooling that never
// expected to find one.
func Resolve(appName string) (Paths, error) {
	root, err := dataRoot(appName)
	if err != nil {
		return Paths{}, err
	}

	p := Paths{
		Data:    root,
		DBFile:  filepath.Join(root, "mizan.db"),
		Backups: filepath.Join(root, "backups"),
		Logs:    filepath.Join(root, "logs"),
	}

	// 0o700: a business's ledger is not other users' business. On a shared machine the
	// default 0o755 would make every invoice world-readable.
	for _, dir := range []string{p.Data, p.Backups, p.Logs} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, errs.Wrap(err, errs.CategoryInternal, CodeNotWritable,
				"creating the data directory").WithParam("path", dir)
		}
	}

	// Verified now rather than discovered at the first sale. "Cannot write to the data
	// directory" is a fixable message at launch and a disaster mid-transaction.
	if err := checkWritable(p.Data); err != nil {
		return Paths{}, err
	}
	return p, nil
}

func dataRoot(appName string) (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvOverride)); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", errs.Wrap(err, errs.CategoryValidation, CodeUnresolvable,
				"resolving "+EnvOverride).WithParam("value", override)
		}
		return abs, nil
	}

	if runtime.GOOS == "linux" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
			return filepath.Join(xdg, strings.ToLower(appName)), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errs.Wrap(err, errs.CategoryInternal, CodeUnresolvable,
				"locating the home directory")
		}
		return filepath.Join(home, ".local", "share", strings.ToLower(appName)), nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeUnresolvable,
			"locating the application data directory")
	}
	return filepath.Join(base, appName), nil
}

// checkWritable proves the directory can actually be written to, rather than trusting that
// MkdirAll succeeding means so. A read-only mount, a full disk, or a locked-down profile all
// pass MkdirAll on an existing directory and fail on the first write.
func checkWritable(dir string) error {
	probe := filepath.Join(dir, ".write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeNotWritable,
			"the data directory is not writable").WithParam("path", dir)
	}
	closeErr := f.Close()
	removeErr := os.Remove(probe)
	if closeErr != nil {
		return errs.Wrap(closeErr, errs.CategoryInternal, CodeNotWritable,
			"the data directory is not writable").WithParam("path", dir)
	}
	if removeErr != nil {
		return errs.Wrap(removeErr, errs.CategoryInternal, CodeNotWritable,
			"the data directory does not permit deletes").WithParam("path", dir)
	}
	return nil
}
