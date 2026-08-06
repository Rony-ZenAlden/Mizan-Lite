package audit

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit/contract"
)

// CodeWriteFailed is the stable code for a failed audit write.
//
// It reaches the user, because a failed audit write ABORTS the operation that caused it
// (phase D7) — so this is what a cashier sees when the trail cannot be written.
const CodeWriteFailed = "audit.write_failed"

// Actor is who is acting, as audit needs to record them.
//
// Declared HERE rather than imported from internal/api/appctx: dependencies point inward
// (§3.1), and a module reaching into the API layer would invert that. The composition root
// supplies an adapter — the same shape identity uses to reach org.
type Actor struct {
	UserID      id.ID
	DisplayName string
	BranchID    id.ID
	SessionID   id.ID
}

// ActorResolver reports who is acting, if anyone is.
//
// "If anyone is" matters: the setup wizard and background jobs act with no user, and an
// unattributed entry is honest where a fabricated one is not.
type ActorResolver interface {
	Actor(ctx context.Context) (Actor, bool)
}

// record writes one audit entry, INSIDE the caller's transaction.
//
// # The atomicity guarantee (phase D7)
//
// This is the whole point of the step, and it is almost entirely inherited:
//
//   - The event bus runs subscribers synchronously in the caller's goroutine and transaction
//     (0.6 §3.2), so ctx here IS the business transaction's context.
//   - db.Writer(ctx) resolves to the live transaction (0.3 §3.2), so the INSERT joins it.
//   - Returning an error aborts the publish, which propagates out of the caller's Do and rolls
//     everything back (0.6 §3.2) — and a panic here is recovered into an error rather than
//     swallowed (0.6 §3.3), because a recovered panic that let the commit proceed would be
//     strictly worse than the crash.
//
// So: either the change and its audit entry both commit, or neither does. There is deliberately
// NO Do() in this function — opening one would join the caller's transaction and add nothing,
// and opening a separate connection would destroy the guarantee.
//
// The cost, stated where someone will read it: IF THIS FAILS, THE BUSINESS OPERATION FAILS. A
// shop that cannot record what it is doing should stop, not continue silently.
func (s *Service) record(ctx context.Context, e contract.Auditable) error {
	if e.Action == "" {
		return errs.Validation(CodeWriteFailed, "an audit entry needs an action")
	}

	before, err := marshalPayload(e.Before)
	if err != nil {
		return err
	}
	after, err := marshalPayload(e.After)
	if err != nil {
		return err
	}

	entry := Entry{
		Action:        e.Action,
		EntityType:    e.EntityType,
		EntityID:      e.EntityID,
		EntityLabel:   e.EntityLabel,
		BeforeJSON:    before,
		AfterJSON:     after,
		ChangedFields: joinChanged(e.Changed),
		Source:        e.Source,
	}

	if actor, ok := s.actors.Actor(ctx); ok {
		entry.ActorUserID = actor.UserID
		// A SNAPSHOT, not a reference (§15.1): the entry must stay readable after the user is
		// renamed or deactivated, which a join at read time would not survive.
		entry.ActorName = actor.DisplayName
		entry.BranchID = actor.BranchID
		entry.SessionID = actor.SessionID
	}
	if entry.Source == "" {
		entry.Source = derivedSource(entry.ActorUserID)
	}

	// Ties every record produced by ONE user action together (§15.1), stamped per call since
	// 0.10 — which is what makes "what did this click actually do?" answerable.
	if correlation, ok := event.CorrelationID(ctx); ok {
		entry.Correlation = correlation
	}

	entryID, err := id.New()
	if err != nil {
		return err
	}
	entry.ID = entryID
	entry.OccurredAt = s.repo.clk.Now()

	return s.repo.insert(ctx, entry)
}

// derivedSource classifies an entry when the publisher did not say.
//
// An actor means a person at the keyboard; no actor means the system acted on its own — a job,
// the setup wizard, a migration. Distinguishing them is what lets an auditor ask "did a human
// do this?".
func derivedSource(actorID id.ID) string {
	if actorID.IsZero() {
		return contract.SourceSystem
	}
	return contract.SourceUI
}

// marshalPayload renders a before/after value.
//
// nil returns the empty string, which the repository stores as SQL NULL — so "there was no
// before state" (a creation) stays distinguishable from a payload that is literally null.
func marshalPayload(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed,
			"an audit payload could not be recorded")
	}
	return string(raw), nil
}

func joinChanged(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return strings.Join(fields, ",")
	}
	return string(raw)
}
