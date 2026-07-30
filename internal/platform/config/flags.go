package config

import (
	"context"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// Feature flags reuse the settings scope chain, snapshot, and locking wholesale — they
// differ only in their table and their lifecycle metadata. Duplicating the resolution
// machinery for them would guarantee the two copies drift.

type flagKey struct {
	scope   Scope
	scopeID id.ID
	key     string
}

// loadFlags reads feature_flags into a fresh map. Called by Reload under the same
// snapshot swap as settings, so a reload is atomic across both.
func (s *Settings) loadFlags(ctx context.Context) (map[flagKey]bool, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT flag_key, scope, scope_id, is_enabled FROM feature_flags`)
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "loading feature flags")
	}
	defer rows.Close()

	out := map[flagKey]bool{}
	var unknown []string
	for rows.Next() {
		var key, scope, scopeID string
		var enabled int64
		if err := rows.Scan(&key, &scope, &scopeID, &enabled); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "scanning feature flags")
		}
		if _, declared := s.reg.LookupFlag(key); !declared {
			unknown = append(unknown, key)
			continue
		}
		out[flagKey{scope: Scope(scope), scopeID: id.ID(scopeID), key: key}] = enabled != 0
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "reading feature flags")
	}
	if len(unknown) > 0 {
		// Same rule as settings (design D3): a removed flag's leftover row is inert, and
		// refusing to start over it would be worse than ignoring it.
		s.log.WarnContext(ctx, "ignoring feature-flag rows with no declared key",
			slog.Int("count", len(unknown)), slog.Any("keys", unknown))
	}
	return out, nil
}

// resolveFlag walks the scope chain, most specific first.
func (s *Settings) resolveFlag(ctx context.Context, key string) (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, scope := range resolutionOrder {
		scopeID, ok := s.scopeID(ctx, scope)
		if !ok {
			continue
		}
		if v, found := s.flags[flagKey{scope: scope, scopeID: scopeID, key: key}]; found {
			return v, true
		}
	}
	return false, false
}

// Get reports whether the flag is enabled for ctx, falling back to the declared default.
//
// Like Setting.Get it cannot fail: an unbound context or an absent row yields the
// declared default, which is always valid.
func (f *Flag) Get(ctx context.Context) bool {
	if v, ok := sessionOverride(ctx, f.key); ok {
		if typed, match := v.(bool); match {
			return typed
		}
	}
	st, ok := fromContext(ctx)
	if !ok {
		return f.fallback
	}
	if v, found := st.resolveFlag(ctx, f.key); found {
		return v
	}
	return f.fallback
}

// SetFlag enables or disables a feature flag at one scope.
func (s *Settings) SetFlag(ctx context.Context, scope Scope, scopeID id.ID, key string, enabled bool) error {
	def, ok := s.reg.LookupFlag(key)
	if !ok {
		return errs.Validation(CodeUndeclared,
			"cannot write an undeclared feature flag").WithParam("key", key)
	}
	if !flagScopeAllowed(def, scope) {
		return errs.Validation(CodeScopeNotAllowed,
			"this flag may not be overridden at that scope").
			WithParam("key", key).WithParam("scope", string(scope))
	}
	if def.Permission != "" && !s.auth.Can(ctx, def.Permission) {
		return errs.Permission(CodeNotPermitted,
			"changing this flag requires a permission the caller does not hold").
			WithParam("key", key).WithParam("permission", def.Permission)
	}
	if scope == ScopeSystem {
		scopeID = ""
	} else if scopeID.IsZero() {
		return errs.Validation(CodeInvalidValue,
			"a non-system scope requires a scope identifier").WithParam("scope", string(scope))
	}

	if err := s.db.Do(ctx, func(ctx context.Context) error {
		return s.writeFlag(ctx, def.Key, scope, scopeID, enabled)
	}); err != nil {
		return err
	}

	s.mu.Lock()
	s.flags[flagKey{scope: scope, scopeID: scopeID, key: key}] = enabled
	s.version++
	s.mu.Unlock()

	s.notify.SettingChanged(ctx, key, scope, scopeID)
	return nil
}

func (s *Settings) writeFlag(ctx context.Context, key string, scope Scope, scopeID id.ID, enabled bool) error {
	newID, err := id.New()
	if err != nil {
		return err
	}
	now := clock.Format(s.clk.Now())

	// Booleans go through the dialect's SMALLINT(0/1) contract (§8.1), never a native
	// bool, so the same statement is correct on an engine without a boolean type.
	value := s.db.Dialect().BooleanLiteral(enabled)

	stmt := s.db.Dialect().Upsert(dialect.UpsertSpec{
		Table: "feature_flags",
		Columns: []string{
			"id", "flag_key", "scope", "scope_id", "is_enabled", "created_at", "updated_at",
		},
		ConflictCols: []string{"flag_key", "scope", "scope_id"},
		UpdateCols:   []string{"is_enabled", "updated_at"},
	})
	if _, err := s.db.Writer(ctx).ExecContext(ctx, stmt,
		newID.String(), key, string(scope), scopeID.String(), value, now, now,
	); err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeWriteFailed, "writing feature flag "+key)
	}
	return nil
}

func flagScopeAllowed(def FlagDef, scope Scope) bool {
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
