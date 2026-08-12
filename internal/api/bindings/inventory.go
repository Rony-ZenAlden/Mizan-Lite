package bindings

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/api/redact"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// StockRowDTO is one variant's stock in one warehouse.
//
// Quantities cross as STRINGS of micro units, for the reason money does (§17): they are integers
// scaled by 10⁶, and JavaScript's number type loses integer precision above 2^53 — which a
// wholesaler's quantities reach. Nothing on the frontend does arithmetic with them.
type StockRowDTO struct {
	VariantID   string `json:"variantId"`
	ProductCode string `json:"productCode"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	Unit        string `json:"unit"`

	OnHandMicro    string `json:"onHandMicro"`
	ReservedMicro  string `json:"reservedMicro"`
	AvailableMicro string `json:"availableMicro"`

	// AverageCostMicro and ValueMinor are behind their own permission: what a business paid for
	// its stock is not something every shelf-stacker should read off a screen.
	//
	// POINTERS, through redact.Visible, so that a caller without the permission gets ABSENT
	// rather than zero. The audit binding established the shape and the reason (1.6): forgetting
	// to redact then hides data instead of exposing it, and a zero cost on a screen reads as
	// free stock rather than as "you may not see this".
	AverageCostMicro *string `json:"averageCostMicro,omitempty"`
	ValueMinor       *string `json:"valueMinor,omitempty"`
}

// MovementRowDTO is one entry in the ledger.
type MovementRowDTO struct {
	ID   string `json:"id"`
	Type string `json:"movementType"`
	// Inward tells the screen which way to point the row without re-deriving it from the type —
	// the same reasoning that puts `depth` on an account and `isSimple` on a product.
	Inward bool `json:"isInward"`

	QuantityMicro     string  `json:"quantityMicro"`
	UnitCostMicro     *string `json:"unitCostMicro,omitempty"`
	ValueMinor        *string `json:"valueMinor,omitempty"`
	BalanceAfterMicro string  `json:"balanceAfterMicro"`

	DocumentType string `json:"documentType"`
	Reason       string `json:"reason"`
	OccurredAt   string `json:"occurredAt"`
}

// LedgerCheckDTO is what the verifier found.
//
// The whole point of putting this on a screen: a projection that has drifted from its ledger is
// invisible until somebody counts, and this is what makes it visible before then.
type LedgerCheckDTO struct {
	Checked       int              `json:"checked"`
	Discrepancies []DiscrepancyDTO `json:"discrepancies"`
	Healthy       bool             `json:"healthy"`
}

// DiscrepancyDTO is one place the projection disagrees with the ledger.
type DiscrepancyDTO struct {
	VariantID       string `json:"variantId"`
	SKU             string `json:"sku"`
	ProjectedOnHand string `json:"projectedOnHandMicro"`
	LedgerOnHand    string `json:"ledgerOnHandMicro"`
	// FirstSuspect turns "the total is wrong" into "it went wrong here", which is the difference
	// between a bug report and an investigation.
	FirstSuspect string `json:"firstSuspectMovementId"`
}

// Inventory is the stock surface.
//
// Reading is separated from adjusting by permission, and deliberately: seeing what is on the
// shelf and changing what the system believes is there are very different acts. A stock
// adjustment is how theft is concealed.
type Inventory struct{ graph }

// inventoryPolicies declares what each method requires.
func inventoryPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Stock":       policy.Requires(inventory.PermStockView),
		"Movements":   policy.Requires(inventory.PermStockView),
		"CheckLedger": policy.Requires(inventory.PermStockView),
		"Adjust":      policy.Requires(inventory.PermStockAdjust),
	}
}

// Stock lists what is on hand, optionally narrowed to one warehouse.
func (i *Inventory) Stock(warehouseID string) envelope.Result[[]StockRowDTO] {
	ctx, app, err := i.guard("Stock")
	if err != nil {
		return envelope.Fail[[]StockRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]StockRowDTO](err)
	}

	levels, err := app.Inventory.Levels(ctx, companyID, id.ID(warehouseID), id.ID(""))
	if err != nil {
		return envelope.Fail[[]StockRowDTO](err)
	}

	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]StockRowDTO](err)
	}
	type productInfo struct{ code, name, unit string }
	byProduct := make(map[id.ID]productInfo, len(products))
	units, err := app.Catalog.Units(ctx)
	if err != nil {
		return envelope.Fail[[]StockRowDTO](err)
	}
	unitCode := make(map[id.ID]string, len(units))
	for _, unit := range units {
		unitCode[unit.ID] = unit.Code
	}
	skuOf := map[id.ID]string{}
	for _, product := range products {
		byProduct[product.ID] = productInfo{
			code: product.Code, name: product.Name, unit: unitCode[product.StockUnitID],
		}
		variants, variantErr := app.Catalog.Variants(ctx, product.ID)
		if variantErr != nil {
			return envelope.Fail[[]StockRowDTO](variantErr)
		}
		for _, variant := range variants {
			skuOf[variant.ID] = variant.SKU
		}
	}

	out := make([]StockRowDTO, 0, len(levels))
	for _, level := range levels {
		info := byProduct[level.ProductID]
		row := StockRowDTO{
			VariantID:   string(level.VariantID),
			ProductCode: info.code, ProductName: info.name, SKU: skuOf[level.VariantID],
			Unit:           info.unit,
			OnHandMicro:    minor(level.State.OnHandMicro),
			ReservedMicro:  minor(level.State.ReservedMicro),
			AvailableMicro: minor(level.State.AvailableMicro()),

			AverageCostMicro: redact.Visible(ctx, app.Identity, inventory.PermCostView,
				minor(level.State.AverageMicro)),
			ValueMinor: redact.Visible(ctx, app.Identity, inventory.PermCostView,
				minor(stockValue(level.State.OnHandMicro, level.State.AverageMicro))),
		}
		out = append(out, row)
	}
	return envelope.Ok(out)
}

// stockValue is what a quantity at an average cost is worth, in whole minor units.
//
// Both inputs are ×10⁶, so the product is ×10¹² and the division is by 10¹². Rounded ONCE, the
// same way the domain does it — computing it differently here would put a second answer to
// "what is this worth" on a screen next to the first.
func stockValue(quantityMicro, averageMicro int64) int64 {
	const scale = 1_000_000_000_000
	product := quantityMicro * averageMicro
	if product < 0 {
		return (product - scale/2) / scale
	}
	return (product + scale/2) / scale
}

// Movements reads one variant's ledger in a warehouse, in occurrence order.
func (i *Inventory) Movements(variantID, warehouseID string) envelope.Result[[]MovementRowDTO] {
	ctx, app, err := i.guard("Movements")
	if err != nil {
		return envelope.Fail[[]MovementRowDTO](err)
	}
	movements, err := app.Inventory.Movements(ctx, id.ID(variantID), id.ID(warehouseID))
	if err != nil {
		return envelope.Fail[[]MovementRowDTO](err)
	}

	out := make([]MovementRowDTO, 0, len(movements))
	for _, m := range movements {
		out = append(out, MovementRowDTO{
			ID: string(m.ID), Type: string(m.Type), Inward: m.Type.IsInward(),
			QuantityMicro:     minor(m.QuantityMicro),
			BalanceAfterMicro: minor(m.BalanceAfterMicro),
			DocumentType:      m.DocumentType, Reason: m.Reason, OccurredAt: m.OccurredAt,

			UnitCostMicro: redact.Visible(ctx, app.Identity, inventory.PermCostView,
				minor(m.UnitCostMicro)),
			ValueMinor: redact.Visible(ctx, app.Identity, inventory.PermCostView,
				minor(m.ValueMinor)),
		})
	}
	return envelope.Ok(out)
}

// CheckLedger replays the movement ledger and reports where the projection disagrees.
//
// Read-only, and it REPORTS: the repair is a separate, deliberate act. A projection that heals
// itself hides the bug that broke it.
func (i *Inventory) CheckLedger() envelope.Result[LedgerCheckDTO] {
	ctx, app, err := i.guard("CheckLedger")
	if err != nil {
		return envelope.Fail[LedgerCheckDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[LedgerCheckDTO](err)
	}

	levels, err := app.Inventory.Levels(ctx, companyID, id.ID(""), id.ID(""))
	if err != nil {
		return envelope.Fail[LedgerCheckDTO](err)
	}
	discrepancies, err := app.Inventory.VerifyLedger(ctx, companyID)
	if err != nil {
		return envelope.Fail[LedgerCheckDTO](err)
	}

	out := LedgerCheckDTO{
		Checked: len(levels), Healthy: len(discrepancies) == 0,
		Discrepancies: make([]DiscrepancyDTO, 0, len(discrepancies)),
	}
	for _, d := range discrepancies {
		out.Discrepancies = append(out.Discrepancies, DiscrepancyDTO{
			VariantID:       string(d.VariantID),
			ProjectedOnHand: minor(d.ProjectedOnHand),
			LedgerOnHand:    minor(d.LedgerOnHand),
			FirstSuspect:    string(d.FirstSuspectMovementID),
		})
	}
	return envelope.Ok(out)
}

// AdjustInput is a hand-entered stock correction.
type AdjustInput struct {
	WarehouseID   string `json:"warehouseId"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	QuantityMicro string `json:"quantityMicro"`
	UnitCostMicro string `json:"unitCostMicro"`
	// Increase says which way. A signed quantity would put the direction in the number, which is
	// the mistake the movement ledger exists to avoid.
	Increase bool   `json:"increase"`
	Reason   string `json:"reason"`
}

// Adjust records a hand-entered correction.
//
// Behind its own permission, because a stock adjustment is how theft is concealed: the person who
// can see stock and the person who can change what the system believes about it are different
// people in any business large enough to have both.
func (i *Inventory) Adjust(in AdjustInput) envelope.Result[MovementRowDTO] {
	ctx, app, err := i.guard("Adjust")
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	quantity, err := parseScaled(in.QuantityMicro)
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}
	cost, err := parseScaled(in.UnitCostMicro)
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	movementType := domain.AdjustmentOut
	if in.Increase {
		movementType = domain.AdjustmentIn
	}

	movement, err := app.Inventory.Move(ctx, inventory.MoveInput{
		CompanyID: companyID, WarehouseID: id.ID(in.WarehouseID),
		ProductID: id.ID(in.ProductID), VariantID: id.ID(in.VariantID),
		Type: movementType, QuantityMicro: quantity, UnitCostMicro: cost,
		ReasonCode: "manual_adjustment", Reason: in.Reason,
	})
	if err != nil {
		return envelope.Fail[MovementRowDTO](err)
	}

	return envelope.Ok(MovementRowDTO{
		ID: string(movement.ID), Type: string(movement.Type),
		Inward:            movement.Type.IsInward(),
		QuantityMicro:     minor(movement.QuantityMicro),
		BalanceAfterMicro: minor(movement.BalanceAfterMicro),
		Reason:            movement.Reason, OccurredAt: movement.OccurredAt,

		UnitCostMicro: redact.Visible(ctx, app.Identity, inventory.PermCostView,
			minor(movement.UnitCostMicro)),
		ValueMinor: redact.Visible(ctx, app.Identity, inventory.PermCostView,
			minor(movement.ValueMinor)),
	})
}

// parseScaled reads a scaled integer that arrived as a string.
//
// Parsed rather than accepted as a number, because a float64 that has been through JSON is not
// the integer that was sent once the value is large enough — which is the whole reason these
// cross as text.
func parseScaled(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errs.Validation(CodeInvalidQuantity,
			"that is not a quantity").WithParam("value", value)
	}
	return parsed, nil
}
