// Package contract is what other modules may use from accounting.
//
// One event: "something happened that the books should record". Nothing here reaches into the
// accounting service, and nothing here is an aggregate — the §3.2 rule that makes `contract`
// the only legal cross-module channel (enforced by module-isolation, 1.1).
//
// The direction is the point. A publisher depends on the EVENT TYPE, never on accounting: sales
// announces that an invoice was posted, and which accounts that moves is not its business. That
// is what §20.3 means by "sales code contains zero accounting logic", and it is what lets a
// country's books differ by seed data rather than by a build.
package contract

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Postable is "this happened, and the ledger should reflect it".
//
// Published on the SYNCHRONOUS domain bus, inside the caller's transaction, so the document and
// its journal entry commit together or neither does — the same guarantee the audit trail gets
// (1.7, phase D7). A sale whose accounting entry is missing is worse than a sale that failed.
type Postable struct {
	// Action is what a posting rule matches on: `<module>.<document>.<verb>`, e.g.
	// "sales.invoice.posted". Stable, and part of the contract between a module and the rules
	// an accountant wrote — renaming one silently stops the books being kept.
	//
	// Named Action rather than EventType to match Auditable (1.7), which faced the same
	// collision with the event.Event interface and resolved it the same way. Two events with
	// the same shape should not have two vocabularies.
	Action string

	CompanyID id.ID
	BranchID  id.ID

	// Date is the BUSINESS date the entry belongs to, which decides its fiscal period. Not the
	// moment the event was published: a sale keyed at 00:30 for the previous trading day
	// belongs to that day.
	Date time.Time

	DocumentType string
	DocumentID   id.ID
	// DocumentNumber is what a human recognises in a ledger — the invoice number, not its id.
	DocumentNumber string

	// Amounts are what the rules may draw on, by name, in the functional currency's minor
	// units: "document.total", "document.net", "document.tax", "document.cost".
	//
	// A flat table rather than a typed struct, deliberately. The publisher decides what it
	// offers and the rule decides what it uses, so a module can start supplying a new figure —
	// and an accountant can start posting it — with no change here and no release. A typed
	// field per amount would make every new one a change to a contract three modules share.
	Amounts map[string]int64

	// Currency and Rate carry the transaction currency when it is not the functional one
	// (§18.1), for the record on each line.
	CurrencyCode string
	RateMicro    int64

	PartnerID id.ID
	Memo      string
}

// EventType identifies the event on the bus.
//
// ONE bus event for every postable thing, rather than one per document type: accounting
// subscribes once, and a new document type needs no new subscription — only a new rule row.
// The specific event is in the EventType FIELD, where it is data a rule can match on.
func (Postable) EventType() string { return "accounting.postable" }

// AggregateType and AggregateID let the bus's envelope carry causality (0.6 §2).
func (p Postable) AggregateType() string { return p.DocumentType }
func (p Postable) AggregateID() id.ID    { return p.DocumentID }

// The amount names this build's rules use. Constants because a rule naming "document.totl"
// resolves to nothing and posts a half-entry — and that surfaces in a trial balance weeks
// later rather than at the typo.
const (
	AmountTotal = "document.total"
	AmountNet   = "document.net"
	AmountTax   = "document.tax"
	AmountCost  = "document.cost"
)
