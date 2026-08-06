package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/api/redact"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// AuditEntryDTO is one audit record as the frontend sees it.
//
// # Why the payload fields are pointers
//
// They are gated by audit.entry.view_payload, and a POINTER with omitempty is what makes the
// key genuinely ABSENT from the JSON rather than present-and-blank (Step 1.6, D1).
//
// Two things follow, and the second is the one that matters:
//
//   - The frontend can tell "you may not see this" from "there was nothing here" — a blank
//     string is indistinguishable from an entry that genuinely had no before-state.
//   - The safe state is the DEFAULT. A future code path that builds this DTO and forgets the
//     redaction call produces nil, which serialises as absent. Forgetting HIDES data instead of
//     leaking it, which is the opposite of what a blank-it-out design gives you.
type AuditEntryDTO struct {
	ID          string `json:"id"`
	OccurredAt  string `json:"occurredAt"`
	ActorUserID string `json:"actorUserId"`
	// ActorName is a SNAPSHOT taken when the entry was written, so a five-year-old record stays
	// readable after the user is renamed or deactivated (§15.1).
	ActorName     string `json:"actorName"`
	CorrelationID string `json:"correlationId"`
	Action        string `json:"action"`
	EntityType    string `json:"entityType"`
	EntityID      string `json:"entityId"`
	EntityLabel   string `json:"entityLabel"`
	Source        string `json:"source"`

	// Restricted. Absent unless the caller holds audit.entry.view_payload.
	BeforeJSON    *string `json:"beforeJson,omitempty"`
	AfterJSON     *string `json:"afterJson,omitempty"`
	ChangedFields *string `json:"changedFields,omitempty"`
}

// AuditFilterDTO narrows an entry query.
type AuditFilterDTO struct {
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	ActorID    string `json:"actorId"`
	Limit      int    `json:"limit"`
}

// Audit exposes the audit trail.
type Audit struct{ graph }

// auditPolicies gate the LIST. The payload within each row is gated separately, by redaction.
//
// The two mechanisms compose without knowing about each other: the guard (1.5) decides whether
// you see anything at all, and redaction (1.6) decides how much of each row. Neither is a
// substitute for the other — a method-level permission cannot express "you may see that this
// changed but not what it changed from".
func auditPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Entries": policy.Requires(audit.PermView),
	}
}

// Entries lists audit records, newest first, with payloads redacted for callers who may not
// see them.
func (a *Audit) Entries(filter AuditFilterDTO) envelope.Result[[]AuditEntryDTO] {
	ctx, app, err := a.guard("Entries")
	if err != nil {
		return envelope.Fail[[]AuditEntryDTO](err)
	}

	entries, err := app.Audit.Entries(ctx, audit.Filter{
		EntityType: filter.EntityType,
		EntityID:   id.ID(filter.EntityID),
		ActorID:    id.ID(filter.ActorID),
		Limit:      filter.Limit,
	})
	if err != nil {
		return envelope.Fail[[]AuditEntryDTO](err)
	}

	// Resolved ONCE for the whole page rather than per row: the answer cannot change mid-list,
	// and asking per row would mean a permission read for every entry on a 100-row screen.
	out := make([]AuditEntryDTO, 0, len(entries))
	for _, e := range entries {
		dto := AuditEntryDTO{
			ID:            string(e.ID),
			OccurredAt:    clock.Format(e.OccurredAt),
			ActorUserID:   string(e.ActorUserID),
			ActorName:     e.ActorName,
			CorrelationID: string(e.Correlation),
			Action:        e.Action,
			EntityType:    e.EntityType,
			EntityID:      string(e.EntityID),
			EntityLabel:   e.EntityLabel,
			Source:        e.Source,
		}

		// The ONLY way these fields get set. Nil — and therefore absent — otherwise.
		dto.BeforeJSON = redact.Visible(ctx, app.Identity, audit.PermViewPayload, e.BeforeJSON)
		dto.AfterJSON = redact.Visible(ctx, app.Identity, audit.PermViewPayload, e.AfterJSON)
		dto.ChangedFields = redact.Visible(ctx, app.Identity, audit.PermViewPayload, e.ChangedFields)

		out = append(out, dto)
	}
	return envelope.Ok(out)
}
