package id

import (
	"github.com/google/uuid"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract —
// never rename once shipped.
const (
	CodeGenerateFailed = "id.generate_failed"
	CodeInvalid        = "id.invalid"
)

// ID is a UUIDv7 in canonical text form, stored as CHAR(36).
//
// Text rather than 16 raw bytes: the portable type contract (ARCHITECTURE_v1 §8.1) fixes
// CHAR(36) across every engine, and an identifier a support engineer can read aloud from a
// customer's screen is worth more than the 20 bytes per row that a binary form would save.
//
// A distinct type rather than a bare string so that a function taking an ID cannot be
// handed an arbitrary string, and so misuse is a compile error.
type ID string

// New returns a fresh UUIDv7.
//
// v7 is time-ordered (ARCHITECTURE_v1 §7.4): sequential inserts land at the right-hand
// edge of the B-tree instead of scattering across it the way v4 does, which matters for a
// database that will accumulate years of documents on modest hardware. It stays
// collision-free across branches, so a future multi-branch sync never has to renumber.
//
// Returns an error rather than panicking: generation reads the system entropy source,
// which can fail, and the architecture rules forbid panic in production code.
func New() (ID, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeGenerateFailed,
			"generating a UUIDv7 identifier")
	}
	return ID(u.String()), nil
}

// Parse validates s and returns it as an ID.
//
// Any valid UUID version is accepted, not only v7: rows written by earlier builds, or
// imported from another system, remain readable. The nil UUID is rejected because it is
// how an uninitialised value reaches the database, and a "valid" all-zero primary key is
// far harder to trace back than a rejected write.
func Parse(s string) (ID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryValidation, CodeInvalid,
			"value is not a valid UUID").WithParam("value", s)
	}
	if u == uuid.Nil {
		return "", errs.Validation(CodeInvalid, "the nil UUID is not a valid identifier")
	}
	return ID(u.String()), nil
}

// IsZero reports whether the ID is unset.
func (i ID) IsZero() bool { return i == "" }

// String returns the canonical text form.
func (i ID) String() string { return string(i) }
