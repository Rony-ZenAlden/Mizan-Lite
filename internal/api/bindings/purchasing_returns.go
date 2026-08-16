package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// SupplierReturnDTO is one return of goods to a supplier.
type SupplierReturnDTO struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	Status       string `json:"status"`
	SupplierID   string `json:"supplierId"`
	SupplierName string `json:"supplierName"`
	ReturnDate   string `json:"returnDate"`
	Reason       string `json:"reason"`
	Currency     string `json:"currency"`

	NetMinor   string `json:"netMinor"`
	TaxMinor   string `json:"taxMinor"`
	TotalMinor string `json:"totalMinor"`
	// CostMinor is what the goods cost US, at the ORIGINAL delivery's figure. It differs from the
	// total, which is what the supplier credits — and the difference is the whole reason both
	// cross the boundary (§D.3).
	CostMinor string `json:"costMinor"`

	Lines []ReturnLineDTO `json:"lines"`
}

// ReturnLineDTO is one line going back.
type ReturnLineDTO struct {
	ID            string `json:"id"`
	LineNumber    int    `json:"lineNumber"`
	ReceiptLineID string `json:"receiptLineId"`
	ProductName   string `json:"productName"`
	VariantSKU    string `json:"variantSku"`
	UomCode       string `json:"uomCode"`

	QuantityMicro  string `json:"quantityMicro"`
	UnitPriceMicro string `json:"unitPriceMicro"`
	// UnitCostMicro is the cost the ORIGINAL delivery carried, not today's average. A screen
	// showing it is showing why the credit and the stock value differ.
	UnitCostMicro string `json:"unitCostMicro"`
	NetMinor      string `json:"netMinor"`
	TotalMinor    string `json:"totalMinor"`
}

// NewReturnInput is what a screen sends to start a return.
type NewReturnInput struct {
	SupplierID        string `json:"supplierId"`
	SupplierName      string `json:"supplierName"`
	WarehouseID       string `json:"warehouseId"`
	ReturnDate        string `json:"returnDate"`
	Reason            string `json:"reason"`
	SupplierReference string `json:"supplierReference"`
	Currency          string `json:"currency"`
}

// ReturnLineInput names what goes back and how much of it.
type ReturnLineInput struct {
	ReturnID      string `json:"returnId"`
	ReceiptLineID string `json:"receiptLineId"`
	QuantityMicro string `json:"quantityMicro"`
	Notes         string `json:"notes"`
}

// LandedCostDTO is one charge added to what a delivery cost.
type LandedCostDTO struct {
	ID          string `json:"id"`
	ReceiptID   string `json:"receiptId"`
	ChargeType  string `json:"chargeType"`
	Description string `json:"description"`
	// Basis is how the charge is spread across the delivery's lines: by value, by quantity, by
	// weight. It is the field that decides where the money lands, so it is shown, not hidden.
	Basis       string `json:"basis"`
	Currency    string `json:"currency"`
	AmountMinor string `json:"amountMinor"`
	Status      string `json:"status"`
}

// NewLandedCostInput is what a screen sends to add a charge.
type NewLandedCostInput struct {
	ReceiptID   string `json:"receiptId"`
	ChargeType  string `json:"chargeType"`
	Description string `json:"description"`
	PartnerID   string `json:"partnerId"`
	Basis       string `json:"basis"`
	Currency    string `json:"currency"`
	AmountMinor string `json:"amountMinor"`
}

// Returns lists a company's supplier returns.
func (p *Purchasing) Returns(status string) envelope.Result[[]SupplierReturnDTO] {
	ctx, app, err := p.guard("Returns")
	if err != nil {
		return envelope.Fail[[]SupplierReturnDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]SupplierReturnDTO](err)
	}

	documents, err := app.Purchasing.Returns(ctx, companyID, domain.ReturnStatus(status))
	if err != nil {
		return envelope.Fail[[]SupplierReturnDTO](err)
	}

	out := make([]SupplierReturnDTO, 0, len(documents))
	for _, document := range documents {
		// The LIST carries no lines. A screen showing forty returns needs their totals, and
		// fetching every line for every one of them is the query that makes a list slow.
		out = append(out, returnDTO(document, nil))
	}
	return envelope.Ok(out)
}

// Return reads one return with its lines.
func (p *Purchasing) Return(returnID string) envelope.Result[SupplierReturnDTO] {
	ctx, app, err := p.guard("Return")
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	document, lines, err := app.Purchasing.Return(ctx, id.ID(returnID))
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	return envelope.Ok(returnDTO(document, lines))
}

// DraftReturn starts a return.
func (p *Purchasing) DraftReturn(in NewReturnInput) envelope.Result[SupplierReturnDTO] {
	ctx, app, err := p.guard("DraftReturn")
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}

	document, err := app.Purchasing.DraftReturn(ctx, purchasing.NewReturnInput{
		CompanyID: companyID, BranchID: branchID,
		WarehouseID: id.ID(in.WarehouseID),
		PartnerID:   id.ID(in.SupplierID), PartnerName: in.SupplierName,
		ReturnDate: in.ReturnDate, Reason: in.Reason,
		SupplierReference: in.SupplierReference, Currency: in.Currency,
	})
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	return envelope.Ok(returnDTO(document, nil))
}

// AddReturnLine sends part of a delivery back.
func (p *Purchasing) AddReturnLine(in ReturnLineInput) envelope.Result[bool] {
	ctx, app, err := p.guard("AddReturnLine")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	quantity, err := parseScaled(in.QuantityMicro)
	if err != nil {
		return envelope.Fail[bool](err)
	}

	if _, err = app.Purchasing.AddReturnLine(ctx, purchasing.ReturnLineInput{
		CompanyID: companyID, ReturnID: id.ID(in.ReturnID),
		ReceiptLineID: id.ID(in.ReceiptLineID), QuantityMicro: quantity,
		Notes: in.Notes,
	}); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// PostReturn sends the goods back and debits the supplier.
func (p *Purchasing) PostReturn(returnID string) envelope.Result[SupplierReturnDTO] {
	ctx, app, err := p.guard("PostReturn")
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	document, err := app.Purchasing.PostReturn(ctx, id.ID(returnID))
	if err != nil {
		return envelope.Fail[SupplierReturnDTO](err)
	}
	return envelope.Ok(returnDTO(document, nil))
}

// CancelReturn abandons a draft return.
func (p *Purchasing) CancelReturn(returnID string) envelope.Result[bool] {
	ctx, app, err := p.guard("CancelReturn")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Purchasing.CancelReturn(ctx, id.ID(returnID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// LandedCosts lists the charges added to one delivery.
func (p *Purchasing) LandedCosts(receiptID string) envelope.Result[[]LandedCostDTO] {
	ctx, app, err := p.guard("LandedCosts")
	if err != nil {
		return envelope.Fail[[]LandedCostDTO](err)
	}
	charges, err := app.Purchasing.LandedCostsFor(ctx, id.ID(receiptID))
	if err != nil {
		return envelope.Fail[[]LandedCostDTO](err)
	}

	out := make([]LandedCostDTO, 0, len(charges))
	for _, charge := range charges {
		out = append(out, LandedCostDTO{
			ID: string(charge.ID), ReceiptID: string(charge.ReceiptID),
			ChargeType: charge.ChargeType, Description: charge.Description,
			Basis: string(charge.Basis), Currency: charge.CurrencyCode,
			AmountMinor: minor(charge.AmountMinor), Status: string(charge.Status),
		})
	}
	return envelope.Ok(out)
}

// AddLandedCost records a charge against a delivery, without applying it.
func (p *Purchasing) AddLandedCost(in NewLandedCostInput) envelope.Result[LandedCostDTO] {
	ctx, app, err := p.guard("AddLandedCost")
	if err != nil {
		return envelope.Fail[LandedCostDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[LandedCostDTO](err)
	}
	amount, err := parseScaled(in.AmountMinor)
	if err != nil {
		return envelope.Fail[LandedCostDTO](err)
	}

	charge, err := app.Purchasing.AddLandedCost(ctx, purchasing.NewLandedCostInput{
		CompanyID: companyID, ReceiptID: id.ID(in.ReceiptID),
		ChargeType: in.ChargeType, Description: in.Description,
		PartnerID: id.ID(in.PartnerID), Basis: domain.Basis(in.Basis),
		Currency: in.Currency, AmountMinor: amount,
	})
	if err != nil {
		return envelope.Fail[LandedCostDTO](err)
	}
	return envelope.Ok(LandedCostDTO{
		ID: string(charge.ID), ReceiptID: string(charge.ReceiptID),
		ChargeType: charge.ChargeType, Description: charge.Description,
		Basis: string(charge.Basis), Currency: charge.CurrencyCode,
		AmountMinor: minor(charge.AmountMinor), Status: string(charge.Status),
	})
}

// ApplyLandedCost spreads a charge across the delivery's lines and raises the stock value.
//
// SEPARATE from adding it, because they are different decisions: recording that freight was
// charged is bookkeeping, and deciding it belongs in the cost of these goods is a judgement
// somebody makes. 6.6 made them two service calls for that reason, and the binding keeps it.
func (p *Purchasing) ApplyLandedCost(chargeID string) envelope.Result[bool] {
	ctx, app, err := p.guard("ApplyLandedCost")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if _, err = app.Purchasing.ApplyLandedCost(ctx, id.ID(chargeID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

func returnDTO(
	document domain.SupplierReturn, lines []domain.ReturnLine,
) SupplierReturnDTO {
	out := SupplierReturnDTO{
		ID: string(document.ID), Number: document.Number,
		Status: string(document.Status), SupplierID: string(document.PartnerID),
		SupplierName: document.PartnerName, ReturnDate: document.ReturnDate,
		Reason: document.Reason, Currency: document.CurrencyCode,
		NetMinor: minor(document.NetMinor), TaxMinor: minor(document.TaxMinor),
		TotalMinor: minor(document.TotalMinor), CostMinor: minor(document.CostMinor),
		Lines: make([]ReturnLineDTO, 0, len(lines)),
	}
	for _, line := range lines {
		out.Lines = append(out.Lines, ReturnLineDTO{
			ID: string(line.ID), LineNumber: line.LineNumber,
			ReceiptLineID: string(line.ReceiptLineID),
			ProductName:   line.ProductName, VariantSKU: line.VariantSKU,
			UomCode:        line.UomCode,
			QuantityMicro:  minor(line.QuantityMicro),
			UnitPriceMicro: minor(line.UnitPriceMicro),
			UnitCostMicro:  minor(line.UnitCostMicro),
			NetMinor:       minor(line.NetMinor), TotalMinor: minor(line.TotalMinor),
		})
	}
	return out
}
