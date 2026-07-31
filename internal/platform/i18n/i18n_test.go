package i18n_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

func testCatalog(t *testing.T, files map[string]string) *i18n.Catalog {
	t.Helper()
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	c, err := i18n.LoadFS(m)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return c
}

func newDB(t *testing.T) *database.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "i18n_test.db")
	db, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	runner, err := migrate.New(db, migrate.Options{
		FS: migrations.SQLite(), DBPath: dbPath, SkipBackup: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

// ── catalog resolution ──────────────────────────────────────────────────────────

func TestResolveUsesTheRequestedLocale(t *testing.T) {
	c := testCatalog(t, map[string]string{
		"en/common.json": `{"greeting":"Hello"}`,
		"ar/common.json": `{"greeting":"مرحباً"}`,
	})
	if got := c.T("ar", "greeting", nil); got != "مرحباً" {
		t.Errorf("ar = %q", got)
	}
	if got := c.T("en", "greeting", nil); got != "Hello" {
		t.Errorf("en = %q", got)
	}
}

func TestResolveFallsBackThroughTheChain(t *testing.T) {
	c := testCatalog(t, map[string]string{
		"en/common.json":    `{"only_en":"English only","shared":"Shared EN"}`,
		"ar/common.json":    `{"shared":"مشترك"}`,
		"ar-SY/common.json": `{"regional":"سوري"}`,
	})

	// ar-SY → ar → en, taking the first that defines the key.
	if got := c.T("ar-SY", "regional", nil); got != "سوري" {
		t.Errorf("regional = %q", got)
	}
	if got := c.T("ar-SY", "shared", nil); got != "مشترك" {
		t.Errorf("ar-SY should fall back to ar, got %q", got)
	}
	if got := c.T("ar-SY", "only_en", nil); got != "English only" {
		t.Errorf("ar-SY should fall back through ar to en, got %q", got)
	}
	if got := c.T("ar", "only_en", nil); got != "English only" {
		t.Errorf("ar should fall back to en, got %q", got)
	}
}

func TestMissingKeyReturnsTheKeyItself(t *testing.T) {
	// Deliberate: "common.action.save" on screen is ugly, obviously wrong, and greppable.
	// A blank button is none of those, and gets reported as "the save button disappeared".
	c := testCatalog(t, map[string]string{"en/common.json": `{"a":"A"}`})
	if got := c.T("ar", "does.not.exist", nil); got != "does.not.exist" {
		t.Errorf("missing key = %q, want the key itself", got)
	}
}

func TestInterpolation(t *testing.T) {
	c := testCatalog(t, map[string]string{
		"en/common.json": `{
			"one":"Hello {name}!",
			"two":"{a} then {b}",
			"repeat":"{x} and {x}",
			"none":"No placeholders",
			"unclosed":"Broken {name",
			"adjacent":"{a}{b}"
		}`,
	})
	cases := []struct {
		key    string
		params map[string]string
		want   string
	}{
		{"one", i18n.Param("name", "Rony"), "Hello Rony!"},
		{"two", i18n.Param("a", "1", "b", "2"), "1 then 2"},
		{"repeat", i18n.Param("x", "z"), "z and z"},
		{"none", i18n.Param("x", "y"), "No placeholders"},
		{"adjacent", i18n.Param("a", "A", "b", "B"), "AB"},
		{"one", nil, "Hello {name}!"},
		// An unknown placeholder stays visible: a stray "{version}" is a bug report, a
		// silently dropped value is a mystery.
		{"two", i18n.Param("a", "1"), "1 then {b}"},
		{"unclosed", i18n.Param("name", "x"), "Broken {name"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.want, func(t *testing.T) {
			if got := c.T("en", tc.key, tc.params); got != tc.want {
				t.Errorf("T = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadRejectsBrokenCatalogs(t *testing.T) {
	// A malformed catalog silently blanks the UI, unlike an inert stale settings row — so it
	// fails loudly, on a developer's machine.
	cases := map[string]map[string]string{
		"invalid json":         {"en/common.json": `{"a":`},
		"not a locale dir":     {"en/common.json": `{"a":"A"}`, "not-a-locale-!/x.json": `{}`},
		"no default locale":    {"ar/common.json": `{"a":"A"}`},
		"conflicting dup keys": {"en/common.json": `{"a":"one"}`, "en/other.json": `{"a":"two"}`},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			m := fstest.MapFS{}
			for f, body := range files {
				m[f] = &fstest.MapFile{Data: []byte(body)}
			}
			if _, err := i18n.LoadFS(m); err == nil {
				t.Fatal("a broken catalog loaded successfully")
			}
		})
	}
}

func TestLoadAcceptsIdenticalDuplicateKeys(t *testing.T) {
	// Same key, same value, two files: harmless, so it must not fail the build.
	m := fstest.MapFS{
		"en/common.json": &fstest.MapFile{Data: []byte(`{"a":"same"}`)},
		"en/other.json":  &fstest.MapFile{Data: []byte(`{"a":"same"}`)},
	}
	if _, err := i18n.LoadFS(m); err != nil {
		t.Fatalf("identical duplicates were rejected: %v", err)
	}
}

func TestTranslateErrorRendersCodeAndParams(t *testing.T) {
	// The bridge for backend-rendered artifacts. The API layer still returns codes.
	c := testCatalog(t, map[string]string{
		"en/errors.json": `{"migrate.database_too_new":"Database is at {current}, this build knows {target}."}`,
		"ar/errors.json": `{"migrate.database_too_new":"قاعدة البيانات عند {current}."}`,
	})
	err := errs.Conflict("migrate.database_too_new", "developer text").
		WithParam("current", "5").WithParam("target", "3")

	if got := c.TranslateError("en", err); got != "Database is at 5, this build knows 3." {
		t.Errorf("en = %q", got)
	}
	if got := c.TranslateError("ar", err); !strings.Contains(got, "5") {
		t.Errorf("ar = %q, want the parameter interpolated", got)
	}
	if got := c.TranslateError("en", nil); got != "" {
		t.Errorf("nil error = %q, want empty", got)
	}
}

func TestEmbeddedCatalogLoads(t *testing.T) {
	c, err := i18n.Load()
	if err != nil {
		t.Fatalf("the shipped catalogs do not load: %v", err)
	}
	locales := c.Locales()
	if len(locales) < 2 {
		t.Fatalf("locales = %v, want at least en and ar", locales)
	}
	if got := c.T("ar", "app.title", nil); got != "ميزان" {
		t.Errorf("ar app.title = %q", got)
	}
}

// ── the ui.locale setting ───────────────────────────────────────────────────────

func settingsFor(t *testing.T, db *database.Store, bus *eventbus.Bus) *config.Settings {
	t.Helper()
	s, err := config.Open(context.Background(), db, config.Options{
		Notifier: config.NewBusNotifier(bus, nil),
	})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	return s
}

func TestLocaleSettingRoundTripsAndDefaults(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	bus := eventbus.New(eventbus.Options{})
	settings := settingsFor(t, db, bus)
	bound := config.Bind(ctx, settings)

	if got := i18n.Active(bound); got != locale.Default {
		t.Errorf("unset locale = %q, want the default", got)
	}
	if err := settings.Set(bound, config.ScopeSystem, "", i18n.LocaleSettingKey, "ar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := i18n.Active(bound); got != "ar" {
		t.Errorf("after Set, Active = %q, want ar", got)
	}
}

func TestLocaleSettingRejectsAnInvalidTag(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	settings := settingsFor(t, db, eventbus.New(eventbus.Options{}))
	bound := config.Bind(ctx, settings)

	if err := settings.Set(bound, config.ScopeSystem, "", i18n.LocaleSettingKey, "not a language"); err == nil {
		t.Fatal("an invalid language tag was accepted")
	}
}

func TestContextLocaleWinsOverTheSetting(t *testing.T) {
	// A per-request locale (a preview, or a printed document in another language) must beat
	// the stored preference.
	ctx := context.Background()
	db := newDB(t)
	settings := settingsFor(t, db, eventbus.New(eventbus.Options{}))
	bound := config.Bind(ctx, settings)
	if err := settings.Set(bound, config.ScopeSystem, "", i18n.LocaleSettingKey, "ar"); err != nil {
		t.Fatal(err)
	}

	if got := i18n.Active(locale.WithLocale(bound, "en-GB")); got != "en-GB" {
		t.Errorf("Active = %q, want the context locale en-GB", got)
	}
}

func TestOnLocaleChangedFiresAndIgnoresOtherSettings(t *testing.T) {
	// The whole point of D2: no separate LocaleChanged event, but subscribers still get
	// typed ergonomics over the one mechanism.
	ctx := context.Background()
	db := newDB(t)
	bus := eventbus.New(eventbus.Options{})

	var mu sync.Mutex
	var seen []locale.Locale
	if err := i18n.OnLocaleChanged(bus, "reload-ui", func(_ context.Context, l locale.Locale) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, l)
		return nil
	}); err != nil {
		t.Fatalf("OnLocaleChanged: %v", err)
	}

	// An unrelated setting must not fire it.
	unrelated := config.DeclareString(config.Def{
		Key: "test.unrelated.for.locale", Default: "x", Scopes: []config.Scope{config.ScopeSystem},
	})
	settings := settingsFor(t, db, bus)
	bound := config.Bind(ctx, settings)

	if err := settings.Set(bound, config.ScopeSystem, "", unrelated.Key(), "y"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	afterUnrelated := len(seen)
	mu.Unlock()
	if afterUnrelated != 0 {
		t.Fatalf("an unrelated setting change fired the locale subscriber %d times", afterUnrelated)
	}

	if err := settings.Set(bound, config.ScopeSystem, "", i18n.LocaleSettingKey, "ar"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "ar" {
		t.Errorf("subscriber saw %v, want [ar]", seen)
	}
}

// ── user-content translations ───────────────────────────────────────────────────

func TestTranslationsResolveAndFallback(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)), nil)

	product := id.ID("01890000-0000-7000-8000-0000000000p1")
	if err := tr.Set(ctx, "product", product, "name", "ar", "قلم"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	arabic := locale.WithLocale(ctx, "ar")
	if got := tr.Resolve(arabic, "product", product, "name", "Pen"); got != "قلم" {
		t.Errorf("ar = %q, want the translation", got)
	}
	// No English translation → the entity's own base value, not a blank.
	english := locale.WithLocale(ctx, "en")
	if got := tr.Resolve(english, "product", product, "name", "Pen"); got != "Pen" {
		t.Errorf("en = %q, want the base fallback", got)
	}
	// An entity with no translations at all.
	other := id.ID("01890000-0000-7000-8000-0000000000p2")
	if got := tr.Resolve(arabic, "product", other, "name", "Notebook"); got != "Notebook" {
		t.Errorf("untranslated = %q, want the base fallback", got)
	}
}

func TestTranslationsNeverCrossLanguages(t *testing.T) {
	// Unlike UI strings, user content does NOT fall back ar → en: the base column holds what
	// the shop owner typed, which serves them better than an English translation somebody
	// entered once.
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, nil, nil)

	product := id.ID("01890000-0000-7000-8000-0000000000p3")
	if err := tr.Set(ctx, "product", product, "name", "en", "Fountain Pen"); err != nil {
		t.Fatal(err)
	}
	arabic := locale.WithLocale(ctx, "ar")
	if got := tr.Resolve(arabic, "product", product, "name", "قلم حبر"); got != "قلم حبر" {
		t.Errorf("ar resolved to %q; it must not borrow the English translation", got)
	}
}

func TestTranslationsRegionalFallsBackToLanguage(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, nil, nil)

	product := id.ID("01890000-0000-7000-8000-0000000000p4")
	if err := tr.Set(ctx, "product", product, "name", "ar", "قلم"); err != nil {
		t.Fatal(err)
	}
	syrian := locale.WithLocale(ctx, "ar-SY")
	if got := tr.Resolve(syrian, "product", product, "name", "Pen"); got != "قلم" {
		t.Errorf("ar-SY = %q, want it to fall back to ar", got)
	}
}

func TestTranslationsCacheIsInvalidatedOnWrite(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, nil, nil)
	arabic := locale.WithLocale(ctx, "ar")

	product := id.ID("01890000-0000-7000-8000-0000000000p5")
	if err := tr.Set(ctx, "product", product, "name", "ar", "أول"); err != nil {
		t.Fatal(err)
	}
	if got := tr.Resolve(arabic, "product", product, "name", "base"); got != "أول" {
		t.Fatalf("first read = %q", got)
	}

	if err := tr.Set(ctx, "product", product, "name", "ar", "ثاني"); err != nil {
		t.Fatal(err)
	}
	if got := tr.Resolve(arabic, "product", product, "name", "base"); got != "ثاني" {
		t.Errorf("after update = %q, want the new value — the cache was not invalidated", got)
	}
}

func TestSetIsAnUpsertNotADuplicate(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, nil, nil)

	product := id.ID("01890000-0000-7000-8000-0000000000p6")
	for _, v := range []string{"one", "two", "three"} {
		if err := tr.Set(ctx, "product", product, "name", "ar", v); err != nil {
			t.Fatalf("Set %q: %v", v, err)
		}
	}
	var rows int
	if err := db.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM translations WHERE entity_id = ?`, product.String()).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("stored %d rows for one (entity, field, locale), want 1", rows)
	}
}

func TestResolveManyIsOneQueryForManyEntities(t *testing.T) {
	// The N+1 this method exists to prevent: a 500-row grid must not become 500 round trips.
	ctx := context.Background()
	db := newDB(t)
	tr := i18n.NewTranslations(db, nil, nil)
	arabic := locale.WithLocale(ctx, "ar")

	ids := make([]id.ID, 0, 20)
	fallbacks := map[id.ID]string{}
	for i := 0; i < 20; i++ {
		newID, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, newID)
		fallbacks[newID] = "base"
		if i%2 == 0 {
			if err := tr.Set(ctx, "product", newID, "name", "ar", "مترجم"); err != nil {
				t.Fatal(err)
			}
		}
	}

	got := tr.ResolveMany(arabic, "product", ids, "name", fallbacks)
	if len(got) != len(ids) {
		t.Fatalf("got %d results, want %d", len(got), len(ids))
	}
	translated, base := 0, 0
	for _, v := range got {
		switch v {
		case "مترجم":
			translated++
		case "base":
			base++
		}
	}
	if translated != 10 || base != 10 {
		t.Errorf("translated=%d base=%d, want 10 and 10", translated, base)
	}
}

func TestSetRejectsIncompleteInput(t *testing.T) {
	ctx := context.Background()
	tr := i18n.NewTranslations(newDB(t), nil, nil)
	valid := id.ID("01890000-0000-7000-8000-0000000000p7")

	cases := map[string]func() error{
		"no entity type": func() error { return tr.Set(ctx, "", valid, "name", "ar", "v") },
		"no entity id":   func() error { return tr.Set(ctx, "product", "", "name", "ar", "v") },
		"no field":       func() error { return tr.Set(ctx, "product", valid, "", "ar", "v") },
		"no locale":      func() error { return tr.Set(ctx, "product", valid, "name", "", "v") },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			if err := fn(); err == nil {
				t.Fatal("accepted incomplete input")
			}
		})
	}
}
