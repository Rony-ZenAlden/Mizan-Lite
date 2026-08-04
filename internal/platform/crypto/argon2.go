// Package crypto holds the password hashing the identity module depends on.
//
// It lives in platform (§4) rather than inside identity because it is a technical capability,
// not a business rule, and because a second consumer — PIN credentials in Phase 5, and
// field-level encryption later — is already foreseen.
//
// The surface is deliberately tiny. Everything that could be got subtly wrong (salt
// generation, constant-time comparison, parameter encoding) happens here once, and no caller
// is offered a knob it could set incorrectly.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeHashFailed      = "crypto.hash_failed"
	CodeInvalidEncoding = "crypto.invalid_encoding"
)

// Algorithm is the discriminator stored beside the encoded hash.
const Algorithm = "argon2id"

// Params are the Argon2id cost parameters.
type Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams are the current cost settings.
//
// OWASP's recommended set offers several (memory, iteration) pairs of comparable strength.
// This picks 19 MiB / 2 passes rather than 46 MiB / 1 pass because a POS switches users
// constantly (§13.1) on a shop's hardware, not a server: a login that allocates 46 MiB and
// stalls a cashier is a real operational cost, and the two options resist offline cracking
// comparably.
//
// Raising these is the intended path as hardware improves — Verify reports when a stored hash
// is below them, and the identity service rehashes on the next successful login, so an
// increase upgrades the whole user base without invalidating a single password (§13.1).
func DefaultParams() Params {
	return Params{
		Memory:      19456, // 19 MiB
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// AtLeast reports whether p meets or exceeds the cost of other.
//
// Compared per-dimension rather than by a combined score: a hash with more memory but fewer
// passes is not obviously "stronger", and pretending a single number orders them would make
// the rehash decision arbitrary. Weaker in ANY dimension means it should be upgraded.
func (p Params) AtLeast(other Params) bool {
	return p.Memory >= other.Memory &&
		p.Iterations >= other.Iterations &&
		p.Parallelism >= other.Parallelism &&
		p.KeyLength >= other.KeyLength
}

// Hasher hashes and verifies passwords.
type Hasher interface {
	// Hash returns a PHC-encoded Argon2id string carrying the salt and the parameters.
	Hash(password string) (string, error)
	// Verify reports whether password matches encoded, and whether the stored parameters have
	// fallen below the current policy.
	//
	// needsRehash is returned alongside the verdict rather than exposing the parameters,
	// because the only legitimate reason to ask about them is to decide whether to upgrade.
	Verify(encoded, password string) (ok bool, needsRehash bool, err error)
}

type argon2Hasher struct{ params Params }

// NewArgon2id builds a hasher with the given parameters.
func NewArgon2id(p Params) Hasher { return &argon2Hasher{params: p} }

func (h *argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		// Entropy failure is real and must not be papered over: a predictable salt is worse
		// than no hash at all, because it looks fine.
		return "", errs.Wrap(err, errs.CategoryInternal, CodeHashFailed,
			"could not generate a salt")
	}
	return encode(h.params, salt, derive(h.params, salt, password)), nil
}

func (h *argon2Hasher) Verify(encoded, password string) (bool, bool, error) {
	stored, salt, digest, err := decode(encoded)
	if err != nil {
		return false, false, err
	}

	candidate := derive(stored, salt, password)

	// Constant-time comparison. A byte-by-byte == leaks the digest one byte at a time to an
	// attacker who can measure it.
	if subtle.ConstantTimeCompare(candidate, digest) != 1 {
		return false, false, nil
	}
	return true, !stored.AtLeast(h.params), nil
}

// derive runs the KDF. Split out so Hash and Verify cannot drift.
func derive(p Params, salt []byte, password string) []byte {
	return argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
}

// ── the PHC encoding ────────────────────────────────────────────────────────────
//
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
//
// One self-describing string rather than a hash column plus a params column (Step 1.2, D2):
// the PHC form already carries the algorithm, version, and every parameter, and a second copy
// of one fact is what Step 0.4 rejected goose over and what 0.7 D2 refused for job state.

var b64 = base64.RawStdEncoding

func encode(p Params, salt, digest []byte) string {
	return "$" + Algorithm +
		"$v=" + strconv.Itoa(argon2.Version) +
		"$m=" + strconv.FormatUint(uint64(p.Memory), 10) +
		",t=" + strconv.FormatUint(uint64(p.Iterations), 10) +
		",p=" + strconv.FormatUint(uint64(p.Parallelism), 10) +
		"$" + b64.EncodeToString(salt) +
		"$" + b64.EncodeToString(digest)
}

func invalid(msg string) error {
	// Deliberately uninformative about WHICH part failed: this string can reach a log, and a
	// parser that narrates the shape of a stored credential is a gift to anyone reading it.
	return errs.Validation(CodeInvalidEncoding, "the stored credential is not readable: "+msg)
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=…,t=…,p=…", salt, hash]
	if len(parts) != 6 || parts[0] != "" {
		return Params{}, nil, nil, invalid("malformed")
	}
	if parts[1] != Algorithm {
		return Params{}, nil, nil, invalid("unsupported algorithm")
	}

	var version int
	if n, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || n != 1 {
		return Params{}, nil, nil, invalid("version")
	}
	if version != argon2.Version {
		return Params{}, nil, nil, invalid("unsupported version")
	}

	var memory, iterations, parallelism uint64
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&memory, &iterations, &parallelism); err != nil || n != 3 {
		return Params{}, nil, nil, invalid("parameters")
	}
	// Sscanf ignores trailing garbage ("p=1XYZ" scans as 1), so the segment is re-rendered and
	// compared. A parser that silently accepts a value it did not fully understand is how a
	// tampered credential gets treated as a valid one.
	canonical := "m=" + strconv.FormatUint(memory, 10) +
		",t=" + strconv.FormatUint(iterations, 10) +
		",p=" + strconv.FormatUint(parallelism, 10)
	if parts[3] != canonical {
		return Params{}, nil, nil, invalid("parameters")
	}
	if memory > math.MaxUint32 || iterations > math.MaxUint32 || parallelism > math.MaxUint8 {
		return Params{}, nil, nil, invalid("parameters out of range")
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, invalid("salt")
	}
	digest, err := b64.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, invalid("digest")
	}
	if len(salt) == 0 || len(digest) == 0 {
		return Params{}, nil, nil, invalid("empty")
	}

	return Params{
		Memory:      uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
		SaltLength:  uint32(len(salt)),
		KeyLength:   uint32(len(digest)),
	}, salt, digest, nil
}
