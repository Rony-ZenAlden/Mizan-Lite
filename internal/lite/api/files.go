package api

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Codes for files.
const (
	CodeNoDialogs  = "lite.api.no_dialogs"
	CodeSaveFailed = "lite.api.save_failed"
)

// Filter is a file type a dialog offers.
type Filter struct {
	Name    string
	Pattern string
}

// Files is the operating system's file dialogs and folder view, provided by apps/lite through Wails — internal/lite may not
// import Wails (L7 §4.4). A cancelled dialog returns "" and no error.
type Files interface {
	SaveFile(ctx context.Context, title, defaultName string, filters []Filter) (string, error)
	OpenFile(ctx context.Context, title string, filters []Filter) (string, error)
	PickFolder(ctx context.Context, title string) (string, error)
	ShowInFolder(path string) error
}

func (c *core) dialogs() (Files, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.files == nil {
		return nil, errs.Conflict(CodeNoDialogs, "file dialogs are not available")
	}
	return c.files, nil
}

// writeAtomically writes a file beside its destination and renames it into place, so a half-written export never stands
// under the name the owner chose (D-L7.7).
func writeAtomically(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mizan-lite-*")
	if err != nil {
		return errs.Wrap(err, errs.CategoryConflict, CodeSaveFailed, "saving the file").WithParam("path", path)
	}
	name := tmp.Name()
	if _, err = tmp.Write(body); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return errs.Wrap(err, errs.CategoryConflict, CodeSaveFailed, "saving the file").WithParam("path", path)
	}
	return nil
}
