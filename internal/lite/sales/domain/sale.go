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
	CodeEmptyCart          = "lite.sales.empty_cart"
	CodeTooManyLines       = "lite.sales.too_many_lines"
	CodeQuantityRequired   = "lite.sales.quantity_required"
	CodeQuantityDecimals   = "lite.sales.quantity_decimals"
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
	CodeCreditNotAvailable = "lite.sales.credit_not_available"
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
	FieldSaleDiscount    = "saleDiscount"
	FieldTendered        = "tendered"
	FieldChangeCurrency  = "changeCurrency"
	FieldReason          = "reason"
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

// The payments. Credit exists in the schema and is refused until customers do (L5, A-L4.2).
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
	Active        bool
	RowVersion    int64
}

// Stocked is what the till needs about a product's stock: supplied by the stock module through a port.
type Stocked struct {
	OnHandMicro  int64
	CostKnown    bool
	AvgCostMicro int64
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
	Lines              []Line
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

// ErrNotFound reports a sale that does not exist.
func ErrNotFound() error { return errs.NotFound(CodeSaleNotFound, "no such sale") }
