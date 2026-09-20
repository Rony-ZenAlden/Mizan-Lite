// Package sales is Mizan Lite's till: quotes, checkout, receipts, voids and the day's sales (L4).
package sales

import (
	"context"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// The acts only the owner may do. Their codes are what the owner's history records.
const (
	ActVoid     = "sales.sale.void"
	ActDiscount = "sales.discount"
	ActCashNote = "sales.cash_note.set"
)

// CodeOwnerRequired is the owner module's refusal, returned by a guarded read outside owner mode; a bootstrap test
// holds it equal to the owner module's.
const CodeOwnerRequired = "lite.owner.required"

// CodeInvalidDate refuses a business date that is not YYYY-MM-DD.
const CodeInvalidDate = "lite.sales.invalid_date"

// Store persists sales. infra/sqlite implements it; salestest.Fake implements it in memory, and one contract suite runs
// against both.
type Store interface {
	// NextReceiptNo is one more than the highest receipt number, or 1.
	NextReceiptNo(ctx context.Context) (int64, error)
	// Insert writes a sale and its lines.
	Insert(ctx context.Context, s domain.Sale) error
	// Get returns a sale with its lines, or domain.ErrNotFound.
	Get(ctx context.Context, saleID id.ID) (domain.Sale, error)
	// Day returns, with their lines, the sales sold on a business date and those voided on it, by receipt number.
	Day(ctx context.Context, businessDate string) ([]domain.Sale, error)
	// Range returns, with their lines, the sales sold or voided on a business date from..to inclusive, by receipt number.
	Range(ctx context.Context, from, to string) ([]domain.Sale, error)
	// Void writes a sale's void if the stored sale is still posted at its row version.
	Void(ctx context.Context, s domain.Sale) (domain.Sale, error)
	// Each calls fn with every sale and its lines, by receipt number.
	Each(ctx context.Context, fn func(domain.Sale) error) error
}

// Catalogue is what the till needs from the product catalogue.
type Catalogue interface {
	// Product returns a product, or the catalogue's NotFound.
	Product(ctx context.Context, productID id.ID) (domain.Product, error)
	// ByBarcode returns the product with a barcode as scanned, digits in any script, or found=false.
	ByBarcode(ctx context.Context, code string) (domain.Product, bool, error)
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// StockLine is one sold line's stock.
type StockLine struct {
	ProductID     id.ID
	QuantityMicro int64
	SaleID        id.ID
	SaleLineID    id.ID
}

// Stock is what the till needs from the stock module.
type Stock interface {
	Stocked(ctx context.Context, productID id.ID) (domain.Stocked, error)
	// RecordSale and RecordSaleVoid join the caller's transaction.
	RecordSale(ctx context.Context, line StockLine) error
	RecordSaleVoid(ctx context.Context, saleLineID id.ID) error
	EachSaleMovement(ctx context.Context, fn func(domain.StockMovement) error) error
}

// ChargeInput is what a credit sale adds to its customer's debt.
type ChargeInput struct {
	CustomerID   id.ID
	SaleID       id.ID
	Currency     string
	AmountMinor  int64
	BusinessDate string
	At           time.Time
}

// Debts is what the till needs from the debt book (L5 §10.1).
type Debts interface {
	// Customer returns a customer, or found=false.
	Customer(ctx context.Context, customerID id.ID) (domain.Customer, bool, error)
	// Balances is a customer's balance per currency.
	Balances(ctx context.Context, customerID id.ID) (map[string]int64, error)
	// Charge and ReverseCharge join the caller's transaction.
	Charge(ctx context.Context, in ChargeInput) (domain.Credit, error)
	ReverseCharge(ctx context.Context, saleID id.ID, reason string, at time.Time, businessDate string) error
	// CreditOf returns a sale's charge as the receipt shows it, or found=false.
	CreditOf(ctx context.Context, saleID id.ID) (domain.Credit, bool, error)
	EachCharge(ctx context.Context, fn func(domain.Charge) error) error
}

// Rates is what the till needs from the exchange-rate module: the rate in force and the local currency's code.
type Rates interface {
	InForce(ctx context.Context) (rate domain.Rate, localCurrency string, found bool, err error)
}

// Settings is what the till needs from settings.
type Settings interface {
	ShopName(ctx context.Context) (string, error)
	CashNote(ctx context.Context) (int64, error)
	SetCashNote(ctx context.Context, raw string) (int64, error)
}

// OwnerGate is what the till needs from the owner.
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

// Service is the till.
type Service struct {
	tx        Transactor
	store     Store
	catalogue Catalogue
	stock     Stock
	debts     Debts
	rates     Rates
	settings  Settings
	gate      OwnerGate
	clk       clock.Clock
	loc       *time.Location
	newID     func() (id.ID, error)

	// Returns are wired after construction (UseReturns), so the module's existing fakes and its store contract suite
	// keep compiling where returns are not in play.
	returns     ReturnStore
	returnStock ReturnStock
	returnDebts ReturnDebts
}

// NewService builds the service.
func NewService(tx Transactor, store Store, catalogue Catalogue, stock Stock, debts Debts, rates Rates, settings Settings, gate OwnerGate,
	clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, catalogue: catalogue, stock: stock, debts: debts, rates: rates, settings: settings, gate: gate,
		clk: clk, loc: loc, newID: id.New}
}

// Scanned is a product found by its barcode, with what the till warns about.
type Scanned struct {
	Product domain.Product
	Stocked domain.Stocked
}

// Scan finds a product by barcode — digits typed in any script, as a scanner with an Arabic layout delivers them.
func (s *Service) Scan(ctx context.Context, code string) (Scanned, bool, error) {
	p, found, err := s.catalogue.ByBarcode(ctx, code)
	if err != nil || !found {
		return Scanned{}, false, err
	}
	stocked, err := s.stock.Stocked(ctx, p.ID)
	return Scanned{Product: p, Stocked: stocked}, true, err
}

// Quote prices a cart. It writes nothing.
func (s *Service) Quote(ctx context.Context, in domain.CartInput) (domain.Quote, error) {
	c, err := s.context(ctx, in)
	if err != nil {
		return domain.Quote{}, err
	}
	return domain.Price(in, c)
}

// CheckoutInput is a cart — how it is paid included — and the token of the quote the cashier saw.
type CheckoutInput struct {
	Cart  domain.CartInput
	Token string
}

// Checkout records a sale, moves its stock, charges a credit sale's customer and issues its receipt, atomically (L4 §4,
// L5 §5.4). A discount needs the owner.
func (s *Service) Checkout(ctx context.Context, in CheckoutInput) (domain.Sale, error) {
	var out domain.Sale
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		c, err := s.context(ctx, in.Cart)
		if err != nil {
			return err
		}
		q, err := domain.Price(in.Cart, c)
		if err != nil {
			return err
		}
		if q.Token != in.Token {
			return errs.Conflict(domain.CodeQuoteStale, "the rate or a price changed since the quote")
		}
		if q.NeedsCustomer {
			return errs.Validation(domain.CodeCustomerRequired, "a credit sale needs a customer").
				WithField(domain.FieldCustomer, domain.CodeCustomerRequired, "required")
		}
		if q.Discounted {
			if err = s.gate.Require(ctx, GuardedAct{Action: ActDiscount, Before: grossText(q), After: moneyText(q.TotalMinor, q.Settlement)}); err != nil {
				return err
			}
		}
		st, err := s.stamp(ctx, len(q.Lines))
		if err != nil {
			return err
		}
		sale := domain.Assemble(q, c.Local, st)
		if err = s.store.Insert(ctx, sale); err != nil {
			return err
		}
		for _, l := range sale.Lines {
			if err = s.stock.RecordSale(ctx, StockLine{ProductID: l.ProductID, QuantityMicro: l.QuantityMicro, SaleID: sale.ID, SaleLineID: l.ID}); err != nil {
				return err
			}
		}
		if sale.Payment == domain.PaymentCredit {
			if sale.Credit, err = s.debts.Charge(ctx, ChargeInput{CustomerID: q.Customer.ID, SaleID: sale.ID, Currency: q.Settlement.Code,
				AmountMinor: q.DebtMinor, BusinessDate: sale.BusinessDate, At: sale.SoldAt}); err != nil {
				return err
			}
		}
		out = sale
		return nil
	})
	return out, err
}

func (s *Service) stamp(ctx context.Context, lines int) (domain.Stamp, error) {
	receiptNo, err := s.store.NextReceiptNo(ctx)
	if err != nil {
		return domain.Stamp{}, err
	}
	shop, err := s.settings.ShopName(ctx)
	if err != nil {
		return domain.Stamp{}, err
	}
	saleID, err := s.newID()
	if err != nil {
		return domain.Stamp{}, err
	}
	st := domain.Stamp{SaleID: saleID, ReceiptNo: receiptNo, ShopName: shop}
	for range lines {
		lineID, err := s.newID()
		if err != nil {
			return domain.Stamp{}, err
		}
		st.LineIDs = append(st.LineIDs, lineID)
	}
	st.SoldAt = s.clk.Now().UTC().Truncate(time.Millisecond)
	st.BusinessDate = bizdate.Date(st.SoldAt, s.loc)
	return st, nil
}

// context reads, in one moment, everything a cart is priced against.
func (s *Service) context(ctx context.Context, in domain.CartInput) (domain.Context, error) {
	rate, local, found, err := s.rates.InForce(ctx)
	if err != nil {
		return domain.Context{}, err
	}
	if !found {
		return domain.Context{}, errs.Conflict(domain.CodeNoRate, "set an exchange rate before selling")
	}
	currencies, err := s.catalogue.Currencies(ctx)
	if err != nil {
		return domain.Context{}, err
	}
	c := domain.Context{Rate: rate, Products: map[id.ID]domain.Product{}, Stock: map[id.ID]domain.Stocked{}}
	for _, cur := range currencies {
		switch cur.Code {
		case local:
			c.Local = cur
		case domain.USD:
			c.USD = cur
		}
	}
	if c.Local.Code == "" || c.USD.Code == "" {
		return domain.Context{}, errs.Internal(domain.CodeUnknownCurrency, "the local currency or USD is missing").WithParam("local", local)
	}
	if c.CashNote, err = s.settings.CashNote(ctx); err != nil {
		return domain.Context{}, err
	}
	for _, l := range in.Lines {
		if _, seen := c.Products[l.ProductID]; seen {
			continue
		}
		p, err := s.catalogue.Product(ctx, l.ProductID)
		if errs.IsCategory(err, errs.CategoryNotFound) {
			continue // the domain refuses it, naming the line
		}
		if err != nil {
			return domain.Context{}, err
		}
		c.Products[p.ID] = p
		if c.Stock[p.ID], err = s.stock.Stocked(ctx, p.ID); err != nil {
			return domain.Context{}, err
		}
	}
	if in.Payment == domain.PaymentCredit && in.CustomerID != "" {
		customer, found, err := s.debts.Customer(ctx, in.CustomerID)
		if err != nil {
			return domain.Context{}, err
		}
		if found {
			c.Customer = customer
			if c.Balances, err = s.debts.Balances(ctx, customer.ID); err != nil {
				return domain.Context{}, err
			}
		}
	}
	return c, nil
}

// Day is the sales of one business date and what came in, per currency (L4 §10.3).
type Day struct {
	BusinessDate string
	Sales        []domain.Sale
	Totals       map[string]*CurrencyTotals
}

// CurrencyTotals is one currency's side of a day.
//
// A void belongs to the day it is made (§8.1, D-L4.14): a day's sales stay what the till charged that day even when one is
// voided later, and the void is counted on its own day. So a closed day's totals never change, and charged less voided
// is the day's takings.
type CurrencyTotals struct {
	// Sales and ChargedMinor count every sale rung up that day, by settlement currency, whatever happened to it since.
	Sales        int
	ChargedMinor int64
	// CashInMinor and ChangeOutMinor are what the drawer took and gave back for every sale made that day.
	CashInMinor    int64
	ChangeOutMinor int64
	// Voids and VoidedMinor count the voids made that day and the value they took back, by settlement currency, whenever
	// the sale was — a sales figure, not cash (L6 H9, A-L6.6).
	Voids       int
	VoidedMinor int64
	// OnCreditMinor is what the day's credit sales added to debts, in the currency they were charged in (L5 §5.7).
	OnCreditMinor int64
}

// Day returns the sales of a business date, today when empty.
func (s *Service) Day(ctx context.Context, businessDate string) (Day, error) {
	if businessDate == "" {
		businessDate = bizdate.Date(s.clk.Now(), s.loc)
	}
	if _, err := time.Parse(bizdate.Layout, businessDate); err != nil {
		return Day{}, errs.Validation(CodeInvalidDate, "a date is YYYY-MM-DD").WithParam("value", businessDate)
	}
	sales, err := s.store.Day(ctx, businessDate)
	if err != nil {
		return Day{}, err
	}
	d := Day{BusinessDate: businessDate, Sales: sales, Totals: map[string]*CurrencyTotals{}}
	totals := func(code string) *CurrencyTotals {
		if d.Totals[code] == nil {
			d.Totals[code] = &CurrencyTotals{}
		}
		return d.Totals[code]
	}
	for i, sale := range sales {
		if sale.Payment == domain.PaymentCredit {
			if sales[i], err = s.withCredit(ctx, sale); err != nil {
				return Day{}, err
			}
			sale = sales[i]
		}
		if sale.BusinessDate == businessDate {
			totals(sale.TenderedCurrency).CashInMinor += sale.TenderedMinor
			totals(sale.ChangeCurrency).ChangeOutMinor += sale.ChangeMinor
			t := totals(sale.SettlementCurrency)
			t.Sales++
			t.ChargedMinor += sale.TotalMinor
			t.OnCreditMinor += sale.Credit.AmountMinor
		}
		if sale.Status == domain.StatusVoided && sale.VoidBusinessDate == businessDate {
			t := totals(sale.SettlementCurrency)
			t.Voids++
			t.VoidedMinor += sale.TotalMinor
		}
	}
	return d, nil
}

// Facts returns the sales sold or voided on business dates from..to, each credit sale with its charge — what the
// reports read (L6 §9.1). It writes nothing.
func (s *Service) Facts(ctx context.Context, from, to string) ([]domain.Sale, error) {
	sales, err := s.store.Range(ctx, from, to)
	if err != nil {
		return nil, err
	}
	for i, sale := range sales {
		if sale.Payment == domain.PaymentCredit {
			if sales[i], err = s.withCredit(ctx, sale); err != nil {
				return nil, err
			}
		}
	}
	return sales, nil
}

// Receipt returns a sale as it was recorded — a credit sale with its charge from the debt book.
func (s *Service) Receipt(ctx context.Context, saleID id.ID) (domain.Sale, error) {
	sale, err := s.store.Get(ctx, saleID)
	if err != nil || sale.Payment != domain.PaymentCredit {
		return sale, err
	}
	return s.withCredit(ctx, sale)
}

func (s *Service) withCredit(ctx context.Context, sale domain.Sale) (domain.Sale, error) {
	credit, found, err := s.debts.CreditOf(ctx, sale.ID)
	if err != nil {
		return domain.Sale{}, err
	}
	if found {
		sale.Credit = credit
	}
	return sale, nil
}

// VoidInput is a void as asked for.
type VoidInput struct {
	SaleID id.ID
	Reason string
}

// Void voids a whole sale and returns its stock (L4 §8.1). Owner only; the reason is required.
func (s *Service) Void(ctx context.Context, in VoidInput) (domain.Sale, error) {
	var out domain.Sale
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		sale, err := s.store.Get(ctx, in.SaleID)
		if err != nil {
			return err
		}
		at := s.clk.Now().UTC().Truncate(time.Millisecond)
		voided, err := sale.Void(at, bizdate.Date(at, s.loc), in.Reason)
		if err != nil {
			return err
		}
		cur, err := s.currency(ctx, sale.SettlementCurrency)
		if err != nil {
			return err
		}
		err = s.gate.Require(ctx, GuardedAct{
			Action: ActVoid, SubjectID: sale.ID,
			Before: "No. " + strconv.FormatInt(sale.ReceiptNo, 10) + " · " + moneyText(sale.TotalMinor, cur), After: voided.VoidReason,
		})
		if err != nil {
			return err
		}
		if out, err = s.store.Void(ctx, voided); err != nil {
			return err
		}
		for _, l := range sale.Lines {
			if err = s.stock.RecordSaleVoid(ctx, l.ID); err != nil {
				return err
			}
		}
		if sale.Payment == domain.PaymentCredit {
			if err = s.debts.ReverseCharge(ctx, sale.ID, voided.VoidReason, at, voided.VoidBusinessDate); err != nil {
				return err
			}
			out, err = s.withCredit(ctx, out)
		}
		return err
	})
	return out, err
}

// CashNote is the smallest local note totals round to.
func (s *Service) CashNote(ctx context.Context) (int64, error) { return s.settings.CashNote(ctx) }

// SetCashNote changes the note. Owner only: it changes what every customer is charged.
func (s *Service) SetCashNote(ctx context.Context, raw string) (int64, error) {
	var out int64
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.settings.CashNote(ctx)
		if err != nil {
			return err
		}
		normalised := numinput.LatinDigits(raw)
		if normalised == strconv.FormatInt(current, 10) {
			out = current
			return nil
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActCashNote, Before: strconv.FormatInt(current, 10), After: normalised}); err != nil {
			return err
		}
		out, err = s.settings.SetCashNote(ctx, normalised)
		return err
	})
	return out, err
}

// Verify checks every sale against itself and its stock (L4 §8.3). Owner mode only.
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
		_, local, _, err := s.rates.InForce(ctx)
		if err != nil {
			return err
		}
		currencies, err := s.catalogue.Currencies(ctx)
		if err != nil {
			return err
		}
		var localCur, usdCur domain.Currency
		for _, c := range currencies {
			switch c.Code {
			case local:
				localCur = c
			case domain.USD:
				usdCur = c
			}
		}
		var sales []domain.Sale
		if err = s.store.Each(ctx, func(sale domain.Sale) error { sales = append(sales, sale); return nil }); err != nil {
			return err
		}
		var moves []domain.StockMovement
		if err = s.stock.EachSaleMovement(ctx, func(m domain.StockMovement) error { moves = append(moves, m); return nil }); err != nil {
			return err
		}
		var charges []domain.Charge
		if err = s.debts.EachCharge(ctx, func(ch domain.Charge) error { charges = append(charges, ch); return nil }); err != nil {
			return err
		}
		findings = domain.Verify(sales, moves, charges, localCur, usdCur)
		return nil
	})
	return findings, err
}

// currency returns a currency's decimals from the catalogue.
func (s *Service) currency(ctx context.Context, code string) (domain.Currency, error) {
	currencies, err := s.catalogue.Currencies(ctx)
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

func moneyText(minor int64, c domain.Currency) string {
	return numinput.FormatFixed(minor, c.Decimals, c.Decimals) + " " + c.Code
}

func grossText(q domain.Quote) string {
	var gross int64
	for _, l := range q.Lines {
		if q.Settlement.Code == domain.USD {
			gross += l.GrossUSDMinor
		} else {
			gross += l.GrossLocalMinor
		}
	}
	return moneyText(gross, q.Settlement)
}
