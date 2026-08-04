package envelope_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/buildinfo"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// update regenerates the cross-language contract fixture: go test ./internal/api/envelope -update
var update = flag.Bool("update", false, "regenerate the envelope contract fixture")

// contractFixturePath is read by the TypeScript bindings wrapper's tests.
//
// It lives under frontend/ rather than in a Go testdata directory because its ONLY consumer is
// the TypeScript side; a fixture the frontend cannot import proves nothing about the frontend.
const contractFixturePath = "../../../frontend/src/lib/wails/__fixtures__/envelope.contract.json"

// TestEnvelopeContractFixture pins the wire shape both languages must agree on.
//
// Step 0.11 §1.2: the frontend's Health wrapper kept returning the pre-0.10 flat shape after
// the binding started returning Result[T], and nothing caught it — partly because the dev mock
// returned the old shape too, so `npm run dev` certified the broken path.
//
// This is the fix for that class of drift rather than for the one instance. The Go side owns
// the shape and writes it here; the TypeScript tests parse this exact file. A change to the
// envelope fails THIS test first (regenerate with -update), and the regenerated fixture then
// fails the TypeScript tests until the wrapper is updated to match. Neither side can move
// alone.
func TestEnvelopeContractFixture(t *testing.T) {
	type sample struct {
		Name  string
		Value any
	}

	// A typed kernel error: code and params cross, developer prose does not.
	typed := errs.Conflict("migrate.database_too_new", "developer-facing text only").
		WithParam("current", "5").
		WithParam("target", "3")

	// A validation error carrying field-level detail.
	invalid := errs.Validation("config.invalid_value", "developer-facing text only").
		WithField("locale", "i18n.invalid_locale", "developer-facing text only")

	samples := []sample{
		// A struct payload — the shape App.Health() returns.
		{"healthOk", envelope.Ok(buildinfo.Info{
			Version:   "1.2.3",
			Commit:    "abc1234",
			BuildTime: "2026-08-04T00:00:00.000Z",
			GoVersion: "go1.26.0",
			Platform:  "darwin/arm64",
		})},
		// An EMPTY list. Before the Data field dropped omitempty this serialised as
		// {"ok":true} with no data key at all, so the frontend received undefined where it
		// expected [] — the same species of bug as §1.2, one screen away from shipping.
		{"emptyListOk", envelope.Ok([]string{})},
		{"listOk", envelope.Ok([]string{"SYP", "USD"})},
		{"typedFailure", envelope.Fail[buildinfo.Info](typed)},
		{"validationFailure", envelope.Fail[buildinfo.Info](invalid)},
		// An untyped error must NOT leak its Go message across the boundary.
		{"untypedFailure", envelope.Fail[buildinfo.Info](errors.New("some internal go text"))},
	}

	out := make(map[string]json.RawMessage, len(samples))
	for _, s := range samples {
		raw, err := json.Marshal(s.Value)
		if err != nil {
			t.Fatalf("marshal %s: %v", s.Name, err)
		}
		out[s.Name] = raw
	}

	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	got = append(got, '\n')

	if *update {
		if mkErr := os.MkdirAll(filepath.Dir(contractFixturePath), 0o750); mkErr != nil {
			t.Fatalf("create fixture dir: %v", mkErr)
		}
		if writeErr := os.WriteFile(contractFixturePath, got, 0o600); writeErr != nil {
			t.Fatalf("write fixture: %v", writeErr)
		}
		t.Log("fixture regenerated")
		return
	}

	want, readErr := os.ReadFile(contractFixturePath)
	if readErr != nil {
		t.Fatalf("read fixture (regenerate with -update): %v", readErr)
	}
	if string(got) != string(want) {
		t.Errorf("the envelope wire shape changed.\n"+
			"Regenerate with: go test ./internal/api/envelope -update\n"+
			"Then update frontend/src/lib/wails to match, or the frontend tests will fail.\n"+
			"got:\n%s\nwant:\n%s", got, want)
	}
}

// TestEmptyListSerialisesAsAnArray is the regression guard for the omitempty defect.
//
// Asserted directly rather than only through the fixture, because the fixture is a golden file
// someone could regenerate without reading — this states the invariant in a form that fails
// with an explanation.
func TestEmptyListSerialisesAsAnArray(t *testing.T) {
	raw, err := json.Marshal(envelope.Ok([]string{}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded struct {
		OK   bool      `json:"ok"`
		Data *[]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Data == nil {
		t.Fatalf("an empty list serialised without a data key (%s); "+
			"the frontend would receive undefined and crash on .map()", raw)
	}
	if len(*decoded.Data) != 0 {
		t.Errorf("data = %v, want []", *decoded.Data)
	}
}
