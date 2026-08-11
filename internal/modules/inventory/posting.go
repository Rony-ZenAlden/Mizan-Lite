package inventory

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	accountingc "github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// The postable actions this module publishes.
//
// # Why an increase and a decrease are DIFFERENT actions
//
// Phase 2's posting engine refuses a negative amount, deliberately: a negative would flip a
// line's side silently, and the ledger's "a line is debit XOR credit, both non-negative" rule is
// what makes an entry checkable at all. So the reverse operation is a different event with its
// own rule — exactly as a credit note is a different event from an invoice.
//
// The alternative, one `inventory.stock.adjusted` action carrying a signed amount, would push the
// sign into the rule engine and make every rule an accountant writes have to reason about it.
const (
	ActionStockIncreased = "inventory.stock.increased"
	ActionStockDecreased = "inventory.stock.decreased"

	// DocumentTypeAdjustment is what the ledger calls a movement that no sales or purchase
	// document caused. It appears as the document type on the journal entry, so a bookkeeper
	// reading the ledger can see the entry came from a stock adjustment rather than a sale.
	DocumentTypeAdjustment = "inventory.adjustment"
)

// postingFor decides whether inventory posts this movement, and what.
//
// # The rule that prevents double-posting
//
// A movement caused by a DOCUMENT is posted by that document's module: a receipt against a
// purchase bill is posted by purchasing, and an issue against a sales invoice is posted by
// sales — each as part of one entry that also records the payable or the revenue. If inventory
// posted those too, every purchase would debit inventory twice.
//
// A movement with no document — an adjustment somebody entered, a count, a revaluation — has
// nobody else to post it, and posting it is exactly what makes stock value reconcile to the
// inventory account. So:
//
//	DocumentType set   → somebody else posts it; inventory publishes nothing.
//	DocumentType empty → inventory posts it.
//
// A transfer is the third case and posts NOTHING either way: value moves between warehouses and
// the company's total inventory value is unchanged, so there is no entry to make. Posting a
// transfer would be a journal entry whose two sides are the same account.
func postingFor(m domain.Movement) (action string, amountMinor int64, posts bool) {
	if m.DocumentType != "" {
		return "", 0, false
	}
	switch m.Type {
	case domain.TransferIn, domain.TransferOut:
		// Nothing to post: the company owns the same goods, in a different place.
		return "", 0, false
	case domain.Issue, domain.ReturnIn:
		// An issue or a return with no document is not something this module can post
		// meaningfully — the other side is a customer, a project, or a write-off, and only the
		// caller knows which. Phase 5 supplies the document; until then there is nothing to say.
		return "", 0, false
	}

	// ValueMinor is signed: positive when stock is worth more afterwards. The sign chooses the
	// ACTION, and the amount is then always positive — which is what the engine requires.
	switch {
	case m.ValueMinor > 0:
		return ActionStockIncreased, m.ValueMinor, true
	case m.ValueMinor < 0:
		return ActionStockDecreased, -m.ValueMinor, true
	default:
		// A movement worth nothing posts nothing: an adjustment of a variant whose average cost
		// is zero is a real quantity change with no money in it.
		//
		// DEFENSIVE, not load-bearing, and the mutation drill is what established that. Phase 2's
		// engine already skips lines that resolve to zero and declines to create an entry with
		// none, so removing this changes no balance and leaves no stray journal header. What it
		// saves is the publish and the rules lookup on every costless movement — real work, but
		// not a different answer. No test pins it, because a test that passes either way would
		// claim otherwise.
		return "", 0, false
	}
}

// publishPosting announces a movement to the books, if it is inventory's to announce.
//
// Published on the SYNCHRONOUS bus inside the caller's transaction, so the movement and its
// journal entry commit together or neither does. A stock write-off whose accounting entry is
// missing is worse than a write-off that failed.
//
// # This module contains no accounting logic
//
// It says what happened and how much it was worth. Which accounts that moves is Phase 2's
// table-driven decision (§20.3) — a country that books shrinkage differently is a seed file,
// not a build.
func (s *Service) publishPosting(
	ctx context.Context, companyID, branchID id.ID, m domain.Movement,
) error {
	action, amountMinor, posts := postingFor(m)
	if !posts {
		return nil
	}

	date, ok := clock.ParseTimestamp(m.OccurredAt)
	if !ok {
		// The occurrence time decides the fiscal period. A movement whose date cannot be read
		// must not be posted to whatever period today happens to be — that is how an entry lands
		// in a closed year, or in the wrong one.
		return errs.Internal(domain.CodeInvalidMovement,
			"a stock movement has an unreadable date").WithParam("occurred_at", m.OccurredAt)
	}

	return s.bus.Publish(ctx, accountingc.Postable{
		Action:    action,
		CompanyID: companyID,
		BranchID:  branchID,
		Date:      date,

		DocumentType:   DocumentTypeAdjustment,
		DocumentID:     m.ID,
		DocumentNumber: string(m.ID),

		Amounts: map[string]int64{
			accountingc.AmountTotal: amountMinor,
			// The cost is the same figure here, offered under its own name so that a rule an
			// accountant writes can draw on whichever reads more naturally to them.
			accountingc.AmountCost: amountMinor,
		},

		Memo: m.Reason,
	})
}
