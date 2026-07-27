package errs

import (
	"errors"
	"fmt"
)

// Category classifies an error by how the application (and ultimately the UI) should
// treat it. It maps directly to the treatment table in ARCHITECTURE_v1 §7.6.
type Category uint8

const (
	// CategoryInternal is a bug or infrastructure fault — show a generic message, log detail.
	CategoryInternal Category = iota
	// CategoryValidation is bad input, usually field-level — show inline errors.
	CategoryValidation
	// CategoryNotFound is a missing entity — show an empty state.
	CategoryNotFound
	// CategoryConflict is an invariant/state violation — explain and let the user retry.
	CategoryConflict
	// CategoryPermission is an authorization denial — block with a reason.
	CategoryPermission
)

func (c Category) String() string {
	switch c {
	case CategoryValidation:
		return "validation"
	case CategoryNotFound:
		return "not_found"
	case CategoryConflict:
		return "conflict"
	case CategoryPermission:
		return "permission"
	default:
		return "internal"
	}
}

// FieldError is a per-field validation problem. Code doubles as an i18n key.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is a typed, coded domain error.
//
// Every Error carries a Category (how to treat it), a stable Code (which doubles as
// the i18n key — the backend returns codes, not prose, per §5.4), optional
// interpolation Params, optional field-level errors, and an optional wrapped cause
// for logs. Codes are part of the public contract and must never be renamed once shipped.
//
// Errors are built at the throw site with the builder methods; do not share a single
// *Error value and mutate it.
type Error struct {
	Category Category
	Code     string
	Message  string // developer-facing (English); the UI renders Code+Params instead
	Params   map[string]string
	Fields   []FieldError
	cause    error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s [%s]: %s: %v", e.Category, e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s [%s]: %s", e.Category, e.Code, e.Message)
}

// Unwrap exposes the wrapped cause to errors.Is/As.
func (e *Error) Unwrap() error { return e.cause }

// New builds an Error with an explicit category.
func New(cat Category, code, msg string) *Error {
	return &Error{Category: cat, Code: code, Message: msg}
}

// Category constructors.
func Internal(code, msg string) *Error   { return New(CategoryInternal, code, msg) }
func Validation(code, msg string) *Error { return New(CategoryValidation, code, msg) }
func NotFound(code, msg string) *Error   { return New(CategoryNotFound, code, msg) }
func Conflict(code, msg string) *Error   { return New(CategoryConflict, code, msg) }
func Permission(code, msg string) *Error { return New(CategoryPermission, code, msg) }

// Wrap builds an Error that wraps an underlying cause (visible in logs and errors.Is/As,
// but never surfaced to the UI, which uses Code+Params).
func Wrap(cause error, cat Category, code, msg string) *Error {
	return &Error{Category: cat, Code: code, Message: msg, cause: cause}
}

// WithParam adds an interpolation parameter for the UI to render into the message.
func (e *Error) WithParam(key, value string) *Error {
	if e.Params == nil {
		e.Params = make(map[string]string)
	}
	e.Params[key] = value
	return e
}

// WithField appends a field-level error (for CategoryValidation).
func (e *Error) WithField(field, code, msg string) *Error {
	e.Fields = append(e.Fields, FieldError{Field: field, Code: code, Message: msg})
	return e
}

// AsError extracts an *Error from err's chain, if present.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// CategoryOf returns the category of err, defaulting to CategoryInternal for errors
// that are not (and do not wrap) an *Error.
func CategoryOf(err error) Category {
	if e, ok := AsError(err); ok {
		return e.Category
	}
	return CategoryInternal
}

// IsCategory reports whether err is (or wraps) an *Error of the given category.
func IsCategory(err error, cat Category) bool {
	e, ok := AsError(err)
	return ok && e.Category == cat
}

// CodeOf returns the stable code of err, or "" if it is not an *Error.
func CodeOf(err error) string {
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return ""
}
