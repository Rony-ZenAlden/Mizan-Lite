package support_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/support"
)

func TestTheZipHoldsThePartsInOrderAndIsReproducible(t *testing.T) {
	at := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	parts := []support.Part{{Name: "logs/b.log", Body: []byte("b")}, {Name: "diagnostics.json", Body: []byte(`{"a":1}`)}}
	one, err := support.Zip(parts, at)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := support.Zip(parts, at)
	if !bytes.Equal(one, two) {
		t.Fatal("the same parts made different files")
	}
	r, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.File) != 2 || r.File[0].Name != "diagnostics.json" || r.File[1].Name != "logs/b.log" {
		t.Fatalf("files %v", r.File)
	}
	rc, _ := r.File[1].Open()
	body, _ := io.ReadAll(rc)
	if string(body) != "b" {
		t.Fatalf("body %q", body)
	}
}

func TestOnlyTheLastWeeksLogsAreTaken(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for name, age := range map[string]time.Duration{"mizan-lite.log": time.Hour, "mizan-lite-old.log": 9 * 24 * time.Hour} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := support.RecentLogs(dir, now)
	if err != nil || len(parts) != 1 || parts[0].Name != "logs/mizan-lite.log" {
		t.Fatalf("parts %+v, %v", parts, err)
	}
	if parts, err := support.RecentLogs(filepath.Join(dir, "missing"), now); err != nil || parts != nil {
		t.Fatalf("a missing folder: %+v, %v", parts, err)
	}
}
