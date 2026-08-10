package catalog_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc   *catalog.Service
	audit *audit.Service
	store *database.Store
	ctx   context.Context
}

func newFixture(t *testing.T, opts catalog.Options) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	merged := migrate.Merge(
		migrations.SQLite(),
		audit.NewModule(nil).Migrations(),
		catalog.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	opts.Bus = bus
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	svc, err := catalog.NewService(store, opts)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return fixture{svc: svc, audit: auditSvc, store: store, ctx: ctx}
}

func overlay(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// ── the shipped units ───────────────────────────────────────────────────────────

// If any shipped unit set fails to parse or validate, NewService returns an error and this
// fails here, on a developer's machine.
func TestTheShippedUnitsLoad(t *testing.T) {
	f := newFixture(t, catalog.Options{})

	sets := f.svc.UnitSets()
	if len(sets) == 0 {
		t.Fatal("no unit sets shipped")
	}
	if len(f.svc.Problems()) != 0 {
		t.Errorf("shipped units reported problems: %+v", f.svc.Problems())
	}
}

func TestApplyingUnitsCreatesEveryCategory(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	units, err := f.svc.Units(f.ctx)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	if len(units) == 0 {
		t.Fatal("no units were created")
	}

	// The measures real trade needs on day one (§B): kilograms, litres, metres, and a count.
	byCode := map[string]domain.Unit{}
	for _, unit := range units {
		byCode[unit.Code] = unit
	}
	for _, code := range []string{"PCS", "KG", "G", "L", "M", "CM", "M2"} {
		if _, found := byCode[code]; !found {
			t.Errorf("%s is missing", code)
		}
	}

	// A piece cannot be divided; a kilogram can. Per-unit, because one business has both.
	if byCode["PCS"].AllowsFractional {
		t.Error("pieces are divisible; half a chair would be sellable")
	}
	if !byCode["KG"].AllowsFractional {
		t.Error("kilograms are not divisible; 1.5 kg of timber would be unsellable")
	}
}

// Applying twice must change nothing — so a re-run after an upgrade is safe, and a unit an
// administrator renamed survives it.
func TestApplyingUnitsIsIdempotent(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	first, err := f.svc.Units(f.ctx)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}

	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE units_of_measure SET name = 'Kilo' WHERE code = 'KG'`); err != nil {
		t.Fatalf("renaming a unit: %v", err)
	}

	if err = f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("second ApplyUnits: %v", err)
	}

	second, err := f.svc.Units(f.ctx)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("%d units after a second apply, want %d", len(second), len(first))
	}
	// The rename survived: a row an administrator edited is theirs (0.5's rule).
	renamed, err := f.svc.UnitByCode(f.ctx, "KG")
	if err != nil {
		t.Fatalf("UnitByCode: %v", err)
	}
	if renamed.Name != "Kilo" {
		t.Errorf("name = %q; the re-apply overwrote an administrator's edit", renamed.Name)
	}
}

// ── conversion through the real tables ──────────────────────────────────────────

func TestConversionThroughTheService(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	// 2.5 kg is 2500 g.
	grams, err := f.svc.ConvertQuantity(f.ctx, 2_500_000, "KG", "G")
	if err != nil {
		t.Fatalf("ConvertQuantity: %v", err)
	}
	if grams != 2_500_000_000 {
		t.Errorf("2.5 kg = %d g-micro, want 2500000000", grams)
	}

	// And a tonne is a million grams.
	tonneToGram, err := f.svc.ConvertQuantity(f.ctx, 1_000_000, "T", "G")
	if err != nil {
		t.Fatalf("ConvertQuantity: %v", err)
	}
	if tonneToGram != 1_000_000_000_000 {
		t.Errorf("1 t = %d g-micro, want 1000000000000", tonneToGram)
	}
}

// Across categories it is a typed error, not a silent zero.
func TestCrossCategoryConversionIsRefusedThroughTheService(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	_, err := f.svc.ConvertQuantity(f.ctx, 1_000_000, "KG", "M")
	if err == nil {
		t.Fatal("kilograms were converted to metres")
	}
	if code := errs.CodeOf(err); code != domain.CodeCrossCategory {
		t.Errorf("code = %q, want %q", code, domain.CodeCrossCategory)
	}
}

// Every shipped category satisfies the invariant conversion depends on.
func TestEveryShippedCategoryHasExactlyOneReference(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	units, err := f.svc.Units(f.ctx)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	byCategory := map[string][]domain.Unit{}
	for _, unit := range units {
		byCategory[string(unit.CategoryID)] = append(byCategory[string(unit.CategoryID)], unit)
	}
	for category, list := range byCategory {
		if err = domain.ValidateCategoryUnits(list); err != nil {
			t.Errorf("category %s: %v", category, err)
		}
	}
}

// ── a customer's own units ──────────────────────────────────────────────────────

// A trade that measures in something the product never heard of adds a file — the fourth
// consumer of the loader Step 1.8 built.
func TestATradeCanAddItsOwnUnits(t *testing.T) {
	f := newFixture(t, catalog.Options{UserFS: overlay(map[string]string{
		"seeds/uom/textile.json": `{
			"code": "textile", "name": "Textile", "description": "bolts and yards",
			"categories": [{
				"code": "TEXTILE_LENGTH", "name": "Cloth", "name_key": "uom.category.cloth",
				"units": [
					{ "code": "YD", "name": "Yard", "symbol": "yd", "factor_nano": 1000000000, "reference": true, "fractional": true, "precision": 1000, "decimals": 3 },
					{ "code": "BOLT", "name": "Bolt", "symbol": "bolt", "factor_nano": 40000000000, "fractional": false, "precision": 1000000, "decimals": 0 }
				]
			}]
		}`,
	})})

	if err := f.svc.ApplyUnits(f.ctx, "textile"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	// A bolt is 40 yards.
	yards, err := f.svc.ConvertQuantity(f.ctx, 2_000_000, "BOLT", "YD")
	if err != nil {
		t.Fatalf("ConvertQuantity: %v", err)
	}
	if yards != 80_000_000 {
		t.Errorf("2 bolts = %d yd-micro, want 80000000", yards)
	}
}

// A category with no reference makes every conversion in it impossible, so the file is refused
// rather than half-applied.
func TestAUnitSetWithNoReferenceIsRefused(t *testing.T) {
	f := newFixture(t, catalog.Options{UserFS: overlay(map[string]string{
		"seeds/uom/broken.json": `{
			"code": "broken", "name": "Broken", "description": "",
			"categories": [{
				"code": "BROKEN", "name": "Broken", "name_key": "",
				"units": [
					{ "code": "XX", "name": "X", "symbol": "x", "factor_nano": 2000000000, "fractional": true, "precision": 1000, "decimals": 3 }
				]
			}]
		}`,
	})})

	if err := f.svc.ApplyUnits(f.ctx, "broken"); err == nil {
		t.Fatal("a category with no reference unit was applied")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v, want the broken set reported", f.svc.Problems())
	}
	if f.svc.Problems()[0].Origin != seeds.OriginUser {
		t.Errorf("origin = %q, want %q", f.svc.Problems()[0].Origin, seeds.OriginUser)
	}
}

// A unit code is globally unique: a document line names a unit by code, and "KG" meaning two
// different things is unresolvable.
func TestADuplicateUnitCodeIsRefused(t *testing.T) {
	f := newFixture(t, catalog.Options{UserFS: overlay(map[string]string{
		"seeds/uom/clash.json": `{
			"code": "clash", "name": "Clash", "description": "",
			"categories": [{
				"code": "A", "name": "A", "name_key": "",
				"units": [
					{ "code": "KG", "name": "Kilo", "symbol": "kg", "factor_nano": 1000000000, "reference": true, "fractional": true, "precision": 1000, "decimals": 3 },
					{ "code": "KG", "name": "Again", "symbol": "kg", "factor_nano": 2000000000, "fractional": true, "precision": 1000, "decimals": 3 }
				]
			}]
		}`,
	})})

	if _, ok := unitSetByCode(f.svc.UnitSets(), "clash"); ok {
		t.Error("a set listing the same unit code twice was accepted")
	}
	if len(f.svc.Problems()) != 1 {
		t.Fatalf("problems = %+v", f.svc.Problems())
	}
	if !strings.Contains(f.svc.Problems()[0].Err.Error(), "twice") {
		t.Errorf("the problem does not name the duplicate: %v", f.svc.Problems()[0].Err)
	}
}

func unitSetByCode(sets []catalog.UnitSet, code string) (catalog.UnitSet, bool) {
	for _, set := range sets {
		if set.Code == code {
			return set, true
		}
	}
	return catalog.UnitSet{}, false
}

// Applying units is an audited act, inside the same transaction (1.7).
func TestApplyingUnitsIsAudited(t *testing.T) {
	f := newFixture(t, catalog.Options{})
	if err := f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: catalog.EntityUnits})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != catalog.ActionUnitsApplied {
		t.Fatalf("audit entries = %+v, want one units-applied", entries)
	}

	// A second apply creates nothing, so it records nothing — an entry per boot saying "no
	// units were created" is the noise that trains people to ignore the trail.
	if err = f.svc.ApplyUnits(f.ctx, "standard"); err != nil {
		t.Fatalf("second ApplyUnits: %v", err)
	}
	entries, err = f.audit.Entries(f.ctx, audit.Filter{EntityType: catalog.EntityUnits})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("%d entries after a no-op apply, want 1", len(entries))
	}
}
