package config

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
)

// Registry holds every declared setting and feature flag.
//
// Declaration happens in package-level variables, which cannot return an error, and the
// architecture rules forbid panic in production code. Problems found while declaring
// (duplicate key, default of the wrong type, enum default outside the enum) are therefore
// RECORDED and reported together by Validate, which the bootstrap calls once at startup.
// A duplicate key fails on a developer's machine at first boot instead of silently
// resolving last-one-wins by package init order.
type Registry struct {
	mu       sync.RWMutex
	defs     map[string]Definition
	flags    map[string]FlagDef
	problems []error
}

// Definition is a declared setting as the registry stores it: the Def plus the resolved
// storage type and the encoded default. It is what the generated settings UI reads.
type Definition struct {
	Def
	Type           ValueType
	DefaultEncoded string
}

// NewRegistry returns an empty registry. Tests use their own; production uses Default.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}, flags: map[string]FlagDef{}}
}

// defaultRegistry is the registry that package-level Declare* functions write to. It is
// process-wide because setting declarations are package-level variables, which is the
// point: the set of declared settings is a property of the built binary, not of a runtime
// object graph.
var defaultRegistry = NewRegistry()

// Default returns the process-wide registry.
func Default() *Registry { return defaultRegistry }

// ── declaration ─────────────────────────────────────────────────────────────────

// Setting is a typed handle to one declared setting. It is the only way application code
// reads configuration, so call sites hold no key strings and cannot mismatch types.
type Setting[T any] struct {
	key      string
	vt       ValueType
	fallback T
	decode   func(string) (T, error)
	reg      *Registry
}

// Key returns the setting's stable identifier.
func (s *Setting[T]) Key() string { return s.key }

// Default returns the declared default.
func (s *Setting[T]) Default() T { return s.fallback }

// Type returns the declared storage type.
func (s *Setting[T]) Type() ValueType { return s.vt }

// declare validates and records one definition, returning the encoded default.
func (r *Registry) declare(d Def, vt ValueType, def any) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(d.Key) == "" {
		r.problems = append(r.problems, errs.Validation(CodeInvalidKey,
			"a setting was declared with an empty key"))
		return ""
	}
	if existing, dup := r.defs[d.Key]; dup {
		r.problems = append(r.problems, errs.Validation(CodeDuplicateKey,
			"setting key declared more than once").
			WithParam("key", d.Key).
			WithParam("existing_type", string(existing.Type)))
		return ""
	}
	for _, sc := range d.Scopes {
		if !sc.Valid() {
			r.problems = append(r.problems, errs.Validation(CodeScopeNotAllowed,
				"setting declares an unknown scope").
				WithParam("key", d.Key).WithParam("scope", string(sc)))
			return ""
		}
	}

	encoded, err := encodeAny(vt, def)
	if err != nil {
		r.problems = append(r.problems, errs.Wrap(err, errs.CategoryValidation, CodeInvalidDefault,
			"setting default is not valid for its declared type").WithParam("key", d.Key))
		return ""
	}
	if vt == TypeEnum {
		s, _ := def.(string)
		if !contains(d.Enum, s) {
			r.problems = append(r.problems, errs.Validation(CodeInvalidDefault,
				"enum setting default is not one of its permitted values").
				WithParam("key", d.Key).WithParam("default", s))
			return ""
		}
	}

	r.defs[d.Key] = Definition{Def: d, Type: vt, DefaultEncoded: encoded}
	return encoded
}

func declareTyped[T any](r *Registry, d Def, vt ValueType, zero T, dec func(string) (T, error)) *Setting[T] {
	fallback, ok := d.Default.(T)
	if !ok && d.Default != nil {
		// Recorded by declare below via encodeAny; keep the zero value so the handle is
		// still usable and the process still reaches Validate, which reports the problem.
		fallback = zero
	}
	if d.Default == nil {
		d.Default = fallback
	}
	r.declare(d, vt, d.Default)
	return &Setting[T]{key: d.Key, vt: vt, fallback: fallback, decode: dec, reg: r}
}

// DeclareBool declares a boolean setting on the registry.
func (r *Registry) DeclareBool(d Def) *Setting[bool] {
	return declareTyped(r, d, TypeBool, false, decodeBool)
}

// DeclareString declares a free-text setting.
func (r *Registry) DeclareString(d Def) *Setting[string] {
	return declareTyped(r, d, TypeString, "", decodeString)
}

// DeclareInt declares an integer setting.
func (r *Registry) DeclareInt(d Def) *Setting[int64] {
	return declareTyped(r, d, TypeInt, 0, decodeInt)
}

// DeclareEnum declares a setting restricted to Def.Enum.
func (r *Registry) DeclareEnum(d Def) *Setting[string] {
	return declareTyped(r, d, TypeEnum, "", decodeString)
}

// DeclareDuration declares a duration setting, stored as integer nanoseconds.
func (r *Registry) DeclareDuration(d Def) *Setting[time.Duration] {
	return declareTyped(r, d, TypeDuration, 0, decodeDuration)
}

// DeclareID declares a setting holding a reference to another entity.
func (r *Registry) DeclareID(d Def) *Setting[id.ID] {
	return declareTyped(r, d, TypeID, "", decodeID)
}

// DeclareMoney declares a monetary setting. The currency is stored with the amount, so
// the value stays meaningful independently of the currency table.
func (r *Registry) DeclareMoney(d Def) *Setting[money.Money] {
	return declareTyped(r, d, TypeMoney, money.Money{}, decodeMoney)
}

// Package-level declaration against the process registry — the form modules use.
func DeclareBool(d Def) *Setting[bool]              { return defaultRegistry.DeclareBool(d) }
func DeclareString(d Def) *Setting[string]          { return defaultRegistry.DeclareString(d) }
func DeclareInt(d Def) *Setting[int64]              { return defaultRegistry.DeclareInt(d) }
func DeclareEnum(d Def) *Setting[string]            { return defaultRegistry.DeclareEnum(d) }
func DeclareDuration(d Def) *Setting[time.Duration] { return defaultRegistry.DeclareDuration(d) }
func DeclareID(d Def) *Setting[id.ID]               { return defaultRegistry.DeclareID(d) }
func DeclareMoney(d Def) *Setting[money.Money]      { return defaultRegistry.DeclareMoney(d) }

// ── flags ───────────────────────────────────────────────────────────────────────

// Flag is a typed handle to a declared feature flag.
type Flag struct {
	key      string
	fallback bool
	reg      *Registry
}

// Key returns the flag's stable identifier.
func (f *Flag) Key() string { return f.key }

// Default returns the declared default.
func (f *Flag) Default() bool { return f.fallback }

// DeclareFlag declares a feature flag on the registry.
func (r *Registry) DeclareFlag(d FlagDef) *Flag {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case strings.TrimSpace(d.Key) == "":
		r.problems = append(r.problems, errs.Validation(CodeInvalidKey,
			"a feature flag was declared with an empty key"))
	case r.hasFlagLocked(d.Key):
		r.problems = append(r.problems, errs.Validation(CodeDuplicateKey,
			"feature flag key declared more than once").WithParam("key", d.Key))
	case d.Stability == Deprecated && strings.TrimSpace(d.RemoveBy) == "":
		// A deprecation with no removal target is how dead branches become permanent.
		r.problems = append(r.problems, errs.Validation(CodeInvalidDefault,
			"deprecated feature flag has no RemoveBy release").WithParam("key", d.Key))
	default:
		r.flags[d.Key] = d
	}
	return &Flag{key: d.Key, fallback: d.Default, reg: r}
}

// DeclareFlag declares a feature flag on the process registry.
func DeclareFlag(d FlagDef) *Flag { return defaultRegistry.DeclareFlag(d) }

func (r *Registry) hasFlagLocked(key string) bool {
	_, ok := r.flags[key]
	return ok
}

// ── inspection & validation ─────────────────────────────────────────────────────

// Lookup returns the definition for key.
func (r *Registry) Lookup(key string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.defs[key]
	return d, ok
}

// LookupFlag returns the declaration for a feature flag key.
func (r *Registry) LookupFlag(key string) (FlagDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.flags[key]
	return f, ok
}

// Definitions returns every declared setting, ordered by key.
//
// Sorted, not map order: this drives the generated settings UI, and an unsorted list
// would reshuffle the screen on every launch.
func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Flags returns every declared feature flag, ordered by key.
func (r *Registry) Flags() []FlagDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FlagDef, 0, len(r.flags))
	for _, f := range r.flags {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Validate reports every problem recorded during declaration, as one aggregated error.
// The bootstrap calls it once, before anything else runs.
func (r *Registry) Validate() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.problems) == 0 {
		return nil
	}
	return errs.Wrap(errors.Join(r.problems...), errs.CategoryInternal, CodeRegistryInvalid,
		"the configuration registry has "+strconv.Itoa(len(r.problems))+" declaration problem(s)")
}

// ExpiredFlags returns deprecated flags whose RemoveBy release has been reached, so a
// build that still carries them can be failed in CI rather than accumulating dead
// branches forever (ARCHITECTURE_v1 §17).
//
// Separate from Validate because it needs the current version, which a development build
// does not meaningfully have.
func (r *Registry) ExpiredFlags(currentVersion string) []FlagDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []FlagDef
	for _, f := range r.flags {
		if f.RemoveBy == "" {
			continue
		}
		if compareVersions(currentVersion, f.RemoveBy) >= 0 {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// compareVersions compares dotted numeric versions ("v1.4.0", "1.4"), returning -1, 0, or
// 1. Non-numeric components (a "dev" build) compare as lower than any release.
func compareVersions(a, b string) int {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	var out []int
	for _, seg := range strings.Split(v, ".") {
		n, err := strconv.Atoi(seg)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
