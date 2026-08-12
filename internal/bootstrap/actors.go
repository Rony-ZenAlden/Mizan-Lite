package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/sales"
)

// auditActors tells the audit module who is acting.
//
// The adapter lives HERE, in the composition root, because it is the only place that may know
// both sides. `appctx` is the API layer and `audit` is a module; a module importing the API
// layer would point a dependency outward (§3.1), and the API layer owning an audit-shaped type
// would put a module's vocabulary in the wrong package. Neither knows the other exists — one
// declares the port at its point of use, the other satisfies it, and eleven lines of wiring in
// between is what that costs.
type auditActors struct{}

var _ audit.ActorResolver = auditActors{}

// Actor reads the principal the 1.5 guard stamped onto the context.
//
// `false` is a legitimate answer, not a failure: the setup wizard, a background job, and the
// login screen all act with no user, and the entry is then recorded unattributed.
func (auditActors) Actor(ctx context.Context) (audit.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return audit.Actor{}, false
	}
	return audit.Actor{
		UserID:      a.UserID,
		DisplayName: a.DisplayName,
		BranchID:    a.BranchID,
		SessionID:   a.SessionID,
	}, true
}

// identityActors tells identity who is acting.
//
// A second adapter rather than reusing auditActors: the two modules declare different ports
// because they need different things — audit wants a name and a branch to snapshot, identity
// wants only an identifier to compare. Making one satisfy both would mean one of them carrying
// a field it has no use for, which is shape without meaning.
type identityActors struct{}

var _ identity.ActingUser = identityActors{}

func (identityActors) UserID(ctx context.Context) (id.ID, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return id.ID(""), false
	}
	return a.UserID, true
}

// inventoryActors satisfies inventory's own ActorResolver from the same context principal.
//
// A second type rather than one satisfying both interfaces: inventory declares its own port so
// that it does not depend on the audit package (§10.3's module isolation), and the two shapes
// are deliberately different — inventory needs a user and a branch, not a session or a display
// name. One implementation, two narrow views of it, and the composition root is where they meet.
type inventoryActors struct{}

var _ inventory.ActorResolver = inventoryActors{}

// Actor reads the principal the 1.5 guard stamped onto the context.
func (inventoryActors) Actor(ctx context.Context) (inventory.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return inventory.Actor{}, false
	}
	return inventory.Actor{UserID: a.UserID, BranchID: a.BranchID}, true
}

// inventoryProducts satisfies inventory's Products port from the catalog service.
//
// The composition root is where a module's narrow port meets the module that can answer it —
// which is what keeps inventory from importing catalog and breaking module isolation (§10.3).
type inventoryProducts struct{ catalog *catalog.Service }

var _ inventory.Products = inventoryProducts{}

// TrackingOf reports how finely a product is tracked.
//
// # A missing product and a broken database are NOT the same answer
//
// The first draft returned "quantity" for any error at all, and golangci-lint's `nilerr` caught
// it — the second time that rule has found a real defect in this codebase, after Phase 2's tax
// repository read a database fault as "this company charges no tax".
//
// The failure it prevents here: a transient database error would report a lot-tracked product as
// quantity-tracked, and the movement would then be accepted WITHOUT a lot. The batch becomes
// untraceable, silently, at exactly the moment traceability was being recorded — and nothing in
// the trail says why.
//
// So a product that does not EXIST reads as `quantity`, which is defensible: the movement is
// about to fail on its foreign key with a message naming the product, and that is clearer than a
// tracking question failing first. Any other error propagates and stops the movement.
func (p inventoryProducts) TrackingOf(
	ctx context.Context, productID id.ID,
) (inventory.Tracking, error) {
	if p.catalog == nil {
		return inventory.Tracking(catalogdomain.TrackQuantity), nil
	}
	product, err := p.catalog.ProductByID(ctx, productID)
	if err != nil {
		if errs.CategoryOf(err) == errs.CategoryNotFound {
			return inventory.Tracking(catalogdomain.TrackQuantity), nil
		}
		return "", err
	}
	return inventory.Tracking(product.Tracking), nil
}

// inventoryLedger satisfies inventory's Ledger port from the accounting service.
//
// One number, by mapping KEY rather than by account code — so inventory never learns which
// account holds stock, and a country whose chart differs is a seed file rather than a change
// here (§20.3).
type inventoryLedger struct{ accounting *accounting.Service }

var _ inventory.Ledger = inventoryLedger{}

// BalanceOfMapping reports what the books say an account role holds.
func (l inventoryLedger) BalanceOfMapping(
	ctx context.Context, companyID id.ID, mappingKey string,
) (int64, error) {
	if l.accounting == nil {
		return 0, nil
	}
	return l.accounting.BalanceOfMapping(ctx, companyID, mappingKey)
}

// salesActors satisfies sales' ActorResolver from the same context principal.
type salesActors struct{}

var _ sales.ActorResolver = salesActors{}

// Actor reads the principal the 1.5 guard stamped onto the context.
func (salesActors) Actor(ctx context.Context) (sales.Actor, bool) {
	a, ok := appctx.ActorFrom(ctx)
	if !ok || a.UserID.IsZero() {
		return sales.Actor{}, false
	}
	return sales.Actor{UserID: a.UserID, BranchID: a.BranchID}, true
}

// salesCatalog satisfies sales' Catalog port from the catalog service.
//
// The conversion and the refusals — half a chair, kilograms sold as metres — happen inside
// catalog, which owns units. Sales gets the answer and the facts to snapshot.
type salesCatalog struct{ catalog *catalog.Service }

var _ sales.Catalog = salesCatalog{}

// LineFacts reports what a line must snapshot, and converts the quantity into stock units.
func (c salesCatalog) LineFacts(
	ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
) (sales.LineFacts, error) {
	if c.catalog == nil {
		return sales.LineFacts{}, errs.Internal("sales.catalog_missing",
			"the catalog is not available")
	}
	product, variant, unit, inStock, err := c.catalog.SaleFacts(
		ctx, companyID, variantID, uomID, quantityMicro)
	if err != nil {
		return sales.LineFacts{}, err
	}
	return sales.LineFacts{
		ProductID: product.ID, ProductName: product.Name, VariantSKU: variant.SKU,
		UomID: unit.ID, UomCode: unit.Code, QuantityStockMicro: inStock,
	}, nil
}
