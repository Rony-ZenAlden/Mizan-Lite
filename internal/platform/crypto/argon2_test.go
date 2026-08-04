package crypto_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
)

// fast keeps the test suite quick without weakening what is under test: the encoding, the
// comparison, and the rehash decision are all parameter-independent.
func fast() crypto.Params {
	p := crypto.DefaultParams()
	p.Memory, p.Iterations = 1024, 1
	return p
}

func TestHashVerifies(t *testing.T) {
	h := crypto.NewArgon2id(fast())

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, _, err := h.Verify(encoded, "correct horse battery staple")
	if err != nil || !ok {
		t.Errorf("the password did not verify against its own hash (ok=%v, err=%v)", ok, err)
	}

	ok, _, err = h.Verify(encoded, "correct horse battery stapl")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("a wrong password verified")
	}
}

// TestTheSamePasswordHashesDifferently is the salt.
//
// Without a per-password salt, two users who choose the same password share a digest, and one
// precomputed table breaks both at once.
func TestTheSamePasswordHashesDifferently(t *testing.T) {
	h := crypto.NewArgon2id(fast())

	first, err := h.Hash("same password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Hash("same password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("the same password produced identical hashes — the salt is not random")
	}
	// Both must still verify.
	for i, encoded := range []string{first, second} {
		if ok, _, vErr := h.Verify(encoded, "same password"); vErr != nil || !ok {
			t.Errorf("hash %d did not verify", i)
		}
	}
}

func TestEncodingIsPHC(t *testing.T) {
	h := crypto.NewArgon2id(fast())
	encoded, err := h.Hash("x-long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Errorf("encoding = %q, want a PHC string carrying the parameters", encoded)
	}
	if n := len(strings.Split(encoded, "$")); n != 6 {
		t.Errorf("encoding has %d segments, want 6", n)
	}
}

// TestMalformedEncodingIsATypedErrorNotATrue is the important negative case.
//
// A parser that returns (false, nil) on garbage is survivable; one that returns (true, nil) is
// a total authentication bypass. Every one of these must be an error, never a match.
func TestMalformedEncodingIsATypedErrorNotATrue(t *testing.T) {
	h := crypto.NewArgon2id(fast())

	valid, err := h.Hash("a valid password here")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid, "$")

	cases := map[string]string{
		"empty":               "",
		"not phc":             "just-a-string",
		"too few segments":    "$argon2id$v=19$m=1024,t=1,p=1",
		"wrong algorithm":     "$bcrypt$v=19$m=1024,t=1,p=1$" + parts[4] + "$" + parts[5],
		"wrong version":       "$argon2id$v=18$m=1024,t=1,p=1$" + parts[4] + "$" + parts[5],
		"garbage params":      "$argon2id$v=19$m=x,t=1,p=1$" + parts[4] + "$" + parts[5],
		"trailing param junk": "$argon2id$v=19$m=1024,t=1,p=1XYZ$" + parts[4] + "$" + parts[5],
		"bad base64 salt":     "$argon2id$v=19$m=1024,t=1,p=1$!!!!$" + parts[5],
		"empty digest":        "$argon2id$v=19$m=1024,t=1,p=1$" + parts[4] + "$",
	}

	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			ok, _, vErr := h.Verify(encoded, "a valid password here")
			if ok {
				t.Fatalf("a malformed encoding VERIFIED — this is an authentication bypass")
			}
			if vErr == nil {
				t.Error("a malformed encoding was reported as a plain mismatch, not an error")
			}
			if code := errs.CodeOf(vErr); code != crypto.CodeInvalidEncoding {
				t.Errorf("code = %q, want %q", code, crypto.CodeInvalidEncoding)
			}
		})
	}
}

// TestNeedsRehashTracksTheCurrentPolicy is what makes "raise the cost over time" real.
func TestNeedsRehashTracksTheCurrentPolicy(t *testing.T) {
	weak := fast()
	strong := weak
	strong.Memory *= 4

	oldHasher := crypto.NewArgon2id(weak)
	newHasher := crypto.NewArgon2id(strong)

	encoded, err := oldHasher.Hash("a stable long password")
	if err != nil {
		t.Fatal(err)
	}

	// The old hash still verifies under the new policy — passwords are NOT invalidated.
	ok, needsRehash, err := newHasher.Verify(encoded, "a stable long password")
	if err != nil || !ok {
		t.Fatalf("an old hash stopped verifying after the policy was raised (ok=%v, err=%v)", ok, err)
	}
	if !needsRehash {
		t.Error("a hash below the current policy was not flagged for rehashing")
	}

	// A hash made at the current policy is not flagged.
	fresh, err := newHasher.Hash("a stable long password")
	if err != nil {
		t.Fatal(err)
	}
	if _, needsRehash, err = newHasher.Verify(fresh, "a stable long password"); err != nil || needsRehash {
		t.Errorf("a current-policy hash was flagged for rehashing (err=%v)", err)
	}
}

func TestParamsAtLeastComparesEveryDimension(t *testing.T) {
	base := crypto.Params{Memory: 1000, Iterations: 2, Parallelism: 1, KeyLength: 32}

	if !base.AtLeast(base) {
		t.Error("params are not at least themselves")
	}
	// Weaker in ANY single dimension must fail: a hash with more memory but fewer passes is
	// not obviously stronger, so there is no combined score to compare.
	for name, weaker := range map[string]crypto.Params{
		"memory":      {Memory: 999, Iterations: 2, Parallelism: 1, KeyLength: 32},
		"iterations":  {Memory: 1000, Iterations: 1, Parallelism: 1, KeyLength: 32},
		"parallelism": {Memory: 1000, Iterations: 2, Parallelism: 0, KeyLength: 32},
		"key length":  {Memory: 1000, Iterations: 2, Parallelism: 1, KeyLength: 16},
	} {
		if weaker.AtLeast(base) {
			t.Errorf("params weaker in %s reported as meeting the policy", name)
		}
	}
}

func TestDefaultParamsAreNotAccidentallyWeak(t *testing.T) {
	p := crypto.DefaultParams()
	// A guard on the defaults themselves: an editing accident that drops memory to a few KiB
	// would leave every test passing while making the hash cheap to crack.
	if p.Memory < 15000 {
		t.Errorf("default memory = %d KiB, which is far below the OWASP recommendation", p.Memory)
	}
	if p.Iterations < 1 || p.SaltLength < 16 || p.KeyLength < 32 {
		t.Errorf("default params look wrong: %+v", p)
	}
}
