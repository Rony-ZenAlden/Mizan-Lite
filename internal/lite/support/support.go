// Package support writes the support file (L8 §9.2): a zip of the last week's logs and a diagnostics document, and — only when
// the owner chooses — a verified copy of the database. The application never sends it anywhere; the owner saves it and hands it
// over. Pure but for reading the log folder: the caller gathers everything else.
package support

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// CodeWriteFailed is a support file that could not be assembled.
const CodeWriteFailed = "lite.support.write_failed"

// LogDays is how far back the logs go.
const LogDays = 7

// Part is one file in the zip.
type Part struct {
	Name string
	Body []byte
}

// Zip writes the parts in name order, each stamped at the given time, so the same parts make the same file.
func Zip(parts []Part, at time.Time) ([]byte, error) {
	sorted := append([]Part(nil), parts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range sorted {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.Name, Method: zip.Deflate, Modified: at})
		if err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "adding "+p.Name)
		}
		if _, err = w.Write(p.Body); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing "+p.Name)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "closing the zip")
	}
	return buf.Bytes(), nil
}

// RecentLogs reads every file in the log folder changed in the last LogDays days, as logs/<name>.
func RecentLogs(dir string, now time.Time) ([]Part, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "reading the log folder")
	}
	since := now.AddDate(0, 0, -LogDays)
	var out []Part
	for _, e := range entries {
		info, infoErr := e.Info()
		if e.IsDir() || infoErr != nil || info.ModTime().Before(since) {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			return nil, errs.Wrap(readErr, errs.CategoryInternal, CodeWriteFailed, "reading "+e.Name())
		}
		out = append(out, Part{Name: "logs/" + e.Name(), Body: body})
	}
	return out, nil
}
