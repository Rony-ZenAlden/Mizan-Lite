// Package salestest is the in-memory sales store, fakes of the till's ports, and the contract every store must meet —
// run against this fake AND SQLite (L0 D-L0.3).
package salestest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// ErrInjected is returned once a failure has been armed.
var ErrInjected = errors.New("salestest: injected failure")

// Fake is an in-memory sales.Store enforcing the schema's rules the service could lean on.
type Fake struct {
	mu    sync.Mutex
	sales map[id.ID]domain.Sale
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{sales: map[id.ID]domain.Sale{}} }

func (f *Fake) NextReceiptNo(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := int64(1)
	for _, s := range f.sales {
		if s.ReceiptNo >= next {
			next = s.ReceiptNo + 1
		}
	}
	return next, nil
}

func (f *Fake) Insert(_ context.Context, s domain.Sale) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, other := range f.sales {
		if other.ID == s.ID || other.ReceiptNo == s.ReceiptNo {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	if s.ReceiptNo < 1 || s.Status != domain.StatusPosted || len(s.Lines) == 0 {
		return errs.Validation("database.constraint_violation", "check constraint")
	}
	s.RowVersion = 1
	f.sales[s.ID] = copySale(s)
	return nil
}

func (f *Fake) Get(_ context.Context, saleID id.ID) (domain.Sale, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sales[saleID]
	if !ok {
		return domain.Sale{}, domain.ErrNotFound()
	}
	return copySale(s), nil
}

func (f *Fake) Day(_ context.Context, businessDate string) ([]domain.Sale, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Sale
	for _, s := range f.sales {
		if s.BusinessDate == businessDate || s.VoidBusinessDate == businessDate {
			out = append(out, copySale(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReceiptNo < out[j].ReceiptNo })
	return out, nil
}

func (f *Fake) Range(_ context.Context, from, to string) ([]domain.Sale, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Sale
	for _, s := range f.sales {
		if (s.BusinessDate >= from && s.BusinessDate <= to) || (s.VoidBusinessDate != "" && s.VoidBusinessDate >= from && s.VoidBusinessDate <= to) {
			out = append(out, copySale(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReceiptNo < out[j].ReceiptNo })
	return out, nil
}

func (f *Fake) Void(_ context.Context, s domain.Sale) (domain.Sale, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.sales[s.ID]
	if !ok || stored.RowVersion != s.RowVersion || stored.Status != domain.StatusPosted {
		return domain.Sale{}, errs.Conflict("database.concurrent_modification", "the sale changed")
	}
	stored.Status, stored.VoidedAt, stored.VoidBusinessDate, stored.VoidReason = s.Status, s.VoidedAt, s.VoidBusinessDate, s.VoidReason
	stored.RowVersion++
	f.sales[s.ID] = stored
	return copySale(stored), nil
}

func (f *Fake) Each(ctx context.Context, fn func(domain.Sale) error) error {
	f.mu.Lock()
	all := make([]domain.Sale, 0, len(f.sales))
	for _, s := range f.sales {
		all = append(all, copySale(s))
	}
	f.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].ReceiptNo < all[j].ReceiptNo })
	for _, s := range all {
		if err := fn(s); err != nil {
			return err
		}
	}
	return nil
}

func copySale(s domain.Sale) domain.Sale {
	s.Lines = append([]domain.Line(nil), s.Lines...)
	return s
}

// Catalogue is a fake catalogue port.
type Catalogue struct {
	mu       sync.Mutex
	products map[id.ID]domain.Product
	barcodes map[string]id.ID
}

// NewCatalogue returns an empty catalogue.
func NewCatalogue() *Catalogue {
	return &Catalogue{products: map[id.ID]domain.Product{}, barcodes: map[string]id.ID{}}
}

// CodeProductNotFound mirrors the catalogue's code.
const CodeProductNotFound = "lite.catalog.not_found"

// Add registers a product, with an optional barcode, and returns it with its id.
func (c *Catalogue) Add(p domain.Product, barcode string) domain.Product {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.ID == "" {
		p.ID, _ = id.New()
	}
	if p.RowVersion == 0 {
		p.RowVersion = 1
	}
	c.products[p.ID] = p
	if barcode != "" {
		c.barcodes[barcode] = p.ID
	}
	return p
}

// Update replaces a product, as a price change or a deactivation would, and increments its version.
func (c *Catalogue) Update(p domain.Product) domain.Product {
	c.mu.Lock()
	defer c.mu.Unlock()
	p.RowVersion = c.products[p.ID].RowVersion + 1
	c.products[p.ID] = p
	return p
}

func (c *Catalogue) Product(_ context.Context, productID id.ID) (domain.Product, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.products[productID]
	if !ok {
		return domain.Product{}, errs.NotFound(CodeProductNotFound, "no such product")
	}
	return p, nil
}

func (c *Catalogue) ByBarcode(_ context.Context, code string) (domain.Product, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	productID, ok := c.barcodes[numinput.LatinDigits(code)]
	if !ok {
		return domain.Product{}, false, nil
	}
	return c.products[productID], true, nil
}

func (c *Catalogue) Currencies(context.Context) ([]domain.Currency, error) {
	return []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}, nil
}

// Stock is a fake stock port: levels, and the movements the till recorded.
type Stock struct {
	mu       sync.Mutex
	levels   map[id.ID]domain.Stocked
	sold     []sales.StockLine
	voided   []id.ID
	failSale bool
}

// NewStock returns stock with nothing on hand.
func NewStock() *Stock { return &Stock{levels: map[id.ID]domain.Stocked{}} }

// Set sets a product's level.
func (s *Stock) Set(productID id.ID, level domain.Stocked) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.levels[productID] = level
}

// FailSales makes RecordSale fail — after the sale was inserted.
func (s *Stock) FailSales() { s.mu.Lock(); s.failSale = true; s.mu.Unlock() }

// Sold returns the sale lines recorded.
func (s *Stock) Sold() []sales.StockLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sales.StockLine(nil), s.sold...)
}

// Voided returns the sale lines whose stock was returned.
func (s *Stock) Voided() []id.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]id.ID(nil), s.voided...)
}

func (s *Stock) Stocked(_ context.Context, productID id.ID) (domain.Stocked, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.levels[productID], nil
}

func (s *Stock) RecordSale(_ context.Context, line sales.StockLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSale {
		return ErrInjected
	}
	l := s.levels[line.ProductID]
	l.OnHandMicro -= line.QuantityMicro
	s.levels[line.ProductID] = l
	s.sold = append(s.sold, line)
	return nil
}

func (s *Stock) RecordSaleVoid(_ context.Context, saleLineID id.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range s.sold {
		if line.SaleLineID == saleLineID {
			l := s.levels[line.ProductID]
			l.OnHandMicro += line.QuantityMicro
			s.levels[line.ProductID] = l
			s.voided = append(s.voided, saleLineID)
			return nil
		}
	}
	return errs.NotFound("lite.stock.movement_not_found", "no such movement")
}

func (s *Stock) EachSaleMovement(_ context.Context, fn func(domain.StockMovement) error) error {
	s.mu.Lock()
	sold, voided := append([]sales.StockLine(nil), s.sold...), map[id.ID]bool{}
	for _, v := range s.voided {
		voided[v] = true
	}
	s.mu.Unlock()
	for _, line := range sold {
		if err := fn(domain.StockMovement{Kind: "sale", ProductID: line.ProductID, QuantityMicro: -line.QuantityMicro, SaleID: line.SaleID, SaleLineID: line.SaleLineID}); err != nil {
			return err
		}
		if voided[line.SaleLineID] {
			if err := fn(domain.StockMovement{Kind: "sale_void", ProductID: line.ProductID, QuantityMicro: line.QuantityMicro, SaleID: line.SaleID, SaleLineID: line.SaleLineID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Rates is a fake rates port.
type Rates struct {
	mu   sync.Mutex
	Rate domain.Rate
	Set  bool
}

// NewRates returns a rate of 15,000 recorded at 06:00 UTC on 14 September 2026.
func NewRates() *Rates {
	rateID, _ := id.New()
	return &Rates{Set: true, Rate: domain.Rate{ID: rateID, Nano: 15_000_000_000_000, RecordedAt: time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)}}
}

// Change sets a new rate, as the owner would.
func (r *Rates) Change(nano int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Rate.ID, _ = id.New()
	r.Rate.Nano = nano
}

func (r *Rates) InForce(context.Context) (domain.Rate, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Rate, "SYP", r.Set, nil
}

// Settings is fake settings.
type Settings struct {
	mu   sync.Mutex
	Shop string
	Note int64
}

// NewSettings returns a shop with the default 500-pound note.
func NewSettings() *Settings { return &Settings{Shop: "بقالية المونة", Note: 500} }

func (s *Settings) ShopName(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Shop, nil
}

func (s *Settings) CashNote(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Note, nil
}

func (s *Settings) SetCashNote(_ context.Context, raw string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errs.Validation("lite.settings.invalid_cash_note", "invalid")
		}
		v = v*10 + int64(r-'0')
	}
	if v < 1 {
		return 0, errs.Validation("lite.settings.invalid_cash_note", "invalid")
	}
	s.Note = v
	return v, nil
}

// Gate is a recording owner gate.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []sales.GuardedAct
	Asked    int
}

func (g *Gate) Require(_ context.Context, act sales.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Asked++
	if !g.Elevated {
		return errs.Permission(sales.CodeOwnerRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}

func (g *Gate) Allowed(context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Elevated
}

// Debts is a fake debt book: customers, balances per currency, and the charges the till wrote.
type Debts struct {
	mu        sync.Mutex
	customers map[id.ID]domain.Customer
	balances  map[id.ID]map[string]int64
	charges   []domain.Charge
	credits   map[id.ID]domain.Credit
	failNext  bool
}

// NewDebts returns an empty book.
func NewDebts() *Debts {
	return &Debts{customers: map[id.ID]domain.Customer{}, balances: map[id.ID]map[string]int64{}, credits: map[id.ID]domain.Credit{}}
}

// Add registers a customer with balances, and returns it with its id.
func (d *Debts) Add(c domain.Customer, balances map[string]int64) domain.Customer {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c.ID == "" {
		c.ID, _ = id.New()
	}
	d.customers[c.ID] = c
	d.balances[c.ID] = map[string]int64{}
	for k, v := range balances {
		d.balances[c.ID][k] = v
	}
	return c
}

// FailCharges makes the next Charge fail — after the sale and its stock were written.
func (d *Debts) FailCharges() { d.mu.Lock(); d.failNext = true; d.mu.Unlock() }

// Charges returns the charges written.
func (d *Debts) Charges() []domain.Charge {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]domain.Charge(nil), d.charges...)
}

func (d *Debts) Customer(_ context.Context, customerID id.ID) (domain.Customer, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.customers[customerID]
	return c, ok, nil
}

func (d *Debts) Balances(_ context.Context, customerID id.ID) (map[string]int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]int64{}
	for k, v := range d.balances[customerID] {
		out[k] = v
	}
	return out, nil
}

func (d *Debts) Charge(_ context.Context, in sales.ChargeInput) (domain.Credit, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failNext {
		d.failNext = false
		return domain.Credit{}, ErrInjected
	}
	c, ok := d.customers[in.CustomerID]
	if !ok || !c.Active {
		return domain.Credit{}, errs.Conflict("lite.customers.inactive", "inactive")
	}
	for _, ch := range d.charges {
		if ch.SaleID == in.SaleID {
			return domain.Credit{}, errs.Conflict("database.duplicate", "one charge per sale")
		}
	}
	d.balances[c.ID][in.Currency] += in.AmountMinor
	d.charges = append(d.charges, domain.Charge{SaleID: in.SaleID, CustomerID: c.ID, Currency: in.Currency, AmountMinor: in.AmountMinor})
	credit := domain.Credit{CustomerID: c.ID, CustomerName: c.Name, Currency: in.Currency, AmountMinor: in.AmountMinor, BalanceAfterMinor: d.balances[c.ID][in.Currency]}
	d.credits[in.SaleID] = credit
	return credit, nil
}

func (d *Debts) ReverseCharge(_ context.Context, saleID id.ID, _ string, _ time.Time, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, ch := range d.charges {
		if ch.SaleID == saleID && !ch.Reversed {
			d.charges[i].Reversed = true
			d.balances[ch.CustomerID][ch.Currency] -= ch.AmountMinor
			credit := d.credits[saleID]
			credit.Reversed = true
			d.credits[saleID] = credit
			return nil
		}
	}
	return errs.NotFound("lite.customers.charge_not_found", "no charge")
}

func (d *Debts) CreditOf(_ context.Context, saleID id.ID) (domain.Credit, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.credits[saleID]
	return c, ok, nil
}

func (d *Debts) EachCharge(_ context.Context, fn func(domain.Charge) error) error {
	for _, ch := range d.Charges() {
		if err := fn(ch); err != nil {
			return err
		}
	}
	return nil
}
