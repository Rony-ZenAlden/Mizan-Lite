package api

import (
	"context"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	"sort"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// Customers is the debt book: customers, who owes what, statements, repayments and the owner's corrections (L5 §11).
//
// Balances cross per currency, as Go-formatted strings. No DTO carries a figure summed across currencies: a balance's
// equivalent in the other currency is a labelled reference with the rate it was worked out at (Q-L5.8).
type Customers struct{ core *core }

// BalanceDTO is one currency's side of a customer.
type BalanceDTO struct {
	Currency    string `json:"currency"`
	Balance     string `json:"balance"`
	OwedSince   string `json:"owedSince"`
	LastPayment string `json:"lastPayment"`
	// Reference is the balance in the other currency at the rate in force, for reference only; "" with no rate.
	Reference         string `json:"reference"`
	ReferenceCurrency string `json:"referenceCurrency"`
}

// CustomerDTO is a customer with a balance per currency they have entries in.
type CustomerDTO struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Phone      string       `json:"phone"`
	Note       string       `json:"note"`
	City       string       `json:"city"`
	Active     bool         `json:"active"`
	RowVersion int64        `json:"rowVersion"`
	Balances   []BalanceDTO `json:"balances"`
}

// CustomerQueryDTO searches customers by name or phone.
type CustomerQueryDTO struct {
	Text            string `json:"text"`
	OwingOnly       bool   `json:"owingOnly"`
	IncludeInactive bool   `json:"includeInactive"`
}

// CustomerInput is a new customer.
type CustomerInput struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Note  string `json:"note"`
	City  string `json:"city"`
}

// UpdateCustomerInput edits a customer at a version.
type UpdateCustomerInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Note       string `json:"note"`
	City       string `json:"city"`
}

// SetCustomerActiveInput activates or deactivates a customer.
type SetCustomerActiveInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Active     bool   `json:"active"`
}

// StatementQueryDTO asks for one chain.
type StatementQueryDTO struct {
	CustomerID string `json:"customerId"`
	Currency   string `json:"currency"`
}

// EntryDTO is one line of a statement.
type EntryDTO struct {
	ID           string `json:"id"`
	Seq          int64  `json:"seq"`
	BusinessDate string `json:"businessDate"`
	OccurredAt   string `json:"occurredAt"`
	Kind         string `json:"kind"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	BalanceAfter string `json:"balanceAfter"`
	SaleID       string `json:"saleId"`
	ReversesID   string `json:"reversesId"`
	// The cash of a payment or refund, "" otherwise.
	TenderedCurrency string `json:"tenderedCurrency"`
	Tendered         string `json:"tendered"`
	ChangeCurrency   string `json:"changeCurrency"`
	Change           string `json:"change"`
	Rate             string `json:"rate"`
	Note             string `json:"note"`
	CustomerName     string `json:"customerName"`
	// Reversed is true once reversed; Reversible when the owner may reverse it (not a charge, not a reversal, not yet).
	Reversed   bool `json:"reversed"`
	Reversible bool `json:"reversible"`
}

// StatementDTO is one customer's chain in one currency, whole.
type StatementDTO struct {
	Customer    CustomerDTO `json:"customer"`
	Currency    string      `json:"currency"`
	Balance     string      `json:"balance"`
	OwedSince   string      `json:"owedSince"`
	LastPayment string      `json:"lastPayment"`
	Entries     []EntryDTO  `json:"entries"`
	// Rate is the rate the balances' references were worked out at; "" with no rate.
	Rate          string `json:"rate"`
	LocalCurrency string `json:"localCurrency"`
}

// DebtDayDTO is one currency's side of the debt book's day.
type DebtDayDTO struct {
	Currency   string `json:"currency"`
	Payments   int    `json:"payments"`
	Settled    string `json:"settled"`
	CashIn     string `json:"cashIn"`
	ChangeOut  string `json:"changeOut"`
	Refunds    int    `json:"refunds"`
	RefundOut  string `json:"refundOut"`
	Charged    string `json:"charged"`
	WrittenOff string `json:"writtenOff"`
}

// OutstandingDTO is who owes what, and today's debt book per currency.
type OutstandingDTO struct {
	BusinessDate  string        `json:"businessDate"`
	Customers     []CustomerDTO `json:"customers"`
	Today         []DebtDayDTO  `json:"today"`
	Rate          string        `json:"rate"`
	LocalCurrency string        `json:"localCurrency"`
}

// PaymentInput is a repayment (or its quote, without the token). Amount is in TenderCurrency ("" for the debt's); All
// pays the whole balance; ChangeCurrency is "" for the default.
type PaymentInput struct {
	CustomerID     string `json:"customerId"`
	Currency       string `json:"currency"`
	TenderCurrency string `json:"tenderCurrency"`
	Amount         string `json:"amount"`
	All            bool   `json:"all"`
	ChangeCurrency string `json:"changeCurrency"`
	Note           string `json:"note"`
	Token          string `json:"token"`
}

// PaymentQuoteDTO is a repayment worked out by Go.
type PaymentQuoteDTO struct {
	Currency       string `json:"currency"`
	TenderCurrency string `json:"tenderCurrency"`
	Tendered       string `json:"tendered"`
	ChangeCurrency string `json:"changeCurrency"`
	Change         string `json:"change"`
	Settled        string `json:"settled"`
	BalanceBefore  string `json:"balanceBefore"`
	BalanceAfter   string `json:"balanceAfter"`
	All            bool   `json:"all"`
	Rate           string `json:"rate"`
	Token          string `json:"token"`
}

// DebtAmountInput is an opening balance or a write-off. All writes off the whole balance; Note is the write-off's
// required reason.
type DebtAmountInput struct {
	CustomerID string `json:"customerId"`
	Currency   string `json:"currency"`
	Amount     string `json:"amount"`
	All        bool   `json:"all"`
	Note       string `json:"note"`
}

// RefundInput pays back a balance below zero.
type RefundInput struct {
	CustomerID     string `json:"customerId"`
	Currency       string `json:"currency"`
	TenderCurrency string `json:"tenderCurrency"`
	Amount         string `json:"amount"`
	All            bool   `json:"all"`
	Reason         string `json:"reason"`
}

// ReverseEntryInput reverses an entry.
type ReverseEntryInput struct {
	EntryID string `json:"entryId"`
	Reason  string `json:"reason"`
}

// debtView is the reference data and rate a debt DTO is formatted against.
type debtView struct {
	tillView
	rateNano int64
	local    string
}

func newDebtView(ctx context.Context, app *bootstrap.App) (debtView, error) {
	v, err := newTillView(ctx, app)
	if err != nil {
		return debtView{}, err
	}
	current, err := app.FX.Current(ctx)
	if err != nil {
		return debtView{}, err
	}
	dv := debtView{tillView: v, local: current.Local}
	if current.Found {
		dv.rateNano = current.Rate.Nano
	}
	return dv, nil
}

func (v debtView) rate() string {
	if v.rateNano == 0 {
		return ""
	}
	return rateText(v.shop, v.rateNano)
}

func (v debtView) currency(code string) tender.Currency {
	return tender.Currency{Code: code, Decimals: v.ref.Currencies[code].Decimals}
}

// balance formats one summary, with its reference in the other currency (Q-L5.8) — none in a dollars-only shop, which
// reads no pounds (0.10.1).
func (v debtView) balance(s domain.Summary) BalanceDTO {
	dto := BalanceDTO{Currency: s.Currency, Balance: v.money(s.BalanceMinor, s.Currency), OwedSince: s.OwedSince, LastPayment: s.LastPayment}
	if v.rateNano > 0 && !v.shop.USDOnly() {
		other := tender.USD
		if s.Currency == tender.USD {
			other = v.local
		}
		dto.ReferenceCurrency = other
		dto.Reference = v.money(tender.Round(tender.Convert(s.BalanceMinor, v.currency(s.Currency), v.currency(other), v.rateNano), 1), other)
	}
	return dto
}

func (v debtView) customer(w customers.WithBalances) CustomerDTO {
	c := w.Customer
	dto := CustomerDTO{ID: c.ID.String(), Name: c.Name, Phone: c.Phone, Note: c.Note, City: c.City, Active: c.Active, RowVersion: c.RowVersion,
		Balances: make([]BalanceDTO, 0, len(w.Summaries))}
	for _, s := range w.Summaries {
		dto.Balances = append(dto.Balances, v.balance(s))
	}
	return dto
}

func (v debtView) entry(e domain.Entry, reversed bool) EntryDTO {
	dto := EntryDTO{
		ID: e.ID.String(), Seq: e.Seq, BusinessDate: e.BusinessDate, OccurredAt: clock.Format(e.OccurredAt), Kind: string(e.Kind),
		Currency: e.Currency, Amount: v.money(e.AmountMinor, e.Currency), BalanceAfter: v.money(e.BalanceAfterMinor, e.Currency),
		SaleID: e.SaleID.String(), ReversesID: e.ReversesID.String(), Note: e.Note, CustomerName: e.CustomerName, Reversed: reversed,
		// A conversion moved a balance between currencies and is never reversed (0.10.0).
		Reversible: !reversed && e.Kind != domain.KindCharge && e.Kind != domain.KindReversal && e.Kind != domain.KindConversion,
	}
	if e.Kind == domain.KindPayment || e.Kind == domain.KindRefund {
		dto.TenderedCurrency, dto.Tendered = e.Cash.TenderedCurrency, v.money(e.Cash.TenderedMinor, e.Cash.TenderedCurrency)
		dto.ChangeCurrency, dto.Change = e.Cash.ChangeCurrency, v.money(e.Cash.ChangeMinor, e.Cash.ChangeCurrency)
		dto.Rate = rateText(v.shop, e.Cash.RateNano)
	}
	return dto
}

func parseCustomerID(raw string) (id.ID, error) {
	parsed, err := id.Parse(raw)
	if err != nil {
		return "", domain.ErrNotFound()
	}
	return parsed, nil
}

// Search finds customers by name in any spelling, or by phone in any digits, with their balances.
func (c *Customers) Search(q CustomerQueryDTO) envelope.Result[[]CustomerDTO] {
	return call(c.core, "Customers.Search", func(ctx context.Context, app *bootstrap.App) ([]CustomerDTO, error) {
		found, err := app.Customers.Search(ctx, q.Text, q.OwingOnly, q.IncludeInactive)
		if err != nil {
			return nil, err
		}
		v, err := newDebtView(ctx, app)
		if err != nil {
			return nil, err
		}
		out := make([]CustomerDTO, 0, len(found))
		for _, w := range found {
			out = append(out, v.customer(w))
		}
		return out, nil
	})
}

// withCustomer formats one customer, with balances, after an act.
func (c *Customers) withCustomer(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Customer, error)) envelope.Result[CustomerDTO] {
	return call(c.core, method, func(ctx context.Context, app *bootstrap.App) (CustomerDTO, error) {
		customer, err := fn(ctx, app)
		if err != nil {
			return CustomerDTO{}, err
		}
		w, err := app.Customers.WithBalancesOf(ctx, customer.ID)
		if err != nil {
			return CustomerDTO{}, err
		}
		v, err := newDebtView(ctx, app)
		return v.customer(w), err
	})
}

// Create adds a customer. Anyone may.
func (c *Customers) Create(in CustomerInput) envelope.Result[CustomerDTO] {
	return c.withCustomer("Customers.Create", func(ctx context.Context, app *bootstrap.App) (domain.Customer, error) {
		return app.Customers.Create(ctx, domain.Draft{Name: in.Name, Phone: in.Phone, Note: in.Note, City: in.City})
	})
}

// Update edits a customer's name, phone and note. Anyone may.
func (c *Customers) Update(in UpdateCustomerInput) envelope.Result[CustomerDTO] {
	return c.withCustomer("Customers.Update", func(ctx context.Context, app *bootstrap.App) (domain.Customer, error) {
		customerID, err := parseCustomerID(in.ID)
		if err != nil {
			return domain.Customer{}, err
		}
		return app.Customers.Update(ctx, customers.UpdateInput{ID: customerID, RowVersion: in.RowVersion, Draft: domain.Draft{Name: in.Name, Phone: in.Phone, Note: in.Note, City: in.City}})
	})
}

// SetActive activates or deactivates a customer. Deactivating one with a balance returns lite.owner.required outside
// owner mode.
func (c *Customers) SetActive(in SetCustomerActiveInput) envelope.Result[CustomerDTO] {
	return c.withCustomer("Customers.SetActive", func(ctx context.Context, app *bootstrap.App) (domain.Customer, error) {
		customerID, err := parseCustomerID(in.ID)
		if err != nil {
			return domain.Customer{}, err
		}
		return app.Customers.SetActive(ctx, customerID, in.RowVersion, in.Active)
	})
}

// Statement is one customer's entries in one currency.
func (c *Customers) Statement(q StatementQueryDTO) envelope.Result[StatementDTO] {
	return call(c.core, "Customers.Statement", func(ctx context.Context, app *bootstrap.App) (StatementDTO, error) {
		return statementDTO(ctx, app, q)
	})
}

// statementDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func statementDTO(ctx context.Context, app *bootstrap.App, q StatementQueryDTO) (StatementDTO, error) {
	customerID, err := parseCustomerID(q.CustomerID)
	if err != nil {
		return StatementDTO{}, err
	}
	st, err := app.Customers.Statement(ctx, customerID, q.Currency)
	if err != nil {
		return StatementDTO{}, err
	}
	v, err := newDebtView(ctx, app)
	if err != nil {
		return StatementDTO{}, err
	}
	w, err := app.Customers.WithBalancesOf(ctx, customerID)
	if err != nil {
		return StatementDTO{}, err
	}
	dto := StatementDTO{Customer: v.customer(w), Currency: st.Currency,
		Balance: v.money(st.Summary.BalanceMinor, st.Currency), OwedSince: st.Summary.OwedSince, LastPayment: st.Summary.LastPayment,
		Entries: make([]EntryDTO, 0, len(st.Entries)), Rate: v.rate(), LocalCurrency: v.local}
	// Newest first: the statement is read from the top.
	for i := len(st.Entries) - 1; i >= 0; i-- {
		e := st.Entries[i]
		dto.Entries = append(dto.Entries, v.entry(e, st.Reversed[e.ID]))
	}
	return dto, nil
}

// Outstanding is who owes what — by name, never totalled across currencies — and today's debt book.
func (c *Customers) Outstanding() envelope.Result[OutstandingDTO] {
	return call(c.core, "Customers.Outstanding", outstandingDTO)
}

// outstandingDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func outstandingDTO(ctx context.Context, app *bootstrap.App) (OutstandingDTO, error) {
	out, err := app.Customers.Outstanding(ctx)
	if err != nil {
		return OutstandingDTO{}, err
	}
	v, err := newDebtView(ctx, app)
	if err != nil {
		return OutstandingDTO{}, err
	}
	dto := OutstandingDTO{BusinessDate: out.BusinessDate, Customers: make([]CustomerDTO, 0, len(out.Customers)),
		Today: make([]DebtDayDTO, 0, len(out.Today)), Rate: v.rate(), LocalCurrency: v.local}
	for _, w := range out.Customers {
		dto.Customers = append(dto.Customers, v.customer(w))
	}
	for code, t := range out.Today {
		dto.Today = append(dto.Today, DebtDayDTO{Currency: code, Payments: t.Payments, Settled: v.money(t.SettledMinor, code),
			CashIn: v.money(t.CashInMinor, code), ChangeOut: v.money(t.ChangeOutMinor, code), Refunds: t.Refunds,
			RefundOut: v.money(t.RefundOutMinor, code), Charged: v.money(t.ChargedMinor, code), WrittenOff: v.money(t.WrittenOffMinor, code)})
	}
	sort.Slice(dto.Today, func(i, j int) bool {
		if (dto.Today[i].Currency == tender.USD) != (dto.Today[j].Currency == tender.USD) {
			return dto.Today[j].Currency == tender.USD
		}
		return dto.Today[i].Currency < dto.Today[j].Currency
	})
	return dto, nil
}

// cash is the payment taken back into the pounds the books hold, in the currency actually handed over — the debt's own
// when the screen named none, resolved before the conversion because Base with no currency changes nothing (0.9.9).
func (in PaymentInput) cash(shop moneyfmt.Shop) domain.CashInput {
	return domain.CashInput{Currency: in.Currency, TenderCurrency: in.TenderCurrency,
		Amount: shop.Base(in.Amount, tenderOrDebt(in.TenderCurrency, in.Currency)), All: in.All, ChangeCurrency: in.ChangeCurrency}
}

// tenderOrDebt is the currency cash changed hands in: the one named, or the debt's own.
func tenderOrDebt(tender, debt string) string {
	if tender != "" {
		return tender
	}
	return debt
}

// QuotePayment works out a repayment and writes nothing.
func (c *Customers) QuotePayment(in PaymentInput) envelope.Result[PaymentQuoteDTO] {
	return call(c.core, "Customers.QuotePayment", func(ctx context.Context, app *bootstrap.App) (PaymentQuoteDTO, error) {
		customerID, err := parseCustomerID(in.CustomerID)
		if err != nil {
			return PaymentQuoteDTO{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return PaymentQuoteDTO{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency, in.TenderCurrency, in.ChangeCurrency); err != nil {
			return PaymentQuoteDTO{}, err
		}
		q, err := app.Customers.QuotePayment(ctx, customerID, in.cash(shop))
		if err != nil {
			return PaymentQuoteDTO{}, err
		}
		v, err := newDebtView(ctx, app)
		return PaymentQuoteDTO{
			Currency: q.Debt.Code, TenderCurrency: q.Tender.Code, Tendered: v.money(q.TenderedMinor, q.Tender.Code),
			ChangeCurrency: q.Change.Code, Change: v.money(q.ChangeMinor, q.Change.Code), Settled: v.money(q.SettledMinor, q.Debt.Code),
			BalanceBefore: v.money(q.BalanceBeforeMinor, q.Debt.Code), BalanceAfter: v.money(q.BalanceAfterMinor, q.Debt.Code),
			All: q.All, Rate: rateText(v.shop, q.RateNano), Token: q.Token,
		}, err
	})
}

// entryAct runs a debt-book act and formats the entry it wrote.
func (c *Customers) entryAct(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Entry, error)) envelope.Result[EntryDTO] {
	return call(c.core, method, func(ctx context.Context, app *bootstrap.App) (EntryDTO, error) {
		e, err := fn(ctx, app)
		if err != nil {
			return EntryDTO{}, err
		}
		v, err := newDebtView(ctx, app)
		return v.entry(e, false), err
	})
}

// RecordPayment records a repayment if its quote still stands; otherwise lite.customers.payment_stale.
func (c *Customers) RecordPayment(in PaymentInput) envelope.Result[EntryDTO] {
	return c.entryAct("Customers.RecordPayment", func(ctx context.Context, app *bootstrap.App) (domain.Entry, error) {
		customerID, err := parseCustomerID(in.CustomerID)
		if err != nil {
			return domain.Entry{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency, in.TenderCurrency, in.ChangeCurrency); err != nil {
			return domain.Entry{}, err
		}
		return app.Customers.RecordPayment(ctx, customers.PaymentInput{CustomerID: customerID, Cash: in.cash(shop), Note: in.Note, Token: in.Token})
	})
}

// Opening enters a debt from the paper book. Owner only.
func (c *Customers) Opening(in DebtAmountInput) envelope.Result[EntryDTO] {
	return c.entryAct("Customers.Opening", func(ctx context.Context, app *bootstrap.App) (domain.Entry, error) {
		customerID, err := parseCustomerID(in.CustomerID)
		if err != nil {
			return domain.Entry{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency); err != nil {
			return domain.Entry{}, err
		}
		return app.Customers.Opening(ctx, customers.AmountInput{CustomerID: customerID, Currency: in.Currency,
			Amount: shop.Base(in.Amount, in.Currency), Note: in.Note})
	})
}

// WriteOff forgives a debt, whole or part. Owner only; a reason is required.
func (c *Customers) WriteOff(in DebtAmountInput) envelope.Result[EntryDTO] {
	return c.entryAct("Customers.WriteOff", func(ctx context.Context, app *bootstrap.App) (domain.Entry, error) {
		customerID, err := parseCustomerID(in.CustomerID)
		if err != nil {
			return domain.Entry{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency); err != nil {
			return domain.Entry{}, err
		}
		return app.Customers.WriteOff(ctx, customers.AmountInput{CustomerID: customerID, Currency: in.Currency,
			Amount: shop.Base(in.Amount, in.Currency), All: in.All, Note: in.Note})
	})
}

// Refund pays back a balance below zero. Owner only; a reason is required.
func (c *Customers) Refund(in RefundInput) envelope.Result[EntryDTO] {
	return c.entryAct("Customers.Refund", func(ctx context.Context, app *bootstrap.App) (domain.Entry, error) {
		customerID, err := parseCustomerID(in.CustomerID)
		if err != nil {
			return domain.Entry{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency, in.TenderCurrency); err != nil {
			return domain.Entry{}, err
		}
		return app.Customers.Refund(ctx, customers.RefundInput{CustomerID: customerID, Reason: in.Reason,
			Cash: domain.CashInput{Currency: in.Currency, TenderCurrency: in.TenderCurrency,
				Amount: shop.Base(in.Amount, tenderOrDebt(in.TenderCurrency, in.Currency)), All: in.All}})
	})
}

// Reverse reverses an opening, payment, write-off or refund, once. Owner only; a reason is required.
func (c *Customers) Reverse(in ReverseEntryInput) envelope.Result[EntryDTO] {
	return c.entryAct("Customers.Reverse", func(ctx context.Context, app *bootstrap.App) (domain.Entry, error) {
		entryID, err := id.Parse(in.EntryID)
		if err != nil {
			return domain.Entry{}, errNoEntry()
		}
		return app.Customers.Reverse(ctx, entryID, in.Reason)
	})
}

func errNoEntry() error { return errs.NotFound(domain.CodeEntryNotFound, "no such entry") }
