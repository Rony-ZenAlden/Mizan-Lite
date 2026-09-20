// Package cashbook is Mizan Lite's cash book: expenses, money taken from or put into the drawer, and closing counts
// (L6 §7.3). Everything else the drawer holds is read from sales and the debt book by the reports.
package cashbook

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// The acts only the owner may do (Q-L6.3, Q-L6.7). A count is the counter's.
const (
	ActExpense    = "cashbook.expense"
	ActWithdrawal = "cashbook.withdrawal"
	ActDeposit    = "cashbook.deposit"
	ActReverse    = "cashbook.reverse"
)

// CodeOwnerRequired is the owner module's refusal; a bootstrap test holds it equal.
const CodeOwnerRequired = "lite.owner.required"

// Store persists the cash book. infra/sqlite implements it; cashbooktest.Fake implements it in memory, and one contract
// suite runs against both.
type Store interface {
	// NextSeq is one more than the highest place, or 1.
	NextSeq(ctx context.Context) (int64, error)
	Insert(ctx context.Context, e domain.Entry) error
	// Entry returns an entry, or CodeEntryNotFound.
	Entry(ctx context.Context, entryID id.ID) (domain.Entry, error)
	ReversalOf(ctx context.Context, entryID id.ID) (domain.Entry, bool, error)
	// Between returns the entries of business dates from..to inclusive, by place.
	Between(ctx context.Context, from, to string) ([]domain.Entry, error)
	// Counts returns every count of a currency on business dates before a date, by place.
	CountsBefore(ctx context.Context, businessDate, currency string) ([]domain.Entry, error)
}

// Rates is what the cash book needs of the exchange rate: the rate in force, to snapshot on money.
type Rates interface {
	InForce(ctx context.Context) (rateID id.ID, localPerUSDNano int64, localCurrency string, found bool, err error)
}

// Currencies is what the cash book needs of the catalogue.
type Currencies interface {
	Currencies(ctx context.Context) ([]domain.Currency, error)
}

// OwnerGate is what the cash book needs of the owner.
type OwnerGate interface {
	Require(ctx context.Context, act GuardedAct) error
}

// Expected is what the drawer should hold at the end of a business date in a currency — the reports' figure, reached
// through the composition root so a count records what it was measured against.
type Expected interface {
	ExpectedCash(ctx context.Context, businessDate, currency string) (int64, error)
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

// Service is the cash book.
type Service struct {
	tx         Transactor
	store      Store
	rates      Rates
	currencies Currencies
	gate       OwnerGate
	expected   Expected
	clk        clock.Clock
	loc        *time.Location
	newID      func() (id.ID, error)
}

// NewService builds the service.
func NewService(tx Transactor, store Store, rates Rates, currencies Currencies, gate OwnerGate, expected Expected, clk clock.Clock, loc *time.Location) *Service {
	return &Service{tx: tx, store: store, rates: rates, currencies: currencies, gate: gate, expected: expected, clk: clk, loc: loc, newID: id.New}
}

// RecordInput is an expense, a withdrawal or a deposit as typed.
type RecordInput struct {
	Kind       domain.Kind
	Currency   string
	Amount     string
	Category   string
	FromDrawer bool
	// Recurrence is "" or "once" for the day's small change, "monthly" for rent and the bills (2026-09-20).
	Recurrence string
	Note       string
}

// Record writes an expense, a withdrawal or a deposit. Owner only; each snapshots the rate in force (L6 §8).
func (s *Service) Record(ctx context.Context, in RecordInput) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		cur, err := s.currency(ctx, in.Currency)
		if err != nil {
			return err
		}
		e, err := domain.NewMoney(domain.Draft{Kind: in.Kind, Currency: cur, Amount: in.Amount, Category: in.Category,
			FromDrawer: in.FromDrawer, Recurrence: in.Recurrence, Note: in.Note})
		if err != nil {
			return err
		}
		rateID, nano, _, found, err := s.rates.InForce(ctx)
		if err != nil {
			return err
		}
		if !found {
			return errs.Conflict(domain.CodeNoRate, "set an exchange rate before recording money")
		}
		e.RateID, e.RateNano = rateID, nano
		action := map[domain.Kind]string{domain.KindExpense: ActExpense, domain.KindWithdrawal: ActWithdrawal, domain.KindDeposit: ActDeposit}[e.Kind]
		if err = s.gate.Require(ctx, GuardedAct{Action: action, After: text(e.AmountMinor, cur) + " " + e.Category + " " + e.Note}); err != nil {
			return err
		}
		out, err = s.write(ctx, e)
		return err
	})
	return out, err
}

// CountInput is a closing count as typed.
type CountInput struct {
	Currency string
	Counted  string
	Note     string
}

// Count records what the drawer holds now, in one currency, against what it was expected to hold today. Anyone at the
// counter may (Q-L6.7).
func (s *Service) Count(ctx context.Context, in CountInput) (domain.Entry, error) {
	var out domain.Entry
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		cur, err := s.currency(ctx, in.Currency)
		if err != nil {
			return err
		}
		today := bizdate.Date(s.clk.Now(), s.loc)
		expected, err := s.expected.ExpectedCash(ctx, today, cur.Code)
		if err != nil {
			return err
		}
		e, err := domain.NewCount(cur, in.Counted, expected, in.Note)
		if err != nil {
			return err
		}
		out, err = s.write(ctx, e)
		return err
	})
	return out, err
}

// Reverse undoes an entry once, with a reason. Owner only.
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
			return errs.Conflict(domain.CodeAlreadyReversed, "this entry was already reversed")
		}
		r, err := domain.Reverse(original, reason)
		if err != nil {
			return err
		}
		if err = s.gate.Require(ctx, GuardedAct{Action: ActReverse, SubjectID: original.ID, Before: string(original.Kind), After: r.Note}); err != nil {
			return err
		}
		out, err = s.write(ctx, r)
		return err
	})
	return out, err
}

func (s *Service) write(ctx context.Context, e domain.Entry) (domain.Entry, error) {
	var err error
	if e.ID, err = s.newID(); err != nil {
		return domain.Entry{}, err
	}
	if e.Seq, err = s.store.NextSeq(ctx); err != nil {
		return domain.Entry{}, err
	}
	e.OccurredAt = s.clk.Now().UTC().Truncate(time.Millisecond)
	e.BusinessDate = bizdate.Date(e.OccurredAt, s.loc)
	return e, s.store.Insert(ctx, e)
}

// Fact is an entry as the reports read it: a reversal with the entry it undoes.
type Fact struct {
	Entry    domain.Entry
	Reverses domain.Entry
	// Reversed is true when a later entry undoes this one.
	Reversed bool
}

// Between returns the cash book's entries of business dates from..to, each reversal with its original (L6 §9.1).
func (s *Service) Between(ctx context.Context, from, to string) ([]Fact, error) {
	entries, err := s.store.Between(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]Fact, 0, len(entries))
	for _, e := range entries {
		f := Fact{Entry: e}
		if e.Kind == domain.KindReversal {
			if f.Reverses, err = s.store.Entry(ctx, e.ReversesID); err != nil {
				return nil, err
			}
		}
		if _, f.Reversed, err = s.store.ReversalOf(ctx, e.ID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// LastCountBefore returns the newest count of a currency on a business date before the one given that no reversal
// undid — where the drawer's expected cash starts (L6 §7.3).
func (s *Service) LastCountBefore(ctx context.Context, businessDate, currency string) (domain.Entry, bool, error) {
	counts, err := s.store.CountsBefore(ctx, businessDate, currency)
	if err != nil {
		return domain.Entry{}, false, err
	}
	for i := len(counts) - 1; i >= 0; i-- {
		_, reversed, err := s.store.ReversalOf(ctx, counts[i].ID)
		if err != nil {
			return domain.Entry{}, false, err
		}
		if !reversed {
			return counts[i], true, nil
		}
	}
	return domain.Entry{}, false, nil
}

// Entry returns one entry.
func (s *Service) Entry(ctx context.Context, entryID id.ID) (domain.Entry, error) {
	return s.store.Entry(ctx, entryID)
}

func (s *Service) currency(ctx context.Context, code string) (domain.Currency, error) {
	currencies, err := s.currencies.Currencies(ctx)
	if err != nil {
		return domain.Currency{}, err
	}
	for _, c := range currencies {
		if c.Code == code {
			return c, nil
		}
	}
	return domain.Currency{}, errs.Validation(domain.CodeUnknownCurrency, "unknown currency").WithParam("value", code)
}

func text(minor int64, c domain.Currency) string {
	return numinput.FormatFixed(minor, c.Decimals, c.Decimals) + " " + c.Code
}
