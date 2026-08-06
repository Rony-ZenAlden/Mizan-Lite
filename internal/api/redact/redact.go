// Package redact gates individual FIELDS of a response behind a permission.
//
// §14.2: "can see cost price" and "can see profit margin" are permissions, not screens, and
// they are enforced at the DTO-mapping layer "so restricted fields are never serialized to the
// frontend at all". Hiding a field in the UI is not security — the value has already crossed
// the boundary and sits in the webview's memory, in any IPC log, and in a devtools inspector.
//
// # Populate only if permitted (Step 1.6, D1)
//
// The obvious implementation builds the DTO and then blanks the restricted value. It is secure,
// and it is still the wrong design:
//
//   - A blank field is indistinguishable from a genuinely empty one. The user cannot tell "there
//     is nothing here" from "there is something here you may not see" — different facts, and the
//     second is the one that makes someone go and ask.
//   - The safe state depends on REMEMBERING to blank. Every new path that builds the DTO is
//     another place to forget, and forgetting is a silent disclosure.
//
// So the field is a pointer, nil by default, with `json:",omitempty"` — and the only way to set
// it is the function below, which checks first. The property that buys:
//
//	Forgetting the redaction call HIDES data rather than leaking it.
//
// A path that neglects to call Visible produces a nil field, which serialises as absent. The
// failure mode of carelessness is a missing field someone reports as a bug, not a leaked one
// nobody notices.
//
// # No registry, deliberately (D2)
//
// A registry mapping "field X requires permission P" would let a field be declared restricted
// and still be populated by a path that never consults the registry. That is exactly the
// failure this design removes. Here there is one way to set the field, and it checks.
package redact

import (
	"context"

	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// Visible returns a pointer to value when the actor holds permission, and nil when they do not.
//
// Nil is the safe answer for every reason it could be nil: no actor, no grant, no authorizer.
// A restricted field is absent unless someone has affirmatively earned it.
func Visible[T any](ctx context.Context, authz auth.Authorizer, permission string, value T) *T {
	return VisibleIn(ctx, authz, permission, auth.Global(), value)
}

// VisibleIn is Visible at a non-global scope.
func VisibleIn[T any](
	ctx context.Context, authz auth.Authorizer, permission string, scope auth.Scope, value T,
) *T {
	if authz == nil || permission == "" {
		// A missing authorizer is a wiring defect, and a missing permission is a caller defect.
		// Both deny: a redaction helper that fails open would defeat its own purpose.
		return nil
	}
	if !authz.Can(ctx, permission, scope) {
		return nil
	}
	return &value
}
