package domain_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/owner/domain"
)

func TestNormalisePIN(t *testing.T) {
	accepted := []struct{ in, want string }{
		{"246813", "246813"},
		{"٢٤٦٨١٣", "246813"},             // typed on an Arabic keyboard
		{" 739251 ", "739251"},           // surrounding space is forgiven
		{"739251846028", "739251846028"}, // twelve
		{"890123", "890123"},             // wrapping past 9 is not a run a person reads as one
	}
	for _, tc := range accepted {
		if got, err := domain.NormalisePIN(tc.in); err != nil || got != tc.want {
			t.Errorf("NormalisePIN(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	refused := map[string]string{
		"24681":         domain.CodePINInvalid, // five digits
		"7392518460281": domain.CodePINInvalid, // thirteen
		"24a813":        domain.CodePINInvalid,
		"246 813":       domain.CodePINInvalid,
		"":              domain.CodePINInvalid,
		"111111":        domain.CodePINWeak,
		"123456":        domain.CodePINWeak,
		"987654":        domain.CodePINWeak,
		"٠١٢٣٤٥":        domain.CodePINWeak, // weak in any digit set
	}
	for in, want := range refused {
		if _, err := domain.NormalisePIN(in); errs.CodeOf(err) != want {
			t.Errorf("NormalisePIN(%q) = %v; want %s", in, err, want)
		}
	}
}

func TestLockoutDoublesAndCaps(t *testing.T) {
	want := map[int]time.Duration{
		0: 0, 1: 0, 4: 0,
		5: 30 * time.Second, 6: time.Minute, 7: 2 * time.Minute, 8: 4 * time.Minute, 9: 8 * time.Minute,
		10: 15 * time.Minute, 11: 15 * time.Minute, 1000: 15 * time.Minute,
	}
	for failures, delay := range want {
		if got := domain.LockDelay(failures); got != delay {
			t.Errorf("LockDelay(%d) = %v, want %v", failures, got, delay)
		}
	}
}

func TestTheWaitIsNeverPermanent(t *testing.T) {
	for failures := 0; failures < 100_000; failures += 997 {
		if domain.LockDelay(failures) > domain.MaxDelay {
			t.Fatalf("LockDelay(%d) exceeds the cap", failures)
		}
	}
}

func TestRecoveryCodesAreWellFormedAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := domain.NewRecoveryCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		groups := strings.Split(code, "-")
		if len(groups) != 4 {
			t.Fatalf("%q is not XXXX-XXXX-XXXX-XXXX", code)
		}
		for _, g := range groups {
			if len(g) != 4 {
				t.Fatalf("%q has a group of %d", code, len(g))
			}
			for _, r := range g {
				if !strings.ContainsRune(domain.RecoveryAlphabet, r) {
					t.Fatalf("%q contains %q, outside the alphabet", code, r)
				}
			}
		}
		if seen[code] {
			t.Fatalf("a recovery code repeated in 200 draws: %q", code)
		}
		seen[code] = true
	}
}

func TestTheAlphabetHasNoLookAlikes(t *testing.T) {
	for _, r := range "01OIL" {
		if strings.ContainsRune(domain.RecoveryAlphabet, r) {
			t.Errorf("%q is in the alphabet; it is read as another character from paper", r)
		}
	}
	if len(domain.RecoveryAlphabet) != 31 {
		t.Fatalf("alphabet has %d symbols", len(domain.RecoveryAlphabet))
	}
}

func TestAFailedDrawIsAnError(t *testing.T) {
	if _, err := domain.NewRecoveryCode(bytes.NewReader(nil)); err == nil {
		t.Fatal("an exhausted random source produced a code")
	}
	if _, err := domain.NewRecoveryCode(failing{}); err == nil {
		t.Fatal("a failing random source produced a code")
	}
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestCanonicalRecoveryCodeForgivesCaseDashesAndSpaces(t *testing.T) {
	want := "ABCD2345EFGH6789"
	for _, in := range []string{"ABCD-2345-EFGH-6789", "abcd-2345-efgh-6789", "abcd 2345 efgh 6789", "ABCD2345EFGH6789"} {
		if got := domain.CanonicalRecoveryCode(in); got != want {
			t.Errorf("CanonicalRecoveryCode(%q) = %q", in, got)
		}
	}
}
