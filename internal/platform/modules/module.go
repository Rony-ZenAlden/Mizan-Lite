// Package modules defines the contract every business module implements.
//
// It is finalised in Phase 0 (PHASE_0_FOUNDATION §MOD) so Phase 1 has a stable target, and it
// is the seam that later becomes the plugin contract (ARCHITECTURE_v1 §25). A module declares
// what it owns — schema, settings, flags, reference data, subscriptions, jobs, bindings — and
// the composition root wires it.
package modules

import (
	"io/fs"

	"github.com/mizan-erp/mizan/internal/platform/auth"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// Module is a self-contained business capability.
//
// Two deliberate departures from the §MOD sketch, both to avoid speculative code:
//
//   - There is no Permissions() method. auth.PermissionDef does not exist until Phase 1, and
//     inventing the type now to satisfy a signature would be the same fiction as declaring
//     placeholder event interfaces. It is added when RBAC lands.
//   - There is no RegisterStrategies(). A cross-module strategy registry needs at least one
//     extension point owned by somebody other than its only user; today the single point
//     (rate providers) is defined and owned by the currency module, which exposes its own
//     typed registry directly. The method arrives with the second consumer.
type Module interface {
	// Name is the stable module identifier, e.g. "currency". Used for dependency ordering
	// and to namespace anything the module registers.
	Name() string

	// DependsOn lists module names that must be constructed first. The composition root
	// enforces that this graph is acyclic.
	DependsOn() []string

	// Migrations returns the module's own schema files, embedded in its package. Version
	// numbers are globally sequenced across every module (§10.3), and the runner rejects a
	// duplicate version from any two sources.
	Migrations() fs.FS

	// Settings are the module's declared configuration knobs (§CFG.2). Returned for
	// validation and for the generated settings UI; reads go through the typed handles the
	// module exports.
	Settings() []config.Definition

	// FeatureFlags are the module's declared flags (§17).
	FeatureFlags() []config.FlagDef

	// Metadata is reference data seeded idempotently by code (§CFG.3).
	Metadata() []metadata.SeedSpec

	// Subscribe registers event handlers. Both buses are offered because Step 0.6
	// established two distinct mechanisms — synchronous domain events and the durable
	// outbox — and a module may legitimately use either.
	Subscribe(bus *eventbus.Bus, integration *outbox.Subscribers) error

	// Permissions are the permissions this module protects (§14.1).
	//
	// Deferred by Step 0.9 (D5) because auth.PermissionDef did not exist and declaring a
	// method returning a type invented on the spot would have been the speculative fiction
	// 0.6 avoided with placeholder events. It exists now, so the method joins the contract.
	//
	// Permissions are CODE-DEFINED and never user-created: the composition root reconciles
	// them into the table at startup, which is what stops the permission list drifting from
	// what the code actually checks.
	Permissions() []auth.PermissionDef

	// Jobs are the module's background tasks (§24).
	Jobs() []jobs.Registration

	// There is deliberately NO Bindings() method.
	//
	// 0.9 (D5) added one, expecting each module to return its own Wails binding struct. 0.11
	// (D2) then inverted boot so the window opens BEFORE the object graph exists — which means
	// Wails is handed its fixed []any while no module has been constructed. A per-module
	// binding cannot be collected in time, and every one of the six modules returned nil for
	// two phases.
	//
	// The Phase 1 Definition-of-Done review (1.12) closed the 0.11 D8 debt item by removing it
	// rather than by finding a use: the binding surface is a property of the BUILD, assembled
	// statically in internal/api/bindings, and a contract method with no possible implementor
	// is a promise the architecture cannot keep.
}
