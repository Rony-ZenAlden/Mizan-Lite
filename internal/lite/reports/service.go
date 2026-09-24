// Package reports is Mizan Lite's reports: profit per day, month and product; stock value, its reconciliation and the
// profit on the shelf; and the cash drawer (L6). It owns no table and writes nothing. Each module supplies its own facts
// through a port declared here, and the composition root satisfies each port with that module's service (A-L6.4).
package reports

import (
	"context"
	"sort"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// CodeOwnerRequired is the owner module's refusal; a bootstrap test holds it equal.
const CodeOwnerRequired = "lite.owner.required"

// Sales supplies the sales rung up or voided in a range, with their lines.
type Sales interface {
	Facts(ctx context.Context, from, to string) ([]domain.Sale, error)
}

// Returns is where the returns of a period come from (2026-09-20). Wired after construction (UseReturns) so the
// module's fakes and its existing tests keep compiling where returns are not in play; nil reads as no returns, which
// is what every shop before this release had.
type Returns interface {
	ReturnsBetween(ctx context.Context, from, to string) ([]domain.Return, error)
}

// Payables is where the drawer's supplier money comes from (0.10.0): payments to suppliers out of the drawer and their
// refunds into it. Wired after construction (UsePayables); nil reads as none, which is every shop before 0.10.0.
type Payables interface {
	SupplierCashBetween(ctx context.Context, from, to string) ([]domain.SupplierCash, error)
	// DamagedOnArrival is the goods that arrived damaged on the period's purchases — the loss report shows them.
	DamagedOnArrival(ctx context.Context, from, to string) ([]domain.ArrivalDamage, error)
}

// Stock supplies ledger rows.
type Stock interface {
	// Between returns the rows of business dates from..to, by product and place.
	Between(ctx context.Context, from, to string) ([]domain.Movement, error)
	// LastOnOrBefore returns each product's newest row on or before a business date.
	LastOnOrBefore(ctx context.Context, businessDate string) ([]domain.Movement, error)
}

// Debts supplies debt book entries.
type Debts interface {
	EntriesBetween(ctx context.Context, from, to string) ([]domain.DebtEntry, error)
}

// Rates supplies every rate with its business date, and the local currency.
type Rates interface {
	AllRates(ctx context.Context) ([]domain.Rate, string, error)
}

// Catalogue supplies products and currencies.
type Catalogue interface {
	Products(ctx context.Context) ([]domain.Product, error)
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// CashBook supplies the cash book.
type CashBook interface {
	Between(ctx context.Context, from, to string) ([]domain.CashEntry, error)
	LastCountBefore(ctx context.Context, businessDate, currency string) (domain.CashEntry, bool, error)
}

// OwnerGate is what the reports need of the owner: whether owner mode is on. Viewing records nothing.
type OwnerGate interface {
	Allowed(ctx context.Context) bool
}

// Service computes reports. Every method reads; none writes.
type Service struct {
	sales     Sales
	stock     Stock
	debts     Debts
	rates     Rates
	catalogue Catalogue
	cash      CashBook
	returns   Returns
	payables  Payables
	gate      OwnerGate
	clk       clock.Clock
	loc       *time.Location
}

// NewService builds the service.
func NewService(sales Sales, stock Stock, debts Debts, rates Rates, catalogue Catalogue, cash CashBook, gate OwnerGate,
	clk clock.Clock, loc *time.Location) *Service {
	return &Service{sales: sales, stock: stock, debts: debts, rates: rates, catalogue: catalogue, cash: cash, gate: gate, clk: clk, loc: loc}
}

func ownerRequired() error {
	return errs.Permission(CodeOwnerRequired, "the owner's PIN is required")
}

// Today is the shop's business date now.
func (s *Service) Today() string { return bizdate.Date(s.clk.Now(), s.loc) }

func (s *Service) dateOrToday(raw string) (string, error) {
	if raw == "" {
		return s.Today(), nil
	}
	_, err := domain.ParseDate(raw)
	return raw, err
}

func (s *Service) pair(ctx context.Context) (domain.Pair, []domain.Rate, error) {
	rates, local, err := s.rates.AllRates(ctx)
	if err != nil {
		return domain.Pair{}, nil, err
	}
	currencies, err := s.catalogue.Currencies(ctx)
	if err != nil {
		return domain.Pair{}, nil, err
	}
	p := domain.Pair{Local: domain.Currency{Code: local}, USD: domain.Currency{Code: domain.USD, Decimals: 2}}
	for _, c := range currencies {
		switch c.Code {
		case local:
			p.Local = c
		case domain.USD:
			p.USD = c
		}
	}
	return p, rates, nil
}

func (s *Service) facts(ctx context.Context, from, to string) (domain.Facts, error) {
	f := domain.Facts{}
	var err error
	if f.Pair, f.Rates, err = s.pair(ctx); err != nil {
		return f, err
	}
	if f.Sales, err = s.sales.Facts(ctx, from, to); err != nil {
		return f, err
	}
	if f.Movements, err = s.stock.Between(ctx, from, to); err != nil {
		return f, err
	}
	if f.Debts, err = s.debts.EntriesBetween(ctx, from, to); err != nil {
		return f, err
	}
	if f.Cash, err = s.cash.Between(ctx, from, to); err != nil {
		return f, err
	}
	if s.returns != nil {
		f.Returns, err = s.returns.ReturnsBetween(ctx, from, to)
	}
	return f, err
}

// UseReturns gives the reports the returns of a period. The composition root calls it once.
func (s *Service) UseReturns(r Returns) { s.returns = r }

// UsePayables gives the drawer the payables book's money. The composition root calls it once.
func (s *Service) UsePayables(p Payables) { s.payables = p }

// Day is a business day's statement — today when empty. Owner only (Q-L6.7).
func (s *Service) Day(ctx context.Context, date string) (domain.Day, error) {
	if !s.gate.Allowed(ctx) {
		return domain.Day{}, ownerRequired()
	}
	date, err := s.dateOrToday(date)
	if err != nil {
		return domain.Day{}, err
	}
	f, err := s.facts(ctx, date, date)
	if err != nil {
		return domain.Day{}, err
	}
	return domain.DayOf(f, date), nil
}

// Pair is the shop's two currencies, for formatting.
func (s *Service) Pair(ctx context.Context) (domain.Pair, error) {
	p, _, err := s.pair(ctx)
	return p, err
}

// Month is a calendar month by business date — this month when empty. Owner only.
func (s *Service) Month(ctx context.Context, month string) (domain.Month, error) {
	if !s.gate.Allowed(ctx) {
		return domain.Month{}, ownerRequired()
	}
	if month == "" {
		month = s.Today()[:7]
	}
	from, to, err := domain.MonthRange(month)
	if err != nil {
		return domain.Month{}, err
	}
	f, err := s.facts(ctx, from, to)
	if err != nil {
		return domain.Month{}, err
	}
	return domain.MonthOf(f, month, from, to), nil
}

// Period is the profit of any span of days — a week, a year, the last thirty days, or a range the shop typed (the owner's
// request, 2026-09-17). It is the month report over different bounds: MonthOf was already written in terms of a range, so
// what a period needed was a way to ask for one, not a second way to add days up.
//
// Empty bounds mean the month to date, as every other ranged report here does.
func (s *Service) Period(ctx context.Context, from, to string) (domain.Month, error) {
	if !s.gate.Allowed(ctx) {
		return domain.Month{}, ownerRequired()
	}
	start, end, err := s.Range(from, to)
	if err != nil {
		return domain.Month{}, err
	}
	f, err := s.facts(ctx, start, end)
	if err != nil {
		return domain.Month{}, err
	}
	// The label is the range itself: a period is not a month and must not claim to be one.
	return domain.MonthOf(f, "", start, end), nil
}

func (s *Service) productsByID(ctx context.Context) ([]domain.Product, map[id.ID]domain.Product, error) {
	all, err := s.catalogue.Products(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[id.ID]domain.Product, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}
	return all, byID, nil
}

// Range checks a range, defaulting to the month to date.
func (s *Service) Range(from, to string) (string, string, error) {
	today := s.Today()
	if to == "" {
		to = today
	}
	if from == "" {
		from = to[:8] + "01"
	}
	return from, to, domain.ParseRange(from, to)
}

// Products is per-product profit over a range — the month to date when empty. Owner only.
func (s *Service) Products(ctx context.Context, from, to string) (domain.Products, error) {
	if !s.gate.Allowed(ctx) {
		return domain.Products{}, ownerRequired()
	}
	from, to, err := s.Range(from, to)
	if err != nil {
		return domain.Products{}, err
	}
	pair, _, err := s.pair(ctx)
	if err != nil {
		return domain.Products{}, err
	}
	sales, err := s.sales.Facts(ctx, from, to)
	if err != nil {
		return domain.Products{}, err
	}
	_, byID, err := s.productsByID(ctx)
	if err != nil {
		return domain.Products{}, err
	}
	return domain.ProductsOver(sales, from, to, pair, byID), nil
}

// Losses is a period's losses — the month to date when empty — at what the stock cost: written off as damaged, expired
// or spoiled, given away, found short; and the goods that arrived damaged, which the shop never paid for (0.10.0). Owner
// only: every figure is a cost.
func (s *Service) Losses(ctx context.Context, from, to string) (domain.LossReport, error) {
	if !s.gate.Allowed(ctx) {
		return domain.LossReport{}, ownerRequired()
	}
	from, to, err := s.Range(from, to)
	if err != nil {
		return domain.LossReport{}, err
	}
	pair, rates, err := s.pair(ctx)
	if err != nil {
		return domain.LossReport{}, err
	}
	rows, err := s.stock.Between(ctx, from, to)
	if err != nil {
		return domain.LossReport{}, err
	}
	_, byID, err := s.productsByID(ctx)
	if err != nil {
		return domain.LossReport{}, err
	}
	var arrival []domain.ArrivalDamage
	if s.payables != nil {
		if arrival, err = s.payables.DamagedOnArrival(ctx, from, to); err != nil {
			return domain.LossReport{}, err
		}
	}
	return domain.LossesBetween(rows, byID, rates, pair, from, to, arrival), nil
}

// StockReport is the stock on a date, a period's movements to it reconciled, and the profit on the shelf now.
type StockReport struct {
	Value          domain.StockValue
	Reconciliation domain.Reconciliation
	Shelf          domain.Shelf
}

// endOfTime is a business date after every row.
const endOfTime = "9999-12-31"

// Stock is the stock report for a range — the month to date when empty. Owner only.
func (s *Service) Stock(ctx context.Context, from, to string) (StockReport, error) {
	if !s.gate.Allowed(ctx) {
		return StockReport{}, ownerRequired()
	}
	from, to, err := s.Range(from, to)
	if err != nil {
		return StockReport{}, err
	}
	pair, rates, err := s.pair(ctx)
	if err != nil {
		return StockReport{}, err
	}
	all, byID, err := s.productsByID(ctx)
	if err != nil {
		return StockReport{}, err
	}
	before, err := s.stock.LastOnOrBefore(ctx, domain.AddDays(from, -1))
	if err != nil {
		return StockReport{}, err
	}
	rows, err := s.stock.Between(ctx, from, to)
	if err != nil {
		return StockReport{}, err
	}
	end, err := s.stock.LastOnOrBefore(ctx, to)
	if err != nil {
		return StockReport{}, err
	}
	now, err := s.stock.LastOnOrBefore(ctx, endOfTime)
	if err != nil {
		return StockReport{}, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].NameAR < all[j].NameAR })
	return StockReport{
		Value:          domain.ValueOn(end, to, rates, pair, byID),
		Reconciliation: domain.Reconcile(from, to, before, rows, end, pair),
		Shelf:          domain.ShelfProfit(now, all, rates, pair),
	}, nil
}

// Drawer is a business day's drawer — today when empty. Anyone at the counter may see it (Q-L6.7); outside owner mode the
// cash book's categories and notes are cleared, and expenses paid from the drawer are shown with withdrawals as money
// taken out, here rather than on the screen, so no caller can forget to.
type Drawer struct {
	domain.Drawer
	OwnerView bool
}

// Drawer computes the drawer.
func (s *Service) Drawer(ctx context.Context, date string) (Drawer, error) {
	date, err := s.dateOrToday(date)
	if err != nil {
		return Drawer{}, err
	}
	d, err := s.drawer(ctx, date)
	if err != nil {
		return Drawer{}, err
	}
	out := Drawer{Drawer: d, OwnerView: s.gate.Allowed(ctx)}
	if !out.OwnerView {
		for i := range out.Currencies {
			t := &out.Currencies[i]
			t.WithdrawalOut += t.ExpensesOut
			t.ExpensesOut = 0
		}
		var visible []domain.CashEntry
		for _, e := range out.Entries {
			if e.Kind == domain.CashCount {
				visible = append(visible, e)
			}
		}
		out.Entries = visible
	}
	return out, nil
}

func (s *Service) drawer(ctx context.Context, date string) (domain.Drawer, error) {
	pair, _, err := s.pair(ctx)
	if err != nil {
		return domain.Drawer{}, err
	}
	f := domain.DrawerFacts{Pair: pair, LastCounts: map[string]domain.CashEntry{}}
	from := date
	for _, c := range []string{pair.USD.Code, pair.Local.Code} {
		last, found, countErr := s.cash.LastCountBefore(ctx, date, c)
		if countErr != nil {
			return domain.Drawer{}, countErr
		}
		start := "0001-01-01" // never counted: every day before counts
		if found {
			f.LastCounts[c] = last
			start = domain.AddDays(last.BusinessDate, 1)
		}
		if start < from {
			from = start
		}
	}
	if f.Sales, err = s.sales.Facts(ctx, from, date); err != nil {
		return domain.Drawer{}, err
	}
	if f.Debts, err = s.debts.EntriesBetween(ctx, from, date); err != nil {
		return domain.Drawer{}, err
	}
	if f.Cash, err = s.cash.Between(ctx, from, date); err != nil {
		return domain.Drawer{}, err
	}
	// A cash return takes notes out of the drawer, and like a cash sale it writes no cash entry: the drawer derives
	// it from the return itself, so it must be read here or the drawer is over by every refund.
	if s.returns != nil {
		if f.Returns, err = s.returns.ReturnsBetween(ctx, from, date); err != nil {
			return domain.Drawer{}, err
		}
	}
	// Money paid to a supplier out of the drawer is gone from it as surely as an expense (0.10.0).
	if s.payables != nil {
		if f.Suppliers, err = s.payables.SupplierCashBetween(ctx, from, date); err != nil {
			return domain.Drawer{}, err
		}
	}
	return domain.DrawerOf(f, date), nil
}

// ExpectedCash is what the drawer should hold in a currency at the end of a business date — the figure a count is recorded
// against. Unguarded: the counter counts.
func (s *Service) ExpectedCash(ctx context.Context, date, currency string) (int64, error) {
	d, err := s.drawer(ctx, date)
	if err != nil {
		return 0, err
	}
	for _, t := range d.Currencies {
		if t.Currency == currency {
			return t.Expected, nil
		}
	}
	return 0, nil
}
