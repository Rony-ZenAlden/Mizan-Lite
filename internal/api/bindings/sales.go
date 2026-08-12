package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// SalesDocumentDTO is one document in a list.
//
// Every amount crosses as a STRING of minor units (§17), like every other money figure in this
// application. A till's totals are the last place to start trusting float64.
type SalesDocumentDTO struct {
	ID     string `json:"id"`
	Type   string `json:"documentType"`
	Status string `json:"status"`
	Number string `json:"number"`

	PartnerName string `json:"partnerName"`
	Date        string `json:"date"`
	Currency    string `json:"currency"`

	NetMinor      string `json:"netMinor"`
	TaxMinor      string `json:"taxMinor"`
	DiscountMinor string `json:"discountMinor"`
	TotalMinor    string `json:"totalMinor"`

	// OutstandingMinor is what is still owed: the total less what posted payments settled.
	// Derived on read, never stored — a maintained column drifts from the allocations that
	// justify it.
	OutstandingMinor string `json:"outstandingMinor"`

	IsHeld    bool   `json:"isHeld"`
	HoldLabel string `json:"holdLabel"`
}

// SalesLineDTO is one line, with its snapshot.
//
// `productName` and `uomCode` are what things were CALLED when the line was added, not what they
// are called now (§9.3). A screen showing today's name on a two-year-old invoice would be
// reprinting something the customer never received.
type SalesLineDTO struct {
	ID          string `json:"id"`
	LineNumber  int    `json:"lineNumber"`
	VariantID   string `json:"variantId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	UomCode     string `json:"uomCode"`

	QuantityMicro  string `json:"quantityMicro"`
	UnitPriceMinor string `json:"unitPriceMinor"`
	DiscountMinor  string `json:"discountMinor"`
	TaxAmountMinor string `json:"taxAmountMinor"`
	NetMinor       string `json:"netMinor"`
	TotalMinor     string `json:"totalMinor"`

	// PriceListCode is WHY this price. §2.6 requires resolution to record which list answered,
	// and a salesperson who cannot explain a price will override it by hand.
	PriceListCode string `json:"priceListCode"`
}

// SalesDetailDTO is a document with its lines.
type SalesDetailDTO struct {
	Document SalesDocumentDTO `json:"document"`
	Lines    []SalesLineDTO   `json:"lines"`
	// Editable tells the screen whether to show a form or a record. A posted document is
	// immutable, and offering an edit control that always refuses teaches people the software
	// is broken.
	Editable bool `json:"editable"`
}

// ShiftDTO is a till's trading session.
type ShiftDTO struct {
	ID       string `json:"id"`
	Terminal string `json:"terminal"`
	Status   string `json:"status"`

	OpenedAt          string `json:"openedAt"`
	OpeningFloatMinor string `json:"openingFloatMinor"`

	ClosedAt string `json:"closedAt"`
	// The three reconciliation figures. Present only on a closed shift — half a reconciliation
	// is a number somebody will read as complete.
	ExpectedMinor   *string `json:"expectedMinor,omitempty"`
	CountedMinor    *string `json:"countedMinor,omitempty"`
	DifferenceMinor *string `json:"differenceMinor,omitempty"`
}

// Sales is the selling surface.
type Sales struct{ graph }

// salesPolicies declares what each method requires.
//
// Drafting and POSTING are separate grants: in many shops an assistant prepares an invoice and
// somebody else commits it. Posting is the irreversible act — it issues stock, moves the books,
// and consumes a number.
func salesPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Documents":    policy.Requires(sales.PermSaleView),
		"Document":     policy.Requires(sales.PermSaleView),
		"Draft":        policy.Requires(sales.PermSaleDraft),
		"AddLine":      policy.Requires(sales.PermSaleDraft),
		"RemoveLine":   policy.Requires(sales.PermSaleDraft),
		"Hold":         policy.Requires(sales.PermSaleDraft),
		"Resume":       policy.Requires(sales.PermSaleDraft),
		"HeldSales":    policy.Requires(sales.PermSaleDraft),
		"Post":         policy.Requires(sales.PermSalePost),
		"Cancel":       policy.Requires(sales.PermSaleCancel),
		"TakePayment":  policy.Requires(sales.PermSalePost),
		"OpenShift":    policy.Requires(sales.PermShiftOpen),
		"CloseShift":   policy.Requires(sales.PermShiftClose),
		"CurrentShift": policy.Requires(sales.PermShiftOpen),
	}
}

// Documents lists a company's sales documents, newest first.
func (s *Sales) Documents(documentType, status string) envelope.Result[[]SalesDocumentDTO] {
	ctx, app, err := s.guard("Documents")
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}

	documents, err := app.Sales.Documents(ctx, companyID,
		domain.Type(documentType), domain.Status(status))
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}

	out := make([]SalesDocumentDTO, 0, len(documents))
	for _, document := range documents {
		row := salesDocumentRow(document)
		// Only a posted document can owe anything: a draft has not been agreed and a cancelled
		// one never will be.
		if document.Status == domain.Posted {
			outstanding, oweErr := app.Sales.Outstanding(ctx, document.ID)
			if oweErr != nil {
				return envelope.Fail[[]SalesDocumentDTO](oweErr)
			}
			row.OutstandingMinor = minor(outstanding)
		}
		out = append(out, row)
	}
	return envelope.Ok(out)
}

// Document reads one document with its lines.
func (s *Sales) Document(documentID string) envelope.Result[SalesDetailDTO] {
	ctx, app, err := s.guard("Document")
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	document, lines, err := app.Sales.Document(ctx, id.ID(documentID))
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}

	row := salesDocumentRow(document)
	if document.Status == domain.Posted {
		outstanding, oweErr := app.Sales.Outstanding(ctx, document.ID)
		if oweErr != nil {
			return envelope.Fail[SalesDetailDTO](oweErr)
		}
		row.OutstandingMinor = minor(outstanding)
	}

	out := SalesDetailDTO{
		Document: row,
		Lines:    make([]SalesLineDTO, 0, len(lines)),
		Editable: document.Status == domain.Draft,
	}
	for _, line := range lines {
		out.Lines = append(out.Lines, SalesLineDTO{
			ID: string(line.ID), LineNumber: line.LineNumber,
			VariantID:   string(line.VariantID),
			ProductName: line.ProductName, SKU: line.VariantSKU, UomCode: line.UomCode,
			QuantityMicro:  minor(line.QuantityMicro),
			UnitPriceMinor: minor(line.UnitPriceMinor),
			DiscountMinor:  minor(line.DiscountMinor),
			TaxAmountMinor: minor(line.TaxAmountMinor),
			NetMinor:       minor(line.NetMinor),
			TotalMinor:     minor(line.TotalMinor),
			PriceListCode:  line.PriceListCode,
		})
	}
	return envelope.Ok(out)
}

// DraftInput opens a sale.
type DraftInput struct {
	WarehouseID string `json:"warehouseId"`
	PartnerID   string `json:"partnerId"`
	PartnerName string `json:"partnerName"`
	Date        string `json:"date"`
	Currency    string `json:"currency"`
}

// Draft opens a sale.
func (s *Sales) Draft(in DraftInput) envelope.Result[SalesDocumentDTO] {
	ctx, app, err := s.guard("Draft")
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}

	document, err := app.Sales.Draft(ctx, sales.NewDocumentInput{
		CompanyID: companyID, BranchID: branchID, WarehouseID: id.ID(in.WarehouseID),
		Type: domain.Invoice, Date: in.Date, Currency: in.Currency,
		PartnerID: id.ID(in.PartnerID), PartnerName: in.PartnerName,
	})
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	return envelope.Ok(salesDocumentRow(document))
}

// AddLineToSaleInput puts an item on a sale.
//
// There is no price field, deliberately. The caller says what and how many; the price is resolved
// at posting from the price lists. A till operator who can type a price is a discount nobody
// approved.
type AddLineToSaleInput struct {
	DocumentID    string `json:"documentId"`
	VariantID     string `json:"variantId"`
	UomID         string `json:"uomId"`
	QuantityMicro string `json:"quantityMicro"`
}

// AddLine puts an item on a sale.
func (s *Sales) AddLine(in AddLineToSaleInput) envelope.Result[SalesDetailDTO] {
	ctx, app, err := s.guard("AddLine")
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	quantity, err := parseScaled(in.QuantityMicro)
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}

	if _, err = app.Sales.AddLine(ctx, sales.AddLineInput{
		CompanyID: companyID, DocumentID: id.ID(in.DocumentID),
		VariantID: id.ID(in.VariantID), UomID: id.ID(in.UomID), QuantityMicro: quantity,
	}); err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	// The whole document comes back, so a till redraws from one answer rather than stitching a
	// line onto state it is holding — which is how a screen and a database come to disagree.
	return s.Document(in.DocumentID)
}

// RemoveLine takes an item off a sale.
func (s *Sales) RemoveLine(documentID, lineID string) envelope.Result[SalesDetailDTO] {
	ctx, app, err := s.guard("RemoveLine")
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	if err = app.Sales.RemoveLine(ctx, id.ID(documentID), id.ID(lineID)); err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	return s.Document(documentID)
}

// Post commits a sale.
func (s *Sales) Post(documentID string) envelope.Result[SalesDocumentDTO] {
	ctx, app, err := s.guard("Post")
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	document, err := app.Sales.Post(ctx, id.ID(documentID))
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	return envelope.Ok(salesDocumentRow(document))
}

// Cancel abandons a draft.
func (s *Sales) Cancel(documentID string) envelope.Result[bool] {
	ctx, app, err := s.guard("Cancel")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Sales.Cancel(ctx, id.ID(documentID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// Hold parks a sale so the next customer can be served.
func (s *Sales) Hold(documentID, label string) envelope.Result[bool] {
	ctx, app, err := s.guard("Hold")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Sales.Hold(ctx, id.ID(documentID), label); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// Resume takes a held sale back off the shelf.
func (s *Sales) Resume(documentID string) envelope.Result[SalesDetailDTO] {
	ctx, app, err := s.guard("Resume")
	if err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	if err = app.Sales.Resume(ctx, id.ID(documentID)); err != nil {
		return envelope.Fail[SalesDetailDTO](err)
	}
	return s.Document(documentID)
}

// HeldSales lists what is parked, so a till can offer them back.
func (s *Sales) HeldSales() envelope.Result[[]SalesDocumentDTO] {
	ctx, app, err := s.guard("HeldSales")
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}
	documents, err := app.Sales.Documents(ctx, companyID, domain.Invoice, domain.Draft)
	if err != nil {
		return envelope.Fail[[]SalesDocumentDTO](err)
	}

	out := make([]SalesDocumentDTO, 0, 4)
	for _, document := range documents {
		if document.IsHeld {
			out = append(out, salesDocumentRow(document))
		}
	}
	return envelope.Ok(out)
}

// TakePaymentInput records money received.
type TakePaymentInput struct {
	DocumentID  string `json:"documentId"`
	ShiftID     string `json:"shiftId"`
	Method      string `json:"method"`
	AmountMinor string `json:"amountMinor"`
	Reference   string `json:"reference"`
	Date        string `json:"date"`
	Currency    string `json:"currency"`
}

// TakePayment records money received against a sale.
func (s *Sales) TakePayment(in TakePaymentInput) envelope.Result[SalesDocumentDTO] {
	ctx, app, err := s.guard("TakePayment")
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	amount, err := parseScaled(in.AmountMinor)
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}

	settle := []sales.SettleInput{}
	if in.DocumentID != "" {
		settle = append(settle, sales.SettleInput{
			DocumentID: id.ID(in.DocumentID), AmountMinor: amount,
		})
	}

	if _, err = app.Sales.TakePayment(ctx, sales.NewPaymentInput{
		CompanyID: companyID, BranchID: branchID, Date: in.Date,
		Method: domain.Method(in.Method), Currency: in.Currency, AmountMinor: amount,
		Reference: in.Reference, ShiftID: id.ID(in.ShiftID), Settle: settle,
	}); err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}

	if in.DocumentID == "" {
		return envelope.Ok(SalesDocumentDTO{})
	}
	detail, _, err := app.Sales.Document(ctx, id.ID(in.DocumentID))
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	row := salesDocumentRow(detail)
	outstanding, err := app.Sales.Outstanding(ctx, id.ID(in.DocumentID))
	if err != nil {
		return envelope.Fail[SalesDocumentDTO](err)
	}
	row.OutstandingMinor = minor(outstanding)
	return envelope.Ok(row)
}

// OpenShift starts a till.
func (s *Sales) OpenShift(terminal, floatMinor string) envelope.Result[ShiftDTO] {
	ctx, app, err := s.guard("OpenShift")
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	opening, err := parseScaled(floatMinor)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}

	shift, err := app.Sales.OpenShift(ctx, sales.OpenShiftInput{
		CompanyID: companyID, BranchID: branchID, Terminal: terminal, FloatMinor: opening,
	})
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	return envelope.Ok(shiftRow(shift))
}

// CloseShift reconciles a till.
func (s *Sales) CloseShift(shiftID, countedMinor, notes string) envelope.Result[ShiftDTO] {
	ctx, app, err := s.guard("CloseShift")
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	counted, err := parseScaled(countedMinor)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}

	shift, err := app.Sales.CloseShift(ctx, companyID, id.ID(shiftID), counted, notes)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	return envelope.Ok(shiftRow(shift))
}

// CurrentShift reports the shift a till is trading under.
func (s *Sales) CurrentShift(terminal string) envelope.Result[ShiftDTO] {
	ctx, app, err := s.guard("CurrentShift")
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	shift, err := app.Sales.CurrentShift(ctx, branchID, terminal)
	if err != nil {
		return envelope.Fail[ShiftDTO](err)
	}
	return envelope.Ok(shiftRow(shift))
}

func salesDocumentRow(d domain.Document) SalesDocumentDTO {
	return SalesDocumentDTO{
		ID: string(d.ID), Type: string(d.Type), Status: string(d.Status), Number: d.Number,
		PartnerName: d.PartnerName, Date: d.Date, Currency: d.CurrencyCode,
		NetMinor: minor(d.NetMinor), TaxMinor: minor(d.TaxMinor),
		DiscountMinor: minor(d.DiscountMinor), TotalMinor: minor(d.TotalMinor),
		IsHeld: d.IsHeld, HoldLabel: d.HoldLabel,
	}
}

func shiftRow(s domain.Shift) ShiftDTO {
	row := ShiftDTO{
		ID: string(s.ID), Terminal: s.Terminal, Status: string(s.Status),
		OpenedAt: s.OpenedAt, OpeningFloatMinor: minor(s.OpeningFloatMinor),
		ClosedAt: s.ClosedAt,
	}
	// Present only on a closed shift. An open one has no expectation and no count, and sending
	// zeros would let a screen show a reconciliation that has not happened.
	if s.Status == domain.ShiftClosed {
		expected, counted, difference :=
			minor(s.ExpectedMinor), minor(s.CountedMinor), minor(s.DifferenceMinor)
		row.ExpectedMinor = &expected
		row.CountedMinor = &counted
		row.DifferenceMinor = &difference
	}
	return row
}

// currentBranch reads the branch the caller is acting in.
func currentBranch(ctx context.Context, app *bootstrap.App) (id.ID, error) {
	actor, ok := appctx.ActorFrom(ctx)
	if ok && !actor.BranchID.IsZero() {
		return actor.BranchID, nil
	}
	return app.Org.DefaultBranchID(ctx)
}
