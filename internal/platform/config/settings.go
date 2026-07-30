package config

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// Database is the persistence surface Settings needs: the executor resolution of
// database.DB plus the Unit of Work, both satisfied by *database.Store.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures a Settings.
type Options struct {
	// Registry defaults to the process registry.
	Registry *Registry
	// Scopes reports the acting company/branch/user. Defaults to NoScopes.
	Scopes ScopeProvider
	// Notifier receives change notifications. Defaults to NoNotifier.
	Notifier ChangeNotifier
	// Authorizer gates permissioned settings. Defaults to AllowAll (Phase 0 has no RBAC).
	Authorizer Authorizer
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// Clock defaults to the system clock.
	Clock clock.Clock
}

// Settings resolves declared settings against the database, by scope, through an
// in-memory snapshot.
//
// The whole table is loaded once and swapped atomically on write (design D5). Settings
// are a small, bounded set on a single-user desktop database, so per-key lazy caching
// would buy nothing and cost an invalidation protocol; a map lookup under an RWMutex is
// the entire read path.
type Settings struct {
	db     Database
	reg    *Registry
	scopes ScopeProvider
	notify ChangeNotifier
	auth   Authorizer
	log    *slog.Logger
	clk    clock.Clock

	mu      sync.RWMutex
	values  map[valueKey]string
	flags   map[flagKey]bool
	version int64
}

type valueKey struct {
	scope   Scope
	scopeID id.ID
	key     string
}

// Open builds a Settings and loads the snapshot.
func Open(ctx context.Context, db Database, opts Options) (*Settings, error) {
	if db == nil {
		return nil, errs.Internal(CodeLoadFailed, "config: a database is required")
	}
	if opts.Registry == nil {
		opts.Registry = Default()
	}
	if opts.Scopes == nil {
		opts.Scopes = NoScopes{}
	}
	if opts.Notifier == nil {
		opts.Notifier = NoNotifier{}
	}
	if opts.Authorizer == nil {
		opts.Authorizer = AllowAll{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}

	s := &Settings{
		db: db, reg: opts.Registry, scopes: opts.Scopes,
		notify: opts.Notifier, auth: opts.Authorizer,
		log: opts.Logger, clk: opts.Clock,
		values: map[valueKey]string{},
		flags:  map[flagKey]bool{},
	}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Version is the snapshot generation, incremented on every successful write. Components
// that cache derived state compare it to detect staleness.
func (s *Settings) Version() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// Reload replaces the snapshot from the database.
//
// Also the recovery path when a Set joined an outer transaction that later rolled back
// (see Set): the cache then holds a value the database does not, and Reload restores
// truth.
func (s *Settings) Reload(ctx context.Context) error {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT scope, scope_id, setting_key, setting_value FROM settings`)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "loading settings")
	}
	defer rows.Close()

	next := map[valueKey]string{}
	var unknown []string
	for rows.Next() {
		var scope, scopeID, key string
		var value *string
		if scanErr := rows.Scan(&scope, &scopeID, &key, &value); scanErr != nil {
			return errs.Wrap(scanErr, errs.CategoryInternal, CodeLoadFailed, "scanning settings")
		}
		if value == nil {
			continue
		}
		if _, declared := s.reg.Lookup(key); !declared {
			unknown = append(unknown, key)
			continue
		}
		next[valueKey{scope: Scope(scope), scopeID: id.ID(scopeID), key: key}] = *value
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return errs.Wrap(rowsErr, errs.CategoryInternal, CodeLoadFailed, "reading settings")
	}

	// Unknown keys are reported and ignored, never fatal (design D3). A customer who
	// downgrades past a removed setting must still be able to open their shop; the row
	// is inert because no code reads it. Contrast the migration version gate, where
	// refusing to start prevents data corruption — here, continuing costs nothing.
	if len(unknown) > 0 {
		s.log.WarnContext(ctx, "ignoring settings rows with no declared key",
			slog.Int("count", len(unknown)),
			slog.Any("keys", unknown))
	}

	nextFlags, err := s.loadFlags(ctx)
	if err != nil {
		return err
	}

	// Both maps are swapped under one lock, so a reader never sees settings from the new
	// snapshot alongside flags from the old one.
	s.mu.Lock()
	s.values = next
	s.flags = nextFlags
	s.version++
	s.mu.Unlock()
	return nil
}

// UnknownKeys returns stored keys that no code declares, for the diagnostics screen.
func (s *Settings) UnknownKeys(ctx context.Context) ([]string, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT DISTINCT setting_key FROM settings`)
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "listing setting keys")
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "scanning setting keys")
		}
		if _, declared := s.reg.Lookup(key); !declared {
			out = append(out, key)
		}
	}
	return out, rows.Err()
}

// resolveEncoded walks the scope chain, most specific first, and returns the first stored
// value found.
func (s *Settings) resolveEncoded(ctx context.Context, key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, scope := range resolutionOrder {
		scopeID, ok := s.scopeID(ctx, scope)
		if !ok {
			continue // no such scope is active; skip this tier
		}
		if v, found := s.values[valueKey{scope: scope, scopeID: scopeID, key: key}]; found {
			return v, true
		}
	}
	return "", false
}

// scopeID returns the active identifier for a scope. System scope is always active and
// uses the empty identifier, matching scope_id's NOT NULL DEFAULT ” in the schema.
func (s *Settings) scopeID(ctx context.Context, scope Scope) (id.ID, bool) {
	switch scope {
	case ScopeSystem:
		return "", true
	case ScopeCompany:
		return s.scopes.CompanyID(ctx)
	case ScopeBranch:
		return s.scopes.BranchID(ctx)
	case ScopeUser:
		return s.scopes.UserID(ctx)
	default:
		return "", false
	}
}

// Set writes a setting at one scope.
//
// It runs inside a Unit of Work; a nested call joins the caller's transaction (Step 0.3
// semantics). The snapshot is patched only after Do returns successfully. When Do JOINED
// an outer transaction that later rolls back, the snapshot will hold a value the database
// does not — call Reload in that case. Settings changes are normally their own use case,
// so this is a documented edge rather than the usual path.
func (s *Settings) Set(ctx context.Context, scope Scope, scopeID id.ID, key string, v any) error {
	def, ok := s.reg.Lookup(key)
	if !ok {
		return errs.Validation(CodeUndeclared,
			"cannot write an undeclared setting").WithParam("key", key)
	}
	if !scopeAllowed(def, scope) {
		return errs.Validation(CodeScopeNotAllowed,
			"this setting may not be overridden at that scope").
			WithParam("key", key).WithParam("scope", string(scope))
	}
	if def.Permission != "" && !s.auth.Can(ctx, def.Permission) {
		return errs.Permission(CodeNotPermitted,
			"changing this setting requires a permission the caller does not hold").
			WithParam("key", key).WithParam("permission", def.Permission)
	}
	if def.Type == TypeEnum {
		sv, _ := v.(string)
		if !contains(def.Enum, sv) {
			return errs.Validation(CodeInvalidValue,
				"value is not one of the permitted values for this setting").
				WithParam("key", key).WithParam("value", sv)
		}
	}
	if def.Validate != nil {
		if err := def.Validate(v); err != nil {
			return errs.Wrap(err, errs.CategoryValidation, CodeInvalidValue,
				"setting value failed its declared validation").WithParam("key", key)
		}
	}
	encoded, err := encodeAny(def.Type, v)
	if err != nil {
		return err
	}
	// System scope always stores the empty identifier, so the unique constraint on
	// (scope, scope_id, setting_key) has exactly one row per system setting.
	if scope == ScopeSystem {
		scopeID = ""
	} else if scopeID.IsZero() {
		return errs.Validation(CodeInvalidValue,
			"a non-system scope requires a scope identifier").WithParam("scope", string(scope))
	}

	if err := s.db.Do(ctx, func(ctx context.Context) error {
		return s.write(ctx, def, scope, scopeID, encoded)
	}); err != nil {
		return err
	}

	s.mu.Lock()
	s.values[valueKey{scope: scope, scopeID: scopeID, key: key}] = encoded
	s.version++
	s.mu.Unlock()

	s.notify.SettingChanged(ctx, key, scope, scopeID)
	return nil
}

func (s *Settings) write(ctx context.Context, def Definition, scope Scope, scopeID id.ID, encoded string) error {
	newID, err := id.New()
	if err != nil {
		return err
	}
	now := clock.Format(s.clk.Now())

	// row_version is deliberately absent from UpdateCols: settings are last-write-wins by
	// design. Two users racing to change the same knob is not a conflict worth surfacing,
	// and the column exists for schema uniformity with entities where it does matter.
	stmt := s.db.Dialect().Upsert(dialect.UpsertSpec{
		Table: "settings",
		Columns: []string{
			"id", "scope", "scope_id", "setting_key", "setting_value",
			"value_type", "created_at", "updated_at",
		},
		ConflictCols: []string{"scope", "scope_id", "setting_key"},
		UpdateCols:   []string{"setting_value", "value_type", "updated_at"},
	})
	if _, err := s.db.Writer(ctx).ExecContext(ctx, stmt,
		newID.String(), string(scope), scopeID.String(), def.Key, encoded,
		string(def.Type), now, now,
	); err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeWriteFailed, "writing setting "+def.Key)
	}
	return nil
}

// scopeAllowed reports whether def may be overridden at scope. An empty Scopes list means
// system-only, which is the safe default for a setting whose author did not think about
// per-company override.
func scopeAllowed(def Definition, scope Scope) bool {
	if len(def.Scopes) == 0 {
		return scope == ScopeSystem
	}
	for _, s := range def.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ── context binding ─────────────────────────────────────────────────────────────

type settingsCtxKey struct{}

// Bind attaches s to ctx so typed handles can resolve. The bootstrap does this once on
// the root context; every derived context inherits it.
func Bind(ctx context.Context, s *Settings) context.Context {
	return context.WithValue(ctx, settingsCtxKey{}, s)
}

func fromContext(ctx context.Context) (*Settings, bool) {
	s, ok := ctx.Value(settingsCtxKey{}).(*Settings)
	return s, ok
}

type overrideCtxKey struct{}

// WithSessionOverride sets an in-memory, request-scoped value that wins over every
// persisted scope. It is never written to the database.
//
// This is the top tier of ARCHITECTURE_v1 §16.1 and exists for preview flows — "show me
// this invoice priced in USD" — where a user changes a setting for one screen without
// altering anyone's stored configuration.
func WithSessionOverride(ctx context.Context, key string, value any) context.Context {
	existing, _ := ctx.Value(overrideCtxKey{}).(map[string]any)
	next := make(map[string]any, len(existing)+1)
	for k, v := range existing {
		next[k] = v
	}
	next[key] = value
	return context.WithValue(ctx, overrideCtxKey{}, next)
}

func sessionOverride(ctx context.Context, key string) (any, bool) {
	m, ok := ctx.Value(overrideCtxKey{}).(map[string]any)
	if !ok {
		return nil, false
	}
	v, found := m[key]
	return v, found
}

// ── typed read ──────────────────────────────────────────────────────────────────

// Get resolves the setting for ctx.
//
// It cannot fail. Every failure mode resolves to the declared default: no Settings bound
// to the context, no value at any scope, or a stored value that will not decode. A
// configuration read at a call site should never force error handling — that is precisely
// what makes people ignore it — so problems are logged and the declared default, which is
// always valid, is returned.
func (s *Setting[T]) Get(ctx context.Context) T {
	if v, ok := sessionOverride(ctx, s.key); ok {
		if typed, match := v.(T); match {
			return typed
		}
	}
	st, ok := fromContext(ctx)
	if !ok {
		return s.fallback
	}
	encoded, found := st.resolveEncoded(ctx, s.key)
	if !found {
		return s.fallback
	}
	out, err := s.decode(encoded)
	if err != nil {
		st.log.WarnContext(ctx, "stored setting value is malformed; using the declared default",
			slog.String("key", s.key), slog.Any("error", err))
		return s.fallback
	}
	return out
}
