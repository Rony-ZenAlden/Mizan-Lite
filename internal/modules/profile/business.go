package profile

import (
	"context"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// businessDir is where business-profile bundles live, in both layers.
const businessDir = "seeds/business_profiles"

// Business is a named bundle of settings and feature flags (§16.4).
//
// §16.4 calls this "the concrete mechanism that satisfies 'generic ERP through configuration,
// not code'", and that is not decoration: selecting "Pharmacy" at setup must configure a
// pharmacy without a pharmacy build.
//
// # Two halves, on purpose (D2)
//
// The CATALOGUE — code, name, description, active — is a list the UI shows and an administrator
// may deactivate, so it is metadata and it has a table (0009). The BUNDLE — the settings and
// flags below — is a script run once, so it stays in the file. Storing the bundle would invite
// editing the stored copy, which would then disagree with the file it came from and there would
// be no way to tell which had been applied.
type Business struct {
	Code        string `json:"code"`
	NameKey     string `json:"name_key"`
	Name        string `json:"name"`
	Description string `json:"description"`

	// Settings are written at COMPANY scope when the profile is applied. Values are whatever
	// JSON holds — string, bool, number — and are checked against the declared type at load.
	Settings map[string]any `json:"settings"`
	// Flags are feature flags to force on or off for this trade.
	Flags map[string]bool `json:"flags"`
}

// Registry is the narrow view of the settings registry this module needs.
//
// Declared at the point of use rather than taking *config.Registry, so a test can validate a
// bundle against a registry it controls instead of the process-wide default.
type Registry interface {
	Lookup(key string) (config.Definition, bool)
	LookupFlag(key string) (config.FlagDef, bool)
}

// validate checks a bundle against the filename and the settings registry.
//
// # Why the registry check happens HERE (D5)
//
// Settings.Set already rejects an undeclared key. But it rejects it at APPLY time — halfway
// through the setup wizard, on the customer's machine, with three settings already written.
// Checking at load turns the same defect into a boot failure on a developer's machine for a
// file we ship, and a reported-and-skipped file for one the customer wrote.
//
// Same bug, caught somewhere it can still be fixed cheaply.
func (b Business) validate(name string, reg Registry) error {
	invalid := func(what string, value any) error {
		return errs.Validation(CodeInvalidProfile, "the business profile is not valid").
			WithParam("field", what).WithParam("value", param(value))
	}

	if b.Code == "" || b.Code != strings.ToLower(b.Code) {
		return invalid("code", b.Code)
	}
	if name != "" && name != b.Code {
		return errs.Validation(CodeInvalidProfile,
			"the business profile's code does not match its filename").
			WithParam("file", name).WithParam("value", b.Code)
	}
	if b.Name == "" {
		return invalid("name", b.Name)
	}

	for key, value := range b.Settings {
		def, ok := reg.Lookup(key)
		if !ok {
			return errs.Validation(CodeUndeclaredKey,
				"the profile sets a setting that no module declares").WithParam("key", key)
		}
		// A setting that cannot be overridden per company cannot be part of a business profile,
		// because applying one is a company-scoped act. Catching it here beats discovering it
		// as a scope error during setup.
		if !scopedToCompany(def) {
			return errs.Validation(CodeUndeclaredKey,
				"the profile sets a setting that cannot be overridden per company").
				WithParam("key", key)
		}
		if err := checkValueType(key, def, value); err != nil {
			return err
		}
	}

	for key := range b.Flags {
		if _, ok := reg.LookupFlag(key); !ok {
			return errs.Validation(CodeUndeclaredKey,
				"the profile sets a feature flag that no module declares").WithParam("key", key)
		}
	}
	return nil
}

func scopedToCompany(def config.Definition) bool {
	for _, s := range def.Scopes {
		if s == config.ScopeCompany {
			return true
		}
	}
	return false
}

// checkValueType rejects a value JSON decoded into a Go type the setting cannot hold.
//
// Deliberately shallow: it catches "true" where a number belongs, which is the mistake a human
// editing JSON actually makes. The exact-value checks — enum membership, declared validators —
// stay in Settings.Set, which owns them and would otherwise have two implementations that
// drift.
func checkValueType(key string, def config.Definition, value any) error {
	mismatch := errs.Validation(CodeInvalidProfile,
		"the profile sets a setting to a value of the wrong kind").
		WithParam("key", key).WithParam("value", param(value))

	switch def.Type {
	case config.TypeBool:
		if _, ok := value.(bool); !ok {
			return mismatch
		}
	case config.TypeInt, config.TypeDuration:
		// encoding/json produces float64 for every number. That is a decoding artefact, not a
		// value: a whole number decoded from JSON is still whole, and rejecting it here would
		// make every integer setting unwritable from a file.
		f, ok := value.(float64)
		if !ok || f != float64(int64(f)) {
			return mismatch
		}
	case config.TypeString, config.TypeEnum, config.TypeID, config.TypeMoney:
		if _, ok := value.(string); !ok {
			return mismatch
		}
	}
	return nil
}

// loadBusiness discovers, decodes, and validates every bundle.
func loadBusiness(layers []seeds.Layer, reg Registry) ([]Business, []seeds.Problem, error) {
	set, err := seeds.Discover(businessDir, layers...)
	if err != nil {
		return nil, nil, err
	}

	result, err := seeds.Decode(set, func(data []byte, b *Business) error {
		return seeds.StrictJSON(data, b)
	})
	if err != nil {
		return nil, nil, err
	}

	profiles := make([]Business, 0, len(result.Docs))
	problems := result.Problems
	for _, doc := range result.Docs {
		if validateErr := doc.Value.validate(doc.File.Name, reg); validateErr != nil {
			if doc.File.Origin == seeds.OriginShipped {
				return nil, nil, errs.Wrap(validateErr, errs.CategoryInternal, CodeShippedInvalid,
					"a business profile shipped with this build is invalid").
					WithParam("file", doc.File.Path)
			}
			problems = append(problems, seeds.Problem{
				Path: doc.File.Path, Origin: doc.File.Origin, Err: validateErr,
			})
			continue
		}
		profiles = append(profiles, doc.Value)
	}

	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Code < profiles[j].Code })
	return profiles, problems, nil
}

// catalogueSeed turns the loaded bundles into the metadata seed for business_profiles.
//
// The catalogue comes FROM the files, which is what made this step need no change to
// modules.Module: Metadata() already returns []SeedSpec, and where those specs came from was
// never part of its contract.
func catalogueSeed(profiles []Business) metadata.SeedSpec {
	rows := make([]metadata.Row, 0, len(profiles))
	for _, p := range profiles {
		rows = append(rows, metadata.Row{
			Code:     p.Code,
			Name:     p.Name,
			IsSystem: true,
			Columns:  map[string]any{"description": p.Description, "name_key": p.NameKey},
		})
	}
	return metadata.SeedSpec{
		Table:        "business_profiles",
		Rows:         rows,
		ExtraColumns: []string{"description", "name_key"},
	}
}

// ── applying a profile ──────────────────────────────────────────────────────────

// ActionApplied is the audited action. Stable: renaming it orphans its history.
const ActionApplied = "profile.business.applied"

// EntityBusinessProfile is what applied entries are filed under.
const EntityBusinessProfile = "profile.business"

// Apply writes a profile's settings and flags at company scope, in one transaction.
//
// # Why company scope
//
// Because a business profile describes a BUSINESS, and 0.5 §3.3 settled that applying one is a
// seeding operation rather than a resolution tier. Writing rows means every value stays
// individually editable afterwards — which is the difference between "the pharmacy profile
// configured this" and "the pharmacy profile controls this forever".
//
// # Atomicity
//
// One Unit of Work. A half-applied profile is a company configured by nobody's decision: some
// values from the bundle, the rest from defaults, and no way to tell which. It is also audited
// (1.7), so the entry and the settings commit together or neither does.
func (s *Service) Apply(ctx context.Context, companyID id.ID, code string) error {
	if s.settings == nil {
		return errs.Internal(CodeNotConfigured, "the profile service has no settings to write")
	}
	bundle, ok := s.Business(code)
	if !ok {
		return errs.NotFound(CodeUnknownProfile, "no such business profile").
			WithParam("code", code)
	}
	if companyID.IsZero() {
		return errs.Validation(CodeInvalidProfile, "a business profile is applied to a company")
	}

	return s.db.Do(ctx, func(ctx context.Context) error {
		// Sorted, so applying the same bundle twice writes in the same order — which makes the
		// audit trail and any change notification reproducible rather than map-order noise.
		for _, key := range sortedKeys(bundle.Settings) {
			if err := s.settings.Set(ctx, config.ScopeCompany, companyID, key, bundle.Settings[key]); err != nil {
				return err
			}
		}
		for _, key := range sortedFlagKeys(bundle.Flags) {
			if err := s.settings.SetFlag(ctx, config.ScopeCompany, companyID, key, bundle.Flags[key]); err != nil {
				return err
			}
		}

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionApplied,
			EntityType:  EntityBusinessProfile,
			EntityID:    companyID,
			EntityLabel: bundle.Name,
			// The bundle itself, so the trail answers "what did selecting Furniture actually
			// change?" — a question that is otherwise unanswerable once the values have been
			// edited.
			After: appliedSnapshot{Code: bundle.Code, Settings: bundle.Settings, Flags: bundle.Flags},
		})
	})
}

type appliedSnapshot struct {
	Code     string          `json:"code"`
	Settings map[string]any  `json:"settings,omitempty"`
	Flags    map[string]bool `json:"flags,omitempty"`
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFlagKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
