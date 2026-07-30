// Tests live in package config so the codecs and the resolution internals can be
// exercised directly, and so a test can inject its own registry rather than polluting the
// process-wide one.
package config

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

const (
	companyID = id.ID("01890000-0000-7000-8000-00000000c001")
	branchID  = id.ID("01890000-0000-7000-8000-00000000b001")
	userID    = id.ID("01890000-0000-7000-8000-00000000ce01")
)

func newDB(t *testing.T) *database.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "config_test.db")
	st, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Against the real platform schema, so the UNIQUE constraints and CHECK constraints
	// on settings/feature_flags are genuinely exercised — not a mock's idea of them.
	runner, err := migrate.New(st, migrate.Options{
		FS:         migrations.SQLite(),
		DBPath:     dbPath,
		SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	if got := errs.CodeOf(err); got != want {
		t.Fatalf("error code = %q, want %q (err: %v)", got, want, err)
	}
}

func syp(t *testing.T) money.Currency {
	t.Helper()
	c, err := money.NewCurrency("SYP", 0, round.HalfAwayFromZero)
	if err != nil {
		t.Fatalf("currency: %v", err)
	}
	return c
}

// ── declaration & validation ────────────────────────────────────────────────────

func TestRegistryValidatesCleanWhenDeclarationsAreSound(t *testing.T) {
	r := NewRegistry()
	r.DeclareBool(Def{Key: "a.bool", Default: false, Scopes: []Scope{ScopeSystem}})
	r.DeclareInt(Def{Key: "a.int", Default: int64(7), Scopes: []Scope{ScopeSystem}})
	r.DeclareEnum(Def{Key: "a.enum", Default: "x", Enum: []string{"x", "y"}, Scopes: []Scope{ScopeSystem}})

	if err := r.Validate(); err != nil {
		t.Fatalf("Validate on a sound registry: %v", err)
	}
}

func TestDuplicateKeyIsReportedNotLastOneWins(t *testing.T) {
	// Two modules claiming one key would otherwise resolve by package init order, which
	// can differ between builds.
	r := NewRegistry()
	r.DeclareBool(Def{Key: "dup.key", Default: false})
	r.DeclareBool(Def{Key: "dup.key", Default: true})

	assertCode(t, r.Validate(), CodeRegistryInvalid)
}

func TestInvalidDeclarationsAreReported(t *testing.T) {
	cases := map[string]func(*Registry){
		"empty key":            func(r *Registry) { r.DeclareBool(Def{Key: "  ", Default: false}) },
		"enum default outside": func(r *Registry) { r.DeclareEnum(Def{Key: "k", Default: "z", Enum: []string{"x"}}) },
		"unknown scope":        func(r *Registry) { r.DeclareBool(Def{Key: "k", Default: false, Scopes: []Scope{"galaxy"}}) },
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry()
			declare(r)
			assertCode(t, r.Validate(), CodeRegistryInvalid)
		})
	}
}

func TestDefinitionsAreSorted(t *testing.T) {
	// Drives the generated settings UI; map order would reshuffle the screen per launch.
	r := NewRegistry()
	r.DeclareBool(Def{Key: "z.last", Default: false})
	r.DeclareBool(Def{Key: "a.first", Default: false})
	r.DeclareBool(Def{Key: "m.middle", Default: false})

	got := r.Definitions()
	want := []string{"a.first", "m.middle", "z.last"}
	if len(got) != len(want) {
		t.Fatalf("got %d definitions, want %d", len(got), len(want))
	}
	for i, d := range got {
		if d.Key != want[i] {
			t.Errorf("position %d = %q, want %q", i, d.Key, want[i])
		}
	}
}

// ── codecs ──────────────────────────────────────────────────────────────────────

func TestCodecRoundTrips(t *testing.T) {
	cur := syp(t)

	t.Run("string", func(t *testing.T) {
		enc, err := encodeString(`a "quoted" ; value`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeString(enc)
		if err != nil || got != `a "quoted" ; value` {
			t.Errorf("round trip = %q, %v", got, err)
		}
	})

	t.Run("int64 extremes", func(t *testing.T) {
		for _, v := range []int64{0, -1, 1 << 62, -(1 << 62), 9007199254740993} {
			enc, err := encodeInt(v)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeInt(enc)
			if err != nil {
				t.Fatal(err)
			}
			// 9007199254740993 is 2^53+1: it survives only if nothing goes via float64.
			if got != v {
				t.Errorf("round trip %d → %d (precision lost through a float?)", v, got)
			}
		}
	})

	t.Run("duration keeps nanosecond precision", func(t *testing.T) {
		v := 90*time.Minute + 123*time.Nanosecond
		enc, err := encodeDuration(v)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeDuration(enc)
		if err != nil || got != v {
			t.Errorf("round trip = %v (%v), want %v", got, err, v)
		}
	})

	t.Run("id", func(t *testing.T) {
		original, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		enc, err := encodeID(original)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeID(enc)
		if err != nil || got != original {
			t.Errorf("round trip = %q (%v), want %q", got, err, original)
		}
	})

	t.Run("money carries its currency", func(t *testing.T) {
		original := money.FromMinor(cur, 123456)
		enc, err := encodeMoney(original)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeMoney(enc)
		if err != nil {
			t.Fatal(err)
		}
		if got.Minor() != original.Minor() {
			t.Errorf("amount = %d, want %d", got.Minor(), original.Minor())
		}
		if got.Currency().Code() != "SYP" || got.Currency().Decimals() != 0 {
			t.Errorf("currency not preserved: %+v", got.Currency())
		}
		if got.Currency().Rounding() != round.HalfAwayFromZero {
			t.Errorf("rounding mode not preserved: %v", got.Currency().Rounding())
		}
	})
}

func TestMoneyCodecIsExactForEveryInt64(t *testing.T) {
	// The property that matters: a stored amount is the amount that comes back. If any
	// part of the codec routed through binary floating point, values beyond 2^53 would
	// drift — and this is a customer's money.
	cur := syp(t)
	rapid.Check(t, func(rt *rapid.T) {
		minor := rapid.Int64().Draw(rt, "minor")
		enc, err := encodeMoney(money.FromMinor(cur, minor))
		if err != nil {
			rt.Fatalf("encode: %v", err)
		}
		got, err := decodeMoney(enc)
		if err != nil {
			rt.Fatalf("decode: %v", err)
		}
		if got.Minor() != minor {
			rt.Fatalf("round trip changed the amount: %d → %d", minor, got.Minor())
		}
	})
}

func TestEncodeAnyRejectsFloatsAndMismatches(t *testing.T) {
	// A float reaching a money or int setting is a bug; truncating it silently would hide
	// that bug at exactly the point it costs money.
	cases := map[string]struct {
		vt ValueType
		v  any
	}{
		"float into int":    {TypeInt, 1.5},
		"float into money":  {TypeMoney, 12.34},
		"string into bool":  {TypeBool, "true"},
		"int into string":   {TypeString, 5},
		"bool into int":     {TypeInt, true},
		"string into money": {TypeMoney, "10 SYP"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := encodeAny(tc.vt, tc.v); err == nil {
				t.Fatalf("accepted %v as %s", tc.v, tc.vt)
			}
		})
	}
}

func TestDecodeMoneyRejectsUnknownRoundingMode(t *testing.T) {
	_, err := decodeMoney(`{"amount":100,"currency":"SYP","decimals":0,"rounding":"bogus"}`)
	assertCode(t, err, CodeInvalidValue)
}

// ── the precedence table ────────────────────────────────────────────────────────

func TestScopeResolutionPrecedence(t *testing.T) {
	// The core guarantee of the settings system: the most specific defined scope wins,
	// and resolution falls through to the declared default.
	//
	//   Session ► User ► Branch ► Company ► System ► default
	const key = "prec.value"

	type seedRow struct {
		scope   Scope
		scopeID id.ID
		value   string
	}
	all := []seedRow{
		{ScopeSystem, "", `"system"`},
		{ScopeCompany, companyID, `"company"`},
		{ScopeBranch, branchID, `"branch"`},
		{ScopeUser, userID, `"user"`},
	}

	cases := []struct {
		name    string
		present []Scope
		want    string
	}{
		{"nothing stored falls back to the declared default", nil, "default"},
		{"system only", []Scope{ScopeSystem}, "system"},
		{"company beats system", []Scope{ScopeSystem, ScopeCompany}, "company"},
		{"branch beats company", []Scope{ScopeSystem, ScopeCompany, ScopeBranch}, "branch"},
		{"user beats branch", []Scope{ScopeSystem, ScopeCompany, ScopeBranch, ScopeUser}, "user"},
		{"user alone", []Scope{ScopeUser}, "user"},
		{"branch alone", []Scope{ScopeBranch}, "branch"},
		{"gap: user and system, branch absent", []Scope{ScopeSystem, ScopeUser}, "user"},
		{"gap: branch and system, company absent", []Scope{ScopeSystem, ScopeBranch}, "branch"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st := newDB(t)

			reg := NewRegistry()
			handle := reg.DeclareString(Def{
				Key: key, Default: "default",
				Scopes: []Scope{ScopeSystem, ScopeCompany, ScopeBranch, ScopeUser},
			})
			if err := reg.Validate(); err != nil {
				t.Fatalf("registry: %v", err)
			}

			wanted := map[Scope]bool{}
			for _, s := range tc.present {
				wanted[s] = true
			}
			for _, row := range all {
				if !wanted[row.scope] {
					continue
				}
				newID, err := id.New()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.WriterPool().ExecContext(ctx,
					`INSERT INTO settings (id, scope, scope_id, setting_key, setting_value,
					   value_type, created_at, updated_at)
					 VALUES (?, ?, ?, ?, ?, 'string', '2026-07-30T00:00:00.000Z', '2026-07-30T00:00:00.000Z')`,
					newID.String(), string(row.scope), row.scopeID.String(), key, row.value); err != nil {
					t.Fatalf("seed %s: %v", row.scope, err)
				}
			}

			settings, err := Open(ctx, st, Options{
				Registry: reg,
				Scopes:   FixedScopes(companyID, branchID, userID),
				Clock:    clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)),
			})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			if got := handle.Get(Bind(ctx, settings)); got != tc.want {
				t.Errorf("resolved %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInactiveScopesAreSkipped(t *testing.T) {
	// With no company/branch/user active — the state before the setup wizard — resolution
	// must fall through to system scope rather than matching an empty scope id.
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	handle := reg.DeclareString(Def{Key: "k", Default: "default",
		Scopes: []Scope{ScopeSystem, ScopeCompany}})

	settings, err := Open(ctx, st, Options{Registry: reg, Scopes: NoScopes{}})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	if err := settings.Set(bound, ScopeCompany, companyID, "k", "company"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := handle.Get(bound); got != "default" {
		t.Errorf("resolved %q with no active company; want the declared default", got)
	}
}

func TestSessionOverrideWinsOverEveryPersistedScope(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	handle := reg.DeclareString(Def{Key: "k", Default: "default",
		Scopes: []Scope{ScopeSystem, ScopeUser}})

	settings, err := Open(ctx, st, Options{Registry: reg, Scopes: FixedScopes(companyID, branchID, userID)})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)
	if err := settings.Set(bound, ScopeUser, userID, "k", "user"); err != nil {
		t.Fatal(err)
	}
	if got := handle.Get(bound); got != "user" {
		t.Fatalf("baseline resolved %q, want %q", got, "user")
	}

	preview := WithSessionOverride(bound, "k", "preview")
	if got := handle.Get(preview); got != "preview" {
		t.Errorf("session override resolved %q, want %q", got, "preview")
	}
	// The override must not leak into the parent context or the database.
	if got := handle.Get(bound); got != "user" {
		t.Errorf("session override leaked: parent resolved %q", got)
	}
}

func TestGetOnUnboundContextReturnsDefault(t *testing.T) {
	reg := NewRegistry()
	handle := reg.DeclareInt(Def{Key: "k", Default: int64(42)})
	if got := handle.Get(context.Background()); got != 42 {
		t.Errorf("unbound Get = %d, want the declared default 42", got)
	}
}

// ── degradation (design D3) ─────────────────────────────────────────────────────

func TestUnknownStoredKeyIsIgnoredNotFatal(t *testing.T) {
	// A customer who downgrades past a removed setting must still be able to open their
	// shop. The leftover row is inert because no code reads it.
	ctx := context.Background()
	st := newDB(t)

	newID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, insErr := st.WriterPool().ExecContext(ctx,
		`INSERT INTO settings (id, scope, scope_id, setting_key, setting_value, value_type,
		   created_at, updated_at)
		 VALUES (?, 'system', '', 'sales.removed_in_1_4', '"x"', 'string',
		   '2026-07-30T00:00:00.000Z', '2026-07-30T00:00:00.000Z')`, newID.String()); insErr != nil {
		t.Fatal(insErr)
	}

	reg := NewRegistry()
	handle := reg.DeclareString(Def{Key: "still.here", Default: "ok", Scopes: []Scope{ScopeSystem}})

	settings, err := Open(ctx, st, Options{Registry: reg})
	if err != nil {
		t.Fatalf("Open refused to start over an unknown stored key: %v", err)
	}
	if got := handle.Get(Bind(ctx, settings)); got != "ok" {
		t.Errorf("declared setting resolved %q, want %q", got, "ok")
	}

	unknown, err := settings.UnknownKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 1 || unknown[0] != "sales.removed_in_1_4" {
		t.Errorf("UnknownKeys = %v, want [sales.removed_in_1_4]", unknown)
	}
}

func TestMalformedStoredValueFallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	newID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, insErr := st.WriterPool().ExecContext(ctx,
		`INSERT INTO settings (id, scope, scope_id, setting_key, setting_value, value_type,
		   created_at, updated_at)
		 VALUES (?, 'system', '', 'n.count', 'not-json-at-all', 'int',
		   '2026-07-30T00:00:00.000Z', '2026-07-30T00:00:00.000Z')`, newID.String()); insErr != nil {
		t.Fatal(insErr)
	}

	reg := NewRegistry()
	handle := reg.DeclareInt(Def{Key: "n.count", Default: int64(9), Scopes: []Scope{ScopeSystem}})

	settings, err := Open(ctx, st, Options{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if got := handle.Get(Bind(ctx, settings)); got != 9 {
		t.Errorf("malformed value resolved %d, want the declared default 9", got)
	}
}

// ── writes ──────────────────────────────────────────────────────────────────────

func TestSetRoundTripsThroughTheDatabase(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)
	cur := syp(t)

	reg := NewRegistry()
	limit := reg.DeclareMoney(Def{Key: "sales.credit_limit",
		Default: money.FromMinor(cur, 0), Scopes: []Scope{ScopeSystem, ScopeCompany}})

	settings, err := Open(ctx, st, Options{Registry: reg, Scopes: FixedScopes(companyID, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	if setErr := settings.Set(bound, ScopeCompany, companyID, limit.Key(), money.FromMinor(cur, 500000)); setErr != nil {
		t.Fatalf("Set: %v", setErr)
	}
	if got := limit.Get(bound).Minor(); got != 500000 {
		t.Errorf("in-memory read after Set = %d, want 500000", got)
	}

	// And it survives a reload from the database, not just the cache patch.
	fresh, err := Open(ctx, st, Options{Registry: reg, Scopes: FixedScopes(companyID, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	if got := limit.Get(Bind(ctx, fresh)).Minor(); got != 500000 {
		t.Errorf("after reload = %d, want 500000", got)
	}
}

func TestSetIsUpsertNotDuplicate(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	h := reg.DeclareString(Def{Key: "k", Default: "d", Scopes: []Scope{ScopeSystem}})
	settings, err := Open(ctx, st, Options{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	for _, v := range []string{"one", "two", "three"} {
		if err := settings.Set(bound, ScopeSystem, "", "k", v); err != nil {
			t.Fatalf("Set %q: %v", v, err)
		}
	}
	var rows int
	if err := st.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM settings WHERE setting_key = 'k'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("stored %d rows for one key/scope, want 1 (upsert, not insert)", rows)
	}
	if got := h.Get(bound); got != "three" {
		t.Errorf("resolved %q, want the last write %q", got, "three")
	}
}

func TestSetRejections(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	reg.DeclareString(Def{Key: "sys.only", Default: "d"}) // no Scopes ⇒ system-only
	reg.DeclareEnum(Def{Key: "e.mode", Default: "a", Enum: []string{"a", "b"},
		Scopes: []Scope{ScopeSystem}})
	reg.DeclareString(Def{Key: "p.guarded", Default: "d", Scopes: []Scope{ScopeSystem},
		Permission: "settings.manage"})
	reg.DeclareInt(Def{Key: "v.checked", Default: int64(1), Scopes: []Scope{ScopeSystem},
		Validate: func(v any) error {
			if n, _ := v.(int64); n < 0 {
				return errs.Validation("test.negative", "must not be negative")
			}
			return nil
		}})
	// Permits company scope, so a missing scope id reaches the identifier check rather
	// than being turned away earlier by the scope check.
	reg.DeclareString(Def{Key: "c.scoped", Default: "d",
		Scopes: []Scope{ScopeSystem, ScopeCompany}})

	settings, err := Open(ctx, st, Options{
		Registry:   reg,
		Scopes:     FixedScopes(companyID, "", ""),
		Authorizer: denyAll{},
	})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	t.Run("undeclared key", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeSystem, "", "nope.not.declared", "x"), CodeUndeclared)
	})
	t.Run("scope not permitted", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeCompany, companyID, "sys.only", "x"), CodeScopeNotAllowed)
	})
	t.Run("value outside enum", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeSystem, "", "e.mode", "z"), CodeInvalidValue)
	})
	t.Run("permission denied", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeSystem, "", "p.guarded", "x"), CodeNotPermitted)
	})
	t.Run("declared validation", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeSystem, "", "v.checked", int64(-5)), CodeInvalidValue)
	})
	t.Run("wrong Go type", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeSystem, "", "v.checked", "not an int"), CodeInvalidValue)
	})
	t.Run("non-system scope without an id", func(t *testing.T) {
		assertCode(t, settings.Set(bound, ScopeCompany, "", "c.scoped", "x"), CodeInvalidValue)
	})
}

type denyAll struct{}

func (denyAll) Can(context.Context, string) bool { return false }

func TestSetNotifiesAndBumpsVersion(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	reg.DeclareString(Def{Key: "k", Default: "d", Scopes: []Scope{ScopeSystem}})

	spy := &recordingNotifier{}
	settings, err := Open(ctx, st, Options{Registry: reg, Notifier: spy})
	if err != nil {
		t.Fatal(err)
	}
	before := settings.Version()

	if err := settings.Set(Bind(ctx, settings), ScopeSystem, "", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if settings.Version() <= before {
		t.Errorf("version did not advance: %d then %d", before, settings.Version())
	}
	if len(spy.keys) != 1 || spy.keys[0] != "k" {
		t.Errorf("notifications = %v, want [k] — live reactivity depends on this", spy.keys)
	}
}

type recordingNotifier struct {
	mu   sync.Mutex
	keys []string
}

func (r *recordingNotifier) SettingChanged(_ context.Context, key string, _ Scope, _ id.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, key)
}

func TestConcurrentReadsDuringWrites(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	h := reg.DeclareString(Def{Key: "k", Default: "d", Scopes: []Scope{ScopeSystem}})
	settings, err := Open(ctx, st, Options{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					// Any value is fine; -race is what this asserts.
					_ = h.Get(bound)
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if err := settings.Set(bound, ScopeSystem, "", "k", "v"); err != nil {
			t.Errorf("Set: %v", err)
			break
		}
	}
	close(stop)
	wg.Wait()
}

// ── feature flags ───────────────────────────────────────────────────────────────

func TestFlagResolutionAndWrite(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	reg := NewRegistry()
	flag := reg.DeclareFlag(FlagDef{
		Key: "accounting.ui", Default: false, Stability: Experimental,
		Scopes: []Scope{ScopeSystem, ScopeCompany},
	})
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry: %v", err)
	}

	settings, err := Open(ctx, st, Options{Registry: reg, Scopes: FixedScopes(companyID, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	bound := Bind(ctx, settings)

	if flag.Get(bound) {
		t.Error("flag should start at its declared default (false)")
	}
	if setErr := settings.SetFlag(bound, ScopeCompany, companyID, flag.Key(), true); setErr != nil {
		t.Fatalf("SetFlag: %v", setErr)
	}
	if !flag.Get(bound) {
		t.Error("flag did not read back as enabled")
	}

	// Company override must beat a system-scope value.
	if setErr := settings.SetFlag(bound, ScopeSystem, "", flag.Key(), false); setErr != nil {
		t.Fatalf("SetFlag system: %v", setErr)
	}
	if !flag.Get(bound) {
		t.Error("system scope overrode a company setting; precedence is inverted")
	}

	fresh, err := Open(ctx, st, Options{Registry: reg, Scopes: FixedScopes(companyID, "", "")})
	if err != nil {
		t.Fatal(err)
	}
	if !flag.Get(Bind(ctx, fresh)) {
		t.Error("flag value did not survive a reload")
	}
}

func TestDeprecatedFlagMustDeclareRemoveBy(t *testing.T) {
	// A deprecation with no removal target is how dead branches become permanent.
	r := NewRegistry()
	r.DeclareFlag(FlagDef{Key: "old.thing", Stability: Deprecated})
	assertCode(t, r.Validate(), CodeRegistryInvalid)
}

func TestExpiredFlagsSurfaceDeadBranches(t *testing.T) {
	r := NewRegistry()
	r.DeclareFlag(FlagDef{Key: "gone.by.1.2", Stability: Deprecated, RemoveBy: "v1.2"})
	r.DeclareFlag(FlagDef{Key: "gone.by.9.0", Stability: Deprecated, RemoveBy: "v9.0"})
	if err := r.Validate(); err != nil {
		t.Fatalf("registry: %v", err)
	}

	expired := r.ExpiredFlags("v1.4.0")
	if len(expired) != 1 || expired[0].Key != "gone.by.1.2" {
		t.Errorf("ExpiredFlags(v1.4.0) = %v, want just gone.by.1.2", expired)
	}
	if got := r.ExpiredFlags("v1.0.0"); len(got) != 0 {
		t.Errorf("ExpiredFlags(v1.0.0) = %v, want none", got)
	}
	// A development build has no meaningful version and must not report everything.
	if got := r.ExpiredFlags("dev"); len(got) != 0 {
		t.Errorf("ExpiredFlags(dev) = %v, want none", got)
	}
}

func TestUnknownStoredFlagIsIgnored(t *testing.T) {
	ctx := context.Background()
	st := newDB(t)

	newID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriterPool().ExecContext(ctx,
		`INSERT INTO feature_flags (id, flag_key, scope, scope_id, is_enabled, created_at, updated_at)
		 VALUES (?, 'removed.flag', 'system', '', 1,
		   '2026-07-30T00:00:00.000Z', '2026-07-30T00:00:00.000Z')`, newID.String()); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	if _, err := Open(ctx, st, Options{Registry: reg}); err != nil {
		t.Fatalf("Open refused to start over an unknown stored flag: %v", err)
	}
}
