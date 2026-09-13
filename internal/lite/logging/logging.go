// Package logging opens Mizan Lite's log file.
//
// # Why a file at all
//
// A packaged desktop application has no terminal. On Windows a GUI process's standard error goes
// nowhere; on macOS it goes to the unified log, where no shopkeeper will find it. A file in the data
// directory is the one place a support conversation can ask for.
//
// # Why it rotates, and why only at launch
//
// A log that only grows is a disk leak discovered months later as "the application stopped
// saving" (Mizan 10.16 found exactly that shape in its backups). Rotation happens once, at launch,
// BEFORE the file is opened: on Windows an open file cannot be renamed, so rotating a file the
// process is writing to would fail on every Windows machine while passing on every Mac.
package logging

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// FileName is the active log.
const FileName = "mizan-lite.log"

// DefaultMaxBytes is the size at which the log is rotated at the next launch.
const DefaultMaxBytes int64 = 5 << 20

// Open rotates the log in dir if it has reached maxBytes, then opens it for appending.
//
// One previous generation is kept (FileName + ".1"). A support conversation needs the session that
// went wrong and perhaps the one before; older history is not worth the disk.
func Open(dir string, maxBytes int64) (*os.File, error) {
	path := filepath.Join(dir, FileName)
	if info, err := os.Stat(path); err == nil && info.Size() >= maxBytes {
		previous := path + ".1"
		// Removed explicitly first. os.Rename replaces an existing target on both platforms today,
		// but relying on that is relying on a detail; removing makes the intent the code.
		if err := os.Remove(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.Rename(path, previous); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// New builds the application logger: JSON, so a line can be searched by field.
func New(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
