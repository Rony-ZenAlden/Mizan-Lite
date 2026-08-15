package partner

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/settle"
)

// Stable codes for balances.
const (
	CodeLedgerMissing   = "partner.ledger_missing"
	CodeBalanceMismatch = "partner.balance_mismatch"
)

// PermBalanceView gates seeing what a partner owes or is owed.
//
// Separate from viewing the partner: a shop assistant may need a customer's telephone number
// without being shown what everybody in the town owes.
const PermBalanceView = "partner.balance.view"

// ── the ports ───────────────────────────────────────────────────────────────────
//
// A partner's balance spans sales, purchasing, and expenses. No single module owns it, and this
// one cannot import any of them (`module-isolation`) — so each contributes through a port
// satisfied in the composition root.
//
// The ports are deliberately about MONEY, not documents. This module must not learn what an
// invoice is; it asks "what does this partner owe you, and for what" and every answer has the
// same shape whatever produced it.

// Receivables reports what customers owe.
type Receivables interface {
	OpenFor(ctx context.Context, companyID, partnerID id.ID) ([]OpenItem, error)
}

// Payables reports what the business owes.
type Payables interface {
	OpenFor(ctx context.Context, companyID, partnerID id.ID) ([]OpenItem, error)
}

// ControlLedger reports what the GENERAL LEDGER says a partner's control accounts hold.
//
// The other side of invariant 5. Without it a balance is a sum of documents that agrees with
// itself and with nothing else.
type ControlLedger interface {
	// ControlBalances returns the receivable and payable balances the ledger carries for one
	// partner, as net movement in minor units. Positive receivable means owed TO the business.
	ControlBalances(ctx context.Context, companyID, partnerID id.ID) (
		receivableMinor, payableMinor int64, err error)
}

// OpenItem is one document a partner still owes something on, or is owed.
//
// The snapshot fields matter as much as the money: a statement is something a business SENDS, and
// "invoice 4471, 12 March, 1,150.00 outstanding" is what a customer can check against their own
// records. A row that said only "1,150.00" would be unarguable and therefore useless.
type OpenItem struct {
	DocumentType   string
	DocumentID     id.ID
	DocumentNumber string
	Date           string
	DueDate        string

	TotalMinor       int64
	SettledMinor     int64
	OutstandingMinor int64
}

// Balance is what a partner owes and is owed.
type Balance struct {
	PartnerID   id.ID
	PartnerName string

	// ReceivableMinor is what they owe the business.
	ReceivableMinor int64
	// PayableMinor is what the business owes them.
	PayableMinor int64

	// NetMinor is receivable less payable. POSITIVE means they owe the business on balance.
	//
	// A net figure is what somebody deciding whether to extend more credit actually wants, and
	// the two halves are kept alongside it because netting them away hides the case that matters
	// most: a partner who is both a customer and a supplier, owing 5,000 and owed 4,900, is not
	// the same risk as one who simply owes 100.
	NetMinor int64
}

// BalanceOf computes what one partner owes and is owed.
//
// # Derived, never stored
//
// §7.13 invariant 5 names partner balances, and the temptation is a `partner_balances` table
// updated on every posting. Phases 2 and 4 both rejected that shape and both were right: a
// maintained projection that is never checked is a number nobody should trust — and this one would
// be updated from three modules, which triples the chances of one of them forgetting.
func (s *Service) BalanceOf(
	ctx context.Context, companyID, partnerID id.ID,
) (Balance, error) {
	if s.receivables == nil || s.payables == nil {
		return Balance{}, errs.Internal(CodeLedgerMissing,
			"the partner service was built without the ledgers a balance needs")
	}

	partner, found, err := s.repos.PartnerByID(ctx, companyID, partnerID)
	if err != nil {
		return Balance{}, err
	}
	if !found {
		return Balance{}, errs.NotFound(CodeUnknownPartner,
			"there is no partner with that identity")
	}

	receivable, err := s.receivables.OpenFor(ctx, companyID, partnerID)
	if err != nil {
		return Balance{}, err
	}
	payable, err := s.payables.OpenFor(ctx, companyID, partnerID)
	if err != nil {
		return Balance{}, err
	}

	balance := Balance{PartnerID: partnerID, PartnerName: partner.Name}
	for _, item := range receivable {
		balance.ReceivableMinor += item.OutstandingMinor
	}
	for _, item := range payable {
		balance.PayableMinor += item.OutstandingMinor
	}
	balance.NetMinor = balance.ReceivableMinor - balance.PayableMinor
	return balance, nil
}

// Statement is a partner's open items, in the order somebody reads them.
type Statement struct {
	Balance Balance
	// Receivable and Payable are kept apart rather than merged into one signed list.
	//
	// A statement sent to a customer shows what they owe; one sent to a supplier shows what is
	// owed to them. Merging them would produce a document nobody can send to either.
	Receivable []OpenItem
	Payable    []OpenItem
}

// StatementFor builds a partner's statement.
func (s *Service) StatementFor(
	ctx context.Context, companyID, partnerID id.ID,
) (Statement, error) {
	balance, err := s.BalanceOf(ctx, companyID, partnerID)
	if err != nil {
		return Statement{}, err
	}

	receivable, err := s.receivables.OpenFor(ctx, companyID, partnerID)
	if err != nil {
		return Statement{}, err
	}
	payable, err := s.payables.OpenFor(ctx, companyID, partnerID)
	if err != nil {
		return Statement{}, err
	}

	// OLDEST FIRST, by due date then by document date. That is the order a collections call goes
	// through them in, and the order an ageing analysis reads — newest-first would put the least
	// urgent row at the top of a document whose whole purpose is urgency.
	sortOpenItems(receivable)
	sortOpenItems(payable)

	return Statement{Balance: balance, Receivable: receivable, Payable: payable}, nil
}

func sortOpenItems(items []OpenItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].DueDate, items[j].DueDate
		if left == "" {
			left = items[i].Date
		}
		if right == "" {
			right = items[j].Date
		}
		if left != right {
			return left < right
		}
		return items[i].Date < items[j].Date
	})
}

// ── the verifier ────────────────────────────────────────────────────────────────

// BalanceDiscrepancy is one partner whose documents disagree with the ledger.
type BalanceDiscrepancy struct {
	PartnerID   id.ID
	PartnerName string

	// DocumentsMinor is what the open documents come to.
	DocumentsMinor int64
	// LedgerMinor is what the control accounts say.
	LedgerMinor int64
	// DifferenceMinor is documents less ledger.
	DifferenceMinor int64
	// Side names which control account disagreed.
	Side string
}

// VerifyBalances checks every partner's open documents against the general ledger.
//
// # Invariant 5, and why it is a REPORT
//
// §7.13: *"Partner balances equal the sum of their open documents minus settlements."*
//
// The subsidiary ledger (what each partner owes) and the control account (what receivables total
// in the GL) are computed by completely different paths: one sums documents and their
// allocations, the other sums journal lines written by posting rules. They must agree, and when
// they do not, ONE of them is wrong in a way that nothing else will surface — an invoice that
// posted to the wrong partner, a payment allocated to a document in another company, a rule
// edited after the fact.
//
// It reports and does not repair, for the reason 4.3 gives about stock: a projection that
// silently heals hides the bug that broke it, and the next thing it hides is the one that
// mattered.
func (s *Service) VerifyBalances(
	ctx context.Context, companyID id.ID,
) ([]BalanceDiscrepancy, error) {
	if s.control == nil {
		return nil, errs.Internal(CodeLedgerMissing,
			"the partner service was built without a control ledger to verify against")
	}

	partners, err := s.repos.Partners(ctx, companyID, Filter{})
	if err != nil {
		return nil, err
	}

	discrepancies := make([]BalanceDiscrepancy, 0)
	for _, p := range partners {
		balance, balErr := s.BalanceOf(ctx, companyID, p.ID)
		if balErr != nil {
			return nil, balErr
		}
		receivableLedger, payableLedger, ctrlErr := s.control.ControlBalances(
			ctx, companyID, p.ID)
		if ctrlErr != nil {
			return nil, ctrlErr
		}

		if balance.ReceivableMinor != receivableLedger {
			discrepancies = append(discrepancies, BalanceDiscrepancy{
				PartnerID: p.ID, PartnerName: p.Name, Side: "receivable",
				DocumentsMinor: balance.ReceivableMinor, LedgerMinor: receivableLedger,
				DifferenceMinor: balance.ReceivableMinor - receivableLedger,
			})
		}
		if balance.PayableMinor != payableLedger {
			discrepancies = append(discrepancies, BalanceDiscrepancy{
				PartnerID: p.ID, PartnerName: p.Name, Side: "payable",
				DocumentsMinor: balance.PayableMinor, LedgerMinor: payableLedger,
				DifferenceMinor: balance.PayableMinor - payableLedger,
			})
		}
	}
	return discrepancies, nil
}

// AgeBands groups what is outstanding by how overdue it is.
//
// The four buckets every collections conversation uses. Fixed rather than configurable, because a
// business that wants different ones wants a different report, and a configurable bucket boundary
// is a setting nobody ever changes and everybody has to read past.
type AgeBands struct {
	CurrentMinor int64
	Days30Minor  int64
	Days60Minor  int64
	Days90Minor  int64
	OlderMinor   int64
}

// Age groups open items by how far past due they are, as at a date.
//
// The date is a PARAMETER rather than read from a clock: an ageing analysis printed for a month
// end must say what it said at that month end, and one that quietly re-ages itself on reopening
// is a report nobody can file.
func Age(items []OpenItem, asAt string) AgeBands {
	var bands AgeBands
	for _, item := range items {
		due := item.DueDate
		if due == "" {
			// No due date means due when it was raised. Treating it as never-due would park a
			// debt in "current" forever, which is exactly the row a collections call is about.
			due = item.Date
		}
		switch {
		case due >= asAt:
			bands.CurrentMinor += item.OutstandingMinor
		case daysBetween(due, asAt) <= 30:
			bands.Days30Minor += item.OutstandingMinor
		case daysBetween(due, asAt) <= 60:
			bands.Days60Minor += item.OutstandingMinor
		case daysBetween(due, asAt) <= 90:
			bands.Days90Minor += item.OutstandingMinor
		default:
			bands.OlderMinor += item.OutstandingMinor
		}
	}
	return bands
}

// Outstanding re-exports the kernel's rule, so callers of this package need not learn where it
// lives.
func Outstanding(totalMinor, settledMinor int64) int64 {
	return settle.Outstanding(totalMinor, settledMinor)
}

// daysBetween counts days between two ISO dates.
//
// String arithmetic on `YYYY-MM-DD` rather than time parsing, for the reason every date in this
// application is a string: a date is a CALENDAR fact, and parsing it into an instant introduces a
// timezone that then has to be decided, defended, and got wrong at a month end.
//
// Approximate to the month, which is exactly the precision an ageing analysis has: "over 60 days"
// is a bucket, not a measurement, and nobody has ever chased a debt differently because it was 61
// days rather than 62.
func daysBetween(from, to string) int {
	fy, fm, fd := splitDate(from)
	ty, tm, td := splitDate(to)
	return (ty-fy)*360 + (tm-fm)*30 + (td - fd)
}

func splitDate(date string) (year, month, day int) {
	if len(date) < 10 {
		return 0, 0, 0
	}
	return atoi(date[0:4]), atoi(date[5:7]), atoi(date[8:10])
}

func atoi(s string) int {
	out := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return out
		}
		out = out*10 + int(r-'0')
	}
	return out
}

// AttachLedgers gives the service the ports a balance needs.
//
// # Why this is set after construction rather than passed in
//
// A partner's balance needs sales, purchasing and expenses — and every one of those needs the
// partner service to check credit limits and name a counterparty. Passing the ports into
// `NewService` would need all four constructed before any of them, which is not possible.
//
// The cycle is broken where cycles always are: the module is built able to do less, and gains the
// rest once its neighbours exist. Nothing between the two points can ask for a balance, because
// the composition root is the only caller and it does both in one function.
func (s *Service) AttachLedgers(
	receivables Receivables, payables Payables, control ControlLedger,
) {
	s.receivables = receivables
	s.payables = payables
	s.control = control
}
