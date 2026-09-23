package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// rounding is the one rounding every computed figure of a sale uses.
const rounding = round.HalfUp

// percentScale is Percent's fixed point: 100% is 1,000,000.
const percentScale = 1_000_000

// LineInput is a cart line as the till sends it.
type LineInput struct {
	ProductID id.ID
	Quantity  string
	// DiscountPercent is a line discount in percent, "" for none (owner PIN, L4 §14).
	DiscountPercent string
	// Price is the unit price typed at the till for an open-priced product (2026-09-23), in the product's currency and in
	// the pounds the books hold — the binding has already taken it back through the redenomination. Empty, and refused if
	// given, for every other product: a catalogue price is the catalogue's, not the cashier's.
	Price string
}

// CartInput is a cart as the till sends it: everything that decides what the customer is charged.
type CartInput struct {
	Lines []LineInput
	// Settlement is the currency the total is fixed in: the local currency or USD.
	Settlement string
	// SaleDiscount is a discount on the whole sale in the settlement currency, "" for none (owner PIN).
	SaleDiscount string
	// TenderCurrency and Tendered are what the customer handed over; an empty Tendered is exact money.
	TenderCurrency string
	Tendered       string
	// ChangeCurrency is the currency change is given in; "" for the default (Q-L4.2, Q-L4.3).
	ChangeCurrency string
	// Payment is "" or cash, or credit (L5 §5). On credit, Tendered is what was paid now and change is never given.
	Payment Payment
	// CustomerID is the customer a credit sale is charged to; a quote without one is priced, and checkout refuses it.
	CustomerID id.ID
}

// Context is everything a cart is priced against, read in one moment.
type Context struct {
	Local    Currency
	USD      Currency
	Rate     Rate
	CashNote int64
	Products map[id.ID]Product
	Stock    map[id.ID]Stocked
	// Customer is the credit sale's customer as the debt book has it (ID empty when not found), and Balances their
	// balance per currency — for the quote to show, not to decide what is charged.
	Customer Customer
	Balances map[string]int64
}

// Warning is something the till shows on a line without refusing it.
type Warning string

// The warnings (Q4, Q-L4.8).
const (
	WarnBeyondStock Warning = "beyond_stock"
	WarnNoCost      Warning = "no_cost"
	// WarnLowStock is what the shop asked to be told: this sale takes the product to or below the level the shop set
	// (2026-09-20). Unlike beyond_stock it is not a problem with the sale — it is a reminder to buy more.
	WarnLowStock Warning = "low_stock"
)

// PricedLine is a cart line priced: a Line before it has ids, and its warnings.
type PricedLine struct {
	Line
	OnHandMicro int64
	Warnings    []Warning
}

// Quote is a priced cart: every figure the till shows, and the token checkout compares (L4 §4).
type Quote struct {
	Lines              []PricedLine
	LinesLocalMinor    int64
	LinesUSDMinor      int64
	DiscountLocalMinor int64
	DiscountUSDMinor   int64
	Settlement         Currency
	Other              Currency
	CashNoteMinor      int64
	RoundingMinor      int64
	TotalMinor         int64
	// TotalOtherMinor is the total in the other currency, for reference: its lines less its discount, not rounded.
	TotalOtherMinor int64
	Tender          Currency
	TenderedMinor   int64
	TenderGiven     bool
	Change          Currency
	ChangeMinor     int64
	CostUSDMinor    int64
	// Discounted is true when any discount is on the cart: checkout then needs the owner (L4 §14).
	Discounted bool
	Rate       Rate
	Token      string

	// A credit sale (L5 §5.1): the customer, what is added to their debt in the settlement currency, and their balance
	// in it now and after. NeedsCustomer is true when no customer has been chosen yet.
	Payment            Payment
	Customer           Customer
	NeedsCustomer      bool
	DebtMinor          int64
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
}

// Price prices a cart (L4 §3). It refuses what checkout would refuse, so a quote that succeeds is a sale that can be
// recorded — unless something changes before the click, which the token catches.
func Price(in CartInput, c Context) (Quote, error) {
	if len(in.Lines) == 0 {
		return Quote{}, errs.Validation(CodeEmptyCart, "the cart is empty")
	}
	if len(in.Lines) > MaxLines {
		return Quote{}, errs.Validation(CodeTooManyLines, "too many lines").WithParam("max", strconv.Itoa(MaxLines))
	}
	if c.Rate.Nano <= 0 {
		return Quote{}, errs.Conflict(CodeNoRate, "set an exchange rate before selling")
	}
	cur, err := newCurrencies(c)
	if err != nil {
		return Quote{}, err
	}
	q := Quote{Rate: c.Rate, CashNoteMinor: c.CashNote}
	if q.Settlement, q.Other, err = settlement(in.Settlement, c); err != nil {
		return Quote{}, err
	}

	demand := map[id.ID]int64{}
	for i, li := range in.Lines {
		var line PricedLine
		if line, err = priceLine(i+1, li, c, cur, demand); err != nil {
			return Quote{}, err
		}
		q.Lines = append(q.Lines, line)
		q.LinesLocalMinor += line.NetLocalMinor()
		q.LinesUSDMinor += line.NetUSDMinor()
		q.CostUSDMinor += line.CostUSDMinor
		q.Discounted = q.Discounted || line.DiscountPercentMicro > 0
	}

	if err = q.applySaleDiscount(in.SaleDiscount, c, cur); err != nil {
		return Quote{}, err
	}
	before := q.inSettlement(q.LinesLocalMinor, q.LinesUSDMinor) - q.inSettlement(q.DiscountLocalMinor, q.DiscountUSDMinor)
	if q.Settlement.Code == c.USD.Code {
		q.TotalMinor = before
	} else {
		q.TotalMinor = roundToNote(big.NewRat(before, 1), c.CashNote)
		q.RoundingMinor = q.TotalMinor - before
	}
	q.TotalOtherMinor = q.inOther(q.LinesLocalMinor, q.LinesUSDMinor) - q.inOther(q.DiscountLocalMinor, q.DiscountUSDMinor)

	switch in.Payment {
	case "", PaymentCash:
		q.Payment = PaymentCash
		err = q.applyTender(in, c)
	case PaymentCredit:
		q.Payment = PaymentCredit
		err = q.applyCredit(in, c)
	default:
		err = errs.Validation(CodeUnknownPayment, "a sale is paid in cash or on credit").WithParam("value", string(in.Payment))
	}
	if err != nil {
		return Quote{}, err
	}
	q.Token = token(c, q)
	return q, nil
}

type currencies struct{ local, usd money.Currency }

func newCurrencies(c Context) (currencies, error) {
	local, err := money.NewCurrency(c.Local.Code, uint8(c.Local.Decimals), rounding) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return currencies{}, err
	}
	usd, err := money.NewCurrency(c.USD.Code, uint8(c.USD.Decimals), rounding) //nolint:gosec // 0–4 by the schema's CHECK
	return currencies{local: local, usd: usd}, err
}

func settlement(code string, c Context) (Currency, Currency, error) {
	switch code {
	case c.Local.Code, "":
		return c.Local, c.USD, nil
	case c.USD.Code:
		return c.USD, c.Local, nil
	}
	return Currency{}, Currency{}, errs.Validation(CodeUnknownCurrency, "a sale settles in the local currency or USD").WithParam("value", code)
}

func (q Quote) inSettlement(local, usd int64) int64 {
	if q.Settlement.Code == USD {
		return usd
	}
	return local
}

func (q Quote) inOther(local, usd int64) int64 {
	if q.Settlement.Code == USD {
		return local
	}
	return usd
}

// anyUnit carries a quantity through the kernel, which reads only its micro value.
var anyUnit, _ = quantity.NewUnit("any", "any", quantity.FactorScale, true)

func priceLine(lineNo int, li LineInput, c Context, cur currencies, demand map[id.ID]int64) (PricedLine, error) {
	p, ok := c.Products[li.ProductID]
	if !ok {
		return PricedLine{}, lineErr(errs.NotFound(CodeUnknownProduct, "no such product"), lineNo, "productId")
	}
	if !p.Active {
		return PricedLine{}, lineErr(errs.Conflict(CodeInactiveProduct, "the product is inactive").WithParam("name", p.NameAR), lineNo, "productId")
	}
	qtyMicro, err := parseQuantity(li.Quantity, p.UnitDecimals)
	if err != nil {
		return PricedLine{}, lineErr(err, lineNo, FieldQuantity)
	}
	percent, err := parsePercent(li.DiscountPercent)
	if err != nil {
		return PricedLine{}, lineErr(err, lineNo, FieldDiscountPercent)
	}
	qty, err := quantity.FromMicro(anyUnit, qtyMicro)
	if err != nil {
		return PricedLine{}, err
	}
	rate := money.RateFromNano(c.Rate.Nano)

	unitPrice, err := lineUnitPrice(li, p, cur)
	if err != nil {
		return PricedLine{}, lineErr(err, lineNo, FieldPrice)
	}
	line := PricedLine{Line: Line{
		LineNo: lineNo, ProductID: p.ID, NameAR: p.NameAR, NameEN: p.NameEN, UnitCode: p.UnitCode, QuantityMicro: qtyMicro,
		PriceCurrency: p.PriceCurrency, UnitPriceMicro: unitPrice, DiscountPercentMicro: percent, OpenPrice: p.OpenPrice,
	}}
	// Both currencies from ONE exact product, each rounded once (DESIGN §4.4, L4 §3.1).
	var local, usd money.Money
	switch p.PriceCurrency {
	case c.Local.Code:
		price := money.UnitFromMicro(cur.local, unitPrice)
		if local, err = money.LineExtension(price, qty, rounding); err == nil {
			usd, err = money.LineExtensionDivRate(price, qty, rate, cur.usd, rounding)
		}
	case c.USD.Code:
		price := money.UnitFromMicro(cur.usd, unitPrice)
		if usd, err = money.LineExtension(price, qty, rounding); err == nil {
			local, err = money.LineExtensionMulRate(price, qty, rate, cur.local, rounding)
		}
	default:
		return PricedLine{}, lineErr(errs.Validation(CodeUnknownCurrency, "the product's price currency is neither").WithParam("value", p.PriceCurrency), lineNo, "productId")
	}
	if err != nil {
		return PricedLine{}, lineErr(tooLarge(err), lineNo, FieldQuantity)
	}
	line.GrossLocalMinor, line.GrossUSDMinor = local.Minor(), usd.Minor()
	if percent > 0 {
		pct := money.PercentFromMicro(percent)
		var dl, du money.Money
		if dl, err = local.MulPercent(pct, rounding); err != nil {
			return PricedLine{}, lineErr(tooLarge(err), lineNo, FieldDiscountPercent)
		}
		if du, err = usd.MulPercent(pct, rounding); err != nil {
			return PricedLine{}, lineErr(tooLarge(err), lineNo, FieldDiscountPercent)
		}
		line.DiscountLocalMinor, line.DiscountUSDMinor = dl.Minor(), du.Minor()
	}

	// An open-priced line sells something the shop never counted: no stock warning, and no cost — not an unknown cost
	// waiting to be entered, but none by design. The reports keep it apart from both (2026-09-23).
	if p.OpenPrice {
		return line, nil
	}
	stocked := c.Stock[p.ID]
	line.OnHandMicro = stocked.OnHandMicro
	demand[p.ID] += qtyMicro
	if demand[p.ID] > stocked.OnHandMicro {
		line.Warnings = append(line.Warnings, WarnBeyondStock)
	} else if p.HasReorder && p.ReorderMicro > 0 && stocked.OnHandMicro-demand[p.ID] <= p.ReorderMicro {
		// Only where the shelf still covers the sale: a line already flagged as beyond the stock does not also need
		// telling that the stock is low. And only where the shop set a level — a product nobody gave one is never low.
		line.Warnings = append(line.Warnings, WarnLowStock)
	}
	// What one unit costs. Since 2026-09-16 the cost price typed on the product is the profit basis where the shop has
	// typed one (the owner's decision); the weighted average of the deliveries stands where it has not. Either way the
	// figure below is in DOLLARS, because that is the currency profit is held in — a cost typed in pounds is converted at
	// this sale's own rate, so the two profit figures still agree (DESIGN C6).
	unitCostMicro, costKnown := stocked.AvgCostMicro, stocked.CostKnown
	if p.HasCost {
		switch p.PriceCurrency {
		case cur.usd.Code():
			unitCostMicro, costKnown = p.CostMicro, true
		default:
			typed := money.UnitFromMicro(cur.local, p.CostMicro)
			inUSD, convErr := typed.DivideByRate(rate, cur.usd, rounding)
			if convErr != nil {
				return PricedLine{}, lineErr(tooLarge(convErr), lineNo, FieldQuantity)
			}
			unitCostMicro, costKnown = inUSD.Micro(), true
		}
	}
	if !costKnown {
		line.Warnings = append(line.Warnings, WarnNoCost)
		return line, nil
	}
	// Cost at the sale's rate, so the two profit figures agree (DESIGN C6).
	avg := money.UnitFromMicro(cur.usd, unitCostMicro)
	costUSD, err := money.LineExtension(avg, qty, rounding)
	if err != nil {
		return PricedLine{}, lineErr(tooLarge(err), lineNo, FieldQuantity)
	}
	costLocal, err := money.LineExtensionMulRate(avg, qty, rate, cur.local, rounding)
	if err != nil {
		return PricedLine{}, lineErr(tooLarge(err), lineNo, FieldQuantity)
	}
	line.UnitCostMicro, line.CostKnown, line.CostUSDMinor, line.CostLocalMinor = unitCostMicro, true, costUSD.Minor(), costLocal.Minor()
	return line, nil
}

// applySaleDiscount reads a whole-sale discount in the settlement currency and keeps it in both.
func (q *Quote) applySaleDiscount(raw string, c Context, cur currencies) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	settle, other := cur.local, cur.usd
	if q.Settlement.Code == c.USD.Code {
		settle, other = cur.usd, cur.local
	}
	amount, err := parseAmount(raw, settle, FieldSaleDiscount, CodeDiscountInvalid)
	if err != nil {
		return err
	}
	if amount.Minor() == 0 {
		return nil
	}
	lines := q.inSettlement(q.LinesLocalMinor, q.LinesUSDMinor)
	if amount.Minor() > lines {
		return errs.Validation(CodeDiscountTooLarge, "the discount is larger than the sale").
			WithField(FieldSaleDiscount, CodeDiscountTooLarge, "too large")
	}
	var converted money.Money
	rate := money.RateFromNano(c.Rate.Nano)
	if q.Settlement.Code == c.USD.Code {
		converted, err = amount.MulRate(rate, other, rounding)
	} else {
		converted, err = amount.DivRate(rate, other, rounding)
	}
	if err != nil {
		return tooLarge(err)
	}
	// A discount of the whole sale converts to at most the whole sale in the other currency: the conversion's one
	// rounding may otherwise pass it by a minor unit.
	otherMinor := min(converted.Minor(), q.inOther(q.LinesLocalMinor, q.LinesUSDMinor))
	if q.Settlement.Code == c.USD.Code {
		q.DiscountUSDMinor, q.DiscountLocalMinor = amount.Minor(), otherMinor
	} else {
		q.DiscountLocalMinor, q.DiscountUSDMinor = amount.Minor(), otherMinor
	}
	q.Discounted = true
	return nil
}

// applyTender works out the change (L4 §3.3, Q-L4.2, Q-L4.3): exact in rationals, then rounded once — to the cash note
// in the local currency, to the cent in dollars.
func (q *Quote) applyTender(in CartInput, c Context) error {
	q.Tender = q.Settlement
	if in.TenderCurrency != "" {
		t, _, err := settlement(in.TenderCurrency, c)
		if err != nil {
			return errs.Validation(CodeUnknownCurrency, "tender is in the local currency or USD").WithParam("value", in.TenderCurrency)
		}
		q.Tender = t
	}
	bothDollars := q.Settlement.Code == c.USD.Code && q.Tender.Code == c.USD.Code
	switch in.ChangeCurrency {
	case "":
		q.Change = c.Local
		if bothDollars {
			q.Change = c.USD
		}
	case c.Local.Code:
		if bothDollars {
			return errs.Validation(CodeChangeCurrency, "a sale paid in dollars gives change in dollars").
				WithField(FieldChangeCurrency, CodeChangeCurrency, "dollars")
		}
		q.Change = c.Local
	case c.USD.Code:
		q.Change = c.USD
	default:
		return errs.Validation(CodeUnknownCurrency, "change is in the local currency or USD").WithParam("value", in.ChangeCurrency)
	}

	if strings.TrimSpace(in.Tendered) == "" {
		// Exact money: tendered is the total, in the settlement currency, and there is no change.
		q.Tender, q.TenderedMinor, q.Change, q.ChangeMinor = q.Settlement, q.TotalMinor, q.Settlement, 0
		return nil
	}
	tenderCur, err := money.NewCurrency(q.Tender.Code, uint8(q.Tender.Decimals), rounding) //nolint:gosec // 0–4 by the schema's CHECK
	if err != nil {
		return err
	}
	tendered, err := parseAmount(in.Tendered, tenderCur, FieldTendered, CodeTenderDecimals)
	if err != nil {
		return err
	}
	q.TenderedMinor, q.TenderGiven = tendered.Minor(), true

	change := new(big.Rat).Sub(inMinor(q.TenderedMinor, q.Tender, q.Change, c), inMinor(q.TotalMinor, q.Settlement, q.Change, c))
	if change.Sign() < 0 {
		return errs.Validation(CodeTenderShort, "the amount handed over is less than the total").
			WithField(FieldTendered, CodeTenderShort, "short")
	}
	if q.Change.Code == c.USD.Code {
		q.ChangeMinor = roundToNote(change, 1)
	} else {
		q.ChangeMinor = roundToNote(change, c.CashNote)
	}
	return nil
}

// applyCredit works out a credit sale (L5 §5.2): the customer, what was paid now in either currency, and what that
// leaves owed in the settlement currency. Paying the whole total is a cash sale.
func (q *Quote) applyCredit(in CartInput, c Context) error {
	switch {
	case in.CustomerID == "":
		q.NeedsCustomer = true
	case c.Customer.ID != in.CustomerID:
		return errs.NotFound(CodeCustomerNotFound, "no such customer").WithField(FieldCustomer, CodeCustomerNotFound, "not found")
	case !c.Customer.Active:
		return errs.Conflict(CodeCustomerInactive, "this customer is inactive").
			WithField(FieldCustomer, CodeCustomerInactive, "inactive").WithParam("name", c.Customer.Name)
	default:
		q.Customer = c.Customer
	}
	q.Tender = q.Settlement
	if in.TenderCurrency != "" {
		t, _, err := settlement(in.TenderCurrency, c)
		if err != nil {
			return errs.Validation(CodeUnknownCurrency, "paid in the local currency or USD").WithParam("value", in.TenderCurrency)
		}
		q.Tender = t
	}
	q.Change, q.ChangeMinor = q.Settlement, 0
	if strings.TrimSpace(in.Tendered) == "" {
		q.Tender, q.TenderedMinor = q.Settlement, 0
	} else {
		tenderCur, err := money.NewCurrency(q.Tender.Code, uint8(q.Tender.Decimals), rounding) //nolint:gosec // 0–4 by the schema's CHECK
		if err != nil {
			return err
		}
		tendered, err := parseAmount(in.Tendered, tenderCur, FieldTendered, CodeTenderDecimals)
		if err != nil {
			return err
		}
		q.TenderedMinor, q.TenderGiven = tendered.Minor(), true
	}
	q.DebtMinor = DebtOf(q.TotalMinor, q.Settlement, q.Tender, q.TenderedMinor, c.Rate.Nano, c.CashNote)
	if q.DebtMinor <= 0 {
		return errs.Validation(CodeCreditPaidInFull, "paid in full: take it as a cash sale").
			WithField(FieldTendered, CodeCreditPaidInFull, "paid in full")
	}
	q.BalanceBeforeMinor = c.Balances[q.Settlement.Code]
	q.BalanceAfterMinor = q.BalanceBeforeMinor + q.DebtMinor
	return nil
}

// DebtOf is what a credit sale adds to the debt (L5 §5.2): the total less what was paid now — exactly in the same
// currency, otherwise converted at the sale's rate and rounded once, to the note in pounds and the cent in dollars.
func DebtOf(totalMinor int64, settle, tendered Currency, tenderedMinor, rateNano, cashNote int64) int64 {
	switch {
	case tenderedMinor == 0:
		return totalMinor
	case tendered.Code == settle.Code:
		return totalMinor - tenderedMinor
	}
	exact := new(big.Rat).Sub(big.NewRat(totalMinor, 1), tender.Convert(tenderedMinor, tender.Currency(tendered), tender.Currency(settle), rateNano))
	return tender.Round(exact, tender.Increment(tender.Currency(settle), cashNote))
}

// inMinor is an amount in minor units of one currency, exactly, in minor units of another, at the context's rate. The
// arithmetic is internal/lite/tender's, shared with the debt book (L5 §10.2).
func inMinor(minor int64, from, to Currency, c Context) *big.Rat {
	return tender.Convert(minor, tender.Currency(from), tender.Currency(to), c.Rate.Nano)
}

// roundToNote rounds an amount in minor units to the nearest multiple of note, half up (Q-L4.1).
func roundToNote(v *big.Rat, note int64) int64 { return tender.Round(v, note) }

func parseQuantity(raw string, decimals int) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, err
	}
	if numinput.Decimals(normalised) > decimals {
		return 0, errs.Validation(CodeQuantityDecimals, "too many decimals for the unit").WithParam("decimals", strconv.Itoa(decimals))
	}
	v, ok := new(big.Rat).SetString(normalised)
	if !ok {
		return 0, errs.Validation(numinput.CodeInvalid, "not a number")
	}
	v.Mul(v, big.NewRat(1_000_000, 1))
	if !v.IsInt() || !v.Num().IsInt64() {
		return 0, errs.Validation(CodeQuantityTooLarge, "quantity too large")
	}
	if v.Num().Int64() <= 0 {
		return 0, errs.Validation(CodeQuantityRequired, "a quantity above zero is required")
	}
	return v.Num().Int64(), nil
}

// parsePercent reads a discount percentage: "" or 0 to 100 with at most two decimals, as a Percent's micro (100% is
// 1,000,000).
func parsePercent(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		return 0, err
	}
	v, ok := new(big.Rat).SetString(normalised)
	if !ok || numinput.Decimals(normalised) > 2 || v.Cmp(big.NewRat(100, 1)) > 0 {
		return 0, errs.Validation(CodeDiscountInvalid, "a discount is 0 to 100 percent, two decimals at most")
	}
	v.Mul(v, big.NewRat(percentScale/100, 1))
	return v.Num().Int64(), nil
}

func parseAmount(raw string, c money.Currency, field, decimalsCode string) (money.Money, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		if typed, ok := errs.AsError(err); ok {
			return money.Money{}, typed.WithField(field, typed.Code, "invalid")
		}
		return money.Money{}, err
	}
	if numinput.Decimals(normalised) > int(c.Decimals()) {
		return money.Money{}, errs.Validation(decimalsCode, "too many decimals for the currency").
			WithField(field, decimalsCode, "decimals").WithParam("currency", c.Code()).WithParam("decimals", strconv.Itoa(int(c.Decimals())))
	}
	m, err := money.Parse(c, normalised)
	if err != nil {
		return money.Money{}, errs.Wrap(err, errs.CategoryValidation, CodeAmountTooLarge, "amount out of range").WithField(field, CodeAmountTooLarge, "too large")
	}
	return m, nil
}

func tooLarge(err error) error {
	return errs.Wrap(err, errs.CategoryValidation, CodeAmountTooLarge, "amount out of range")
}

// lineErr names the line a refusal belongs to, so the till can mark it.
func lineErr(err error, lineNo int, field string) error {
	typed, ok := errs.AsError(err)
	if !ok {
		return err
	}
	return typed.WithField(fmt.Sprintf("lines.%d.%s", lineNo, field), typed.Code, "invalid").WithParam("line", strconv.Itoa(lineNo))
}

// token fingerprints everything that decides what the customer is charged (L4 §4): the rate, each line's product at
// its version and price, the quantities and discounts, the currencies, the tender, and the cash note. Not a secret —
// one operator, one machine — only a record of what the screen showed.
func token(c Context, q Quote) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "rate=%s:%d|note=%d|settle=%s|discount=%d/%d|tender=%s:%d:%t|change=%s|payment=%s:%s\n",
		c.Rate.ID, c.Rate.Nano, c.CashNote, q.Settlement.Code, q.DiscountLocalMinor, q.DiscountUSDMinor,
		q.Tender.Code, q.TenderedMinor, q.TenderGiven, q.Change.Code, q.Payment, q.Customer.ID)
	for _, l := range q.Lines {
		p := c.Products[l.ProductID]
		// The line's own unit price, not only the product's: an open-priced line's price is typed at the till, and a
		// quote must not survive the cashier retyping it.
		_, _ = fmt.Fprintf(h, "%s:%d:%s:%d:%d|%d|%d\n", p.ID, p.RowVersion, p.PriceCurrency, p.PriceMicro, l.UnitPriceMicro,
			l.QuantityMicro, l.DiscountPercentMicro)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// lineUnitPrice is the price one unit of a line is charged at: the catalogue's, or for an open-priced product the price
// typed at the till.
func lineUnitPrice(li LineInput, p Product, cur currencies) (int64, error) {
	if !p.OpenPrice {
		if li.Price != "" {
			return 0, errs.Validation(CodePriceNotOpen, "this product's price is set in the catalogue").
				WithParam("name", p.NameAR)
		}
		return p.PriceMicro, nil
	}
	c := cur.local
	if p.PriceCurrency == cur.usd.Code() {
		c = cur.usd
	}
	normalised, err := numinput.Normalise(li.Price)
	if err != nil {
		return 0, errs.Validation(CodeOpenPriceRequired, "type the price").WithParam("name", p.NameAR)
	}
	if numinput.Decimals(normalised) > int(c.Decimals()) {
		return 0, errs.Validation(CodePriceDecimals, "too many decimals for the currency").
			WithParam("decimals", strconv.Itoa(int(c.Decimals())))
	}
	amount, err := money.ParseUnitAmount(c, normalised)
	if err != nil || amount.Micro() <= 0 {
		return 0, errs.Validation(CodeOpenPriceRequired, "a price above nothing").WithParam("name", p.NameAR)
	}
	return amount.Micro(), nil
}
