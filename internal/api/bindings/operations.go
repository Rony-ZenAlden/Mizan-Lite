package bindings

import (
	"context"
	"encoding/base64"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/imports"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// BackupDTO is one snapshot on disk.
type BackupDTO struct {
	// Name is the identifier a caller passes back. NOT a path: a binding that accepted one would
	// let a caller name any file on the machine.
	Name          string `json:"name"`
	TakenAt       string `json:"takenAt"`
	Reason        string `json:"reason"`
	SchemaVersion int64  `json:"schemaVersion"`
	SizeBytes     int64  `json:"sizeBytes"`
	AppVersion    string `json:"appVersion"`
	// Restorable is whether THIS build can read it. A backup from a newer version is listed —
	// hiding it would leave a user wondering where it went — and marked.
	Restorable bool `json:"restorable"`
}

// RestoreIntentDTO is a restore waiting for a restart.
type RestoreIntentDTO struct {
	From             string `json:"from"`
	ReplacingVersion int64  `json:"replacingVersion"`
	RestoringVersion int64  `json:"restoringVersion"`
	SafetyBackup     string `json:"safetyBackup"`
	PreparedAt       string `json:"preparedAt"`
}

// ImportRowDTO is what happened to one row.
type ImportRowDTO struct {
	// Line is the line in the user's SPREADSHEET, header counted.
	Line    int    `json:"line"`
	Key     string `json:"key"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// ImportReportDTO is what an import did, or would do.
type ImportReportDTO struct {
	DryRun    bool           `json:"dryRun"`
	Kind      string         `json:"kind"`
	Total     int            `json:"total"`
	Succeeded int            `json:"succeeded"`
	Failed    int            `json:"failed"`
	Rows      []ImportRowDTO `json:"rows"`
}

// NoticeDTO is one thing a user should be told.
type NoticeDTO struct {
	Key      string `json:"key"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	// MessageKey and Params are an i18n key and its arguments. Never a rendered sentence: the
	// backend has no locale, and the frontend already owns the catalogue.
	MessageKey string            `json:"messageKey"`
	Params     map[string]string `json:"params"`
	EntityID   string            `json:"entityId"`
	EntityKind string            `json:"entityKind"`
}

// NoticesDTO is the whole centre.
type NoticesDTO struct {
	Notices []NoticeDTO `json:"notices"`
	// Dismissed counts what is hidden, so a screen can offer to show it. A user who silenced
	// something in error otherwise has no way back.
	Dismissed int `json:"dismissed"`
	// Failed names rules that could not run — because silence reads as "nothing is wrong".
	Failed []string `json:"failed"`
}

// Operations is the surface somebody looks after the application through.
type Operations struct{ graph }

// operationsPolicies declares what each method requires.
func operationsPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		// Reading and taking backups is an administrative act, guarded by the permission that
		// already means "may administer this installation".
		// # Why backups are guarded by USER MANAGEMENT
		//
		// There is no "system.manage" permission. The first version of this file invented one,
		// along with "partner.manage" and an `ops.notice.view` no module could declare — and all
		// three were caught by 7.6's coverage check, which exists for exactly this: a permission
		// no module declares can never be granted, so the method is permanently unreachable.
		//
		// Whoever may create and delete users administers this installation, and a backup
		// contains every user's data. The two powers are the same power, so they share a
		// permission rather than gaining a fourth name for it.
		"Backups":        policy.Requires(identity.PermUserManage),
		"TakeBackup":     policy.Requires(identity.PermUserManage),
		"PrepareRestore": policy.Requires(identity.PermUserManage),
		"PendingRestore": policy.Requires(identity.PermUserManage),
		"CancelRestore":  policy.Requires(identity.PermUserManage),

		// An import CREATES records, so it demands the permission to create them. There is no
		// separate "may import", which would be a second way to grant the same power.
		"ImportProducts": policy.Requires(imports.PermImportProducts),
		"ImportPartners": policy.Requires(imports.PermImportPartners),

		// Notices report on how the installation is RUNNING — failed jobs, missing backups,
		// books that do not balance — which is the same question the Jobs and Runs screens
		// answer, and they are guarded by session view. Reusing it keeps one answer to "who may
		// look at the health of this installation".
		//
		// `ops.PermNoticeView` was declared in the ops package and is not used: ops is not a
		// module, so nothing enumerates its permissions, and a policy naming it would be
		// unreachable. It is removed rather than left as a constant nothing can grant.
		"Notices":       policy.Requires(identity.PermSessionView),
		"DismissNotice": policy.Requires(identity.PermSessionView),
		"RestoreNotice": policy.Requires(identity.PermSessionView),
	}
}

// Backups lists every verified snapshot, newest first.
func (o *Operations) Backups() envelope.Result[[]BackupDTO] {
	_, app, err := o.guard("Backups")
	if err != nil {
		return envelope.Fail[[]BackupDTO](err)
	}
	listed, err := app.Backups.List()
	if err != nil {
		return envelope.Fail[[]BackupDTO](err)
	}

	out := make([]BackupDTO, 0, len(listed))
	for _, candidate := range listed {
		out = append(out, BackupDTO{
			Name: candidate.Name, TakenAt: candidate.Manifest.TakenAt,
			Reason:        string(candidate.Manifest.Reason),
			SchemaVersion: candidate.Manifest.SchemaVersion,
			SizeBytes:     candidate.Manifest.SizeBytes,
			AppVersion:    candidate.Manifest.AppVersion,
			// Listed even when it cannot be restored: hiding it would leave a user wondering
			// where their backup went, and the reason it cannot be used is worth showing.
			Restorable: candidate.Manifest.SchemaVersion <= app.SchemaVersion(),
		})
	}
	return envelope.Ok(out)
}

// TakeBackup snapshots the database now.
func (o *Operations) TakeBackup() envelope.Result[BackupDTO] {
	ctx, app, err := o.guard("TakeBackup")
	if err != nil {
		return envelope.Fail[BackupDTO](err)
	}
	taken, err := app.Backups.Take(ctx, backup.OnDemand)
	if err != nil {
		return envelope.Fail[BackupDTO](err)
	}
	return envelope.Ok(BackupDTO{
		Name: taken.Name, TakenAt: taken.Manifest.TakenAt,
		Reason: string(taken.Manifest.Reason), SchemaVersion: taken.Manifest.SchemaVersion,
		SizeBytes: taken.Manifest.SizeBytes, AppVersion: taken.Manifest.AppVersion,
		Restorable: true,
	})
}

// PrepareRestore stages a restore. Nothing is replaced until the application restarts.
func (o *Operations) PrepareRestore(name string) envelope.Result[RestoreIntentDTO] {
	ctx, app, err := o.guard("PrepareRestore")
	if err != nil {
		return envelope.Fail[RestoreIntentDTO](err)
	}
	intent, err := app.Backups.Prepare(ctx, name, app.Paths.DBFile, app.SchemaVersion())
	if err != nil {
		return envelope.Fail[RestoreIntentDTO](err)
	}
	// Audited AFTER the act, unlike a document's in-transaction entry: there is no transaction
	// here, because the act is a file on disk. What matters is that the entry exists and names
	// the safety snapshot — and a failure to write it must not undo a restore that is staged and
	// correct, so it is reported rather than returned.
	if auditErr := app.AuditRestorePrepared(ctx, intent); auditErr != nil {
		app.LogWarn(ctx, "a prepared restore was not audited", auditErr)
	}
	return envelope.Ok(intentDTO(intent))
}

// PendingRestore reports a restore waiting for a restart, if there is one.
func (o *Operations) PendingRestore() envelope.Result[RestoreIntentDTO] {
	_, app, err := o.guard("PendingRestore")
	if err != nil {
		return envelope.Fail[RestoreIntentDTO](err)
	}
	intent, staged, err := backup.PendingIntent(app.Paths.DBFile)
	if err != nil {
		return envelope.Fail[RestoreIntentDTO](err)
	}
	if !staged {
		// An empty intent, not an error: "nothing is waiting" is the ordinary answer and a
		// screen asking on every load should not have to treat it as a failure.
		return envelope.Ok(RestoreIntentDTO{})
	}
	return envelope.Ok(intentDTO(intent))
}

// CancelRestore discards a staged restore.
func (o *Operations) CancelRestore() envelope.Result[bool] {
	ctx, app, err := o.guard("CancelRestore")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	intent, staged, err := backup.PendingIntent(app.Paths.DBFile)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = backup.Cancel(app.Paths.DBFile); err != nil {
		return envelope.Fail[bool](err)
	}
	if staged {
		if auditErr := app.AuditRestoreCancelled(ctx, intent.From); auditErr != nil {
			app.LogWarn(ctx, "a cancelled restore was not audited", auditErr)
		}
	}
	return envelope.Ok(true)
}

// ImportProducts brings a product list in.
func (o *Operations) ImportProducts(
	contentBase64 string, dryRun bool,
) envelope.Result[ImportReportDTO] {
	return o.runImport("ImportProducts", imports.Products, contentBase64, dryRun)
}

// ImportPartners brings a customer or supplier list in.
func (o *Operations) ImportPartners(
	contentBase64 string, dryRun bool,
) envelope.Result[ImportReportDTO] {
	return o.runImport("ImportPartners", imports.Partners, contentBase64, dryRun)
}

func (o *Operations) runImport(
	method string, kind imports.Kind, contentBase64 string, dryRun bool,
) envelope.Result[ImportReportDTO] {
	ctx, app, err := o.guard(method)
	if err != nil {
		return envelope.Fail[ImportReportDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ImportReportDTO](err)
	}
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return envelope.Fail[ImportReportDTO](
			errs.Validation(imports.CodeImportFailed, "that file could not be read"))
	}

	report, err := app.Imports.Import(ctx, companyID, kind, content, dryRun)
	if err != nil {
		return envelope.Fail[ImportReportDTO](err)
	}

	// Only a COMMITTED import. A dry run writes nothing, and auditing it would fill the trail
	// with entries about decisions nobody made.
	if !dryRun {
		if auditErr := app.AuditImport(
			ctx, string(report.Kind), report.Total, report.Succeeded, report.Failed,
		); auditErr != nil {
			app.LogWarn(ctx, "an import was not audited", auditErr)
		}
	}

	rows := make([]ImportRowDTO, 0, len(report.Rows))
	for _, row := range report.Rows {
		rows = append(rows, ImportRowDTO{
			Line: row.Line, Key: row.Key, OK: row.OK,
			Message: row.Message, Code: row.Code,
		})
	}
	return envelope.Ok(ImportReportDTO{
		DryRun: report.DryRun, Kind: string(report.Kind), Total: report.Total,
		Succeeded: report.Succeeded, Failed: report.Failed, Rows: rows,
	})
}

// Notices reports what this user should be told.
func (o *Operations) Notices() envelope.Result[NoticesDTO] {
	ctx, app, err := o.guard("Notices")
	if err != nil {
		return envelope.Fail[NoticesDTO](err)
	}
	companyID, userID, err := o.actor(ctx, app)
	if err != nil {
		return envelope.Fail[NoticesDTO](err)
	}

	result, err := app.Notices.Check(ctx, companyID, userID)
	if err != nil {
		return envelope.Fail[NoticesDTO](err)
	}

	notices := make([]NoticeDTO, 0, len(result.Notices))
	for _, notice := range result.Notices {
		params := notice.Params
		if params == nil {
			params = map[string]string{}
		}
		notices = append(notices, NoticeDTO{
			Key: notice.Key, Rule: notice.Rule, Severity: string(notice.Severity),
			MessageKey: notice.MessageKey, Params: params,
			EntityID: string(notice.EntityID), EntityKind: notice.EntityKind,
		})
	}
	failed := result.Failed
	if failed == nil {
		failed = []string{}
	}
	return envelope.Ok(NoticesDTO{
		Notices: notices, Dismissed: result.Dismissed, Failed: failed,
	})
}

// DismissNotice hides one for this user.
func (o *Operations) DismissNotice(key string) envelope.Result[bool] {
	return o.notice("DismissNotice", key, true)
}

// RestoreNotice un-hides one, so it returns if its condition still holds.
func (o *Operations) RestoreNotice(key string) envelope.Result[bool] {
	return o.notice("RestoreNotice", key, false)
}

func (o *Operations) notice(method, key string, dismiss bool) envelope.Result[bool] {
	ctx, app, err := o.guard(method)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	companyID, userID, err := o.actor(ctx, app)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if dismiss {
		err = app.Notices.Dismiss(ctx, companyID, userID, key)
	} else {
		err = app.Notices.Restore(ctx, companyID, userID, key)
	}
	if err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// actor reads who is asking, which a notice centre needs because dismissals are PER USER.
//
// From the context rather than from the session holder, because the guard has already stamped it
// — and reading it twice from two places is how the two come to disagree.
func (o *Operations) actor(
	ctx context.Context, app *bootstrap.App,
) (companyID, userID id.ID, err error) {
	acting, found := appctx.ActorFrom(ctx)
	if !found || acting.UserID.IsZero() {
		return id.ID(""), id.ID(""), errs.Internal("ops.no_actor",
			"this call has no signed-in user, so there is nobody to show notices to")
	}
	companyID, err = app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return id.ID(""), id.ID(""), err
	}
	return companyID, acting.UserID, nil
}

func intentDTO(intent backup.Intent) RestoreIntentDTO {
	return RestoreIntentDTO{
		From: intent.From, ReplacingVersion: intent.ReplacingVersion,
		RestoringVersion: intent.RestoringVersion, SafetyBackup: intent.SafetyBackup,
		PreparedAt: intent.PreparedAt,
	}
}
