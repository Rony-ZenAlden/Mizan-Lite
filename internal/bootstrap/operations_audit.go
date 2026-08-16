package bootstrap

import (
	"context"
	"log/slog"

	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// The audited operational acts (§15.3).
//
// # Why these and not the others
//
// A backup and a listing change nothing, and auditing every read would bury the entries that
// matter. These three are the ones an auditor asks about:
//
//   - A RESTORE replaces the shop's data with an older copy. It is the single most destructive
//     act the application offers, and the one where "who did this, and when" has an answer
//     somebody will need.
//   - Cancelling a restore matters because the pair of entries is the story: prepared at 11:00,
//     cancelled at 11:02 reads differently from prepared and then applied.
//   - An IMPORT creates records in bulk. The rows themselves are audited by the services that
//     create them — that is what 9.4's design is for — but "four thousand products arrived at
//     once, from a file, at somebody's instruction" is a fact those four thousand entries do not
//     carry.
const (
	ActionRestorePrepared  = "ops.restore.prepared"
	ActionRestoreCancelled = "ops.restore.cancelled"
	ActionImported         = "ops.import.completed"

	EntityRestore = "ops.restore"
	EntityImport  = "ops.import"
)

// auditRestorePrepared records a staged restore.
//
// The SAFETY SNAPSHOT is in the payload, which is the field that makes the entry useful rather
// than merely present: an auditor reading "a restore was prepared" wants to know what the shop
// could go back to, and that name is the answer.
func (a *App) AuditRestorePrepared(ctx context.Context, intent backup.Intent) error {
	return a.Bus.Publish(ctx, auditc.Auditable{
		Action:      ActionRestorePrepared,
		EntityType:  EntityRestore,
		EntityLabel: intent.From,
		After: map[string]any{
			"from":              intent.From,
			"safety_backup":     intent.SafetyBackup,
			"replacing_version": intent.ReplacingVersion,
			"restoring_version": intent.RestoringVersion,
		},
	})
}

// auditRestoreCancelled records a staged restore being discarded.
func (a *App) AuditRestoreCancelled(ctx context.Context, from string) error {
	return a.Bus.Publish(ctx, auditc.Auditable{
		Action:      ActionRestoreCancelled,
		EntityType:  EntityRestore,
		EntityLabel: from,
		Before:      map[string]any{"from": from},
	})
}

// auditImport records a bulk import.
//
// Only a COMMITTED import. A dry run writes nothing and reads a file the user chose; auditing it
// would fill the trail with entries about decisions nobody made.
func (a *App) AuditImport(
	ctx context.Context, kind string, total, succeeded, failed int,
) error {
	return a.Bus.Publish(ctx, auditc.Auditable{
		Action:      ActionImported,
		EntityType:  EntityImport,
		EntityLabel: kind,
		After: map[string]any{
			"kind": kind, "total": total, "succeeded": succeeded, "failed": failed,
		},
	})
}

// LogWarn records something that went wrong without failing the call.
//
// For the case where the ACT succeeded and only its record did not: failing then would report a
// restore that is staged and correct as having failed, and the operator would go looking for a
// problem that is not there.
func (a *App) LogWarn(ctx context.Context, message string, err error) {
	a.log.WarnContext(ctx, message, slog.String("error", err.Error()))
}
