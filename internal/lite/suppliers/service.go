// Package suppliers is Mizan Lite's payables book: the suppliers a shop buys from, its purchases — cash or on credit,
// with their discounts and their damaged goods — and what it pays them (the owner's request, 2026-09-24).
package suppliers

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// The acts only the owner may do. Their codes are what the owner's history records.
//
// A purchase is the owner's: it records a debt of the shop's. So is money paid out, an opening balance from the paper
// book, a reversal and a void. A refund — money coming IN from a supplier — is not, as a customer's payment is not.
const (
	ActPurchase     = "suppliers.purchase"
	ActVoidPurchase = "suppliers.purchase_void"
	ActPayment      = "suppliers.payment"
	ActOpening      = "suppliers.opening"
	ActReverse      = "suppliers.reverse"
)

// CodeOwnerRequired is the owner module's refusal, returned by a read outside owner mode so the frontend answers it with
// the same PIN dialog. Declared here because suppliers may not import the owner module; a bootstrap test holds the two
// equal.
const CodeOwnerRequired = "lite.owner.required"

// Store persists suppliers, purchases and the payables book. infra/sqlite implements it; supplierstest.Fake implements
// it in memory, and one contract suite runs against both.
type Store interface {
	InsertSupplier(ctx context.Context, s domain.Supplier) error
	// UpdateSupplier writes s if the stored row is still at s.RowVersion, and increments the version.
	UpdateSupplier(ctx context.Context, s domain.Supplier) (domain.Supplier, error)
	// Supplier returns a supplier, or domain.ErrNotFound.
	Supplier(ctx context.Context, supplierID id.ID) (domain.Supplier, error)
	SupplierByNameKey(ctx context.Context, key string) (domain.Supplier, bool, error)
	Search(ctx context.Context, q Query) ([]domain.Supplier, error)

	// Newest is the newest place of one chain; the zero Place for a chain with no entries.
	Newest(ctx context.Context, supplierID id.ID, currency string) (domain.Place, error)
	InsertEntry(ctx context.Context, e domain.Entry) error
	// Entry returns an entry, or CodeEntryNotFound.
	Entry(ctx context.Context, entryID id.ID) (domain.Entry, error)
	ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error)
	// PurchaseEntry is a purchase's entry in the book; none for a purchase that cost nothing.
	PurchaseEntry(ctx context.Context, purchaseID id.ID) (domain.Entry, bool, error)
	// Chain returns one chain's entries in place order.
	Chain(ctx context.Context, supplierID id.ID, currency string) ([]domain.Entry, error)
	// Balances is every chain's newest balance: supplier, then currency.
	Balances(ctx context.Context) (map[id.ID]map[string]int64, error)
	// Between returns the entries of business dates from..to inclusive, in the order they were made.
	Between(ctx context.Context, from, to string) ([]domain.Entry, error)

	NextPurchaseNo(ctx context.Context) (int64, error)
	InsertPurchase(ctx context.Context, p domain.Purchase) error
	// Purchase returns a purchase with its lines, or CodePurchaseNotFound.
	Purchase(ctx context.Context, purchaseID id.ID) (domain.Purchase, error)
	Purchases(ctx context.Context, q PurchaseQuery) ([]domain.Purchase, error)
	// VoidPurchase writes p's void if the stored row is still at p.RowVersion.
	VoidPurchase(ctx context.Context, p domain.Purchase) error
}

// Query is a supplier search: a normalised name fragment or a phone fragment; both empty lists all.
type Query struct {
	Text            string
	Phone           string
	IncludeInactive bool
	Limit           int
}

// PurchaseQuery lists purchases, newest first: one supplier's, or a range of business dates, or both.
type PurchaseQuery struct {
	SupplierID id.ID
	From, To   string
	Limit      int
}

// DefaultLimit bounds a list.
const DefaultLimit = 500

// Stock is what a purchase needs of the stock book: its good units received at their cost, and a void's receipts
// reversed. A receipt is the stock module's own act, with its own rules — a purchase never writes a ledger row itself.
type Stock interface {
	Receive(ctx context.Context, r Receipt) (ledgerID id.ID, err error)
	ReverseReceipt(ctx context.Context, ledgerID id.ID, note string) error
}

// Receipt is a purchase line's good units as the stock book takes them: a total cost in the purchase's currency.
type Receipt struct {
	ProductID id.ID
	Quantity  string
	Total     string
	Currency  string
	Rate      string
	Note      string
}

// Catalogue is what a purchase needs of the products.
type Catalogue interface {
	Products(ctx context.Context, ids []id.ID) (map[id.ID]domain.Product, error)
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// Money is what the book needs to know of the shop's money: which currency is its local one.
type Money interface {
	LocalCurrency(ctx context.Context) (string, error)
}

// OwnerGate is what the book needs of the owner.
type OwnerGate interface {
	Require(ctx context.Context, act GuardedAct) error
	// Allowed reports owner mode without recording anything.
	Allowed(ctx context.Context) bool
}

// GuardedAct describes an owner-only act, for the owner's history.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Transactor runs fn atomically; a call inside another joins it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Ports are the service's dependencies.
type Ports struct {
	Stock     Stock
	Catalogue Catalogue
	Money     Money
	Gate      OwnerGate
}

// Service is the payables book.
type Service struct {
	tx    Transactor
	store Store
	ports Ports
	clk   clock.Clock
	loc   *time.Location
	newID func() (id.ID, error)
}

// NewService builds the service. Every dependency is required.
func NewService(tx Transactor, store Store, ports Ports, clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, ports: ports, clk: clk, loc: loc, newID: id.New}
}

func (s *Service) now() time.Time { return s.clk.Now().UTC().Truncate(time.Millisecond) }

// visible refuses the book to anyone but the owner while the PIN switch is on. Every figure in it is what the shop's goods
// cost or what it owes, which the counter does not read (Q-L2.4) — so the screens are the owner's, suppliers and all.
// With the switch off, as a shop starts, it is open to whoever runs the shop.
func (s *Service) visible(ctx context.Context) error {
	if !s.ports.Gate.Allowed(ctx) {
		return errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
	}
	return nil
}

// ─── Suppliers ─────────────────────────────────────────────────────────────────────────────────────────────────────

// Create adds a supplier. Anyone may, as anyone may add a customer.
func (s *Service) Create(ctx context.Context, d domain.Draft) (domain.Supplier, error) {
	if err := s.visible(ctx); err != nil {
		return domain.Supplier{}, err
	}
	var out domain.Supplier
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		supplierID, err := s.newID()
		if err != nil {
			return err
		}
		sup, err := domain.NewSupplier(supplierID, d)
		if err != nil {
			return err
		}
		if err = s.unique(ctx, sup); err != nil {
			return err
		}
		sup.CreatedAt = s.now()
		if err = s.store.InsertSupplier(ctx, sup); err != nil {
			return err
		}
		out = sup
		return nil
	})
	return out, err
}

// UpdateInput edits a supplier at a version.
type UpdateInput struct {
	ID         id.ID
	RowVersion int64
	Draft      domain.Draft
}

// Update edits a supplier's name, phone, city and note.
func (s *Service) Update(ctx context.Context, in UpdateInput) (domain.Supplier, error) {
	if err := s.visible(ctx); err != nil {
		return domain.Supplier{}, err
	}
	var out domain.Supplier
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
		out, err = s.store.UpdateSupplier(ctx, next)
		return err
	})
	return out, err
}

// SetActive activates or deactivates a supplier. An inactive supplier keeps its book and is left out of the pickers.
func (s *Service) SetActive(ctx context.Context, supplierID id.ID, rowVersion int64, active bool) (domain.Supplier, error) {
	if err := s.visible(ctx); err != nil {
		return domain.Supplier{}, err
	}
	var out domain.Supplier
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.at(ctx, supplierID, rowVersion)
		if err != nil {
			return err
		}
		current.Active = active
		out, err = s.store.UpdateSupplier(ctx, current)
		return err
	})
	return out, err
}

func (s *Service) at(ctx context.Context, supplierID id.ID, rowVersion int64) (domain.Supplier, error) {
	current, err := s.store.Supplier(ctx, supplierID)
	if err != nil {
		return domain.Supplier{}, err
	}
	if current.RowVersion != rowVersion {
		return domain.Supplier{}, domain.ErrStale()
	}
	return current, nil
}

func (s *Service) unique(ctx context.Context, sup domain.Supplier) error {
	other, found, err := s.store.SupplierByNameKey(ctx, sup.NameKey())
	if err != nil {
		return err
	}
	if found && other.ID != sup.ID {
		return errs.Conflict(domain.CodeDuplicateName, "a supplier with this name exists").
			WithField(domain.FieldName, domain.CodeDuplicateName, "duplicate").WithParam("existingName", other.Name)
	}
	return nil
}

// Summary is one currency's balance of a supplier: above zero the shop owes it, below zero the supplier owes the shop.
type Summary struct {
	Currency     string
	BalanceMinor int64
}

// WithBalances is a supplier and its balance in each currency it has entries in.
type WithBalances struct {
	Supplier  domain.Supplier
	Summaries []Summary
}

// Get is one supplier and its balances.
func (s *Service) Get(ctx context.Context, supplierID id.ID) (WithBalances, error) {
	if err := s.visible(ctx); err != nil {
		return WithBalances{}, err
	}
	sup, err := s.store.Supplier(ctx, supplierID)
	if err != nil {
		return WithBalances{}, err
	}
	balances, err := s.store.Balances(ctx)
	if err != nil {
		return WithBalances{}, err
	}
	return withBalances(sup, balances[supplierID]), nil
}

// Search lists suppliers matching a name or phone fragment, with their balances.
func (s *Service) Search(ctx context.Context, text string, includeInactive bool) ([]WithBalances, error) {
	if err := s.visible(ctx); err != nil {
		return nil, err
	}
	phone := numinput.LatinDigits(text)
	if strings.Trim(phone, "0123456789 +-()") != "" {
		phone = ""
	}
	found, err := s.store.Search(ctx, Query{Text: textkey.Normalise(text), Phone: phone, IncludeInactive: includeInactive, Limit: DefaultLimit})
	if err != nil {
		return nil, err
	}
	balances, err := s.store.Balances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WithBalances, 0, len(found))
	for _, sup := range found {
		out = append(out, withBalances(sup, balances[sup.ID]))
	}
	return out, nil
}

func withBalances(sup domain.Supplier, byCurrency map[string]int64) WithBalances {
	w := WithBalances{Supplier: sup}
	for code, minor := range byCurrency {
		w.Summaries = append(w.Summaries, Summary{Currency: code, BalanceMinor: minor})
	}
	// Dollars last, whatever the local currency is called — as the customers' balances read.
	sort.Slice(w.Summaries, func(i, j int) bool {
		if (w.Summaries[i].Currency == "USD") != (w.Summaries[j].Currency == "USD") {
			return w.Summaries[j].Currency == "USD"
		}
		return w.Summaries[i].Currency < w.Summaries[j].Currency
	})
	return w
}

// Statement is one supplier's book in one currency.
type Statement struct {
	Supplier     domain.Supplier
	Currency     string
	Entries      []domain.Entry
	BalanceMinor int64
}

// StatementOf is a supplier's entries in one currency, oldest first, and the balance they come to.
func (s *Service) StatementOf(ctx context.Context, supplierID id.ID, currency string) (Statement, error) {
	if err := s.visible(ctx); err != nil {
		return Statement{}, err
	}
	sup, err := s.store.Supplier(ctx, supplierID)
	if err != nil {
		return Statement{}, err
	}
	entries, err := s.store.Chain(ctx, supplierID, currency)
	if err != nil {
		return Statement{}, err
	}
	st := Statement{Supplier: sup, Currency: currency, Entries: entries}
	if n := len(entries); n > 0 {
		st.BalanceMinor = entries[n-1].BalanceAfterMinor
	}
	return st, nil
}

// Payables is what the shop owes its suppliers, net, in each currency — what comes off its capital. Unguarded: the
// notification engine reads it, and withholds the owner's figures itself.
func (s *Service) Payables(ctx context.Context) (map[string]int64, error) {
	balances, err := s.store.Balances(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, byCurrency := range balances {
		for code, minor := range byCurrency {
			out[code] += minor
		}
	}
	return out, nil
}

// DrawerOn is what the payables book did to the drawer on a business date, per currency: payments out of it are below
// nought, refunds into it above.
func (s *Service) DrawerOn(ctx context.Context, businessDate string) (map[string]int64, error) {
	moves, err := s.DrawerBetween(ctx, businessDate, businessDate)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, m := range moves {
		out[m.Currency] += m.RefundInMinor - m.PaidOutMinor
	}
	return out, nil
}

// DrawerMove is what one entry did to the drawer: money paid to a supplier out of it, or a supplier's refund into it. A
// reversal counts against the kind it undoes, on its own day — a payment reversed is a payment below nought — as the
// debt book's reversals do in the drawer.
type DrawerMove struct {
	BusinessDate  string
	Currency      string
	PaidOutMinor  int64
	RefundInMinor int64
}

// DrawerBetween is every move the payables book made in the drawer on business dates from..to. Money the owner paid
// from their own pocket is not here: it never was in the drawer. The drawer's expected cash reads it (0.10.0).
func (s *Service) DrawerBetween(ctx context.Context, from, to string) ([]DrawerMove, error) {
	entries, err := s.store.Between(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := []DrawerMove{}
	for _, e := range entries {
		m := e.DrawerMinor()
		if m == 0 {
			continue
		}
		move := DrawerMove{BusinessDate: e.BusinessDate, Currency: e.Currency}
		// A payment is always below nought on the book and a refund above (domain.Place.Append), so a reversal's sign
		// says which it undoes: one above nought gives a payment's money back to the drawer.
		switch {
		case e.Kind == domain.KindPayment, e.Kind == domain.KindReversal && m > 0:
			move.PaidOutMinor = -m
		default:
			move.RefundInMinor = m
		}
		out = append(out, move)
	}
	return out, nil
}

// ─── Purchases ─────────────────────────────────────────────────────────────────────────────────────────────────────

// Quote is a purchase worked out and not recorded — what the form shows — with the supplier's balance before and after.
type Quote struct {
	Purchase           domain.Purchase
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
}

// QuotePurchase works a purchase out from what was typed, and writes nothing.
func (s *Service) QuotePurchase(ctx context.Context, in domain.Input) (Quote, error) {
	if err := s.visible(ctx); err != nil {
		return Quote{}, err
	}
	sup, err := s.store.Supplier(ctx, in.SupplierID)
	if err != nil {
		return Quote{}, err
	}
	p, err := s.price(ctx, in)
	if err != nil {
		return Quote{}, err
	}
	p.SupplierName = sup.Name
	place, err := s.store.Newest(ctx, sup.ID, p.Currency)
	if err != nil {
		return Quote{}, err
	}
	return Quote{Purchase: p, BalanceBeforeMinor: place.BalanceMinor,
		BalanceAfterMinor: place.BalanceMinor + p.DueMinor() - p.PaidNowMinor}, nil
}

func (s *Service) price(ctx context.Context, in domain.Input) (domain.Purchase, error) {
	ids := make([]id.ID, 0, len(in.Lines))
	for _, l := range in.Lines {
		ids = append(ids, l.ProductID)
	}
	products, err := s.ports.Catalogue.Products(ctx, ids)
	if err != nil {
		return domain.Purchase{}, err
	}
	currencies, err := s.ports.Catalogue.Currencies(ctx)
	if err != nil {
		return domain.Purchase{}, err
	}
	local, err := s.ports.Money.LocalCurrency(ctx)
	if err != nil {
		return domain.Purchase{}, err
	}
	return domain.Price(in, products, currencies, local)
}

// RecordPurchase records a purchase, all or nothing: each line's good units received into stock at what they cost after
// every discount, the purchase and its lines, what it costs added to the supplier's book, and any money paid on the spot
// taken off it. The owner's act.
func (s *Service) RecordPurchase(ctx context.Context, in domain.Input) (domain.Purchase, error) {
	var out domain.Purchase
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		sup, err := s.store.Supplier(ctx, in.SupplierID)
		if err != nil {
			return err
		}
		if !sup.Active {
			return errs.Conflict(domain.CodeInactive, "reactivate the supplier before buying from them").WithParam("name", sup.Name)
		}
		p, err := s.price(ctx, in)
		if err != nil {
			return err
		}
		at := s.now()
		if p.ID, err = s.newID(); err != nil {
			return err
		}
		if p.PurchaseNo, err = s.store.NextPurchaseNo(ctx); err != nil {
			return err
		}
		p.SupplierName, p.OccurredAt, p.BusinessDate, p.RowVersion = sup.Name, at, bizdate.Date(at, s.loc), 1
		currencies, err := s.ports.Catalogue.Currencies(ctx)
		if err != nil {
			return err
		}
		decimals := decimalsOf(currencies, p.Currency)
		for i := range p.Lines {
			l := &p.Lines[i]
			if l.ID, err = s.newID(); err != nil {
				return err
			}
			if l.GoodMicro() == 0 {
				continue // everything on the line arrived damaged: nothing to receive, nothing charged
			}
			l.StockLedgerID, err = s.ports.Stock.Receive(ctx, Receipt{
				ProductID: l.ProductID, Quantity: numinput.FormatFixed(l.GoodMicro(), 6, 0),
				Total: numinput.FormatFixed(l.DueMinor(), decimals, decimals), Currency: p.Currency, Rate: rateText(p.RateNano),
				Note: "purchase " + strconv.FormatInt(p.PurchaseNo, 10) + " · " + sup.Name,
			})
			if err != nil {
				return lineError(err, l.LineNo)
			}
		}
		if err = s.store.InsertPurchase(ctx, p); err != nil {
			return err
		}
		if due := p.DueMinor(); due > 0 {
			if _, err = s.appendAt(ctx, domain.Entry{SupplierID: sup.ID, Currency: p.Currency, Kind: domain.KindPurchase,
				AmountMinor: due, PurchaseID: p.ID, SupplierName: sup.Name}, at); err != nil {
				return err
			}
		}
		if p.PaidNowMinor > 0 {
			if _, err = s.appendAt(ctx, domain.Entry{SupplierID: sup.ID, Currency: p.Currency, Kind: domain.KindPayment,
				AmountMinor: -p.PaidNowMinor, PurchaseID: p.ID, Source: p.PaidFrom, SupplierName: sup.Name}, at); err != nil {
				return err
			}
		}
		if err = s.ports.Gate.Require(ctx, GuardedAct{Action: ActPurchase, SubjectID: p.ID,
			After: strconv.FormatInt(p.PurchaseNo, 10) + " · " + p.Currency + " " + strconv.FormatInt(p.DueMinor(), 10)}); err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

// VoidPurchase undoes a purchase entered by mistake: its receipts reversed out of stock and what it cost taken off the
// supplier's book, with a reason. Money paid at the purchase stays paid — the supplier then owes it back, which is the
// truth. A receipt can be reversed only while it is its product's newest movement (L2); once goods have moved since,
// the purchase is corrected with a stock adjustment instead, and the void says which line stands in the way.
func (s *Service) VoidPurchase(ctx context.Context, purchaseID id.ID, reason string) (domain.Purchase, error) {
	var out domain.Purchase
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		p, err := s.store.Purchase(ctx, purchaseID)
		if err != nil {
			return err
		}
		if p.Status == domain.StatusVoided {
			return errs.Conflict(domain.CodePurchaseVoided, "the purchase is already voided").
				WithParam("number", strconv.FormatInt(p.PurchaseNo, 10))
		}
		note, err := domain.Reason(reason)
		if err != nil {
			return err
		}
		for _, l := range p.Lines {
			if l.StockLedgerID == "" {
				continue
			}
			if err = s.ports.Stock.ReverseReceipt(ctx, l.StockLedgerID, note); err != nil {
				return errs.Conflict(domain.CodeLineNotReversible, "a later movement uses this delivery's stock").
					WithParam("line", strconv.Itoa(l.LineNo)).WithParam("name", l.NameAR).WithParam("cause", errs.CodeOf(err))
			}
		}
		at := s.now()
		entry, found, err := s.store.PurchaseEntry(ctx, p.ID)
		if err != nil {
			return err
		}
		if found {
			rev, revErr := domain.Reverse(entry, note, true)
			if revErr != nil {
				return revErr
			}
			if _, err = s.appendAt(ctx, rev, at); err != nil {
				return err
			}
		}
		p.Status, p.VoidedAt, p.VoidReason = domain.StatusVoided, at, note
		if err = s.store.VoidPurchase(ctx, p); err != nil {
			return err
		}
		p.RowVersion++
		if err = s.ports.Gate.Require(ctx, GuardedAct{Action: ActVoidPurchase, SubjectID: p.ID,
			Before: strconv.FormatInt(p.PurchaseNo, 10), After: note}); err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

// Purchase is one purchase with its lines.
func (s *Service) Purchase(ctx context.Context, purchaseID id.ID) (domain.Purchase, error) {
	if err := s.visible(ctx); err != nil {
		return domain.Purchase{}, err
	}
	return s.store.Purchase(ctx, purchaseID)
}

// Purchases lists purchases, newest first.
func (s *Service) Purchases(ctx context.Context, q PurchaseQuery) ([]domain.Purchase, error) {
	if err := s.visible(ctx); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > DefaultLimit {
		q.Limit = DefaultLimit
	}
	return s.store.Purchases(ctx, q)
}

// Damaged is the units of one purchase line that arrived damaged: never received, never charged (0.10.0).
type Damaged struct {
	BusinessDate string
	PurchaseNo   int64
	SupplierName string
	ProductID    id.ID
	NameAR       string
	NameEN       string
	UnitCode     string
	DamagedMicro int64
	Currency     string
	// ValueMinor is what they would have cost at the line's unit price, in the purchase's currency.
	ValueMinor int64
}

// DamagedOnArrival is the damaged units on the purchases of business dates from..to, voided purchases left out. Unguarded:
// the loss report reads it, and the loss report is the owner's.
func (s *Service) DamagedOnArrival(ctx context.Context, from, to string) ([]Damaged, error) {
	bought, err := s.store.Purchases(ctx, PurchaseQuery{From: from, To: to, Limit: math.MaxInt32})
	if err != nil {
		return nil, err
	}
	currencies, err := s.ports.Catalogue.Currencies(ctx)
	if err != nil {
		return nil, err
	}
	out := []Damaged{}
	for _, p := range bought {
		if p.Status == domain.StatusVoided {
			continue
		}
		for _, l := range p.Lines {
			if l.DamagedMicro == 0 {
				continue
			}
			out = append(out, Damaged{BusinessDate: p.BusinessDate, PurchaseNo: p.PurchaseNo, SupplierName: p.SupplierName,
				ProductID: l.ProductID, NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode, DamagedMicro: l.DamagedMicro,
				Currency: p.Currency, ValueMinor: l.DamagedValueMinor(decimalsOf(currencies, p.Currency))})
		}
	}
	return out, nil
}

// ─── Money ─────────────────────────────────────────────────────────────────────────────────────────────────────────

// MoneyInput is a payment to a supplier, a refund from one, or an opening balance.
type MoneyInput struct {
	SupplierID id.ID
	Currency   string
	Amount     string
	// Source is where the money came from (a payment) or went (a refund): "drawer" or "owner".
	Source string
	// InShopsFavour makes an opening balance one the supplier owes the shop.
	InShopsFavour bool
	Note          string
}

// Pay records money paid to a supplier, from the drawer or the owner's own money. It may take the balance below
// nought — money paid ahead is the supplier owing the shop. The owner's act.
func (s *Service) Pay(ctx context.Context, in MoneyInput) (domain.Entry, error) {
	return s.money(ctx, in, domain.KindPayment, ActPayment)
}

// Refund records money a supplier paid back against what they owed the shop. Anyone may: money is coming in.
func (s *Service) Refund(ctx context.Context, in MoneyInput) (domain.Entry, error) {
	return s.money(ctx, in, domain.KindRefund, "")
}

// Opening records a balance brought over from the paper book, either way. The owner's act.
func (s *Service) Opening(ctx context.Context, in MoneyInput) (domain.Entry, error) {
	return s.money(ctx, in, domain.KindOpening, ActOpening)
}

func (s *Service) money(ctx context.Context, in MoneyInput, kind domain.Kind, act string) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		sup, err := s.store.Supplier(ctx, in.SupplierID)
		if err != nil {
			return err
		}
		currencies, err := s.ports.Catalogue.Currencies(ctx)
		if err != nil {
			return err
		}
		cur, ok := currencyOf(currencies, in.Currency)
		if !ok {
			return errs.Validation(domain.CodeUnknownCurrency, "unknown currency").WithParam("value", in.Currency)
		}
		amount, err := domain.ParseAmount(in.Amount, cur)
		if err != nil {
			return err
		}
		note, err := domain.Note(in.Note)
		if err != nil {
			return err
		}
		e := domain.Entry{SupplierID: sup.ID, Currency: cur.Code, Kind: kind, SupplierName: sup.Name, Note: note}
		switch kind {
		case domain.KindPayment:
			e.AmountMinor = -amount
		case domain.KindOpening:
			e.AmountMinor = amount
			if in.InShopsFavour {
				e.AmountMinor = -amount
			}
		default:
			e.AmountMinor = amount
		}
		if kind != domain.KindOpening {
			if e.Source, err = domain.ParseSource(in.Source); err != nil {
				return err
			}
		}
		if out, err = s.appendAt(ctx, e, s.now()); err != nil {
			return err
		}
		if act != "" {
			return s.ports.Gate.Require(ctx, GuardedAct{Action: act, SubjectID: sup.ID,
				After: cur.Code + " " + strconv.FormatInt(out.AmountMinor, 10)})
		}
		return nil
	})
	return out, err
}

// Reverse undoes an entry made by mistake, with its reason. A purchase is undone by voiding it. The owner's act.
func (s *Service) Reverse(ctx context.Context, entryID id.ID, reason string) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		original, err := s.store.Entry(ctx, entryID)
		if err != nil {
			return err
		}
		_, reversed, err := s.store.ReversalOf(ctx, entryID)
		if err != nil {
			return err
		}
		if reversed {
			return errs.Conflict(domain.CodeAlreadyReversed, "the entry is already reversed")
		}
		rev, err := domain.Reverse(original, reason, false)
		if err != nil {
			return err
		}
		if out, err = s.appendAt(ctx, rev, s.now()); err != nil {
			return err
		}
		return s.ports.Gate.Require(ctx, GuardedAct{Action: ActReverse, SubjectID: original.SupplierID,
			Before: string(original.Kind) + " " + strconv.FormatInt(original.AmountMinor, 10), After: rev.Note})
	})
	return out, err
}

// Entry is one entry of the book.
func (s *Service) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	if err := s.visible(ctx); err != nil {
		return domain.Entry{}, err
	}
	return s.store.Entry(ctx, entryID)
}

// appendAt places e at the end of its chain, stamps it and writes it, in the caller's transaction.
func (s *Service) appendAt(ctx context.Context, e domain.Entry, at time.Time) (domain.Entry, error) {
	place, err := s.store.Newest(ctx, e.SupplierID, e.Currency)
	if err != nil {
		return domain.Entry{}, err
	}
	placed, err := place.Append(e)
	if err != nil {
		return domain.Entry{}, err
	}
	if placed.ID, err = s.newID(); err != nil {
		return domain.Entry{}, err
	}
	placed.OccurredAt, placed.BusinessDate = at, bizdate.Date(at, s.loc)
	if err = s.store.InsertEntry(ctx, placed); err != nil {
		return domain.Entry{}, err
	}
	return placed, nil
}

func currencyOf(currencies []domain.Currency, code string) (domain.Currency, bool) {
	for _, c := range currencies {
		if c.Code == code {
			return c, true
		}
	}
	return domain.Currency{}, false
}

func decimalsOf(currencies []domain.Currency, code string) int {
	c, _ := currencyOf(currencies, code)
	return c.Decimals
}

// rateText is a rate at 10⁻⁹ as the stock book reads one typed; "" for none.
func rateText(nano int64) string {
	if nano <= 0 {
		return ""
	}
	return numinput.FormatFixed(nano, 9, 0)
}

// lineError names the purchase line a refusal from the stock book belongs to.
func lineError(err error, lineNo int) error {
	typed, ok := errs.AsError(err)
	if !ok {
		return err
	}
	return typed.WithField("lines."+strconv.Itoa(lineNo)+"."+domain.FieldQuantity, typed.Code, "invalid").WithParam("line", strconv.Itoa(lineNo))
}
