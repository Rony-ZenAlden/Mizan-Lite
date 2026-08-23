package bootstrap

import (
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/pricing"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/modules"

	// Imported for its SETTING DECLARATIONS, which register in the default registry at package
	// init and are otherwise never linked into a build that does not reach the API layer.
	//
	// # The defect this fixes, found by the profile validator
	//
	// `ui.theme` and `ui.landing` belong to no module — a colour scheme and a home screen are
	// properties of the application, not of accounting or sales — so they are declared in
	// platform/ui, which until 10.19 only `bindings/config.go` imported.
	//
	// Boot VALIDATES the shipped profiles against the declared settings. A profile that sets
	// `ui.landing` therefore failed to load in any binary that did not happen to link the API
	// layer, with the honest but unhelpful message "the profile sets a setting that no module
	// declares". The setting existed; nothing had told the registry about it.
	//
	// It was latent for `ui.theme` for nine phases and would have surfaced the first time a
	// country profile set a theme. Importing it where profiles are validated is what makes the
	// declaration and the validation reach the same registry.
	_ "github.com/mizan-erp/mizan/internal/platform/ui"
)

// DeclarationModules is every module in the build, constructed with NO service.
//
// # Why this exists
//
// Some things are properties of a MODULE rather than of a built graph: its migrations, the
// permissions it declares, the settings it registers. Reading them needs no database, no
// services, and no boot — which is what makes the migration phase possible at all, since the
// schema has to exist before the graph that reads it.
//
// It is exported because tests need the same list. The policy-coverage test (1.5) previously
// hardcoded three modules and had to be edited every time a fourth declared a permission —
// which meant a new module's permissions could be missing from the check that exists to catch
// exactly that. One list, used by both.
//
// The ORDER here is arbitrary: modules.Order topologically sorts by DependsOn, and Step 0.10
// deliberately hands them over in a wrong order so the sort has to do real work.
func DeclarationModules() []modules.Module {
	return []modules.Module{
		currency.NewModule(nil),
		org.NewModule(nil),
		identity.NewModule(nil),
		audit.NewModule(nil),
		profile.NewModule(nil),
		accounting.NewModule(nil),
		tax.NewModule(nil),
		catalog.NewModule(nil),
		partner.NewModule(nil),
		pricing.NewModule(nil),
		inventory.NewModule(nil),
		sales.NewModule(nil),
		purchasing.NewModule(nil),
		expenses.NewModule(nil),
	}
}

// DeclaredPermissions is every permission code this build declares.
//
// The set the policy-coverage check validates against: a binding requiring a permission no
// module declares could never be granted, so the method would be permanently unreachable —
// a silent outage rather than a security hole, and just as worth catching (1.5).
func DeclaredPermissions() map[string]bool {
	out := map[string]bool{auth.Wildcard: true}
	for _, module := range DeclarationModules() {
		for _, def := range module.Permissions() {
			out[def.Code] = true
		}
	}
	return out
}
