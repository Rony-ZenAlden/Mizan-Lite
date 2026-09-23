// Package domain holds Mizan Lite's sales as values: a cart priced in both currencies, its totals, discounts, cash
// rounding, tender and change, the quote token, the sale a checkout records, and its void (L4). No I/O, no clock.
package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys.
const (
	CodeEmptyCart        = "lite.sales.empty_cart"
	CodeTooManyLines     = "lite.sales.too_many_lines"
	CodeQuantityRequired = "lite.sales.quantity_required"
	CodeQuantityDecimals = "lite.sales.quantity_decimals"
	// CodeOpenPriceRequired: an open-priced line needs a typed price above nothing; CodePriceNotOpen refuses a typed
	// price on a product whose price is the catalogue's; CodePriceDecimals a price finer than the currency (2026-09-23).
	CodeOpenPriceRequired  = "lite.sales.open_price_required"
	CodePriceNotOpen       = "lite.sales.price_not_open"
	CodePriceDecimals      = "lite.sales.price_decimals"
	CodeQuantityTooLarge   = "lite.sales.quantity_too_large"
	CodeInactiveProduct    = "lite.sales.inactive_product"
	CodeUnknownProduct     = "lite.sales.unknown_product"
	CodeUnknownCurrency    = "lite.sales.unknown_currency"
	CodeNoRate             = "lite.sales.no_rate"
	CodeQuoteStale         = "lite.sales.quote_stale"
	CodeTenderShort        = "lite.sales.tender_short"
	CodeTenderDecimals     = "lite.sales.tender_decimals"
	CodeChangeCurrency     = "lite.sales.change_currency"
	CodeDiscountInvalid    = "lite.sales.discount_invalid"
	CodeDiscountTooLarge   = "lite.sales.discount_too_large"
	CodeUnknownPayment     = "lite.sales.unknown_payment"
	CodeCustomerRequired   = "lite.sales.customer_required"
	CodeCustomerNotFound   = "lite.sales.customer_not_found"
	CodeCustomerInactive   = "lite.sales.customer_inactive"
	CodeCreditPaidInFull   = "lite.sales.credit_paid_in_full"
	CodeVoidReasonRequired = "lite.sales.void_reason_required"
	CodeVoidReasonTooLong  = "lite.sales.void_reason_too_long"
	CodeAlreadyVoided      = "lite.sales.already_voided"
	CodeSaleNotFound       = "lite.sales.not_found"
	CodeAmountTooLarge     = "lite.sales.amount_too_large"
)

// Fields named in validation errors: a line's field is "lines.<n>.<field>" so the till can mark the line.
const (
	FieldQuantity        = "quantity"
	FieldDiscountPercent = "discountPercent"
	FieldPrice           = "price"
	FieldSaleDiscount    = "saleDiscount"
	FieldTendered        = "tendered"
	FieldChangeCurrency  = "changeCurrency"
	FieldReason          = "reason"
	FieldCustomer        = "customerId"
)

const (
	// MaxLines bounds a sale (L4 §4).
	MaxLines = 200
	// MaxReasonRunes bounds a void reason, as the schema does.
	MaxReasonRunes = 200
	// USD is the currency every rate is quoted against.
	USD = "USD"
)

// Payment is how a sale is paid.
type Payment string

// The payments. A credit sale's customer, and what it added to their debt, live on its charge in the debt book (A-L4.2).
const (
	PaymentCash   Payment = "cash"
	PaymentCredit Payment = "credit"
)

// Status is a sale's state.
type Status string

// The statuses.
const (
	StatusPosted Status = "posted"
	StatusVoided Status = "voided"
)

// Currency is a currency a figure is in.
type Currency struct {
	Code     string
	Decimals int
}

// Product is what the till needs about a product, supplied by the catalogue through a port.
type Product struct {
	ID            id.ID
	NameAR        string
	NameEN        string
	UnitCode      string
	UnitDecimals  int
	PriceCurrency string
	PriceMicro    int64
	// CostMicro is the cost price the shop typed on the product, in PriceCurrency (L9). Since 2026-09-16 it is what a sale
	// snapshots as its cost when it is set — the owner's decision that the typed cost is the profit basis. Where it is not
	// set the weighted average of the deliveries stands, as it always did.
	CostMicro int64
	HasCost   bool
	// ReorderMicro is the level at or below which the shop wants to be told to buy more, and HasReorder whether one
	// was ever set (2026-09-20). A product with no level is never called low.
	ReorderMicro int64
	HasReorder   bool
	// OpenPrice marks a product sold at a price typed at the till, never counted in stock (2026-09-23).
	OpenPrice bool
	// UnitsPerCartonMicro is how many of the unit make one carton (طرد), or 0 for none (0.10.0).
	UnitsPerCartonMicro int64
	Active              bool
	RowVersion          int64
}

// Stocked is what the till needs about a product's stock: supplied by the stock module through a port.
type Stocked struct {
	OnHandMicro  int64
	CostKnown    bool
	AvgCostMicro int64
}

// Customer is what the till needs about a customer, supplied by the debt book through a port.
type Customer struct {
	ID     id.ID
	Name   string
	Active bool
}

// Credit is a credit sale's charge as the receipt shows it: who, in which currency, what it added, and the balance after
// (L5 §5.5). Read from the debt book, which holds it immutably.
type Credit struct {
	CustomerID        id.ID
	CustomerName      string
	Currency          string
	AmountMinor       int64
	BalanceAfterMinor int64
	Reversed          bool
}

// Charge is a credit sale's charge as the sales verifier checks it.
type Charge struct {
	SaleID      id.ID
	CustomerID  id.ID
	Currency    string
	AmountMinor int64
	Reversed    bool
}

// Rate is the rate in force a sale snapshots.
type Rate struct {
	ID         id.ID
	Nano       int64
	RecordedAt time.Time
	Stale      bool
}

// Sale is a recorded sale — and its receipt (L4 §7.2).
type Sale struct {
	ID                 id.ID
	ReceiptNo          int64
	BusinessDate       string
	SoldAt             time.Time
	Status             Status
	Payment            Payment
	LocalCurrency      string
	RateID             id.ID
	RateNano           int64
	RateRecordedAt     time.Time
	SettlementCurrency string
	LinesLocalMinor    int64
	LinesUSDMinor      int64
	DiscountLocalMinor int64
	DiscountUSDMinor   int64
	CashNoteMinor      int64
	RoundingMinor      int64
	TotalMinor         int64
	TenderedCurrency   string
	TenderedMinor      int64
	ChangeCurrency     string
	ChangeMinor        int64
	CostUSDMinor       int64
	ShopName           string
	VoidedAt           time.Time
	VoidBusinessDate   string
	VoidReason         string
	RowVersion         int64
	// Credit is filled from the debt book for a credit sale; it is not a column of sales.
	Credit Credit
	Lines  []Line
}

// Line is one line of a sale, fully snapshotted.
type Line struct {
	ID                   id.ID
	LineNo               int
	ProductID            id.ID
	NameAR               string
	NameEN               string
	UnitCode             string
	QuantityMicro        int64
	PriceCurrency        string
	UnitPriceMicro       int64
	GrossLocalMinor      int64
	GrossUSDMinor        int64
	DiscountPercentMicro int64
	DiscountLocalMinor   int64
	DiscountUSDMinor     int64
	UnitCostMicro        int64
	CostKnown            bool
	CostUSDMinor         int64
	CostLocalMinor       int64
	// OpenPrice is the line of an open-priced product: sold at a typed price, never taken off a shelf, and with no cost
	// by design rather than one nobody has entered. Kept on the line as the name and unit are (2026-09-23).
	OpenPrice bool
	// UnitsPerCartonMicro is the carton size the product had when it was sold, or 0 for none (0.10.0).
	UnitsPerCartonMicro int64
}

// Cartons is how many cartons (طرد) the line is, in tenths rounded half up — an invoice counts "1.5" cartons, not 1.4999.
// ok is false for a line whose product has no carton size.
func (l Line) Cartons() (tenths int64, ok bool) {
	if l.UnitsPerCartonMicro <= 0 {
		return 0, false
	}
	// quantity ÷ carton × 10, rounded half up: (2 × 10 × quantity + carton) ÷ (2 × carton), exact in integers.
	return (20*l.QuantityMicro + l.UnitsPerCartonMicro) / (2 * l.UnitsPerCartonMicro), true
}

// Cartons is the sale's cartons in tenths, summed over the lines that have a carton size; ok is false when none has.
func (s Sale) Cartons() (tenths int64, ok bool) {
	for _, l := range s.Lines {
		if t, has := l.Cartons(); has {
			tenths += t
			ok = true
		}
	}
	return tenths, ok
}

// NetLocalMinor is the line after its discount, in the local currency.
func (l Line) NetLocalMinor() int64 { return l.GrossLocalMinor - l.DiscountLocalMinor }

// NetUSDMinor is the line after its discount, in dollars.
func (l Line) NetUSDMinor() int64 { return l.GrossUSDMinor - l.DiscountUSDMinor }

// Void marks a posted sale voided, with its reason and the business day of the void (L4 §8.1).
func (s Sale) Void(at time.Time, businessDate, reason string) (Sale, error) {
	if s.Status != StatusPosted {
		return s, errs.Conflict(CodeAlreadyVoided, "the sale is already voided").WithParam("receiptNo", strconv.FormatInt(s.ReceiptNo, 10))
	}
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return s, errs.Validation(CodeVoidReasonRequired, "a reason is required").WithField(FieldReason, CodeVoidReasonRequired, "required")
	}
	if utf8.RuneCountInString(trimmed) > MaxReasonRunes {
		return s, errs.Validation(CodeVoidReasonTooLong, "reason too long").
			WithField(FieldReason, CodeVoidReasonTooLong, "too long").WithParam("max", strconv.Itoa(MaxReasonRunes))
	}
	s.Status, s.VoidedAt, s.VoidBusinessDate, s.VoidReason = StatusVoided, at, businessDate, trimmed
	return s, nil
}

// VoidReturn is the cash a void hands back (L6 §7.2, Q-L6.5): what the receipt says was paid, in the currency it was
// charged in — a cash sale's total; a credit sale's paid now, as the total less the debt it added. It needs the sale's
// Credit filled for a credit sale.
func (s Sale) VoidReturn() (string, int64) {
	if s.Payment == PaymentCredit {
		return s.SettlementCurrency, s.TotalMinor - s.Credit.AmountMinor
	}
	return s.SettlementCurrency, s.TotalMinor
}

// ErrNotFound reports a sale that does not exist.
func ErrNotFound() error { return errs.NotFound(CodeSaleNotFound, "no such sale") }
