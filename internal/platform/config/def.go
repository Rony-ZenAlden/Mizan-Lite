// Package config is the Setting and Feature-Flag registry: the mechanism behind Mizan's
// overriding mandate that the product adapts through configuration, not code.
//
// Settings are DECLARED in code — key, type, default, permitted scopes, permission — and
// read through the typed handle that declaration returns:
//
//	var TaxEnabled = config.DeclareBool(config.Def{Key: "tax.enabled", Default: false, ...})
//	if TaxEnabled.Get(ctx) { ... }
//
// Call sites therefore contain no key strings and no type assertions: a mistyped key is a
// compile error rather than a silently-wrong default on a customer's till, and a setting
// cannot be read as the wrong type. A dynamic, string-keyed path remains for the generated
// settings UI and for writes (see Settings.Set).
//
// See docs/architecture/ARCHITECTURE_v1.md §16–§17 and
// docs/architecture/STEP_0_5_CONFIG_REGISTRIES.md.
package config

// Stable error codes. They double as i18n keys and are part of the public contract —
// never rename once shipped.
const (
	CodeUndeclared      = "config.undeclared_key"
	CodeDuplicateKey    = "config.duplicate_key"
	CodeInvalidDefault  = "config.invalid_default"
	CodeInvalidKey      = "config.invalid_key"
	CodeScopeNotAllowed = "config.scope_not_allowed"
	CodeInvalidValue    = "config.invalid_value"
	CodeNotPermitted    = "config.not_permitted"
	CodeLoadFailed      = "config.load_failed"
	CodeWriteFailed     = "config.write_failed"
	CodeRegistryInvalid = "config.registry_invalid"
)

// Scope is a level at which a setting may be overridden.
//
// Session is deliberately absent: it is an in-memory, request-scoped override carried in
// the context (see WithSessionOverride) and is never persisted, so it has no row in the
// settings table and no entry here.
type Scope string

const (
	ScopeSystem  Scope = "system"
	ScopeCompany Scope = "company"
	ScopeBranch  Scope = "branch"
	ScopeUser    Scope = "user"
)

// resolutionOrder is the persisted part of the chain in ARCHITECTURE_v1 §16.1, most
// specific first. Resolution takes the first value present and stops.
//
//	Session ► User ► Branch ► Company ► System ► code default
//
// A business profile (§16.4) is NOT a tier here: applying one WRITES company-scope rows,
// which is exactly what keeps every value individually editable afterwards.
var resolutionOrder = []Scope{ScopeUser, ScopeBranch, ScopeCompany, ScopeSystem}

// Valid reports whether s is a persisted scope.
func (s Scope) Valid() bool {
	switch s {
	case ScopeSystem, ScopeCompany, ScopeBranch, ScopeUser:
		return true
	default:
		return false
	}
}

// ValueType is the storage type of a setting. The set matches the CHECK constraint on
// settings.value_type in migrations/sqlite/0001_platform.sql; adding one requires a
// migration, which is deliberate.
type ValueType string

const (
	TypeString   ValueType = "string"
	TypeInt      ValueType = "int"
	TypeBool     ValueType = "bool"
	TypeMoney    ValueType = "money"
	TypeJSON     ValueType = "json"
	TypeEnum     ValueType = "enum"
	TypeID       ValueType = "id"
	TypeDuration ValueType = "duration"
)

// Def declares a setting. The owning module states it once; nothing else may invent one.
type Def struct {
	// Key is the stable, namespaced identifier, e.g. "sales.rounding_stage". Part of the
	// public contract once shipped — renaming one orphans every customer's stored value.
	Key string
	// Default is the value used when no scope defines one. Its Go type must match the
	// Declare constructor used; a mismatch is reported by Registry.Validate.
	Default any
	// Scopes lists the scopes permitted to override this setting. A write to any other
	// scope is rejected. Empty means system-only.
	Scopes []Scope
	// Enum lists the legal values for DeclareEnum; ignored otherwise.
	Enum []string
	// Permission, when set, is required to change this setting. Enforced through the
	// Authorizer port, which permits everything until RBAC arrives in Phase 1.
	Permission string
	// Description is an i18n KEY, never prose: the backend never produces user-facing
	// text (ARCHITECTURE_v1 §22.2).
	Description string
	// Validate optionally adds domain validation beyond type and enum checks.
	Validate func(any) error
}

// Stability is a feature flag's lifecycle stage (ARCHITECTURE_v1 §17). Flags declare one
// so the codebase does not accumulate permanent dead branches.
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// FlagDef declares a feature flag.
//
// Flags are not simply bool settings: they carry a lifecycle, and §17 requires them to be
// enforced in three places (navigation, use case, and seeding), which wants them
// enumerable as a distinct kind.
type FlagDef struct {
	Key       string
	Default   bool
	Stability Stability
	// RemoveBy names the release by which a deprecated flag must be gone, e.g. "v1.4".
	// Registry.Validate reports flags that outlived it.
	RemoveBy    string
	Scopes      []Scope
	Permission  string
	Description string
}
