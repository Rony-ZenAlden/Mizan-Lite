// Package errs provides typed, coded domain errors. Every error carries a category
// (Validation, NotFound, Conflict, Permission, Internal), a stable code that doubles
// as an i18n key, interpolation params, and optional field-level errors.
//
// The backend returns codes and params, never human-readable prose — the frontend
// renders them in the active locale.
//
// See docs/architecture/ARCHITECTURE_v1.md §7.6 and §5.4.
package errs
