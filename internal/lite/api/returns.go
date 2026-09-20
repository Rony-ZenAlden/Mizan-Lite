package api

import (
	"context"
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// Partial sales returns (the owner's request, 2026-09-20): the F8 wizard's three calls — find the sale, price what is
// ticked, record it.

// ReturnableLineDTO is one line of a sale as the return wizard shows it: what was sold, what has already come back,
// and therefore what is left to return.
type ReturnableLineDTO struct {
	SaleLineID string `json:"saleLineId"`
	LineNo     int    `json:"lineNo"`
	ProductID  string `json:"productId"`
	NameAR     string `json:"nameAr"`
	NameEN     string `json:"nameEn"`
	UnitCode   string `json:"unitCode"`
	// Sold, Returned and Left are quantities in the line's own unit.
	Sold     string `json:"sold"`
	Returned string `json:"returned"`
	Left     string `json:"left"`
	// NetLocal and NetUSD are what the line was charged after its own discount — what a whole-line return is worth.
	NetLocal string `json:"netLocal"`
	NetUSD   string `json:"netUsd"`
	// Returnable is false once nothing is left, so the screen can show the line without offering it.
	Returnable bool `json:"returnable"`
}

// ReturnableDTO is a sale the counter has found, ready to return part of.
type ReturnableDTO struct {
	SaleID       string `json:"saleId"`
	ReceiptNo    string `json:"receiptNo"`
	BusinessDate string `json:"businessDate"`
	SoldAt       string `json:"soldAt"`
	Payment      string `json:"payment"`
	Settlement   string `json:"settlementCurrency"`
	Total        string `json:"total"`
	Voided       bool   `json:"voided"`
	// CustomerID and CustomerName are set for a credit sale, which may be returned against the debt.
	CustomerID   string              `json:"customerId"`
	CustomerName string              `json:"customerName"`
	Lines        []ReturnableLineDTO `json:"lines"`
	// Returns are what has already come back against this sale.
	Returns []ReturnDTO `json:"returns"`
}

// ReturnLineDTO is one priced line of a return.
type ReturnLineDTO struct {
	SaleLineID  string `json:"saleLineId"`
	LineNo      int    `json:"lineNo"`
	ProductID   string `json:"productId"`
	NameAR      string `json:"nameAr"`
	NameEN      string `json:"nameEn"`
	UnitCode    string `json:"unitCode"`
	Quantity    string `json:"quantity"`
	RefundLocal string `json:"refundLocal"`
	RefundUSD   string `json:"refundUsd"`
	Restocked   bool   `json:"restocked"`
}

// ReturnDTO is a return, priced or recorded.
type ReturnDTO struct {
	ID            string `json:"id"`
	ReturnNo      string `json:"returnNo"`
	SaleID        string `json:"saleId"`
	SaleReceiptNo string `json:"saleReceiptNo"`
	BusinessDate  string `json:"businessDate"`
	ReturnedAt    string `json:"returnedAt"`
	Settlement    string `json:"settlement"`
	// Currency is the currency the refund is paid in, and Refund the amount in it.
	Currency string `json:"currency"`
	Refund   string `json:"refund"`
	// CostKnown is false when any returned line's cost was never recorded, so a screen can say so rather than imply
	// the shop knows what the return cost it.
	CostKnown bool            `json:"costKnown"`
	Reason    string          `json:"reason"`
	Lines     []ReturnLineDTO `json:"lines"`
}

// ReturnLineInput is one line a person ticked.
type ReturnLineInput struct {
	SaleLineID string `json:"saleLineId"`
	Quantity   string `json:"quantity"`
	// Restock is whether the goods go back on the shelf. A damaged tin is refunded and not resold.
	Restock bool `json:"restock"`
}

// ReturnInput is a whole return as the wizard asks for it.
type ReturnInput struct {
	SaleID     string            `json:"saleId"`
	Lines      []ReturnLineInput `json:"lines"`
	Settlement string            `json:"settlement"`
	Reason     string            `json:"reason"`
}

// Returnable finds a sale by the number printed on its receipt and says what is left to return.
//
// By receipt number, because that is what the person at the counter has: the customer handed them the paper.
func (s *Sales) Returnable(receiptNo string) envelope.Result[ReturnableDTO] {
	return call(s.core, "Sales.Returnable", func(ctx context.Context, app *bootstrap.App) (ReturnableDTO, error) {
		n, err := strconv.ParseInt(receiptNo, 10, 64)
		if err != nil || n < 1 {
			return ReturnableDTO{}, salesdomain.ErrNotFound()
		}
		found, err := app.Sales.ByReceiptNo(ctx, n)
		if err != nil {
			return ReturnableDTO{}, err
		}
		// ReturnableOf fills a credit sale's customer, which is what decides whether the debt is offered.
		sale, already, err := app.Sales.ReturnableOf(ctx, found.ID)
		if err != nil {
			return ReturnableDTO{}, err
		}
		past, err := app.Sales.ReturnsOfSale(ctx, sale.ID)
		if err != nil {
			return ReturnableDTO{}, err
		}
		v, err := newTillView(ctx, app)
		if err != nil {
			return ReturnableDTO{}, err
		}
		return v.returnable(sale, already, past), nil
	})
}

// QuoteReturn prices what has been ticked without recording anything, so the counter can say what the customer is
// owed before the goods change hands.
func (s *Sales) QuoteReturn(in ReturnInput) envelope.Result[ReturnDTO] {
	return call(s.core, "Sales.QuoteReturn", func(ctx context.Context, app *bootstrap.App) (ReturnDTO, error) {
		parsed, err := returnInput(in)
		if err != nil {
			return ReturnDTO{}, err
		}
		priced, err := app.Sales.QuoteReturn(ctx, parsed)
		if err != nil {
			return ReturnDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.saleReturn(priced), err
	})
}

// Return records a return: the stock goes back, the money goes back, and the owner's history says it happened.
func (s *Sales) Return(in ReturnInput) envelope.Result[ReturnDTO] {
	return call(s.core, "Sales.Return", func(ctx context.Context, app *bootstrap.App) (ReturnDTO, error) {
		parsed, err := returnInput(in)
		if err != nil {
			return ReturnDTO{}, err
		}
		recorded, err := app.Sales.Return(ctx, parsed)
		if err != nil {
			return ReturnDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.saleReturn(recorded), err
	})
}

func returnInput(in ReturnInput) (sales.ReturnInput, error) {
	saleID, err := id.Parse(in.SaleID)
	if err != nil {
		return sales.ReturnInput{}, salesdomain.ErrNotFound()
	}
	draft := salesdomain.ReturnDraft{
		Settlement: salesdomain.Settlement(in.Settlement),
		Reason:     in.Reason,
	}
	if draft.Settlement == "" {
		draft.Settlement = salesdomain.SettleCash
	}
	for _, l := range in.Lines {
		lineID, err := id.Parse(l.SaleLineID)
		if err != nil {
			return sales.ReturnInput{}, salesdomain.ErrNotFound()
		}
		draft.Lines = append(draft.Lines, salesdomain.ReturnLineDraft{
			SaleLineID: lineID, Quantity: l.Quantity, Restock: l.Restock,
		})
	}
	return sales.ReturnInput{SaleID: saleID, Draft: draft}, nil
}

func (v tillView) returnable(sale salesdomain.Sale, already salesdomain.Returned, past []salesdomain.Return) ReturnableDTO {
	out := ReturnableDTO{
		SaleID: sale.ID.String(), ReceiptNo: strconv.FormatInt(sale.ReceiptNo, 10),
		BusinessDate: sale.BusinessDate, SoldAt: clock.Format(sale.SoldAt), Payment: string(sale.Payment),
		Settlement: sale.SettlementCurrency, Total: v.money(sale.TotalMinor, sale.SettlementCurrency),
		Voided:  sale.Status == salesdomain.StatusVoided,
		Lines:   make([]ReturnableLineDTO, 0, len(sale.Lines)),
		Returns: make([]ReturnDTO, 0, len(past)),
	}
	if !sale.Credit.CustomerID.IsZero() {
		out.CustomerID, out.CustomerName = sale.Credit.CustomerID.String(), sale.Credit.CustomerName
	}
	for _, l := range sale.Lines {
		left := l.QuantityMicro - already[l.ID]
		out.Lines = append(out.Lines, ReturnableLineDTO{
			SaleLineID: l.ID.String(), LineNo: l.LineNo, ProductID: l.ProductID.String(),
			NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode,
			Sold:     v.quantity(l.QuantityMicro, l.UnitCode),
			Returned: v.quantity(already[l.ID], l.UnitCode),
			Left:     v.quantity(left, l.UnitCode),
			NetLocal: v.money(l.NetLocalMinor(), v.shop.Local),
			NetUSD:   v.money(l.NetUSDMinor(), salesdomain.USD),
			// A voided sale has nothing left to return: the whole thing was handed back already.
			Returnable: left > 0 && sale.Status != salesdomain.StatusVoided,
		})
	}
	for _, r := range past {
		out.Returns = append(out.Returns, v.saleReturn(r))
	}
	return out
}

func (v tillView) saleReturn(r salesdomain.Return) ReturnDTO {
	out := ReturnDTO{
		ID: r.ID.String(), ReturnNo: strconv.FormatInt(r.ReturnNo, 10), SaleID: r.SaleID.String(),
		SaleReceiptNo: strconv.FormatInt(r.SaleReceiptNo, 10), BusinessDate: r.BusinessDate,
		ReturnedAt: clock.Format(r.ReturnedAt), Settlement: string(r.Settlement),
		Currency: r.SettlementCurrency, Refund: v.money(r.RefundMinor, r.SettlementCurrency),
		CostKnown: r.CostKnown, Reason: r.Reason,
		Lines: make([]ReturnLineDTO, 0, len(r.Lines)),
	}
	if r.ID.IsZero() {
		out.ID = "" // a quote has no identity: nothing has been recorded
	}
	for _, l := range r.Lines {
		out.Lines = append(out.Lines, ReturnLineDTO{
			SaleLineID: l.SaleLineID.String(), LineNo: l.LineNo, ProductID: l.ProductID.String(),
			NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode,
			Quantity:    v.quantity(l.QuantityMicro, l.UnitCode),
			RefundLocal: v.money(l.RefundLocalMinor, v.shop.Local),
			RefundUSD:   v.money(l.RefundUSDMinor, salesdomain.USD),
			Restocked:   l.Restocked,
		})
	}
	return out
}
