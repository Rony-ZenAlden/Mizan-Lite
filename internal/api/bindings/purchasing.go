package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
)

// PurchaseOrderDTO is one order in a list.
//
// Money crosses as STRINGS of minor units and quantities as strings of micro units (§17), like
// everywhere else. A purchase order for three thousand metres of cable is exactly the document
// where float64 stops being able to hold the answer.
type PurchaseOrderDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Number string `json:"number"`

	SupplierName string `json:"supplierName"`
	OrderDate    string `json:"orderDate"`
	ExpectedDate string `json:"expectedDate"`
	Currency     string `json:"currency"`

	NetMinor   string `json:"netMinor"`
	TaxMinor   string `json:"taxMinor"`
	TotalMinor string `json:"totalMinor"`

	SupplierReference string `json:"supplierReference"`

	// FullyReceived reports whether everything ordered has arrived. Computed from the lines
	// rather than stored, so it cannot disagree with them.
	FullyReceived bool `json:"fullyReceived"`
}

// PurchaseOrderLineDTO is one thing ordered, with its snapshot (§9.3).
type PurchaseOrderLineDTO struct {
	ID          string `json:"id"`
	LineNumber  int    `json:"lineNumber"`
	VariantID   string `json:"variantId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	UomCode     string `json:"uomCode"`

	QuantityMicro    string `json:"quantityMicro"`
	ReceivedMicro    string `json:"receivedMicro"`
	OutstandingMicro string `json:"outstandingMicro"`

	UnitPriceMicro string `json:"unitPriceMicro"`
	TaxAmountMinor string `json:"taxAmountMinor"`
	NetMinor       string `json:"netMinor"`
	TotalMinor     string `json:"totalMinor"`

	SupplierCode string `json:"supplierCode"`
}

// PurchaseOrderDetailDTO is an order with its lines.
type PurchaseOrderDetailDTO struct {
	Order PurchaseOrderDTO       `json:"order"`
	Lines []PurchaseOrderLineDTO `json:"lines"`
	// Editable tells the screen whether to show a form or a record. A placed order has been sent
	// and a supplier is picking from it.
	Editable bool `json:"editable"`
	// Receivable tells it whether goods can still arrive against this order.
	Receivable bool `json:"receivable"`
}

// GoodsReceiptDTO is one delivery.
type GoodsReceiptDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Number string `json:"number"`

	OrderID      string `json:"orderId"`
	SupplierName string `json:"supplierName"`
	ReceiptDate  string `json:"receiptDate"`

	DeliveryNoteReference string `json:"deliveryNoteReference"`
	ReceivedByName        string `json:"receivedByName"`

	ValueMinor string `json:"valueMinor"`
	// Billed reports whether a supplier invoice has taken this delivery up. The GRNI report's
	// central column: a delivery that has been on the shelf for weeks with no invoice is either
	// a missing bill or goods nobody charged us for.
	Billed bool `json:"billed"`
}

// PurchaseBillDTO is one supplier invoice.
type PurchaseBillDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Number string `json:"number"`

	SupplierName          string `json:"supplierName"`
	SupplierInvoiceNumber string `json:"supplierInvoiceNumber"`
	BillDate              string `json:"billDate"`
	DueDate               string `json:"dueDate"`
	Currency              string `json:"currency"`

	NetMinor   string `json:"netMinor"`
	TaxMinor   string `json:"taxMinor"`
	TotalMinor string `json:"totalMinor"`
	// VarianceMinor is what the supplier charged over what was ordered. Shown because a
	// variance is ACCEPTED, which makes it invisible unless something surfaces it.
	VarianceMinor string `json:"varianceMinor"`
	// OutstandingMinor is what is still owed. Derived from allocations, never stored.
	OutstandingMinor string `json:"outstandingMinor"`
}

// Purchasing is the buying surface.
type Purchasing struct{ graph }

// purchasingPolicies declares what each method requires.
//
// Ordering, RECEIVING, billing, and PAYING are four separate grants. That is not ceremony: it is
// the segregation of duties the three-way match depends on. One person who can order, take
// delivery, approve the invoice, and pay it can pay a supplier for nothing — and no amount of
// matching detects it, because they control every side.
func purchasingPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Orders":          policy.Requires(purchasing.PermOrderView),
		"Order":           policy.Requires(purchasing.PermOrderView),
		"DraftOrder":      policy.Requires(purchasing.PermOrderDraft),
		"AddOrderLine":    policy.Requires(purchasing.PermOrderDraft),
		"RemoveOrderLine": policy.Requires(purchasing.PermOrderDraft),
		"PlaceOrder":      policy.Requires(purchasing.PermOrderPlace),
		"CancelOrder":     policy.Requires(purchasing.PermOrderCancel),
		"CloseOrder":      policy.Requires(purchasing.PermOrderCancel),

		"Receipts":       policy.Requires(purchasing.PermOrderView),
		"Receipt":        policy.Requires(purchasing.PermOrderView),
		"DraftReceipt":   policy.Requires(purchasing.PermReceiptRecord),
		"ReceiveLine":    policy.Requires(purchasing.PermReceiptRecord),
		"ConfirmReceipt": policy.Requires(purchasing.PermReceiptRecord),

		"Bills":       policy.Requires(purchasing.PermBillView),
		"Bill":        policy.Requires(purchasing.PermBillView),
		"DraftBill":   policy.Requires(purchasing.PermBillDraft),
		"AddBillLine": policy.Requires(purchasing.PermBillDraft),
		"PostBill":    policy.Requires(purchasing.PermBillPost),

		"Pay":      policy.Requires(purchasing.PermPaymentPost),
		"Payments": policy.Requires(purchasing.PermPaymentView),
	}
}

// Orders lists a company's purchase orders.
func (p *Purchasing) Orders(status string) envelope.Result[[]PurchaseOrderDTO] {
	ctx, app, err := p.guard("Orders")
	if err != nil {
		return envelope.Fail[[]PurchaseOrderDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]PurchaseOrderDTO](err)
	}

	orders, err := app.Purchasing.Orders(ctx, companyID, domain.Status(status))
	if err != nil {
		return envelope.Fail[[]PurchaseOrderDTO](err)
	}

	out := make([]PurchaseOrderDTO, 0, len(orders))
	for _, order := range orders {
		out = append(out, purchaseOrderRow(order, nil))
	}
	return envelope.Ok(out)
}

// Order reads one order with its lines.
func (p *Purchasing) Order(orderID string) envelope.Result[PurchaseOrderDetailDTO] {
	ctx, app, err := p.guard("Order")
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	order, lines, err := app.Purchasing.Order(ctx, id.ID(orderID))
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}

	out := PurchaseOrderDetailDTO{
		Order:      purchaseOrderRow(order, lines),
		Lines:      make([]PurchaseOrderLineDTO, 0, len(lines)),
		Editable:   order.Status == domain.Draft,
		Receivable: order.Status == domain.Placed,
	}
	for _, line := range lines {
		out.Lines = append(out.Lines, PurchaseOrderLineDTO{
			ID: string(line.ID), LineNumber: line.LineNumber,
			VariantID:   string(line.VariantID),
			ProductName: line.ProductName, SKU: line.VariantSKU, UomCode: line.UomCode,
			QuantityMicro:    minor(line.QuantityMicro),
			ReceivedMicro:    minor(line.ReceivedMicro),
			OutstandingMicro: minor(line.OutstandingMicro()),
			UnitPriceMicro:   minor(line.UnitPriceMicro),
			TaxAmountMinor:   minor(line.TaxAmountMinor),
			NetMinor:         minor(line.NetMinor),
			TotalMinor:       minor(line.TotalMinor),
			SupplierCode:     line.SupplierCode,
		})
	}
	return envelope.Ok(out)
}

// NewPurchaseOrderInput opens an order.
type NewPurchaseOrderInput struct {
	WarehouseID       string `json:"warehouseId"`
	SupplierID        string `json:"supplierId"`
	SupplierName      string `json:"supplierName"`
	OrderDate         string `json:"orderDate"`
	ExpectedDate      string `json:"expectedDate"`
	Currency          string `json:"currency"`
	SupplierReference string `json:"supplierReference"`
	Notes             string `json:"notes"`
}

// DraftOrder opens a purchase order.
func (p *Purchasing) DraftOrder(in NewPurchaseOrderInput) envelope.Result[PurchaseOrderDTO] {
	ctx, app, err := p.guard("DraftOrder")
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}

	order, err := app.Purchasing.Draft(ctx, purchasing.NewOrderInput{
		CompanyID: companyID, BranchID: branchID, WarehouseID: id.ID(in.WarehouseID),
		PartnerID: id.ID(in.SupplierID), PartnerName: in.SupplierName,
		OrderDate: in.OrderDate, ExpectedDate: in.ExpectedDate, Currency: in.Currency,
		SupplierReference: in.SupplierReference, Notes: in.Notes,
	})
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}
	return envelope.Ok(purchaseOrderRow(order, nil))
}

// AddPurchaseLineInput puts something on an order.
//
// No price field: the buyer says what and how many, and the purchase price lists answer the rest
// at placement. A buyer who can type any price is a purchase nobody agreed, and the three-way
// match has nothing to compare the bill against.
type AddPurchaseLineInput struct {
	OrderID       string `json:"orderId"`
	VariantID     string `json:"variantId"`
	UomID         string `json:"uomId"`
	QuantityMicro string `json:"quantityMicro"`
	SupplierCode  string `json:"supplierCode"`
}

// AddOrderLine puts something on a draft order.
func (p *Purchasing) AddOrderLine(
	in AddPurchaseLineInput,
) envelope.Result[PurchaseOrderDetailDTO] {
	ctx, app, err := p.guard("AddOrderLine")
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	quantity, err := parseScaled(in.QuantityMicro)
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}

	if _, err = app.Purchasing.AddLine(ctx, purchasing.AddLineInput{
		CompanyID: companyID, OrderID: id.ID(in.OrderID), VariantID: id.ID(in.VariantID),
		UomID: id.ID(in.UomID), QuantityMicro: quantity, SupplierCode: in.SupplierCode,
	}); err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	// The whole order comes back, so a screen redraws from one answer rather than stitching a
	// line onto state it is holding — which is how a screen and a database come to disagree.
	return p.Order(in.OrderID)
}

// RemoveOrderLine takes something off a draft order.
func (p *Purchasing) RemoveOrderLine(
	orderID, lineID string,
) envelope.Result[PurchaseOrderDetailDTO] {
	ctx, app, err := p.guard("RemoveOrderLine")
	if err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	if err = app.Purchasing.RemoveLine(ctx, id.ID(orderID), id.ID(lineID)); err != nil {
		return envelope.Fail[PurchaseOrderDetailDTO](err)
	}
	return p.Order(orderID)
}

// PlaceOrder prices an order, numbers it, and sends it.
func (p *Purchasing) PlaceOrder(orderID string) envelope.Result[PurchaseOrderDTO] {
	ctx, app, err := p.guard("PlaceOrder")
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}
	order, err := app.Purchasing.Place(ctx, id.ID(orderID))
	if err != nil {
		return envelope.Fail[PurchaseOrderDTO](err)
	}
	return envelope.Ok(purchaseOrderRow(order, nil))
}

// CancelOrder abandons a draft.
func (p *Purchasing) CancelOrder(orderID string) envelope.Result[bool] {
	ctx, app, err := p.guard("CancelOrder")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Purchasing.Cancel(ctx, id.ID(orderID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// CloseOrder stops an order receiving more goods.
func (p *Purchasing) CloseOrder(orderID string) envelope.Result[bool] {
	ctx, app, err := p.guard("CloseOrder")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Purchasing.Close(ctx, id.ID(orderID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// Receipts lists deliveries, optionally for one order.
func (p *Purchasing) Receipts(orderID, status string) envelope.Result[[]GoodsReceiptDTO] {
	ctx, app, err := p.guard("Receipts")
	if err != nil {
		return envelope.Fail[[]GoodsReceiptDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]GoodsReceiptDTO](err)
	}

	receipts, err := app.Purchasing.Receipts(
		ctx, companyID, id.ID(orderID), domain.ReceiptStatus(status))
	if err != nil {
		return envelope.Fail[[]GoodsReceiptDTO](err)
	}

	out := make([]GoodsReceiptDTO, 0, len(receipts))
	for _, receipt := range receipts {
		out = append(out, goodsReceiptRow(receipt))
	}
	return envelope.Ok(out)
}

// Receipt reads one delivery.
func (p *Purchasing) Receipt(receiptID string) envelope.Result[GoodsReceiptDTO] {
	ctx, app, err := p.guard("Receipt")
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	receipt, _, err := app.Purchasing.Receipt(ctx, id.ID(receiptID))
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	return envelope.Ok(goodsReceiptRow(receipt))
}

// NewReceiptInput opens a delivery.
type NewReceiptInput struct {
	WarehouseID           string `json:"warehouseId"`
	OrderID               string `json:"orderId"`
	SupplierID            string `json:"supplierId"`
	SupplierName          string `json:"supplierName"`
	ReceiptDate           string `json:"receiptDate"`
	Currency              string `json:"currency"`
	DeliveryNoteReference string `json:"deliveryNoteReference"`
	ReceivedByName        string `json:"receivedByName"`
}

// DraftReceipt opens a delivery.
func (p *Purchasing) DraftReceipt(in NewReceiptInput) envelope.Result[GoodsReceiptDTO] {
	ctx, app, err := p.guard("DraftReceipt")
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}

	receipt, err := app.Purchasing.DraftReceipt(ctx, purchasing.NewReceiptInput{
		CompanyID: companyID, BranchID: branchID, WarehouseID: id.ID(in.WarehouseID),
		OrderID: id.ID(in.OrderID), PartnerID: id.ID(in.SupplierID),
		PartnerName: in.SupplierName, ReceiptDate: in.ReceiptDate, Currency: in.Currency,
		DeliveryNoteReference: in.DeliveryNoteReference, ReceivedByName: in.ReceivedByName,
	})
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	return envelope.Ok(goodsReceiptRow(receipt))
}

// ReceiveLineInput records one thing that arrived.
type ReceiveLineInput struct {
	ReceiptID     string `json:"receiptId"`
	OrderLineID   string `json:"orderLineId"`
	VariantID     string `json:"variantId"`
	QuantityMicro string `json:"quantityMicro"`
}

// ReceiveLine records one thing that arrived.
func (p *Purchasing) ReceiveLine(in ReceiveLineInput) envelope.Result[bool] {
	ctx, app, err := p.guard("ReceiveLine")
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

	if _, err = app.Purchasing.ReceiveLine(ctx, purchasing.ReceiveLineInput{
		CompanyID: companyID, ReceiptID: id.ID(in.ReceiptID),
		OrderLineID: id.ID(in.OrderLineID), VariantID: id.ID(in.VariantID),
		QuantityMicro: quantity,
	}); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// ConfirmReceipt takes the goods into stock and accrues what will be owed.
func (p *Purchasing) ConfirmReceipt(receiptID string) envelope.Result[GoodsReceiptDTO] {
	ctx, app, err := p.guard("ConfirmReceipt")
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	receipt, err := app.Purchasing.ConfirmReceipt(ctx, id.ID(receiptID))
	if err != nil {
		return envelope.Fail[GoodsReceiptDTO](err)
	}
	return envelope.Ok(goodsReceiptRow(receipt))
}

// Bills lists a company's supplier invoices.
func (p *Purchasing) Bills(status string) envelope.Result[[]PurchaseBillDTO] {
	ctx, app, err := p.guard("Bills")
	if err != nil {
		return envelope.Fail[[]PurchaseBillDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]PurchaseBillDTO](err)
	}

	bills, err := app.Purchasing.Bills(ctx, companyID, domain.BillStatus(status))
	if err != nil {
		return envelope.Fail[[]PurchaseBillDTO](err)
	}

	out := make([]PurchaseBillDTO, 0, len(bills))
	for _, bill := range bills {
		row := purchaseBillRow(bill)
		if bill.Status == domain.BillPosted {
			outstanding, oweErr := app.Purchasing.OutstandingOnBill(ctx, bill.ID)
			if oweErr != nil {
				return envelope.Fail[[]PurchaseBillDTO](oweErr)
			}
			row.OutstandingMinor = minor(outstanding)
		}
		out = append(out, row)
	}
	return envelope.Ok(out)
}

// Bill reads one supplier invoice.
func (p *Purchasing) Bill(billID string) envelope.Result[PurchaseBillDTO] {
	ctx, app, err := p.guard("Bill")
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	bill, _, err := app.Purchasing.Bill(ctx, id.ID(billID))
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	row := purchaseBillRow(bill)
	if bill.Status == domain.BillPosted {
		outstanding, oweErr := app.Purchasing.OutstandingOnBill(ctx, bill.ID)
		if oweErr != nil {
			return envelope.Fail[PurchaseBillDTO](oweErr)
		}
		row.OutstandingMinor = minor(outstanding)
	}
	return envelope.Ok(row)
}

// NewBillInput opens a supplier invoice.
type NewBillInput struct {
	SupplierID            string `json:"supplierId"`
	SupplierName          string `json:"supplierName"`
	BillDate              string `json:"billDate"`
	DueDate               string `json:"dueDate"`
	SupplierInvoiceNumber string `json:"supplierInvoiceNumber"`
	Currency              string `json:"currency"`
}

// DraftBill opens a supplier invoice.
func (p *Purchasing) DraftBill(in NewBillInput) envelope.Result[PurchaseBillDTO] {
	ctx, app, err := p.guard("DraftBill")
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}

	bill, err := app.Purchasing.DraftBill(ctx, purchasing.NewBillInput{
		CompanyID: companyID, BranchID: branchID,
		PartnerID: id.ID(in.SupplierID), PartnerName: in.SupplierName,
		BillDate: in.BillDate, DueDate: in.DueDate,
		SupplierInvoiceNumber: in.SupplierInvoiceNumber, Currency: in.Currency,
	})
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	return envelope.Ok(purchaseBillRow(bill))
}

// AddBillLine takes up one delivery line, in full.
func (p *Purchasing) AddBillLine(
	billID, receiptLineID, unitPriceMicro string,
) envelope.Result[PurchaseBillDTO] {
	ctx, app, err := p.guard("AddBillLine")
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	price, err := parseScaled(unitPriceMicro)
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}

	if _, err = app.Purchasing.AddBillLine(ctx, purchasing.BillLineInput{
		CompanyID: companyID, BillID: id.ID(billID),
		ReceiptLineID: id.ID(receiptLineID), UnitPriceMicro: price,
	}); err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	return p.Bill(billID)
}

// PostBill commits a supplier invoice.
func (p *Purchasing) PostBill(billID string) envelope.Result[PurchaseBillDTO] {
	ctx, app, err := p.guard("PostBill")
	if err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	if _, err = app.Purchasing.PostBill(ctx, id.ID(billID)); err != nil {
		return envelope.Fail[PurchaseBillDTO](err)
	}
	return p.Bill(billID)
}

// PayInput pays a supplier.
type PayInput struct {
	SupplierID   string `json:"supplierId"`
	SupplierName string `json:"supplierName"`
	PaymentDate  string `json:"paymentDate"`
	Method       string `json:"method"`
	Reference    string `json:"reference"`
	Currency     string `json:"currency"`
	AmountMinor  string `json:"amountMinor"`
	// BillID is optional: a prepayment against future orders allocates to nothing.
	BillID string `json:"billId"`
}

// Pay records money going out to a supplier.
func (p *Purchasing) Pay(in PayInput) envelope.Result[bool] {
	ctx, app, err := p.guard("Pay")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	amount, err := parseScaled(in.AmountMinor)
	if err != nil {
		return envelope.Fail[bool](err)
	}

	settle := []domain.Allocation{}
	if in.BillID != "" {
		settle = append(settle, domain.Allocation{
			BillID: id.ID(in.BillID), AmountMinor: amount,
		})
	}

	if _, err = app.Purchasing.Pay(ctx, purchasing.NewPaymentInput{
		CompanyID: companyID, BranchID: branchID,
		PartnerID: id.ID(in.SupplierID), PartnerName: in.SupplierName,
		PaymentDate: in.PaymentDate, Method: domain.Method(in.Method),
		Reference: in.Reference, Currency: in.Currency, AmountMinor: amount,
		Settle: settle,
	}); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// Payments lists a company's supplier payments.
func (p *Purchasing) Payments() envelope.Result[[]SupplierPaymentDTO] {
	ctx, app, err := p.guard("Payments")
	if err != nil {
		return envelope.Fail[[]SupplierPaymentDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]SupplierPaymentDTO](err)
	}

	payments, err := app.Purchasing.Payments(ctx, companyID, "")
	if err != nil {
		return envelope.Fail[[]SupplierPaymentDTO](err)
	}

	out := make([]SupplierPaymentDTO, 0, len(payments))
	for _, payment := range payments {
		out = append(out, SupplierPaymentDTO{
			ID: string(payment.ID), Number: payment.Number,
			SupplierName: payment.PartnerName, PaymentDate: payment.PaymentDate,
			Method: string(payment.Method), Reference: payment.Reference,
			AmountMinor: minor(payment.AmountMinor), Status: string(payment.Status),
		})
	}
	return envelope.Ok(out)
}

// SupplierPaymentDTO is one payment out.
type SupplierPaymentDTO struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	SupplierName string `json:"supplierName"`
	PaymentDate  string `json:"paymentDate"`
	Method       string `json:"method"`
	Reference    string `json:"reference"`
	AmountMinor  string `json:"amountMinor"`
	Status       string `json:"status"`
}

func purchaseOrderRow(o domain.Order, lines []domain.Line) PurchaseOrderDTO {
	row := PurchaseOrderDTO{
		ID: string(o.ID), Status: string(o.Status), Number: o.Number,
		SupplierName: o.PartnerName, OrderDate: o.OrderDate, ExpectedDate: o.ExpectedDate,
		Currency: o.CurrencyCode,
		NetMinor: minor(o.NetMinor), TaxMinor: minor(o.TaxMinor),
		TotalMinor: minor(o.TotalMinor), SupplierReference: o.SupplierReference,
	}
	if len(lines) > 0 {
		row.FullyReceived = true
		for _, line := range lines {
			if !line.IsFullyReceived() {
				row.FullyReceived = false
				break
			}
		}
	}
	return row
}

func goodsReceiptRow(g domain.Receipt) GoodsReceiptDTO {
	return GoodsReceiptDTO{
		ID: string(g.ID), Status: string(g.Status), Number: g.Number,
		OrderID: string(g.OrderID), SupplierName: g.PartnerName,
		ReceiptDate: g.ReceiptDate, DeliveryNoteReference: g.DeliveryNoteReference,
		ReceivedByName: g.ReceivedByName, ValueMinor: minor(g.ValueMinor),
		Billed: g.BilledAt != "",
	}
}

func purchaseBillRow(b domain.Bill) PurchaseBillDTO {
	return PurchaseBillDTO{
		ID: string(b.ID), Status: string(b.Status), Number: b.Number,
		SupplierName: b.PartnerName, SupplierInvoiceNumber: b.SupplierInvoiceNumber,
		BillDate: b.BillDate, DueDate: b.DueDate, Currency: b.CurrencyCode,
		NetMinor: minor(b.NetMinor), TaxMinor: minor(b.TaxMinor),
		TotalMinor: minor(b.TotalMinor), VarianceMinor: minor(b.VarianceMinor),
	}
}
