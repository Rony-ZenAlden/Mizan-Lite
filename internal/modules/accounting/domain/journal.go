package domain

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes for the ledger.
const (
	CodeUnbalanced      = "accounting.unbalanced_entry"
	CodeEmptyEntry      = "accounting.empty_entry"
	CodeInvalidLine     = "accounting.invalid_line"
	CodeEntryNotPosted  = "accounting.entry_not_posted"
	CodeAlreadyReversed = "accounting.already_reversed"
	CodeEntryImmutable  = "accounting.entry_immutable"
)

// EntryStatus is where an entry is in its short life.
//
// Short on purpose: an entry is drafted, posted, and possibly reversed. There is no "edited",
// no "cancelled", and no "deleted" — every correction is another entry (§20.2).
type EntryStatus string

const (
	Draft    EntryStatus = "draft"
	Posted   EntryStatus = "posted"
	Reversed EntryStatus = "reversed"
)

// Line is one side of one posting.
//
// Debit XOR credit, both non-negative, in the FUNCTIONAL currency as minor units. A line
// carrying both sides is not a posting — it is two postings somebody netted off, and netting
// destroys the trail the ledger exists to keep.
type Line struct {
	Number    int
	AccountID id.ID

	Debit  int64
	Credit int64

	// The transaction currency (§18.1), when it differs from the functional one. The ledger
	// balances in the functional currency; this is what an auditor asks about.
	CurrencyCode  string
	OriginalMinor int64
	RateMicro     int64

	BranchID     id.ID
	WarehouseID  id.ID
	PartnerID    id.ID
	ProductID    id.ID
	CostCenterID id.ID
	ProjectID    id.ID

	Memo string
}

// Amount is the line's magnitude regardless of side, for totalling.
func (l Line) Amount() int64 {
	if l.Debit > 0 {
		return l.Debit
	}
	return l.Credit
}

// Side reports which column the line lands in.
func (l Line) Side() Side {
	if l.Debit > 0 {
		return Debit
	}
	return Credit
}

// Entry is a balanced set of postings — the ledger's aggregate.
type Entry struct {
	ID        id.ID
	CompanyID id.ID
	Number    string

	Date     time.Time
	PeriodID id.ID
	Status   EntryStatus

	SourceModule       string
	SourceDocumentType string
	SourceDocumentID   id.ID

	ReversalOf id.ID

	Memo     string
	BranchID id.ID

	PostedAt time.Time
	PostedBy id.ID

	Lines []Line
}

// NewEntry builds a balanced entry, or refuses.
//
// # Why this is a constructor and not a validator
//
// The balance rule is the ledger's one inviolable invariant (§20.2), and a validator is
// something a caller can forget to run. An aggregate that CANNOT BE CONSTRUCTED in an invalid
// state has no such failure mode — the same shape as money.Money, which cannot hold a currency
// it does not know, and domain.NewSession, which cannot produce a session with no expiry.
//
// The check is on the FUNCTIONAL-currency columns only. A multi-currency entry balances in the
// books' currency; the original amounts are records, not arithmetic, and requiring them to
// balance too would make any exchange difference impossible to post.
func NewEntry(
	identifier, companyID id.ID, date time.Time, periodID id.ID, sourceModule string, lines []Line,
) (Entry, error) {
	if identifier.IsZero() || companyID.IsZero() {
		return Entry{}, errs.Validation(CodeInvalidLine, "an entry needs an identity and a company")
	}
	if periodID.IsZero() {
		return Entry{}, errs.Validation(CodeInvalidLine, "an entry must belong to a fiscal period")
	}
	if sourceModule == "" {
		return Entry{}, errs.Validation(CodeInvalidLine,
			"an entry must record what produced it")
	}
	if len(lines) == 0 {
		return Entry{}, errs.Validation(CodeEmptyEntry, "an entry needs at least one line")
	}

	var debits, credits int64
	numbered := make([]Line, 0, len(lines))
	for i, line := range lines {
		if line.AccountID.IsZero() {
			return Entry{}, errs.Validation(CodeInvalidLine, "a line needs an account").
				WithParam("line", itoa(i+1))
		}
		if line.Debit < 0 || line.Credit < 0 {
			return Entry{}, errs.Validation(CodeInvalidLine,
				"a line cannot carry a negative amount").WithParam("line", itoa(i+1))
		}
		if line.Debit > 0 && line.Credit > 0 {
			return Entry{}, errs.Validation(CodeInvalidLine,
				"a line is either a debit or a credit, never both").WithParam("line", itoa(i+1))
		}
		if line.Debit == 0 && line.Credit == 0 {
			return Entry{}, errs.Validation(CodeInvalidLine,
				"a line of zero has nothing to record").WithParam("line", itoa(i+1))
		}

		debits += line.Debit
		credits += line.Credit

		line.Number = i + 1
		numbered = append(numbered, line)
	}

	if debits != credits {
		// The difference is in the message because it is the first thing anybody asks, and
		// hunting it down by hand across forty lines is the tedious part.
		return Entry{}, errs.Validation(CodeUnbalanced,
			"the debits and credits of an entry must be equal").
			WithParam("debits", itoa64(debits)).
			WithParam("credits", itoa64(credits)).
			WithParam("difference", itoa64(debits-credits))
	}

	return Entry{
		ID: identifier, CompanyID: companyID,
		Date: date, PeriodID: periodID, Status: Draft,
		SourceModule: sourceModule, Lines: numbered,
	}, nil
}

// Total is the entry's magnitude — the debit side, which equals the credit side by construction.
func (e Entry) Total() int64 {
	var total int64
	for _, line := range e.Lines {
		total += line.Debit
	}
	return total
}

// IsBalanced re-checks the invariant.
//
// # Why this exists when the constructor already guarantees it
//
// Because the constructor only guards entries that come THROUGH it. An entry read back from the
// database, restored from a backup, or written by a repair script has never met it. This is what
// the posting service calls before it writes and what the integrity job calls over history —
// the second and third of the three guards Phase 2 §2.2 asks for.
func (e Entry) IsBalanced() bool {
	var debits, credits int64
	for _, line := range e.Lines {
		debits += line.Debit
		credits += line.Credit
	}
	return debits == credits
}

// RequirePostable refuses an entry that is not in a state to be posted.
func (e Entry) RequirePostable() error {
	if e.Status != Draft {
		return errs.Conflict(CodeEntryImmutable,
			"only a draft entry can be posted").WithParam("status", string(e.Status))
	}
	if !e.IsBalanced() {
		return errs.Validation(CodeUnbalanced, "the entry does not balance")
	}
	return nil
}

// Reverse builds the correcting entry for a posted one.
//
// # Why a new entry rather than an edit
//
// §20.2: "a posted entry is never modified or deleted". The reversal is a first-class entry with
// every debit and credit swapped, linked back by ReversalOf — so the ledger shows what was
// posted, that it was corrected, and when. An edit would show only the current opinion, which
// is exactly what a tax authority is not interested in.
//
// It is dated on its OWN date, not the original's: reversing a March entry in May is a May
// event, and back-dating it would silently restate a month that may already be closed.
func (e Entry) Reverse(identifier id.ID, on time.Time, periodID id.ID, memo string) (Entry, error) {
	if e.Status != Posted {
		return Entry{}, errs.Conflict(CodeEntryNotPosted,
			"only a posted entry can be reversed").WithParam("status", string(e.Status))
	}

	lines := make([]Line, 0, len(e.Lines))
	for _, line := range e.Lines {
		mirrored := line
		mirrored.Debit, mirrored.Credit = line.Credit, line.Debit
		mirrored.OriginalMinor = -line.OriginalMinor
		lines = append(lines, mirrored)
	}

	reversal, err := NewEntry(identifier, e.CompanyID, on, periodID, e.SourceModule, lines)
	if err != nil {
		return Entry{}, err
	}
	reversal.ReversalOf = e.ID
	reversal.SourceDocumentType = e.SourceDocumentType
	reversal.SourceDocumentID = e.SourceDocumentID
	reversal.BranchID = e.BranchID
	reversal.Memo = memo
	return reversal, nil
}

// itoa avoids a strconv import in a file that needs it twice, and keeps the error parameters
// as the strings the i18n boundary requires (§22.2).
func itoa(n int) string { return itoa64(int64(n)) }

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
