// Package domain holds Mizan Lite's reports as pure functions of facts other modules recorded: profit, net profit,
// takings, stock value and its reconciliation, the profit on the shelf, and the cash drawer (L6). No I/O, no clock, and
// nothing here writes: every figure is computed from what a sale, a ledger row, a debt entry or a cash book entry stored.
//
// The facts are this package's own types. The reports module imports no other module (lite-reports-isolated); the
// composition root copies each module's records into these.
package domain

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys.
const (
	CodeBadDate  = "lite.reports.bad_date"
	CodeBadMonth = "lite.reports.bad_month"
	CodeBadRange = "lite.reports.bad_range"
)

// USD is the code every rate is quoted against.
const USD = "USD"

// Currency is a currency's code and decimals.
type Currency struct {
	Code     string
	Decimals int
}

// Pair is the shop's two currencies.
type Pair struct {
	Local Currency
	USD   Currency
}

// Rate is a recorded exchange rate: local per US dollar at 10⁻⁹, its place, and the business date it was recorded on.
type Rate struct {
	ID           id.ID
	Seq          int64
	Nano         int64
	BusinessDate string
}

// Line is a sale line as it was stored.
type Line struct {
	ProductID          id.ID
	NameAR             string
	NameEN             string
	UnitCode           string
	QuantityMicro      int64
	NetLocalMinor      int64
	NetUSDMinor        int64
	DiscountLocalMinor int64
	DiscountUSDMinor   int64
	CostKnown          bool
	CostUSDMinor       int64
	CostLocalMinor     int64
	// OpenPrice is a line of an open-priced product: no cost by design, and kept apart from the lines whose cost
	// nobody has entered yet (2026-09-23).
	OpenPrice bool
}

// Sale is a sale as it was stored, with what its void hands back.
type Sale struct {
	ID                 id.ID
	ReceiptNo          int64
	BusinessDate       string
	Voided             bool
	VoidBusinessDate   string
	Credit             bool
	SettlementCurrency string
	RateNano           int64
	DiscountLocalMinor int64
	DiscountUSDMinor   int64
	RoundingMinor      int64
	TotalMinor         int64
	TenderedCurrency   string
	TenderedMinor      int64
	ChangeCurrency     string
	ChangeMinor        int64
	// CreditMinor is what a credit sale added to the debt, in the settlement currency.
	CreditMinor int64
	// VoidReturnCurrency and VoidReturnMinor are what the till's rule says a void hands back (L6 §7.2) — the sales
	// module's figure, copied, never recomputed here.
	VoidReturnCurrency string
	VoidReturnMinor    int64
	Lines              []Line
}

// Stock ledger kinds and reasons, as the stock module stores them.
const (
	MoveOpening         = "opening"
	MoveReceipt         = "receipt"
	MoveReceiptReversal = "receipt_reversal"
	MoveCount           = "count"
	MoveAdjustment      = "adjustment"
	MovePackageOut      = "package_out"
	MoveContentIn       = "content_in"
	MoveCostCorrection  = "cost_correction"
	MoveSale            = "sale"
	MoveSaleVoid        = "sale_void"

	ReasonDamaged = "damaged"
	ReasonExpired = "expired"
	ReasonSpoiled = "spoiled"
	ReasonOwnUse  = "own_use"
	ReasonGift    = "gift"
	ReasonOther   = "other"
)

// Movement is a stock ledger row: what moved, at what unit cost, and the level before and after it.
type Movement struct {
	// ID and Note name a movement on the loss report (0.10.0).
	ID                 id.ID
	Note               string
	ProductID          id.ID
	Seq                int64
	BusinessDate       string
	Kind               string
	Reason             string
	QuantityMicro      int64
	UnitCostMicro      int64
	OnHandBeforeMicro  int64
	AvgCostBeforeMicro int64
	OnHandAfterMicro   int64
	AvgCostAfterMicro  int64
}

// Debt book kinds, as the customers module stores them.
const (
	DebtOpening  = "opening"
	DebtCharge   = "charge"
	DebtPayment  = "payment"
	DebtWriteOff = "write_off"
	DebtRefund   = "refund"
	DebtReversal = "reversal"
)

// DebtCash is the cash a payment took or a refund paid out.
type DebtCash struct {
	TenderedCurrency string
	TenderedMinor    int64
	ChangeCurrency   string
	ChangeMinor      int64
}

// DebtEntry is a debt book entry; a reversal carries the kind and cash of the entry it reverses.
type DebtEntry struct {
	ID            id.ID
	CustomerName  string
	Currency      string
	BusinessDate  string
	Kind          string
	AmountMinor   int64
	Cash          DebtCash
	ReversesKind  string
	ReversesCash  DebtCash
	ReversesMinor int64
}

// Cash book kinds, as the cash book stores them.
const (
	CashExpense    = "expense"
	CashWithdrawal = "withdrawal"
	CashDeposit    = "deposit"
	CashCount      = "count"
	CashReversal   = "reversal"
)

// CashEntry is a cash book entry; a reversal carries what it reverses.
type CashEntry struct {
	ID            id.ID
	Seq           int64
	BusinessDate  string
	OccurredAt    time.Time
	Kind          string
	Currency      string
	AmountMinor   int64
	ExpectedMinor int64
	Category      string
	FromDrawer    bool
	// Recurrence is "once" for the day's small change, "monthly" for rent and the bills (2026-09-20).
	Recurrence string
	RateNano   int64
	Note       string
	// Reversed is true when a later entry undoes this one.
	Reversed bool
	Reverses *CashEntry
}

// SupplierCash is one move the payables book made in the drawer (0.10.0): money paid to a supplier out of it, or a
// supplier's refund into it. A reversal is the kind it undoes below nought, on the reversal's own day.
type SupplierCash struct {
	BusinessDate  string
	Currency      string
	PaidOutMinor  int64
	RefundInMinor int64
}

// Product is a product as the reports name and price it.
type Product struct {
	ID            id.ID
	NameAR        string
	NameEN        string
	UnitCode      string
	UnitDecimals  int
	PriceCurrency string
	PriceMicro    int64
	Active        bool
}
