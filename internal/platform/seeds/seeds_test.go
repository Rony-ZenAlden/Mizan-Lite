package seeds_test

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

type doc struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func decode(data []byte, d *doc) error { return seeds.StrictJSON(data, d) }

func shipped(files map[string]string) seeds.Layer {
	return seeds.Layer{FS: mapFS(files), Origin: seeds.OriginShipped}
}

func user(files map[string]string) seeds.Layer {
	return seeds.Layer{FS: mapFS(files), Origin: seeds.OriginUser}
}

func mapFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// ── discovery ───────────────────────────────────────────────────────────────────

// The order must not depend on directory iteration, or two installs of the same version apply
// the same documents in different orders and diverge.
func TestFilesAreOrderedByName(t *testing.T) {
	set, err := seeds.Discover("p", shipped(map[string]string{
		"p/sy.json": `{"code":"SY","name":"Syria"}`,
		"p/ae.json": `{"code":"AE","name":"Emirates"}`,
		"p/eg.json": `{"code":"EG","name":"Egypt"}`,
	}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	var names []string
	for _, f := range set.Files {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "ae,eg,sy" {
		t.Errorf("order = %q, want ae,eg,sy", got)
	}
}

// Non-JSON files and subdirectories are not seeds. A README beside the profiles must not be
// parsed as one.
func TestOnlyJSONFilesAreRead(t *testing.T) {
	set, err := seeds.Discover("p", shipped(map[string]string{
		"p/sy.json":     `{"code":"SY","name":"Syria"}`,
		"p/README.md":   "these are the country profiles",
		"p/notes/a.txt": "scratch",
	}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(set.Files) != 1 || set.Files[0].Name != "sy" {
		t.Fatalf("files = %+v, want only sy.json", set.Files)
	}
}

// A missing directory is a legitimate state — a module may ship no profiles of a kind, and a
// customer may never have created the overlay folder.
func TestMissingDirectoryIsNotAnError(t *testing.T) {
	set, err := seeds.Discover("nowhere", shipped(map[string]string{"p/sy.json": `{}`}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(set.Files) != 0 || len(set.Problems) != 0 {
		t.Errorf("set = %+v, want empty", set)
	}
}

// ── layering ────────────────────────────────────────────────────────────────────

// The Addendum §C promise: adding a country is dropping in a file. Replacing one is the same
// file name in the user layer, and it replaces the shipped file WHOLE — never merged, so
// "which half produced this behaviour?" never has to be asked.
func TestAUserFileReplacesAShippedOne(t *testing.T) {
	set, err := seeds.Discover("p",
		shipped(map[string]string{"p/sy.json": `{"code":"SY","name":"Syria"}`}),
		user(map[string]string{
			"p/sy.json": `{"code":"SY","name":"Syrian Arab Republic"}`,
			"p/jo.json": `{"code":"JO","name":"Jordan"}`,
		}),
	)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	result, err := seeds.Decode(set, decode)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(result.Docs) != 2 {
		t.Fatalf("%d documents, want 2 (the replacement and the addition)", len(result.Docs))
	}

	byCode := map[string]seeds.Doc[doc]{}
	for _, d := range result.Docs {
		byCode[d.Value.Code] = d
	}
	sy, ok := byCode["SY"]
	if !ok {
		t.Fatal("SY is missing")
	}
	if sy.Value.Name != "Syrian Arab Republic" {
		t.Errorf("name = %q, want the user's file to have won", sy.Value.Name)
	}
	if sy.File.Origin != seeds.OriginUser {
		t.Errorf("origin = %q, want %q", sy.File.Origin, seeds.OriginUser)
	}
	// Support cannot reproduce a customer's behaviour without knowing a shipped file was
	// shadowed, so the fact is recorded rather than inferred.
	if !sy.File.ReplacedShipped {
		t.Error("the override is not recorded as having replaced a shipped file")
	}
	if byCode["JO"].File.ReplacedShipped {
		t.Error("a purely added file is marked as a replacement")
	}
}

// ── the asymmetry (D4) ──────────────────────────────────────────────────────────

// A broken file WE ship is a bug in our build. It must fail loudly, here, in CI.
func TestABrokenShippedFileIsFatal(t *testing.T) {
	set, err := seeds.Discover("p", shipped(map[string]string{
		"p/sy.json": `{"code":"SY","name":"Syria"}`,
		"p/xx.json": `{ this is not json`,
	}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, err = seeds.Decode(set, decode); err == nil {
		t.Fatal("a malformed file shipped with the build loaded without complaint")
	}
}

// A broken file the CUSTOMER wrote is a typo at 11pm. It must be reported and skipped, and
// whatever it was shadowing must stay in force.
//
// This is the test the whole asymmetry exists for: a seed file must not be able to stop a shop
// from opening.
func TestABrokenUserFileIsSkippedNotFatal(t *testing.T) {
	set, err := seeds.Discover("p",
		shipped(map[string]string{
			"p/sy.json": `{"code":"SY","name":"Syria"}`,
			"p/eg.json": `{"code":"EG","name":"Egypt"}`,
		}),
		user(map[string]string{"p/sy.json": `{ broken`}),
	)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	result, err := seeds.Decode(set, decode)
	if err != nil {
		t.Fatalf("a customer's malformed file stopped the load: %v", err)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("%d problems reported, want 1 — a skipped file must be visible", len(result.Problems))
	}
	if result.Problems[0].Origin != seeds.OriginUser {
		t.Errorf("problem origin = %q, want %q", result.Problems[0].Origin, seeds.OriginUser)
	}

	// Egypt still loads. The customer's mistake in one file must not cost them the others.
	if len(result.Docs) != 1 || result.Docs[0].Value.Code != "EG" {
		t.Fatalf("documents = %+v, want only EG", result.Docs)
	}
}

// ── strictness ──────────────────────────────────────────────────────────────────

// A silently-ignored key is the worst class of configuration bug: the customer believes they
// configured something and the system never saw it. Invisible from both ends.
func TestAnUnknownKeyIsRejected(t *testing.T) {
	var d doc
	err := seeds.StrictJSON([]byte(`{"code":"SY","name":"Syria","naem":"typo"}`), &d)
	if err == nil {
		t.Fatal("an unknown key was accepted; a typo would be silently ignored")
	}
}

// Two documents in one file means the author appended rather than replaced, and one would be
// silently lost.
func TestTrailingContentIsRejected(t *testing.T) {
	var d doc
	if err := seeds.StrictJSON([]byte(`{"code":"SY"} {"code":"EG"}`), &d); err == nil {
		t.Fatal("a file holding two documents was accepted")
	}
}

// ── bounds ──────────────────────────────────────────────────────────────────────

// The data directory is writable by anyone who can reach the customer's machine, so a file
// found there is not read into memory unbounded.
func TestAnOversizedFileIsRefused(t *testing.T) {
	big := `{"code":"SY","name":"` + strings.Repeat("x", seeds.MaxFileBytes) + `"}`
	set, err := seeds.Discover("p", user(map[string]string{"p/sy.json": big}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(set.Files) != 0 {
		t.Errorf("an oversized file was read: %d bytes", len(set.Files[0].Data))
	}
	if len(set.Problems) != 1 {
		t.Fatalf("%d problems, want 1", len(set.Problems))
	}
}

// A file exactly at the limit is fine. Off-by-one at a boundary that rejects customer data is
// worth pinning.
func TestAFileAtTheLimitIsAccepted(t *testing.T) {
	body := `{"code":"SY","name":"` + strings.Repeat("x", 100) + `"}`
	padded := body + strings.Repeat(" ", seeds.MaxFileBytes-len(body))
	if len(padded) != seeds.MaxFileBytes {
		t.Fatalf("test fixture is %d bytes, want exactly the limit", len(padded))
	}
	set, err := seeds.Discover("p", user(map[string]string{"p/sy.json": padded}))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(set.Files) != 1 {
		t.Fatalf("a file exactly at the limit was refused: %+v", set.Problems)
	}
	if !bytes.Equal(set.Files[0].Data, []byte(padded)) {
		t.Error("the file was truncated at the limit rather than read whole")
	}
}
