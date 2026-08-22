package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// NewProductInput is what the "new product" form sends.
//
// # Four fields, three of them optional
//
// The service accepts eleven. A form that asked for all eleven would be the reason a shopkeeper
// adds products in a spreadsheet instead — and until 10.10 there was no form at all, so a
// spreadsheet was the ONLY way.
//
// What is genuinely required is a code and a name. Everything else has an answer that is right
// for most shops most of the time:
//
//   - the unit defaults to the shop's most-used one, which the screen supplies
//   - the type defaults to goods, because a shop that sells services knows it does
//   - the category is optional, because filing can happen later and a product that cannot be
//     saved until it is filed is a product somebody keys into a notebook instead
type NewProductInput struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Unit is a unit CODE — "PCS", "KG". Empty means the service's default.
	Unit string `json:"unit"`
	// Category is a category CODE. Empty is allowed and ordinary.
	Category string `json:"category"`
	// Type is "goods" or "service". Empty means goods.
	Type string `json:"type"`
}

// CreateProduct adds a product and its default variant, in one transaction.
//
// # Why this binding did not exist until 10.10
//
// `catalog.CreateProduct` has existed since Phase 3 and is exercised by the CSV importer. What
// was missing was the binding and the screen — so the service, its refusal of duplicate codes,
// its unit resolution, its default variant and its audit entry were all reachable only by writing
// a spreadsheet and importing it.
//
// A shopkeeper adding one product had to open a text editor. That is the friction 10.10 exists to
// remove, and the fix was not simplification: it was the absence of a form.
func (c *Catalog) CreateProduct(in NewProductInput) envelope.Result[ProductRowDTO] {
	ctx, app, err := c.guard("CreateProduct")
	if err != nil {
		return envelope.Fail[ProductRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ProductRowDTO](err)
	}

	kind := catalogdomain.ProductType(in.Type)
	if in.Type == "" {
		kind = catalogdomain.TypeGoods
	}

	// # The default unit is resolved HERE, not left empty and not guessed by the form
	//
	// The service requires a unit and refuses an empty one, correctly: a product with no unit
	// cannot be counted or sold. The first version of this binding passed the form's empty string
	// straight through and every creation failed with `catalog.invalid_unit`, which is the
	// service being right and this layer being lazy.
	//
	// The form's job is to not ask when it does not have to. Answering the question it skipped is
	// this layer's job, and the answer is the company's own unit list — "PCS" where it exists,
	// because a shop selling things one at a time is the common case, and otherwise whatever the
	// company actually has.
	unit := in.Unit
	if unit == "" {
		unit = defaultUnitFor(app, ctx)
	}

	product, variant, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID:    companyID,
		Code:         in.Code,
		Name:         in.Name,
		Type:         kind,
		StockUnit:    unit,
		CategoryCode: in.Category,
	})
	if err != nil {
		return envelope.Fail[ProductRowDTO](err)
	}

	_ = variant
	return envelope.Ok(ProductRowDTO{
		ID: string(product.ID), Code: product.Code, Name: product.Name,
		NameKey: product.NameKey, CategoryCode: in.Category,
		Type: string(product.Type), StockUnit: unit,
		Tracking: string(product.Tracking),
		// One, always: a product without a variant cannot exist (§3.1), and the count is what the
		// browse screen shows.
		VariantCount: 1,
		Active:       true,
	})
}

// StockCountInput sets what is actually on the shelf.
//
// # Counting, not adjusting
//
// `Inventory.Adjust` takes a DELTA — "add three", "remove two" — which is what the domain records
// and the right shape for a movement.
//
// It is the wrong question to ask a person holding a shelf. They know there are eleven; making
// them work out that eleven is two fewer than the thirteen on screen is arithmetic the computer
// should do, and the subtraction they get wrong at the end of a long day.
//
// So the screen asks what is there and this converts. The MOVEMENT is still a delta — nothing
// about the ledger changes.
type StockCountInput struct {
	VariantID   string `json:"variantId"`
	WarehouseID string `json:"warehouseId"`
	// CountedMicro is what is on the shelf, in stock units ×10⁶.
	CountedMicro string `json:"countedMicro"`
	Reason       string `json:"reason"`
}

// CountStock records a physical count as the adjustment that reconciles it.
func (i *Inventory) CountStock(in StockCountInput) envelope.Result[MovementRowDTO] {
	ctx, app, err := i.guard("CountStock")
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	counted, err := parseScaled(in.CountedMicro)
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}
	if counted < 0 {
		return envelope.Fail[MovementRowDTO](errs.Validation(
			"inventory.negative_count", "a count cannot be negative"))
	}

	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	// A movement needs the PRODUCT as well as the variant, and a shelf row only knows the variant
	// — which is what a shelf row is. The first version of this omitted it and every count failed
	// with `inventory.invalid_movement`: the domain refusing a movement it cannot place, which is
	// correct, and this layer not answering a question the screen should not have to.
	product, err := app.Catalog.ProductOfVariant(ctx, companyID, id.ID(in.VariantID))
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	state, err := app.Inventory.StockOf(ctx, id.ID(in.VariantID), id.ID(in.WarehouseID))
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	delta := counted - state.OnHandMicro
	if delta == 0 {
		// Nothing to record. A movement of zero would be a row in the ledger saying nothing
		// happened, and 4.3 already refuses those — returning early says so without an error,
		// because "the count agreed" is a success.
		return envelope.Ok(MovementRowDTO{})
	}

	// The direction is a FLAG, not a sign. 4.2 put it there deliberately: a signed quantity puts
	// the direction inside the number, which is the mistake the movement ledger exists to avoid.
	// So the delta is split back into a magnitude and a direction here.
	increase := delta > 0
	if delta < 0 {
		delta = -delta
	}
	return i.Adjust(AdjustInput{
		ProductID:     string(product.ID),
		VariantID:     in.VariantID,
		WarehouseID:   in.WarehouseID,
		QuantityMicro: minor(delta),
		Increase:      increase,
		Reason:        in.Reason,
	})
}

// defaultUnitFor picks the unit a product gets when the form did not ask.
//
// "PCS" when the company has it, because selling things one at a time is the common case.
// Otherwise the first unit the company actually has — which is better than failing, and better
// than inventing a code that may not exist in this company's list.
func defaultUnitFor(app *bootstrap.App, ctx context.Context) string {
	units, err := app.Catalog.Units(ctx)
	if err != nil || len(units) == 0 {
		// Let the service refuse it and say so, rather than substituting a code that might not
		// exist. A wrong unit is worse than a clear failure.
		return ""
	}
	for _, unit := range units {
		if unit.Code == "PCS" {
			return unit.Code
		}
	}
	return units[0].Code
}
