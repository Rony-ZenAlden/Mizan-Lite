package id_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

func TestNewProducesCanonicalV7(t *testing.T) {
	got, err := id.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := got.String()
	if len(s) != 36 {
		t.Fatalf("length = %d, want 36 (CHAR(36) storage contract): %q", len(s), s)
	}
	// 8-4-4-4-12 with the version nibble at index 14 and the variant at 19.
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		t.Errorf("not canonical UUID text: %q", s)
	}
	if s[14] != '7' {
		t.Errorf("version nibble = %q, want '7' (UUIDv7 is required for index locality): %q", s[14], s)
	}
	if !strings.ContainsRune("89ab", rune(s[19])) {
		t.Errorf("variant nibble = %q, want one of 8/9/a/b: %q", s[19], s)
	}
}

func TestNewIsTimeOrdered(t *testing.T) {
	// v7 is chosen so sequential inserts land at the right edge of the B-tree rather
	// than scattering. That property is only real if generation is monotonic.
	const n = 200
	prev := ""
	for i := 0; i < n; i++ {
		got, err := id.New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if prev != "" && got.String() < prev {
			t.Fatalf("identifier %d went backwards: %q then %q", i, prev, got)
		}
		prev = got.String()
	}
}

func TestNewIsUniqueUnderConcurrency(t *testing.T) {
	const workers, each = 8, 250

	var mu sync.Mutex
	seen := make(map[id.ID]bool, workers*each)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]id.ID, 0, each)
			for i := 0; i < each; i++ {
				got, err := id.New()
				if err != nil {
					t.Errorf("New: %v", err)
					return
				}
				local = append(local, got)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, got := range local {
				if seen[got] {
					t.Errorf("duplicate identifier generated: %s", got)
				}
				seen[got] = true
			}
		}()
	}
	wg.Wait()

	if len(seen) != workers*each {
		t.Errorf("got %d unique identifiers, want %d", len(seen), workers*each)
	}
}

func TestParseRoundTrip(t *testing.T) {
	original, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := id.Parse(original.String())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed != original {
		t.Errorf("round trip changed the value: %q → %q", original, parsed)
	}
}

func TestParseRejectsNilUUID(t *testing.T) {
	// The nil UUID is how an uninitialised value reaches the database. A "valid" all-zero
	// primary key is far harder to trace from the symptom than a rejected write.
	_, err := id.Parse("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("the nil UUID was accepted")
	}
	if code := errs.CodeOf(err); code != id.CodeInvalid {
		t.Errorf("code = %q, want %q", code, id.CodeInvalid)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "not-a-uuid", "12345", strings.Repeat("a", 36)} {
		t.Run(bad, func(t *testing.T) {
			if _, err := id.Parse(bad); err == nil {
				t.Fatalf("accepted %q", bad)
			}
		})
	}
}

func TestParseAcceptsOtherVersions(t *testing.T) {
	// Rows written by earlier builds, or imported from another system, must stay readable.
	const v4 = "9f1b8a3e-5c2d-4b7a-9e6f-1d2c3b4a5e6f"
	if _, err := id.Parse(v4); err != nil {
		t.Errorf("a valid v4 UUID was rejected: %v", err)
	}
}

func TestIsZero(t *testing.T) {
	var zero id.ID
	if !zero.IsZero() {
		t.Error("the zero ID should report IsZero")
	}
	got, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if got.IsZero() {
		t.Error("a generated ID should not report IsZero")
	}
}
