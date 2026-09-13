package logging_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/logging"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAppendsBelowTheLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logging.FileName)
	write(t, path, 10)

	f, err := logging.Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("more"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if len(raw) != 14 {
		t.Fatalf("size = %d, want 14 (appended, not truncated)", len(raw))
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("rotated below the limit")
	}
}

func TestOpenRotatesAtTheLimitAndKeepsOneGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, logging.FileName)
	write(t, path+".1", 7) // an older generation, which must be replaced
	write(t, path, 100)

	f, err := logging.Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	active, err := os.Stat(path)
	if err != nil || active.Size() != 0 {
		t.Fatalf("the active log was not started fresh: %v %v", active, err)
	}
	previous, err := os.Stat(path + ".1")
	if err != nil || previous.Size() != 100 {
		t.Fatalf("the previous generation is not the rotated log: %v %v", previous, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("%d files in the log directory, want exactly 2", len(entries))
	}
}

func TestOpenCreatesTheLogWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	f, err := logging.Open(dir, logging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := os.Stat(filepath.Join(dir, logging.FileName)); err != nil {
		t.Fatal(err)
	}
}

func TestTheLoggerWritesSearchableJSON(t *testing.T) {
	dir := t.TempDir()
	f, err := logging.Open(dir, logging.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	logging.New(f).Info("health requested by the frontend", slog.String("version", "1.0"))
	_ = f.Close()

	raw, _ := os.ReadFile(filepath.Join(dir, logging.FileName))
	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &line); err != nil {
		t.Fatalf("not JSON: %q (%v)", raw, err)
	}
	if line["msg"] != "health requested by the frontend" || line["version"] != "1.0" {
		t.Fatalf("line = %v", line)
	}
}
