// Package envelope is the uniform shape every Wails binding returns (ARCHITECTURE_v1 §5.4).
//
// One shape means the frontend has exactly one error path instead of a convention per screen.
//
// The rule it enforces is the important part: **the backend never returns a human-readable
// English string.** It returns a stable code and parameters, and the frontend renders them in
// the active locale. That is what makes "no restart on language change" true for error
// messages too — translated text coming from the backend would leave every already-rendered
// error stuck in the old language (§22.2).
//
// Step 0.8's coverage gate is what makes this safe: every declared error code is guaranteed to
// have a translation in every locale, so a code crossing this boundary always renders.
package envelope

import (
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// FieldError is a per-field validation failure.
type FieldError struct {
	Field      string            `json:"field"`
	Code       string            `json:"code"`
	MessageKey string            `json:"messageKey"`
	Params     map[string]string `json:"params,omitempty"`
}

// APIError is a failure as the frontend receives it.
//
// Note what is absent: any message text. Code and MessageKey carry the same value — Code is
// what business logic branches on, MessageKey is what the translation layer looks up — and
// they are separate fields so the two uses can diverge later without breaking either.
type APIError struct {
	Code       string            `json:"code"`
	MessageKey string            `json:"messageKey"`
	Params     map[string]string `json:"params,omitempty"`
	Fields     []FieldError      `json:"fields,omitempty"`
}

// Result is the envelope every binding method returns.
type Result[T any] struct {
	OK    bool      `json:"ok"`
	Data  T         `json:"data,omitempty"`
	Error *APIError `json:"error,omitempty"`
}

// Ok wraps a successful value.
func Ok[T any](v T) Result[T] { return Result[T]{OK: true, Data: v} }

// Fail maps an error into the envelope.
//
// A typed kernel error passes straight through: its Code and Params are already the shape the
// frontend needs. An untyped error becomes a generic internal code — deliberately WITHOUT its
// message, because an unexpected error's text is a Go string written for a developer, and
// leaking it would put untranslated (and possibly sensitive) English in front of a customer.
// The real text goes to the log, where it belongs.
func Fail[T any](err error) Result[T] {
	return Result[T]{OK: false, Error: FromError(err)}
}

// CodeInternal is returned for errors that are not typed kernel errors.
const CodeInternal = "database.internal"

// FromError converts an error into an APIError.
func FromError(err error) *APIError {
	if err == nil {
		return nil
	}
	e, ok := errs.AsError(err)
	if !ok {
		return &APIError{Code: CodeInternal, MessageKey: CodeInternal}
	}

	out := &APIError{Code: e.Code, MessageKey: e.Code, Params: e.Params}
	for _, f := range e.Fields {
		out.Fields = append(out.Fields, FieldError{
			Field: f.Field, Code: f.Code, MessageKey: f.Code,
		})
	}
	return out
}
