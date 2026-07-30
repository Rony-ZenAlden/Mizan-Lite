// Package strategy is the third configuration mechanism: named code behaviours selected
// by data (PHASE_0_FOUNDATION §CFG.4).
//
// The rule it exists to enforce:
//
//	Data selects behaviour; code provides behaviour.
//
// A tax RATE is data; the tax resolution ALGORITHM is code. A payment METHOD is data; the
// payment PROVIDER INTEGRATION is a registered strategy. This is what lets Mizan support a
// new country by editing seed data while the code paths stay finite, testable, and safe.
//
// A `switch` on a business "kind" anywhere under internal/modules is, per §CFG.4, a design
// defect: it makes the set of behaviours closed and invisible to the admin UI. Route it
// through a registry instead.
package strategy

import (
	"sort"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeDuplicate = "strategy.duplicate"
	CodeUnknown   = "strategy.unknown"
	CodeInvalid   = "strategy.invalid"
)

// Extension-point names.
//
// The names are declared here, centrally, so they cannot collide and so the set of
// extension points is discoverable in one place. The typed registries themselves are
// created by the package that owns the contract type — `strategy.New[CostingStrategy]`
// lives with CostingStrategy, in the inventory module.
//
// Deliberately NOT done here: declaring placeholder interfaces for these points now. An
// empty `type CostingStrategy interface{}` would be a fiction that every later phase has
// to replace, and it would let anything at all register itself in the meantime.
const (
	PointCosting          = "costing"
	PointRateProvider     = "rate_provider"
	PointPrintRenderer    = "print_renderer"
	PointBarcodeSymbology = "barcode_symbology"
	PointPaymentProvider  = "payment_provider"
	PointReport           = "report"
	PointExportFormat     = "export_format"
	PointPostingEvaluator = "posting_evaluator"
	PointImportSource     = "import_source"
)

// Points lists every declared extension point, ordered.
func Points() []string {
	out := []string{
		PointCosting, PointRateProvider, PointPrintRenderer, PointBarcodeSymbology,
		PointPaymentProvider, PointReport, PointExportFormat, PointPostingEvaluator,
		PointImportSource,
	}
	sort.Strings(out)
	return out
}

// Registry maps a stable string key to one implementation of T.
type Registry[T any] struct {
	name  string
	mu    sync.RWMutex
	items map[string]T
}

// New returns an empty registry for the named extension point.
func New[T any](name string) *Registry[T] {
	return &Registry[T]{name: name, items: map[string]T{}}
}

// Name returns the extension point's name, used in error messages.
func (r *Registry[T]) Name() string { return r.name }

// Register adds an implementation.
//
// A duplicate key is an error rather than an overwrite: two modules claiming "fifo" is a
// developer mistake whose silent resolution would depend on package init order, and would
// therefore differ between builds.
func (r *Registry[T]) Register(key string, impl T) error {
	if key == "" {
		return errs.Validation(CodeInvalid,
			"a strategy was registered with an empty key").WithParam("point", r.name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[key]; exists {
		return errs.Conflict(CodeDuplicate, "strategy key registered more than once").
			WithParam("point", r.name).WithParam("key", key)
	}
	r.items[key] = impl
	return nil
}

// Resolve returns the implementation registered under key.
//
// A missing key returns a typed NotFound naming both the point and the key — never a zero
// value. Data selects behaviour, and data can be wrong: a payment_methods row pointing at
// an uninstalled provider must produce a clear, translatable error at the point of use,
// not a nil-interface panic three frames later, in front of a customer.
func (r *Registry[T]) Resolve(key string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	impl, ok := r.items[key]
	if !ok {
		var zero T
		return zero, errs.NotFound(CodeUnknown, "no strategy is registered under that key").
			WithParam("point", r.name).WithParam("key", key)
	}
	return impl, nil
}

// Has reports whether a key is registered, for validating persisted data without
// constructing the implementation.
func (r *Registry[T]) Has(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.items[key]
	return ok
}

// Keys returns every registered key, ordered.
//
// Sorted, not map order: these drive admin dropdowns, and map iteration would reshuffle
// the list on every launch.
func (r *Registry[T]) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.items))
	for k := range r.items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of registered implementations.
func (r *Registry[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}
