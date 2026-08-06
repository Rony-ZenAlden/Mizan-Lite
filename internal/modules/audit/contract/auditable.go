// Package contract is what other modules may use from audit.
//
// It contains one type: the event a module publishes to say that something happened. Nothing
// here reaches into the audit service, and nothing here is an aggregate — the §3.2 rule that
// makes `contract` the only legal cross-module channel (enforced by module-isolation, 1.1).
//
// The direction matters. A publisher depends on the EVENT TYPE, never on the audit service:
// a module announces that a password changed, and whether anyone records it is not its
// business. That is §3.2's "downstream modules react to events" preference, and it is what
// keeps the audit module removable.
package contract

import (
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Sources an entry can come from (§15.1).
const (
	SourceUI     = "ui"
	SourceJob    = "job"
	SourceImport = "import"
	SourceSystem = "system"
)

// Auditable is "this happened, and it is worth recording".
//
// Published on the SYNCHRONOUS domain bus, inside the caller's transaction, so the entry and
// the change it records commit together or neither does (phase D7). Publishing it is therefore
// not fire-and-forget: if recording fails, the operation fails.
type Auditable struct {
	// Action is `<module>.<entity>.<verb>`, e.g. "identity.user.password_changed". Stable, and
	// part of what an auditor reads years later — renaming one orphans the history.
	Action string

	EntityType string
	EntityID   id.ID
	// EntityLabel is a SNAPSHOT of how the entity was named at the time, so an entry stays
	// readable after the thing is renamed or deleted (§15.1).
	EntityLabel string

	// Before and After are the domain values themselves; the subscriber marshals them.
	//
	// Passing values rather than pre-built JSON keeps the payload shape consistent across
	// modules — if every publisher built its own, nothing would be comparable. A nil Before
	// means a creation, a nil After means a removal, and both store SQL NULL rather than the
	// string "null", so "there was no before state" stays distinguishable.
	Before any
	After  any

	// Changed names the fields that differed, when the publisher knows. Optional: it is a
	// convenience for the viewer, not a source of truth — the payloads are.
	Changed []string

	// Source overrides the value derived from the context. Empty is the normal case.
	Source string
}

// EventType identifies the event on the bus.
//
// One type for every audited action rather than one per action: the audit module subscribes
// once and switches on nothing, and a new audited action needs no new subscription. The
// specific action lives in the Action field, where it is data.
func (Auditable) EventType() string { return "audit.auditable" }

// AggregateType and AggregateID let the bus's envelope carry causality (0.6 §2).
func (a Auditable) AggregateType() string { return a.EntityType }
func (a Auditable) AggregateID() id.ID    { return a.EntityID }
