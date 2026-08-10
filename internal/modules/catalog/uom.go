package catalog

import (
	"context"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/catalog/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// uomDir is where unit sets live, in both layers.
const uomDir = "seeds/uom"

// UnitSet is a file of unit categories and their units.
//
// A seed FILE rather than a migration, on the loader Step 1.8 built — fourth consumer. A trade
// that measures in bushels or board-feet adds a file; nothing shipped is a jurisdictional or
// trade-specific claim, which is the same discipline the country profiles follow (§C.3).
type UnitSet struct {
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Categories  []UnitCategoryDoc `json:"categories"`
}

// UnitCategoryDoc is one category and the units inside it.
type UnitCategoryDoc struct {
	Code    string    `json:"code"`
	Name    string    `json:"name"`
	NameKey string    `json:"name_key"`
	Units   []UnitDoc `json:"units"`
}

// UnitDoc is one unit as written in a file.
type UnitDoc struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NameKey    string `json:"name_key"`
	Symbol     string `json:"symbol"`
	FactorNano int64  `json:"factor_nano"`
	Reference  bool   `json:"reference"`
	Fractional bool   `json:"fractional"`
	Precision  int64  `json:"precision"`
	Decimals   int    `json:"decimals"`
}

// validate checks a unit set at LOAD.
//
// The category invariant — exactly one reference, factor 1 — is checked here as well as by the
// schema, because a file naming the file is a better error than a constraint naming an index.
func (s UnitSet) validate(name string) error {
	invalid := func(field, value string) error {
		return errs.Validation(CodeInvalidUnitSet, "the unit set is not valid").
			WithParam("field", field).WithParam("value", value)
	}

	if s.Code == "" || s.Code != strings.ToLower(s.Code) {
		return invalid("code", s.Code)
	}
	if name != "" && name != s.Code {
		return errs.Validation(CodeInvalidUnitSet,
			"the unit set's code does not match its filename").
			WithParam("file", name).WithParam("value", s.Code)
	}
	if len(s.Categories) == 0 {
		return invalid("categories", "empty")
	}

	seenUnits := map[string]bool{}
	for _, category := range s.Categories {
		if category.Code == "" || len(category.Units) == 0 {
			return invalid("categories[].code", category.Code)
		}

		units := make([]domain.Unit, 0, len(category.Units))
		for _, doc := range category.Units {
			if seenUnits[doc.Code] {
				// Unit codes are globally unique (`ux_uom_code`), because a document line names
				// a unit by code and "KG" meaning two different things is unresolvable.
				return errs.Validation(CodeInvalidUnitSet,
					"a unit code appears twice").WithParam("code", doc.Code)
			}
			seenUnits[doc.Code] = true
			units = append(units, doc.unit(id.ID(category.Code)))
		}

		if err := domain.ValidateCategoryUnits(units); err != nil {
			return errs.Wrap(err, errs.CategoryValidation, CodeInvalidUnitSet,
				"a unit category is not usable").WithParam("category", category.Code)
		}
	}
	return nil
}

// unit converts a file entry into the domain type, applying the defaults a file may omit.
func (d UnitDoc) unit(categoryID id.ID) domain.Unit {
	precision := d.Precision
	if precision <= 0 {
		precision = 1000
	}
	return domain.Unit{
		ID: id.ID(d.Code), CategoryID: categoryID, Code: d.Code, Name: d.Name,
		NameKey: d.NameKey, Symbol: d.Symbol,
		FactorNano: d.FactorNano, IsReference: d.Reference,
		AllowsFractional: d.Fractional, RoundingPrecision: precision,
		DisplayDecimals: d.Decimals, IsSystem: true, IsActive: true,
	}
}

// loadUnitSets discovers and validates every unit set.
func loadUnitSets(layers []seeds.Layer) ([]UnitSet, []seeds.Problem, error) {
	set, err := seeds.Discover(uomDir, layers...)
	if err != nil {
		return nil, nil, err
	}
	result, err := seeds.Decode(set, func(data []byte, s *UnitSet) error {
		return seeds.StrictJSON(data, s)
	})
	if err != nil {
		return nil, nil, err
	}

	out := make([]UnitSet, 0, len(result.Docs))
	problems := result.Problems
	for _, doc := range result.Docs {
		if validateErr := doc.Value.validate(doc.File.Name); validateErr != nil {
			if doc.File.Origin == seeds.OriginShipped {
				return nil, nil, errs.Wrap(validateErr, errs.CategoryInternal, CodeShippedUnitsInvalid,
					"units shipped with this build are invalid").WithParam("file", doc.File.Path)
			}
			problems = append(problems, seeds.Problem{
				Path: doc.File.Path, Origin: doc.File.Origin, Err: validateErr,
			})
			continue
		}
		out = append(out, doc.Value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, problems, nil
}

// UnitSets lists the unit sets on offer.
func (s *Service) UnitSets() []UnitSet {
	out := make([]UnitSet, len(s.unitSets))
	copy(out, s.unitSets)
	return out
}

// ApplyUnits writes a unit set's categories and units, in one transaction.
//
// Idempotent by CODE: a unit that already exists is left alone, so re-running is safe and a
// customer's edit to a shipped unit's name survives an upgrade — the same rule the metadata
// seeder follows (0.5).
func (s *Service) ApplyUnits(ctx context.Context, code string) error {
	var set UnitSet
	found := false
	for _, candidate := range s.unitSets {
		if candidate.Code == code {
			set, found = candidate, true
			break
		}
	}
	if !found {
		return errs.NotFound(CodeUnknownUnitSet, "no such unit set").WithParam("units", code)
	}

	return s.db.Do(ctx, func(ctx context.Context) error {
		created := 0
		for _, categoryDoc := range set.Categories {
			categoryID, err := s.repos.EnsureUnitCategory(ctx, sqlite.UnitCategoryRow{
				Code: categoryDoc.Code, Name: categoryDoc.Name, NameKey: categoryDoc.NameKey,
			})
			if err != nil {
				return err
			}

			for _, doc := range categoryDoc.Units {
				unit := doc.unit(categoryID)
				identifier, idErr := id.New()
				if idErr != nil {
					return idErr
				}
				unit.ID = identifier
				inserted, insertErr := s.repos.EnsureUnit(ctx, unit)
				if insertErr != nil {
					return insertErr
				}
				if inserted {
					created++
				}
			}
		}

		if created == 0 {
			// Nothing changed, so nothing is recorded. An audit entry per boot saying "no
			// units were created" is the noise that trains people to ignore the trail.
			return nil
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionUnitsApplied,
			EntityType:  EntityUnits,
			EntityLabel: set.Name,
			After:       unitsSnapshot{Code: set.Code, Created: created},
		})
	})
}

type unitsSnapshot struct {
	Code    string `json:"code"`
	Created int    `json:"created"`
}

// Units lists every unit, with its category.
func (s *Service) Units(ctx context.Context) ([]domain.Unit, error) {
	return s.repos.Units(ctx)
}

// UnitByCode finds one unit by its stable key.
func (s *Service) UnitByCode(ctx context.Context, code string) (domain.Unit, error) {
	return s.repos.UnitByCode(ctx, strings.ToUpper(code))
}

// ConvertQuantity converts between two units by code (§B.2).
//
// The service resolves the units and the domain does the arithmetic — so a caller cannot
// convert between units it has not looked up, which is how a stale factor gets used.
func (s *Service) ConvertQuantity(
	ctx context.Context, micro int64, fromCode, toCode string,
) (int64, error) {
	from, err := s.UnitByCode(ctx, fromCode)
	if err != nil {
		return 0, err
	}
	to, err := s.UnitByCode(ctx, toCode)
	if err != nil {
		return 0, err
	}
	// Half away from zero, the same mode money uses (0.2). A unit carries a PRECISION — the
	// smallest meaningful step — but not a mode: rounding a quantity differently from the price
	// on the same line is how a document's total stops matching its parts.
	return domain.Convert(micro, from, to, round.HalfAwayFromZero)
}
