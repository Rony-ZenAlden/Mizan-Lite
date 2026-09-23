package domain

import (
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// A partial sales return: the customer brings back two of the three tins (the owner's request, 2026-09-20).
//
// # Why a return is its own document and not a void
//
// A void says the sale never should have happened: it undoes the whole receipt, hands back everything that was paid,
// and is dated to the day it is discovered. A return says the sale happened and part of it came back. The customer
// keeps the rest, the shop keeps that revenue, and the receipt already in the customer's pocket is still true.
//
// So a return is a document of its own, with its own number, its own date and its own lines. Monday's sale is not
// rewritten when Thursday's tin comes back; Thursday's takings are what change.
//
// # What a returned line is worth
//
// Its share of what the line was actually charged — the NET, after that line's discount. A customer who bought three
// tins at 10% off and brings one back is owed a third of the discounted line, not a third of the list price. The share
// is computed in big.Int and the LAST line of a full return takes the remainder, so the parts of a wholly returned
// sale add up to exactly what the sale charged and never a unit more or less.
//
// # What the cost does
//
// Goes back on the shelf at the cost the SALE snapshotted, not today's average (the reasoning of D-L9.1). The day's
// profit is then reduced by the margin on what came back, not by its whole price — which is what actually happened.
// A line whose cost was never known re-credits nothing and says so, rather than guessing zero.

// Stable error codes. They double as i18n keys.
const (
	CodeReturnNoLines        = "lite.sales.return_no_lines"
	CodeReturnTooMuch        = "lite.sales.return_too_much"
	CodeReturnNotThatLine    = "lite.sales.return_not_that_line"
	CodeReturnVoided         = "lite.sales.return_voided"
	CodeReturnReasonRequired = "lite.sales.return_reason_required"
	CodeReturnNotFound       = "lite.sales.return_not_found"
	CodeReturnNotCredit      = "lite.sales.return_not_credit"
)

// Fields a form marks.
const (
	FieldReturnLines    = "lines"
	FieldReturnQuantity = "quantity"
	FieldReturnReason   = "reason"
)

// ActReturn names a return in the owner's history. Money leaves the drawer, so it is a guarded act.
const ActReturn = "sales.return"

// MaxReturnReasonRunes bounds the reason, as the schema does.
const MaxReturnReasonRunes = 200

// Settlement is how the money went back.
type Settlement string

// The settlements.
const (
	// SettleCash hands notes back out of the drawer.
	SettleCash Settlement = "cash"
	// SettleDebt takes the amount off what the customer owes. Only a credit sale can settle this way.
	SettleDebt Settlement = "debt"
)

// ReturnLineDraft is one line a person ticked: which line of the sale, how much of it, and whether it is fit to sell
// again.
type ReturnLineDraft struct {
	SaleLineID id.ID
	// Quantity is what came back, as typed, in the line's own unit.
	Quantity string
	// Restock is whether the goods go back on the shelf. A damaged tin is refunded and not resold.
	Restock bool
}

// ReturnDraft is a whole return before it is priced.
type ReturnDraft struct {
	Lines      []ReturnLineDraft
	Settlement Settlement
	Reason     string
}

// ReturnLine is one priced line of a return.
type ReturnLine struct {
	ID         id.ID
	LineNo     int
	SaleLineID id.ID
	ProductID  id.ID
	// NameAR, NameEN and UnitCode are the sale's snapshots, so the return's paper reads like the receipt it undoes.
	NameAR           string
	NameEN           string
	UnitCode         string
	UnitDecimals     int
	QuantityMicro    int64
	RefundLocalMinor int64
	RefundUSDMinor   int64
	UnitCostMicro    int64
	CostKnown        bool
	CostUSDMinor     int64
	CostLocalMinor   int64
	Restocked        bool
	// SoldMicro and AlreadyReturnedMicro are what the line sold and what earlier returns already took back — shown so
	// a person can see what is left to return.
	SoldMicro            int64
	AlreadyReturnedMicro int64
}

// Return is a priced return, ready to record or to show.
type Return struct {
	ID                 id.ID
	ReturnNo           int64
	SaleID             id.ID
	SaleReceiptNo      int64
	BusinessDate       string
	ReturnedAt         time.Time
	Settlement         Settlement
	SettlementCurrency string
	RefundMinor        int64
	RefundLocalMinor   int64
	RefundUSDMinor     int64
	CostUSDMinor       int64
	CostLocalMinor     int64
	CostKnown          bool
	RateID             id.ID
	RateNano           int64
	CustomerID         id.ID
	// DebtEntryID names the debt-ledger entry a return settled against a customer's balance wrote. Zero for a cash
	// return, which writes none: the drawer derives a cash refund from the return itself, as it does a cash sale.
	DebtEntryID id.ID
	Reason      string
	Lines       []ReturnLine
}

// Returned is how much of each sale line earlier returns already took back, by sale line.
type Returned map[id.ID]int64

// PriceReturn works out what a return is worth, against the sale it came from and what earlier returns already took.
//
// rate is the rate in force NOW, not the sale's: the refund is money moving today, and the drawer it comes out of holds
// today's pounds. The sale's own rate is what decided the original price and is not re-used here.
func PriceReturn(sale Sale, already Returned, draft ReturnDraft, rate Rate, local, usd Currency,
	units map[string]int, at time.Time, businessDate string) (Return, error) {
	if sale.Status == StatusVoided {
		return Return{}, errs.Conflict(CodeReturnVoided, "a voided sale has nothing to return")
	}
	reason, err := returnReason(draft.Reason)
	if err != nil {
		return Return{}, err
	}
	if draft.Settlement == SettleDebt && sale.Payment != PaymentCredit {
		return Return{}, errs.Validation(CodeReturnNotCredit, "only a credit sale can be returned to the debt").
			WithField("settlement", CodeReturnNotCredit, "not a credit sale")
	}

	byID := map[id.ID]Line{}
	for _, l := range sale.Lines {
		byID[l.ID] = l
	}

	out := Return{
		SaleID: sale.ID, SaleReceiptNo: sale.ReceiptNo, BusinessDate: businessDate, ReturnedAt: at,
		Settlement: draft.Settlement, SettlementCurrency: sale.SettlementCurrency,
		RateID: rate.ID, RateNano: rate.Nano, Reason: reason, CostKnown: true,
	}
	for _, d := range draft.Lines {
		line, ok := byID[d.SaleLineID]
		if !ok {
			return Return{}, errs.Validation(CodeReturnNotThatLine, "that line is not on this sale").
				WithField(FieldReturnLines, CodeReturnNotThatLine, "not on this sale")
		}
		decimals, ok := units[line.UnitCode]
		if !ok {
			return Return{}, errs.Validation(CodeUnknownProduct, "the line's unit is not in the catalogue").
				WithParam("value", line.UnitCode)
		}
		quantity, err := parseQuantity(d.Quantity, decimals)
		if err != nil {
			return Return{}, withReturnField(err, FieldReturnQuantity)
		}
		if quantity <= 0 {
			return Return{}, errs.Validation(CodeReturnTooMuch, "nothing to return on that line").
				WithField(FieldReturnQuantity, CodeReturnTooMuch, "more than nothing")
		}
		left := line.QuantityMicro - already[line.ID]
		if quantity > left {
			return Return{}, errs.Validation(CodeReturnTooMuch, "more than was sold").
				WithField(FieldReturnQuantity, CodeReturnTooMuch, "more than was sold").
				WithParam("left", numinput.FormatFixed(left, 6, decimals)).WithParam("unit", line.UnitCode)
		}

		whole := quantity == left && already[line.ID] == 0
		rl := ReturnLine{
			LineNo: len(out.Lines) + 1, SaleLineID: line.ID, ProductID: line.ProductID,
			NameAR: line.NameAR, NameEN: line.NameEN, UnitCode: line.UnitCode, UnitDecimals: decimals,
			// An open-priced line was never on a shelf, so it cannot go back on one — whatever was ticked.
			QuantityMicro: quantity, Restocked: d.Restock && !line.OpenPrice,
			UnitCostMicro: line.UnitCostMicro, CostKnown: line.CostKnown,
			SoldMicro: line.QuantityMicro, AlreadyReturnedMicro: already[line.ID],
		}
		// The whole line back in one go is worth exactly what the line was charged — no division, no remainder.
		if whole {
			rl.RefundLocalMinor, rl.RefundUSDMinor = line.NetLocalMinor(), line.NetUSDMinor()
			rl.CostUSDMinor, rl.CostLocalMinor = line.CostUSDMinor, line.CostLocalMinor
		} else {
			rl.RefundLocalMinor = share(line.NetLocalMinor(), quantity, line.QuantityMicro)
			rl.RefundUSDMinor = share(line.NetUSDMinor(), quantity, line.QuantityMicro)
			rl.CostUSDMinor = share(line.CostUSDMinor, quantity, line.QuantityMicro)
			rl.CostLocalMinor = share(line.CostLocalMinor, quantity, line.QuantityMicro)
		}
		if !line.CostKnown {
			rl.UnitCostMicro, rl.CostUSDMinor, rl.CostLocalMinor = 0, 0, 0
			out.CostKnown = false
		}
		out.Lines = append(out.Lines, rl)
		out.RefundLocalMinor += rl.RefundLocalMinor
		out.RefundUSDMinor += rl.RefundUSDMinor
		out.CostUSDMinor += rl.CostUSDMinor
		out.CostLocalMinor += rl.CostLocalMinor
	}
	if len(out.Lines) == 0 {
		return Return{}, errs.Validation(CodeReturnNoLines, "nothing was ticked to return").
			WithField(FieldReturnLines, CodeReturnNoLines, "required")
	}
	// The refund is paid in the currency the sale settled in, which is the currency the customer handed over.
	if sale.SettlementCurrency == usd.Code {
		out.RefundMinor = out.RefundUSDMinor
	} else {
		out.RefundMinor = out.RefundLocalMinor
	}
	return out, nil
}

// share is part × whole ÷ all, rounded half away from zero, in big.Int so a line in old pounds cannot overflow
// partway through the multiplication and come back a plausible wrong number.
func share(amount, part, all int64) int64 {
	if all <= 0 || amount == 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(amount), big.NewInt(part))
	divisor := big.NewInt(all)
	quotient, remainder := new(big.Int).QuoRem(product, divisor, new(big.Int))
	twice := new(big.Int).Abs(new(big.Int).Lsh(remainder, 1))
	if twice.Cmp(new(big.Int).Abs(divisor)) >= 0 {
		if product.Sign()*divisor.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0
	}
	return quotient.Int64()
}

// returnReason holds a reason to the schema's bound. A return moves money out of the drawer, so it says why.
func returnReason(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errs.Validation(CodeReturnReasonRequired, "a return says why").
			WithField(FieldReturnReason, CodeReturnReasonRequired, "required")
	}
	if utf8.RuneCountInString(trimmed) > MaxReturnReasonRunes {
		return "", errs.Validation(CodeReturnReasonRequired, "that reason is too long").
			WithField(FieldReturnReason, CodeReturnReasonRequired, "too long")
	}
	return trimmed, nil
}

func withReturnField(err error, field string) error {
	if v, ok := errs.AsError(err); ok {
		return v.WithField(field, v.Code, v.Message)
	}
	return err
}
