package i18n

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// Translations resolves USER-AUTHORED content — product names, category names, account
// names — as opposed to the UI strings in Catalog (ARCHITECTURE_v1 §22.4, §9.5).
//
// The storage shape is the generic `translations` side table rather than name_ar/name_en
// columns, so adding Kurdish is inserting rows, not migrating every entity table.
type Translations struct {
	db  database.DB
	clk clock.Clock
	log *slog.Logger

	mu    sync.RWMutex
	cache map[cacheKey]entityFields
}

type cacheKey struct {
	entityType string
	loc        locale.Locale
}

// entityFields is entityID → field → value.
type entityFields map[id.ID]map[string]string

// NewTranslations builds the resolver.
func NewTranslations(db database.DB, clk clock.Clock, log *slog.Logger) *Translations {
	if clk == nil {
		clk = clock.System()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Translations{db: db, clk: clk, log: log, cache: map[cacheKey]entityFields{}}
}

// Resolve returns the translated field for the context's locale, or fallback.
//
// `fallback` is the entity's own base column (§9.5's "base `name` column (fallback)"), so an
// untranslated product shows the name its owner typed rather than a blank cell.
//
// It cannot fail: a database error is logged and the fallback returned. Rendering a grid must
// not abort because one label could not be looked up.
func (t *Translations) Resolve(ctx context.Context, entityType string, entityID id.ID, field, fallback string) string {
	l := locale.FromContext(ctx)
	for _, candidate := range userContentChain(l) {
		fields, err := t.load(ctx, entityType, candidate)
		if err != nil {
			t.log.WarnContext(ctx, "could not load translations; using the base value",
				slog.String("entity_type", entityType), slog.Any("error", err))
			return fallback
		}
		if byField, ok := fields[entityID]; ok {
			if v, found := byField[field]; found && v != "" {
				return v
			}
		}
	}
	return fallback
}

// ResolveMany returns translations for many entities of one type.
//
// This exists because the alternative is a query per row while rendering a product list — the
// classic N+1 that turns a 500-row grid into 500 round trips. The per-(entity_type, locale)
// cache means it is at most one query regardless of how many ids are asked for.
func (t *Translations) ResolveMany(
	ctx context.Context, entityType string, ids []id.ID, field string, fallbacks map[id.ID]string,
) map[id.ID]string {
	out := make(map[id.ID]string, len(ids))
	l := locale.FromContext(ctx)
	chain := userContentChain(l)

	loaded := make([]entityFields, 0, len(chain))
	for _, candidate := range chain {
		fields, err := t.load(ctx, entityType, candidate)
		if err != nil {
			t.log.WarnContext(ctx, "could not load translations; using base values",
				slog.String("entity_type", entityType), slog.Any("error", err))
			break
		}
		loaded = append(loaded, fields)
	}

	for _, entityID := range ids {
		out[entityID] = fallbacks[entityID]
		for _, fields := range loaded {
			if byField, ok := fields[entityID]; ok {
				if v, found := byField[field]; found && v != "" {
					out[entityID] = v
					break
				}
			}
		}
	}
	return out
}

// userContentChain is the fallback order for user-authored content.
//
// It walks a regional tag down to its language ("ar-SY" → "ar") but DELIBERATELY STOPS THERE
// — it never crosses into another language the way UI strings do.
//
// The difference is the nature of the fallback. English is guaranteed to define every UI key,
// so falling back to it always yields a real string. User content has no such guarantee: the
// base column holds whatever the shop owner typed, which is far more likely to be meaningful
// to them than an English translation somebody entered once. Showing an Arabic shop an
// English product name because a translation row happened to exist would be worse than
// showing them their own original text.
func userContentChain(l locale.Locale) []locale.Locale {
	if l.IsZero() {
		return []locale.Locale{locale.Default}
	}
	chain := []locale.Locale{l}
	if lang := locale.Locale(l.Language()); lang != l {
		chain = append(chain, lang)
	}
	return chain
}

// load returns the cached translations for one entity type and locale, reading through on a
// miss.
//
// Lazy per (entity_type, locale), rather than loading everything at startup: a shop with
// 40,000 products would otherwise hold every name in memory before the first screen renders,
// on a machine also running the UI and SQLite. And rather than caching per key, which would
// miss on every row of a grid. One entity type in one language is the unit a screen actually
// reads.
func (t *Translations) load(ctx context.Context, entityType string, l locale.Locale) (entityFields, error) {
	key := cacheKey{entityType: entityType, loc: l}

	t.mu.RLock()
	cached, ok := t.cache[key]
	t.mu.RUnlock()
	if ok {
		return cached, nil
	}

	rows, err := t.db.Reader(ctx).QueryContext(ctx,
		`SELECT entity_id, field_name, value FROM translations
		  WHERE entity_type = ? AND locale = ?`, entityType, l.String())
	if err != nil {
		return nil, errs.Wrap(t.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeLoadFailed, "loading translations for "+entityType)
	}
	defer rows.Close()

	fields := entityFields{}
	for rows.Next() {
		var entityID, fieldName, value string
		if scanErr := rows.Scan(&entityID, &fieldName, &value); scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, CodeLoadFailed,
				"scanning translations")
		}
		eid := id.ID(entityID)
		if fields[eid] == nil {
			fields[eid] = map[string]string{}
		}
		fields[eid][fieldName] = value
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "reading translations")
	}

	t.mu.Lock()
	t.cache[key] = fields
	t.mu.Unlock()
	return fields, nil
}

// Set writes a translation and invalidates the affected cache entry.
//
// Runs through the caller's executor, so it participates in a surrounding Unit of Work.
func (t *Translations) Set(
	ctx context.Context, entityType string, entityID id.ID, field string, l locale.Locale, value string,
) error {
	if entityType == "" || entityID.IsZero() || field == "" || l.IsZero() {
		return errs.Validation(CodeWriteFailed,
			"a translation needs an entity type, id, field, and locale")
	}

	newID, err := id.New()
	if err != nil {
		return err
	}
	now := clock.Format(t.clk.Now())

	stmt := t.db.Dialect().Upsert(dialect.UpsertSpec{
		Table:        "translations",
		Columns:      []string{"id", "entity_type", "entity_id", "field_name", "locale", "value", "created_at", "updated_at"},
		ConflictCols: []string{"entity_type", "entity_id", "field_name", "locale"},
		UpdateCols:   []string{"value", "updated_at"},
	})
	if _, err := t.db.Writer(ctx).ExecContext(ctx, stmt,
		newID.String(), entityType, entityID.String(), field, l.String(), value, now, now,
	); err != nil {
		return errs.Wrap(t.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeWriteFailed, "writing a translation for "+entityType)
	}

	t.Invalidate(entityType, l)
	return nil
}

// Invalidate drops the cache for one entity type and locale.
//
// Called by Set, and available to the bootstrap after a bulk import. Dropping rather than
// patching: a re-read is one query, and a patch that got the fallback logic subtly wrong
// would be a cache that disagrees with the database.
func (t *Translations) Invalidate(entityType string, l locale.Locale) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cache, cacheKey{entityType: entityType, loc: l})
}

// InvalidateAll clears the whole cache.
func (t *Translations) InvalidateAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cache = map[cacheKey]entityFields{}
}
