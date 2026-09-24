// Package customers is Mizan Lite's debt book: customers, credit charges from the till, repayments in either currency,
// and the owner's corrections (L5).
package customers

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// The acts only the owner may do (L5 §7.5). Their codes are what the owner's history records.
const (
	ActOpening         = "customers.opening"
	ActWriteOff        = "customers.write_off"
	ActRefund          = "customers.refund"
	ActReverse         = "customers.reverse"
	ActDeactivateOwing = "customers.deactivate_owing"
)

// CodeOwnerRequired is the owner module's refusal, for the guarded read; a bootstrap test holds it equal.
const CodeOwnerRequired = "lite.owner.required"

// Store persists customers and the debt book. infra/sqlite implements it; customerstest.Fake implements it in memory,
// and one contract suite runs against both.
type Store interface {
	InsertCustomer(ctx context.Context, c domain.Customer) error
	// UpdateCustomer writes c if the stored row is still at c.RowVersion, and increments the version.
	UpdateCustomer(ctx context.Context, c domain.Customer) (domain.Customer, error)
	// Customer returns a customer, or domain.ErrNotFound.
	Customer(ctx context.Context, customerID id.ID) (domain.Customer, error)
	CustomerByNameKey(ctx context.Context, key string) (domain.Customer, bool, error)
	Search(ctx context.Context, q Query) ([]domain.Customer, error)

	// Newest is the newest place of one chain; the zero Place for a chain with no entries.
	Newest(ctx context.Context, customerID id.ID, currency string) (domain.Place, error)
	InsertEntry(ctx context.Context, e domain.Entry) error
	// Entry returns an entry, or CodeEntryNotFound.
	Entry(ctx context.Context, entryID id.ID) (domain.Entry, error)
	ChargeOf(ctx context.Context, saleID id.ID) (domain.Entry, bool, error)
	ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error)
	// Chain returns one chain's entries in place order.
	Chain(ctx context.Context, customerID id.ID, currency string) ([]domain.Entry, error)
	// ChainCurrencies lists the currencies a customer has entries in.
	ChainCurrencies(ctx context.Context, customerID id.ID) ([]string, error)
	// Owing lists the customers whose newest place in some currency is not zero.
	Owing(ctx context.Context) ([]id.ID, error)
	// Day returns the entries of a business date.
	Day(ctx context.Context, businessDate string) ([]domain.Entry, error)
	// Each calls fn with every entry, by customer, currency and place.
	Each(ctx context.Context, fn func(domain.Entry) error) error
}

// Query is a customer search.
type Query struct {
	// Text is a normalised name fragment; Phone a digits fragment. Either matching is a match; both empty lists all.
	Text            string
	Phone           string
	IncludeInactive bool
	Limit           int
}

// DefaultLimit bounds a search.
const DefaultLimit = 500

// Rates is what the debt book needs of the exchange rate.
type Rates interface {
	InForce(ctx context.Context) (rateID id.ID, localPerUSDNano int64, localCurrency string, found bool, err error)
}

// Settings is what the debt book needs of settings.
type Settings interface {
	CashNote(ctx context.Context) (int64, error)
}

// Currencies is what the debt book needs of the catalogue: the currencies and their decimals.
type Currencies interface {
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// OwnerGate is what the debt book needs of the owner.
type OwnerGate interface {
	Require(ctx context.Context, act GuardedAct) error
	Allowed(ctx context.Context) bool
}

// GuardedAct describes an owner-only act, for the owner's history.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Transactor runs fn atomically.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the debt book.
type Service struct {
	tx         Transactor
	store      Store
	rates      Rates
	settings   Settings
	currencies Currencies
	gate       OwnerGate
	clk        clock.Clock
	loc        *time.Location
	newID      func() (id.ID, error)
	vouchers   Vouchers
}

// NewService builds the service.
func NewService(tx Transactor, store Store, rates Rates, settings Settings, currencies Currencies, gate OwnerGate,
	clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, rates: rates, settings: settings, currencies: currencies, gate: gate, clk: clk, loc: loc, newID: id.New}
}

// ─── customers ───────────────────────────────────────────────────────────────

// Create adds a customer. Anyone may (L5 §3).
func (s *Service) Create(ctx context.Context, d domain.Draft) (domain.Customer, error) {
	var out domain.Customer
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		customerID, err := s.newID()
		if err != nil {
			return err
		}
		c, err := domain.NewCustomer(customerID, d)
		if err != nil {
			return err
		}
		if err = s.unique(ctx, c); err != nil {
			return err
		}
		c.CreatedAt = s.clk.Now().UTC().Truncate(time.Millisecond)
		if err = s.store.InsertCustomer(ctx, c); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

// UpdateInput edits a customer at a version.
type UpdateInput struct {
	ID         id.ID
	RowVersion int64
	Draft      domain.Draft
}

// Update edits a customer's name, phone and note. Anyone may.
func (s *Service) Update(ctx context.Context, in UpdateInput) (domain.Customer, error) {
	var out domain.Customer
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.at(ctx, in.ID, in.RowVersion)
		if err != nil {
			return err
		}
		next, err := current.Edit(in.Draft)
		if err != nil {
			return err
		}
		if err = s.unique(ctx, next); err != nil {
			return err
		}
		out, err = s.store.UpdateCustomer(ctx, next)
		return err
	})
	return out, err
}

// SetActive activates or deactivates a customer. Deactivating one who owes — or is owed — needs the owner: it hides a
// balance from the till's picker (L5 §3).
func (s *Service) SetActive(ctx context.Context, customerID id.ID, rowVersion int64, active bool) (domain.Customer, error) {
	var out domain.Customer
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.at(ctx, customerID, rowVersion)
		if err != nil {
			return err
		}
		if current.Active == active {
			out = current
			return nil
		}
		if !active {
			var summaries []domain.Summary
			if summaries, err = s.summaries(ctx, customerID); err != nil {
				return err
			}
			var owing []string
			for _, sum := range summaries {
				if sum.BalanceMinor != 0 {
					owing = append(owing, sum.Currency)
				}
			}
			if len(owing) > 0 {
				if err = s.gate.Require(ctx, GuardedAct{Action: ActDeactivateOwing, SubjectID: customerID, Before: current.Name, After: strings.Join(owing, ", ")}); err != nil {
					return err
				}
			}
		}
		current.Active = active
		out, err = s.store.UpdateCustomer(ctx, current)
		return err
	})
	return out, err
}

func (s *Service) at(ctx context.Context, customerID id.ID, rowVersion int64) (domain.Customer, error) {
	current, err := s.store.Customer(ctx, customerID)
	if err != nil {
		return domain.Customer{}, err
	}
	if current.RowVersion != rowVersion {
		return domain.Customer{}, domain.ErrStale()
	}
	return current, nil
}

func (s *Service) unique(ctx context.Context, c domain.Customer) error {
	existing, found, err := s.store.CustomerByNameKey(ctx, c.NameKey())
	if err != nil {
		return err
	}
	if found && existing.ID != c.ID {
		return domain.DuplicateName(existing)
	}
	return nil
}

// Customer returns one customer.
func (s *Service) Customer(ctx context.Context, customerID id.ID) (domain.Customer, error) {
	return s.store.Customer(ctx, customerID)
}

// WithBalances is a customer and what each of their chains says.
type WithBalances struct {
	Customer  domain.Customer
	Summaries []domain.Summary
}

// Owes reports a balance other than zero in some currency.
func (w WithBalances) Owes() bool {
	for _, s := range w.Summaries {
		if s.BalanceMinor != 0 {
			return true
		}
	}
	return false
}

// Search finds customers by name in any spelling, or by phone in any digits, with their balances per currency.
func (s *Service) Search(ctx context.Context, text string, owingOnly, includeInactive bool) ([]WithBalances, error) {
	phone := numinput.LatinDigits(text)
	if strings.Trim(phone, "0123456789 +-()") != "" {
		phone = ""
	}
	found, err := s.store.Search(ctx, Query{Text: textkey.Normalise(text), Phone: phone, IncludeInactive: includeInactive, Limit: DefaultLimit})
	if err != nil {
		return nil, err
	}
	out := make([]WithBalances, 0, len(found))
	for _, c := range found {
		w, err := s.withBalances(ctx, c)
		if err != nil {
			return nil, err
		}
		if owingOnly && !w.Owes() {
			continue
		}
		out = append(out, w)
	}
	return out, nil
}

// WithBalancesOf returns one customer with what each of their chains says.
func (s *Service) WithBalancesOf(ctx context.Context, customerID id.ID) (WithBalances, error) {
	c, err := s.store.Customer(ctx, customerID)
	if err != nil {
		return WithBalances{}, err
	}
	return s.withBalances(ctx, c)
}

func (s *Service) withBalances(ctx context.Context, c domain.Customer) (WithBalances, error) {
	summaries, err := s.summaries(ctx, c.ID)
	return WithBalances{Customer: c, Summaries: summaries}, err
}

// summaries reads every chain a customer has, local currency first.
func (s *Service) summaries(ctx context.Context, customerID id.ID) ([]domain.Summary, error) {
	currencies, err := s.store.ChainCurrencies(ctx, customerID)
	if err != nil {
		return nil, err
	}
	sort.Slice(currencies, func(i, j int) bool {
		if (currencies[i] == "USD") != (currencies[j] == "USD") {
			return currencies[j] == "USD"
		}
		return currencies[i] < currencies[j]
	})
	out := make([]domain.Summary, 0, len(currencies))
	for _, cur := range currencies {
		chain, err := s.store.Chain(ctx, customerID, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.Summarise(cur, chain))
	}
	return out, nil
}

// Balances is a customer's balance in every currency they have entries in.
func (s *Service) Balances(ctx context.Context, customerID id.ID) (map[string]int64, error) {
	summaries, err := s.summaries(ctx, customerID)
	out := make(map[string]int64, len(summaries))
	for _, sum := range summaries {
		out[sum.Currency] = sum.BalanceMinor
	}
	return out, err
}

// Balance is a customer's balance in one currency, 0 for a chain with no entries.
func (s *Service) Balance(ctx context.Context, customerID id.ID, currency string) (int64, error) {
	place, err := s.store.Newest(ctx, customerID, currency)
	return place.BalanceMinor, err
}

// Statement is one chain, whole (L5 §11.2).
type Statement struct {
	Customer domain.Customer
	Currency string
	Entries  []domain.Entry
	Summary  domain.Summary
	// Reversed holds the ids of entries that have been reversed.
	Reversed map[id.ID]bool
}

// Statement returns a customer's entries in one currency with their balance after each.
func (s *Service) Statement(ctx context.Context, customerID id.ID, currency string) (Statement, error) {
	c, err := s.store.Customer(ctx, customerID)
	if err != nil {
		return Statement{}, err
	}
	chain, err := s.store.Chain(ctx, customerID, currency)
	if err != nil {
		return Statement{}, err
	}
	st := Statement{Customer: c, Currency: currency, Entries: chain, Summary: domain.Summarise(currency, chain), Reversed: map[id.ID]bool{}}
	for _, e := range chain {
		if e.Kind == domain.KindReversal {
			st.Reversed[e.ReversesID] = true
		}
	}
	return st, nil
}

// DayTotals is one currency's side of the debt book's day.
type DayTotals struct {
	Payments        int
	SettledMinor    int64 // debts settled, in this currency
	CashInMinor     int64 // handed over in this currency
	ChangeOutMinor  int64 // given back in this currency
	Refunds         int
	RefundOutMinor  int64 // paid out in this currency
	ChargedMinor    int64 // credit charged, in this currency
	WrittenOffMinor int64
}

// Outstanding is who owes what, and the day so far (L5 §4.3).
type Outstanding struct {
	BusinessDate string
	Customers    []WithBalances
	Today        map[string]*DayTotals
}

// Outstanding lists every customer with a balance other than zero, and today's debt book per currency. It never totals
// or sorts across currencies: customers come by name.
func (s *Service) Outstanding(ctx context.Context) (Outstanding, error) {
	out := Outstanding{BusinessDate: bizdate.Date(s.clk.Now(), s.loc), Today: map[string]*DayTotals{}}
	owing, err := s.store.Owing(ctx)
	if err != nil {
		return Outstanding{}, err
	}
	for _, customerID := range owing {
		w, withErr := s.WithBalancesOf(ctx, customerID)
		if withErr != nil {
			return Outstanding{}, withErr
		}
		out.Customers = append(out.Customers, w)
	}
	sort.SliceStable(out.Customers, func(i, j int) bool { return out.Customers[i].Customer.NameKey() < out.Customers[j].Customer.NameKey() })

	entries, err := s.store.Day(ctx, out.BusinessDate)
	if err != nil {
		return Outstanding{}, err
	}
	totals := func(code string) *DayTotals {
		if out.Today[code] == nil {
			out.Today[code] = &DayTotals{}
		}
		return out.Today[code]
	}
	for _, e := range entries {
		switch e.Kind {
		case domain.KindPayment:
			totals(e.Currency).Payments++
			totals(e.Currency).SettledMinor += -e.AmountMinor
			totals(e.Cash.TenderedCurrency).CashInMinor += e.Cash.TenderedMinor
			totals(e.Cash.ChangeCurrency).ChangeOutMinor += e.Cash.ChangeMinor
		case domain.KindRefund:
			totals(e.Currency).Refunds++
			totals(e.Cash.TenderedCurrency).RefundOutMinor += e.Cash.TenderedMinor
		case domain.KindCharge:
			totals(e.Currency).ChargedMinor += e.AmountMinor
		case domain.KindWriteOff:
			totals(e.Currency).WrittenOffMinor += -e.AmountMinor
		}
	}
	return out, nil
}

// ─── money ───────────────────────────────────────────────────────────────────

// cashContext reads, in one moment, what a payment or refund on one chain is worked out against.
func (s *Service) cashContext(ctx context.Context, customerID id.ID, currency string) (domain.CashContext, domain.Customer, error) {
	c, err := s.store.Customer(ctx, customerID)
	if err != nil {
		return domain.CashContext{}, domain.Customer{}, err
	}
	rateID, nano, local, found, err := s.rates.InForce(ctx)
	if err != nil {
		return domain.CashContext{}, domain.Customer{}, err
	}
	cc := domain.CashContext{CustomerID: customerID}
	if found {
		cc.RateID, cc.RateNano = rateID, nano
	}
	if cc.Local, cc.USD, err = s.pair(ctx, local); err != nil {
		return domain.CashContext{}, domain.Customer{}, err
	}
	if cc.CashNote, err = s.settings.CashNote(ctx); err != nil {
		return domain.CashContext{}, domain.Customer{}, err
	}
	if currency == cc.Local.Code || currency == cc.USD.Code {
		if cc.Place, err = s.store.Newest(ctx, customerID, currency); err != nil {
			return domain.CashContext{}, domain.Customer{}, err
		}
	}
	return cc, c, nil
}

// pair returns the local currency and dollars with their decimals.
func (s *Service) pair(ctx context.Context, local string) (domain.Currency, domain.Currency, error) {
	currencies, err := s.currencies.Currencies(ctx)
	if err != nil {
		return domain.Currency{}, domain.Currency{}, err
	}
	var l, u domain.Currency
	for _, c := range currencies {
		switch c.Code {
		case local:
			l = c
		case "USD":
			u = c
		}
	}
	if l.Code == "" || u.Code == "" {
		return l, u, errs.Internal(domain.CodeUnknownCurrency, "the local currency or USD is missing").WithParam("local", local)
	}
	return l, u, nil
}

// PaymentInput is a repayment as typed, and the token of the quote the cashier saw.
type PaymentInput struct {
	CustomerID id.ID
	Cash       domain.CashInput
	Note       string
	Token      string
}

// QuotePayment works out a repayment and writes nothing.
func (s *Service) QuotePayment(ctx context.Context, customerID id.ID, in domain.CashInput) (domain.CashQuote, error) {
	cc, _, err := s.cashContext(ctx, customerID, in.Currency)
	if err != nil {
		return domain.CashQuote{}, err
	}
	return domain.PricePayment(in, cc)
}

// RecordPayment records a repayment if its quote still stands (L5 §6.4). Anyone may: money coming in.
func (s *Service) RecordPayment(ctx context.Context, in PaymentInput) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		cc, c, err := s.cashContext(ctx, in.CustomerID, in.Cash.Currency)
		if err != nil {
			return err
		}
		q, err := domain.PricePayment(in.Cash, cc)
		if err != nil {
			return err
		}
		if q.Token != in.Token {
			return errs.Conflict(domain.CodePaymentStale, "the rate or the balance changed since the quote")
		}
		note, err := domain.Note(in.Note)
		if err != nil {
			return err
		}
		if out, err = s.append(ctx, q.Entry(c.ID, c.Name, note), cc.Place); err != nil {
			return err
		}
		return s.numberVoucher(ctx, out)
	})
	return out, err
}

// RefundInput is paying a customer back what the shop owes them.
type RefundInput struct {
	CustomerID id.ID
	Cash       domain.CashInput
	Reason     string
}

// Refund pays back a balance below zero. Owner only; a reason is required (L5 §7.3).
func (s *Service) Refund(ctx context.Context, in RefundInput) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		cc, c, err := s.cashContext(ctx, in.CustomerID, in.Cash.Currency)
		if err != nil {
			return err
		}
		q, err := domain.PriceRefund(in.Cash, cc)
		if err != nil {
			return err
		}
		reason, err := domain.Reason(in.Reason)
		if err != nil {
			return err
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActRefund, SubjectID: c.ID, Before: c.Name,
			After: money(q.SettledMinor, q.Debt) + " · " + reason}); err != nil {
			return err
		}
		if out, err = s.append(ctx, q.Entry(c.ID, c.Name, reason), cc.Place); err != nil {
			return err
		}
		return s.numberVoucher(ctx, out)
	})
	return out, err
}

// AmountInput is an opening balance or a write-off.
type AmountInput struct {
	CustomerID id.ID
	Currency   string
	Amount     string
	// All writes off the whole balance.
	All  bool
	Note string
}

// Opening enters a debt from the paper book. Owner only (L5 §7.1).
func (s *Service) Opening(ctx context.Context, in AmountInput) (domain.Entry, error) {
	return s.amountAct(ctx, in, domain.KindOpening)
}

// WriteOff forgives a debt, whole or part. Owner only; a reason is required (L5 §7.2, Q-L5.7).
func (s *Service) WriteOff(ctx context.Context, in AmountInput) (domain.Entry, error) {
	return s.amountAct(ctx, in, domain.KindWriteOff)
}

func (s *Service) amountAct(ctx context.Context, in AmountInput, kind domain.Kind) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		cc, c, err := s.cashContext(ctx, in.CustomerID, in.Currency)
		if err != nil {
			return err
		}
		cur, err := currencyOf(cc, in.Currency)
		if err != nil {
			return err
		}
		var amount int64
		if kind == domain.KindWriteOff && in.All {
			amount = cc.Place.BalanceMinor
			if amount <= 0 {
				return errs.Conflict(domain.CodeNothingOwed, "nothing is owed in this currency")
			}
		} else if amount, err = domain.ParseAmount(in.Amount, cur, domain.FieldAmount); err != nil {
			return err
		}
		e := domain.Entry{CustomerID: c.ID, Currency: cur.Code, Kind: kind, AmountMinor: amount, CustomerName: c.Name}
		action := ActOpening
		if kind == domain.KindWriteOff {
			e.AmountMinor, action = -amount, ActWriteOff
			if e.Note, err = domain.Reason(in.Note); err != nil {
				return err
			}
		} else if e.Note, err = domain.Note(in.Note); err != nil {
			return err
		}
		// The chain's rules first, so a refusal is the rule and not the PIN dialog (L2 D-L2.i: decided before asked).
		if _, err = cc.Place.Append(e); err != nil {
			return err
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: action, SubjectID: c.ID, Before: c.Name, After: money(amount, cur) + " " + e.Note}); err != nil {
			return err
		}
		out, err = s.append(ctx, e, cc.Place)
		return err
	})
	return out, err
}

func currencyOf(cc domain.CashContext, code string) (domain.Currency, error) {
	switch code {
	case cc.Local.Code:
		return cc.Local, nil
	case cc.USD.Code:
		return cc.USD, nil
	}
	return domain.Currency{}, errs.Validation(domain.CodeUnknownCurrency, "the local currency or USD").WithParam("value", code)
}

// Reverse reverses an opening, payment, write-off or refund, once. Owner only; a reason is required (L5 §7.4).
func (s *Service) Reverse(ctx context.Context, entryID id.ID, reason string) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		original, err := s.store.Entry(ctx, entryID)
		if err != nil {
			return err
		}
		out, err = s.reverse(ctx, original, reason, false)
		return err
	})
	return out, err
}

func (s *Service) reverse(ctx context.Context, original domain.Entry, reason string, bySale bool) (domain.Entry, error) {
	if _, reversed, err := s.store.ReversalOf(ctx, original.ID); err != nil || reversed {
		if err != nil {
			return domain.Entry{}, err
		}
		return domain.Entry{}, errs.Conflict(domain.CodeAlreadyReversed, "this entry was already reversed")
	}
	r, err := domain.Reverse(original, reason, bySale)
	if err != nil {
		return domain.Entry{}, err
	}
	if !bySale {
		var cur domain.Currency
		if cur, err = s.currencyDecimals(ctx, original.Currency); err != nil {
			return domain.Entry{}, err
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActReverse, SubjectID: original.CustomerID, Before: string(original.Kind) + " " + money(original.AmountMinor, cur),
			After: r.Note}); err != nil {
			return domain.Entry{}, err
		}
	}
	place, err := s.store.Newest(ctx, original.CustomerID, original.Currency)
	if err != nil {
		return domain.Entry{}, err
	}
	return s.append(ctx, r, place)
}

func (s *Service) currencyDecimals(ctx context.Context, code string) (domain.Currency, error) {
	currencies, err := s.currencies.Currencies(ctx)
	if err != nil {
		return domain.Currency{}, err
	}
	for _, c := range currencies {
		if c.Code == code {
			return c, nil
		}
	}
	return domain.Currency{}, errs.Internal(domain.CodeUnknownCurrency, "unknown currency").WithParam("value", code)
}

// append places e on its chain after place, stamps it and writes it, in the caller's transaction.
func (s *Service) append(ctx context.Context, e domain.Entry, place domain.Place) (domain.Entry, error) {
	return s.appendAt(ctx, e, place, s.clk.Now().UTC().Truncate(time.Millisecond), "")
}

func (s *Service) appendAt(ctx context.Context, e domain.Entry, place domain.Place, at time.Time, businessDate string) (domain.Entry, error) {
	placed, err := place.Append(e)
	if err != nil {
		return domain.Entry{}, err
	}
	if placed.ID, err = s.newID(); err != nil {
		return domain.Entry{}, err
	}
	placed.OccurredAt = at
	placed.BusinessDate = businessDate
	if placed.BusinessDate == "" {
		placed.BusinessDate = bizdate.Date(at, s.loc)
	}
	if err = s.store.InsertEntry(ctx, placed); err != nil {
		return domain.Entry{}, err
	}
	return placed, nil
}

// ─── dollars only (0.10.0) ───────────────────────────────────────────────────

// CodeConversionStale refuses a conversion whose balance moved since the owner was shown it.
const CodeConversionStale = "lite.customers.conversion_stale"

// LocalBalance is a customer's non-zero balance in one currency.
type LocalBalance struct {
	CustomerID   id.ID
	Name         string
	BalanceMinor int64
}

// BalancesIn lists every customer with a balance in a currency other than nought — what going over to dollars only would
// convert. Unguarded: the conversion's plan reads it, and the conversion itself is the owner's act.
func (s *Service) BalancesIn(ctx context.Context, currency string) ([]LocalBalance, error) {
	// Every customer, not a screen's worth: a balance left out would stay in pounds in a shop that counts in dollars.
	all, err := s.store.Search(ctx, Query{IncludeInactive: true, Limit: math.MaxInt32})
	if err != nil {
		return nil, err
	}
	out := []LocalBalance{}
	for _, c := range all {
		place, err := s.store.Newest(ctx, c.ID, currency)
		if err != nil {
			return nil, err
		}
		if place.BalanceMinor != 0 {
			out = append(out, LocalBalance{CustomerID: c.ID, Name: c.Name, BalanceMinor: place.BalanceMinor})
		}
	}
	return out, nil
}

// ConversionInput moves a customer's whole balance from one currency to another: FromMinor — which must be the From
// chain's balance still — off it, and ToMinor, the same sum at the rate, onto the To chain.
type ConversionInput struct {
	CustomerID id.ID
	From       string
	FromMinor  int64
	To         string
	ToMinor    int64
	Note       string
}

// Convert writes a conversion's two entries in the caller's transaction (0.10.0). The going-over-to-dollars act is the
// owner's and is recorded once by its caller; a balance too small to be a cent in dollars leaves the pound chain at nought
// and writes nothing on the dollar one.
func (s *Service) Convert(ctx context.Context, in ConversionInput) ([]domain.Entry, error) {
	var out []domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		c, err := s.store.Customer(ctx, in.CustomerID)
		if err != nil {
			return err
		}
		note, err := domain.Reason(in.Note)
		if err != nil {
			return err
		}
		from, err := s.store.Newest(ctx, c.ID, in.From)
		if err != nil {
			return err
		}
		if from.BalanceMinor != in.FromMinor || in.FromMinor == 0 {
			return errs.Conflict(CodeConversionStale, "the balance changed since the conversion was shown").WithParam("name", c.Name)
		}
		off, err := s.append(ctx, domain.Entry{CustomerID: c.ID, Currency: in.From, Kind: domain.KindConversion,
			AmountMinor: -in.FromMinor, CustomerName: c.Name, Note: note}, from)
		if err != nil {
			return err
		}
		out = append(out, off)
		if in.ToMinor == 0 {
			return nil
		}
		to, err := s.store.Newest(ctx, c.ID, in.To)
		if err != nil {
			return err
		}
		on, err := s.append(ctx, domain.Entry{CustomerID: c.ID, Currency: in.To, Kind: domain.KindConversion,
			AmountMinor: in.ToMinor, CustomerName: c.Name, Note: note}, to)
		if err != nil {
			return err
		}
		out = append(out, on)
		return nil
	})
	return out, err
}

// ─── the till's port ─────────────────────────────────────────────────────────

// ChargeInput is what a credit sale adds to a customer's debt.
type ChargeInput struct {
	CustomerID   id.ID
	SaleID       id.ID
	Currency     string
	AmountMinor  int64
	BusinessDate string
	At           time.Time
}

// Charge writes a credit sale's charge in the caller's transaction: the customer must be active (L5 §5.4). No guard.
func (s *Service) Charge(ctx context.Context, in ChargeInput) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		c, err := s.store.Customer(ctx, in.CustomerID)
		if err != nil {
			return err
		}
		if !c.Active {
			return errs.Conflict(domain.CodeInactive, "this customer is inactive").WithParam("name", c.Name)
		}
		place, err := s.store.Newest(ctx, c.ID, in.Currency)
		if err != nil {
			return err
		}
		e := domain.Entry{CustomerID: c.ID, Currency: in.Currency, Kind: domain.KindCharge, AmountMinor: in.AmountMinor, SaleID: in.SaleID, CustomerName: c.Name}
		out, err = s.appendAt(ctx, e, place, in.At, in.BusinessDate)
		return err
	})
	return out, err
}

// ReverseCharge writes the reversal of a voided credit sale's charge, in the void's transaction (L5 §5.6).
func (s *Service) ReverseCharge(ctx context.Context, saleID id.ID, reason string, at time.Time, businessDate string) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		charge, found, err := s.store.ChargeOf(ctx, saleID)
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(domain.CodeChargeNotFound, "the credit sale has no charge").WithParam("saleId", saleID.String())
		}
		var reversed bool
		if _, reversed, err = s.store.ReversalOf(ctx, charge.ID); err != nil {
			return err
		}
		if reversed {
			return errs.Conflict(domain.CodeAlreadyReversed, "this charge was already reversed")
		}
		r, err := domain.Reverse(charge, reason, true)
		if err != nil {
			return err
		}
		place, err := s.store.Newest(ctx, charge.CustomerID, charge.Currency)
		if err != nil {
			return err
		}
		out, err = s.appendAt(ctx, r, place, at, businessDate)
		return err
	})
	return out, err
}

// ChargeView is a credit sale's charge as the till shows and checks it.
type ChargeView struct {
	Charge   domain.Entry
	Reversed bool
}

// ChargeOf returns a sale's charge, or found=false.
func (s *Service) ChargeOf(ctx context.Context, saleID id.ID) (ChargeView, bool, error) {
	charge, found, err := s.store.ChargeOf(ctx, saleID)
	if err != nil || !found {
		return ChargeView{}, false, err
	}
	_, reversed, err := s.store.ReversalOf(ctx, charge.ID)
	return ChargeView{Charge: charge, Reversed: reversed}, true, err
}

// EachCharge calls fn with every charge and whether it was reversed, for the sales verifier.
func (s *Service) EachCharge(ctx context.Context, fn func(ChargeView) error) error {
	var charges []domain.Entry
	reversed := map[id.ID]bool{}
	if err := s.store.Each(ctx, func(e domain.Entry) error {
		switch e.Kind {
		case domain.KindCharge:
			charges = append(charges, e)
		case domain.KindReversal:
			reversed[e.ReversesID] = true
		}
		return nil
	}); err != nil {
		return err
	}
	for _, c := range charges {
		if err := fn(ChargeView{Charge: c, Reversed: reversed[c.ID]}); err != nil {
			return err
		}
	}
	return nil
}

// Entry is one debt book entry, for its voucher (L7 §5.1).
func (s *Service) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	return s.store.Entry(ctx, entryID)
}

// ─── vouchers ────────────────────────────────────────────────────────────────

// Vouchers numbers a payment or refund's voucher (L7 Q-L7.3) — the printing module, reached through the composition root.
type Vouchers interface {
	AssignVoucher(ctx context.Context, entryID id.ID) (int64, error)
}

// SetVouchers gives the debt book its voucher numbering. Without it entries are recorded unnumbered, as before L7.
func (s *Service) SetVouchers(v Vouchers) { s.vouchers = v }

// numberVoucher assigns the entry's number inside the transaction that recorded it, so numbers have no gaps.
func (s *Service) numberVoucher(ctx context.Context, e domain.Entry) error {
	if s.vouchers == nil {
		return nil
	}
	_, err := s.vouchers.AssignVoucher(ctx, e.ID)
	return err
}

// ─── the reports' facts ──────────────────────────────────────────────────────

// EntryFact is a debt entry as the reports read it: for a reversal, the entry it reverses — its kind for bad debts, its
// cash for the drawer.
type EntryFact struct {
	Entry    domain.Entry
	Reverses domain.Entry
}

// EntriesBetween returns the debt entries of business dates from..to (L6 §9.1). The debt book is small beside the ledgers;
// it is read whole and filtered.
func (s *Service) EntriesBetween(ctx context.Context, from, to string) ([]EntryFact, error) {
	all := map[id.ID]domain.Entry{}
	var in []domain.Entry
	err := s.store.Each(ctx, func(e domain.Entry) error {
		all[e.ID] = e
		if e.BusinessDate >= from && e.BusinessDate <= to {
			in = append(in, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]EntryFact, 0, len(in))
	for _, e := range in {
		f := EntryFact{Entry: e}
		if e.ReversesID != "" {
			f.Reverses = all[e.ReversesID]
		}
		out = append(out, f)
	}
	return out, nil
}

// ─── the verifier ────────────────────────────────────────────────────────────

// Verify checks the debt book against itself (L5 §9.1). Owner mode only.
func (s *Service) Verify(ctx context.Context) ([]domain.Finding, error) {
	if !s.gate.Allowed(ctx) {
		return nil, errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
	}
	return s.VerifyUnguarded(ctx)
}

// VerifyUnguarded is Verify for the demo seeder and tests.
func (s *Service) VerifyUnguarded(ctx context.Context) ([]domain.Finding, error) {
	var findings []domain.Finding
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		currencies, err := s.currencies.Currencies(ctx)
		if err != nil {
			return err
		}
		var entries []domain.Entry
		if err = s.store.Each(ctx, func(e domain.Entry) error { entries = append(entries, e); return nil }); err != nil {
			return err
		}
		findings = domain.Verify(entries, currencies)
		return nil
	})
	return findings, err
}

func money(minor int64, c domain.Currency) string {
	return numinput.FormatFixed(minor, c.Decimals, c.Decimals) + " " + c.Code
}

// ─── the till's return port ──────────────────────────────────────────────────

// ReturnInput is goods handed back, taken off what a customer owes (2026-09-20).
type ReturnInput struct {
	CustomerID   id.ID
	Currency     string
	AmountMinor  int64
	Reason       string
	BusinessDate string
	At           time.Time
}

// RecordSaleReturn writes a return against a customer's balance in the CALLER's transaction, and returns the entry's
// id so the return document can name it.
//
// Unguarded, like Charge: the sales module has already asked the owner for the return as a whole, and asking twice for
// one act would mean two rows in the owner's history for one thing that happened.
func (s *Service) RecordSaleReturn(ctx context.Context, in ReturnInput) (domain.Entry, error) {
	cc, c, err := s.cashContext(ctx, in.CustomerID, in.Currency)
	if err != nil {
		return domain.Entry{}, err
	}
	if _, err = currencyOf(cc, in.Currency); err != nil {
		return domain.Entry{}, err
	}
	reason, err := domain.Reason(in.Reason)
	if err != nil {
		return domain.Entry{}, err
	}
	return s.appendAt(ctx, domain.Entry{
		CustomerID: c.ID, Currency: in.Currency, Kind: domain.KindSaleReturn,
		AmountMinor: -in.AmountMinor, CustomerName: c.Name, Note: reason,
	}, cc.Place, in.At, in.BusinessDate)
}
