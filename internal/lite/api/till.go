package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// Till is the checkout: scan, quote, pay (L4 §10.1).
//
// The till does no arithmetic. Every figure — a line in pounds and dollars, the rounding, the change — is computed by Go
// and crosses as a decimal string in its currency's or unit's decimals (DESIGN D9, L4 §4). The screen sends the cart as
// typed and shows what comes back; checkout re-prices it and refuses if anything moved since (the token).
type Till struct{ core *core }

// CartLineInput is one cart line as the cashier has it. DiscountPercent is "" for none.
type CartLineInput struct {
	ProductID       string `json:"productId"`
	Quantity        string `json:"quantity"`
	DiscountPercent string `json:"discountPercent"`
}

// CartInput is a cart as typed. Settlement is the currency the total is charged in ("" for the local currency);
// SaleDiscount is an amount in it. TenderCurrency and Tendered are the cash handed over ("" for exact money);
// ChangeCurrency is "" for the default — pounds, or dollars when a dollar total is paid in dollars (Q-L4.2, Q-L4.3).
type CartInput struct {
	Lines          []CartLineInput `json:"lines"`
	Settlement     string          `json:"settlement"`
	SaleDiscount   string          `json:"saleDiscount"`
	TenderCurrency string          `json:"tenderCurrency"`
	Tendered       string          `json:"tendered"`
	ChangeCurrency string          `json:"changeCurrency"`
	// Payment is "cash" (or "") or "credit"; on credit, Tendered is what was paid now and CustomerID who is charged (L5 §5).
	Payment    string `json:"payment"`
	CustomerID string `json:"customerId"`
}

// CheckoutInput is a cart and the token of the quote the cashier saw.
type CheckoutInput struct {
	Cart  CartInput `json:"cart"`
	Token string    `json:"token"`
}

// CartLineDTO is one priced line.
type CartLineDTO struct {
	ProductID     string `json:"productId"`
	NameAR        string `json:"nameAr"`
	NameEN        string `json:"nameEn"`
	UnitCode      string `json:"unitCode"`
	Quantity      string `json:"quantity"`
	PriceCurrency string `json:"priceCurrency"`
	UnitPrice     string `json:"unitPrice"`
	GrossLocal    string `json:"grossLocal"`
	GrossUSD      string `json:"grossUsd"`
	// DiscountPercent is "" when the line has no discount.
	DiscountPercent string `json:"discountPercent"`
	DiscountLocal   string `json:"discountLocal"`
	DiscountUSD     string `json:"discountUsd"`
	NetLocal        string `json:"netLocal"`
	NetUSD          string `json:"netUsd"`
	// OnHand is the shelf before this sale; Warnings holds "beyond_stock" and "no_cost" (Q4, Q-L4.8).
	OnHand   string   `json:"onHand"`
	Warnings []string `json:"warnings"`
}

// CartQuoteDTO is a priced cart: every figure the till shows, and the token checkout compares.
type CartQuoteDTO struct {
	Lines         []CartLineDTO `json:"lines"`
	LocalCurrency string        `json:"localCurrency"`
	LinesLocal    string        `json:"linesLocal"`
	LinesUSD      string        `json:"linesUsd"`
	DiscountLocal string        `json:"discountLocal"`
	DiscountUSD   string        `json:"discountUsd"`
	Settlement    string        `json:"settlement"`
	// Total is what the customer pays, in Settlement, after the cash rounding; Rounding is what the rounding added
	// (negative when it took away) and CashNote the note it rounded to — "" when the settlement is dollars.
	Total    string `json:"total"`
	Rounding string `json:"rounding"`
	CashNote string `json:"cashNote"`
	// OtherCurrency and TotalOther are the total in the other currency, for reference, not rounded.
	OtherCurrency  string `json:"otherCurrency"`
	TotalOther     string `json:"totalOther"`
	TenderCurrency string `json:"tenderCurrency"`
	Tendered       string `json:"tendered"`
	TenderGiven    bool   `json:"tenderGiven"`
	ChangeCurrency string `json:"changeCurrency"`
	Change         string `json:"change"`
	// Discounted is true when checkout will ask for the owner (Q-L4.6).
	Discounted     bool   `json:"discounted"`
	Rate           string `json:"rate"`
	RateRecordedAt string `json:"rateRecordedAt"`
	RateStale      bool   `json:"rateStale"`
	Token          string `json:"token"`
	// A credit sale (L5 §5.1): the customer, what is added to their debt in Settlement, and their balance in it now and
	// after — all "" for a cash sale. NeedsCustomer is true until a customer is chosen.
	Payment       string `json:"payment"`
	CustomerID    string `json:"customerId"`
	CustomerName  string `json:"customerName"`
	NeedsCustomer bool   `json:"needsCustomer"`
	Debt          string `json:"debt"`
	BalanceBefore string `json:"balanceBefore"`
	BalanceAfter  string `json:"balanceAfter"`
}

// ScanDTO is what a barcode found. Found is false for an unknown code — not an error: the cashier types it again.
type ScanDTO struct {
	Found     bool   `json:"found"`
	ProductID string `json:"productId"`
	NameAR    string `json:"nameAr"`
	NameEN    string `json:"nameEn"`
	UnitCode  string `json:"unitCode"`
	// UnitDecimals above zero means a weighed or measured product: the till asks for the quantity first.
	UnitDecimals int    `json:"unitDecimals"`
	Active       bool   `json:"active"`
	OnHand       string `json:"onHand"`
}

// CashNoteDTO is the smallest note totals in the local currency round to.
type CashNoteDTO struct {
	Currency string `json:"currency"`
	Note     string `json:"note"`
}

func (in CartInput) toDomain() (salesdomain.CartInput, error) {
	out := salesdomain.CartInput{
		Settlement: in.Settlement, SaleDiscount: in.SaleDiscount,
		TenderCurrency: in.TenderCurrency, Tendered: in.Tendered, ChangeCurrency: in.ChangeCurrency,
		Payment: salesdomain.Payment(in.Payment), Lines: make([]salesdomain.LineInput, 0, len(in.Lines)),
	}
	if in.CustomerID != "" {
		customerID, err := id.Parse(in.CustomerID)
		if err != nil {
			return salesdomain.CartInput{}, errs.NotFound(salesdomain.CodeCustomerNotFound, "no such customer").
				WithField(salesdomain.FieldCustomer, salesdomain.CodeCustomerNotFound, "not found")
		}
		out.CustomerID = customerID
	}
	for _, l := range in.Lines {
		productID, err := id.Parse(l.ProductID)
		if err != nil {
			return salesdomain.CartInput{}, errs.NotFound(salesdomain.CodeUnknownProduct, "no such product").WithParam("productId", l.ProductID)
		}
		out.Lines = append(out.Lines, salesdomain.LineInput{ProductID: productID, Quantity: l.Quantity, DiscountPercent: l.DiscountPercent})
	}
	return out, nil
}

// money formats a minor amount in a currency's decimals.
func (v tillView) money(minor int64, code string) string {
	d := v.ref.Currencies[code].Decimals
	return numinput.FormatFixed(minor, d, d)
}

// quantity formats a ×10⁶ quantity in its unit's decimals.
func (v tillView) quantity(micro int64, unitCode string) string {
	return numinput.FormatFixed(micro, 6, v.ref.Units[unitCode].InputDecimals)
}

// tillView is the reference data a sale is formatted against.
type tillView struct{ ref catalogdomain.Reference }

func newTillView(ctx context.Context, app *bootstrap.App) (tillView, error) {
	ref, err := app.Catalog.Reference(ctx)
	return tillView{ref: ref}, err
}

func percentText(micro int64) string {
	if micro == 0 {
		return ""
	}
	return numinput.FormatFixed(micro, 4, 0)
}

func (v tillView) quote(q salesdomain.Quote, local string) CartQuoteDTO {
	usd := salesdomain.USD
	dto := CartQuoteDTO{
		Lines: make([]CartLineDTO, 0, len(q.Lines)), LocalCurrency: local,
		LinesLocal: v.money(q.LinesLocalMinor, local), LinesUSD: v.money(q.LinesUSDMinor, usd),
		DiscountLocal: v.money(q.DiscountLocalMinor, local), DiscountUSD: v.money(q.DiscountUSDMinor, usd),
		Settlement: q.Settlement.Code, Total: v.money(q.TotalMinor, q.Settlement.Code), Rounding: v.money(q.RoundingMinor, q.Settlement.Code),
		OtherCurrency: q.Other.Code, TotalOther: v.money(q.TotalOtherMinor, q.Other.Code),
		TenderCurrency: q.Tender.Code, Tendered: v.money(q.TenderedMinor, q.Tender.Code), TenderGiven: q.TenderGiven,
		ChangeCurrency: q.Change.Code, Change: v.money(q.ChangeMinor, q.Change.Code),
		Discounted: q.Discounted, Rate: fxdomain.FormatRate(q.Rate.Nano), RateRecordedAt: clock.Format(q.Rate.RecordedAt),
		RateStale: q.Rate.Stale, Token: q.Token,
	}
	if q.Settlement.Code != usd {
		dto.CashNote = v.money(q.CashNoteMinor, local)
	}
	dto.Payment = string(q.Payment)
	if q.Payment == salesdomain.PaymentCredit {
		dto.CustomerID, dto.CustomerName, dto.NeedsCustomer = q.Customer.ID.String(), q.Customer.Name, q.NeedsCustomer
		dto.Debt = v.money(q.DebtMinor, q.Settlement.Code)
		dto.BalanceBefore, dto.BalanceAfter = v.money(q.BalanceBeforeMinor, q.Settlement.Code), v.money(q.BalanceAfterMinor, q.Settlement.Code)
	}
	for _, l := range q.Lines {
		line := v.line(l.Line, local)
		warnings := make([]string, 0, len(l.Warnings))
		for _, w := range l.Warnings {
			warnings = append(warnings, string(w))
		}
		out := CartLineDTO{
			ProductID: line.ProductID, NameAR: line.NameAR, NameEN: line.NameEN, UnitCode: line.UnitCode, Quantity: line.Quantity,
			PriceCurrency: line.PriceCurrency, UnitPrice: line.UnitPrice, GrossLocal: line.GrossLocal, GrossUSD: line.GrossUSD,
			DiscountPercent: line.DiscountPercent, DiscountLocal: line.DiscountLocal, DiscountUSD: line.DiscountUSD,
			NetLocal: line.NetLocal, NetUSD: line.NetUSD, OnHand: v.quantity(l.OnHandMicro, l.UnitCode), Warnings: warnings,
		}
		dto.Lines = append(dto.Lines, out)
	}
	return dto
}

// Scan finds a product by barcode, digits in any script.
func (t *Till) Scan(code string) envelope.Result[ScanDTO] {
	return call(t.core, "Till.Scan", func(ctx context.Context, app *bootstrap.App) (ScanDTO, error) {
		found, ok, err := app.Sales.Scan(ctx, code)
		if err != nil || !ok {
			return ScanDTO{}, err
		}
		v, err := newTillView(ctx, app)
		p := found.Product
		return ScanDTO{
			Found: true, ProductID: p.ID.String(), NameAR: p.NameAR, NameEN: p.NameEN, UnitCode: p.UnitCode,
			UnitDecimals: p.UnitDecimals, Active: p.Active, OnHand: v.quantity(found.Stocked.OnHandMicro, p.UnitCode),
		}, err
	})
}

// Quote prices a cart and writes nothing.
func (t *Till) Quote(in CartInput) envelope.Result[CartQuoteDTO] {
	return call(t.core, "Till.Quote", func(ctx context.Context, app *bootstrap.App) (CartQuoteDTO, error) {
		cart, err := in.toDomain()
		if err != nil {
			return CartQuoteDTO{}, err
		}
		q, err := app.Sales.Quote(ctx, cart)
		if err != nil {
			return CartQuoteDTO{}, err
		}
		v, err := newTillView(ctx, app)
		if err != nil {
			return CartQuoteDTO{}, err
		}
		local := q.Settlement.Code
		if local == salesdomain.USD {
			local = q.Other.Code
		}
		return v.quote(q, local), nil
	})
}

// Checkout records the sale and answers with its receipt. A stale token returns lite.sales.quote_stale; a discount
// outside owner mode returns lite.owner.required.
func (t *Till) Checkout(in CheckoutInput) envelope.Result[SaleDTO] {
	return call(t.core, "Till.Checkout", func(ctx context.Context, app *bootstrap.App) (SaleDTO, error) {
		cart, err := in.Cart.toDomain()
		if err != nil {
			return SaleDTO{}, err
		}
		sale, err := app.Sales.Checkout(ctx, sales.CheckoutInput{Cart: cart, Token: in.Token})
		if err != nil {
			return SaleDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.sale(sale), err
	})
}

// CashNote is the smallest local note.
func (t *Till) CashNote() envelope.Result[CashNoteDTO] {
	return call(t.core, "Till.CashNote", func(ctx context.Context, app *bootstrap.App) (CashNoteDTO, error) {
		note, err := app.Sales.CashNote(ctx)
		if err != nil {
			return CashNoteDTO{}, err
		}
		return cashNote(ctx, app, note)
	})
}

// SetCashNote changes the smallest local note. Owner only.
func (t *Till) SetCashNote(note string) envelope.Result[CashNoteDTO] {
	return call(t.core, "Till.SetCashNote", func(ctx context.Context, app *bootstrap.App) (CashNoteDTO, error) {
		set, err := app.Sales.SetCashNote(ctx, note)
		if err != nil {
			return CashNoteDTO{}, err
		}
		return cashNote(ctx, app, set)
	})
}

func cashNote(ctx context.Context, app *bootstrap.App, note int64) (CashNoteDTO, error) {
	current, err := app.FX.Current(ctx)
	if err != nil {
		return CashNoteDTO{}, err
	}
	v, err := newTillView(ctx, app)
	return CashNoteDTO{Currency: current.Local, Note: v.money(note, current.Local)}, err
}
