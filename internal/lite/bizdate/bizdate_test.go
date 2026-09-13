package bizdate_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/bizdate"
)

func TestAMovementAt0100DamascusBelongsToToday(t *testing.T) {
	damascus := time.FixedZone("Damascus", 3*3600)
	// 01:00 on 14 September in Damascus is 22:00 on 13 September in UTC.
	at := time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC)
	if got := bizdate.Date(at, damascus); got != "2026-09-14" {
		t.Fatalf("got %s, want the shop's day 2026-09-14", got)
	}
	if got := bizdate.Date(at, time.UTC); got != "2026-09-13" {
		t.Fatalf("got %s in UTC", got)
	}
}

func TestANilLocationIsTheMachinesZone(t *testing.T) {
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	if bizdate.Date(at, nil) != at.In(time.Local).Format(bizdate.Layout) {
		t.Fatal("a nil location is not time.Local")
	}
}

// TestNoLoadLocationAnywhere fails if any Lite production file calls time.LoadLocation, which needs time-zone data that
// Windows does not ship (see the package comment).
func TestNoLoadLocationAnywhere(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	scanned := 0
	for _, dir := range []string{"internal/lite", "apps/lite", "cmd/lite-demoseed"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			if strings.Contains(string(raw), "LoadLocation(") {
				t.Errorf("%s calls time.LoadLocation — it fails on Windows without embedded tzdata", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 30 {
		t.Fatalf("scanned only %d files; the walk is not reaching the source", scanned)
	}
}
