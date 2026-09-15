package api

import (
	"context"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/backups"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Backups is the Backups screen and Home's safety line (L7 §6): the list, back up now, the outside folder, and restore.
type Backups struct{ core *core }

// ActOutsideFolder is choosing where backups are copied — the owner's.
const ActOutsideFolder = "backups.outside_folder"

// BackupDTO is a backup as listed.
type BackupDTO struct {
	Name          string `json:"name"`
	Reason        string `json:"reason"`
	TakenAt       string `json:"takenAt"`
	SizeBytes     string `json:"sizeBytes"`
	SchemaVersion int64  `json:"schemaVersion"`
	Outside       bool   `json:"outside"`
	// AgeSeconds is how long ago it was taken, by Go's clock — the screen never compares the webview's clock with Go's.
	AgeSeconds int64 `json:"ageSeconds"`
}

// RestoredDTO is the restore applied at this start.
type RestoredDTO struct {
	From   string `json:"from"`
	Safety string `json:"safety"`
}

// BackupStatusDTO is what Home says about safety.
type BackupStatusDTO struct {
	Last          *BackupDTO   `json:"last"`
	Folder        string       `json:"folder"`
	LastOutside   *BackupDTO   `json:"lastOutside"`
	OutsideStale  bool         `json:"outsideStale"`
	OutsideFailed string       `json:"outsideFailed"`
	Restored      *RestoredDTO `json:"restored"`
}

// LossDTO is a restore's warning: the backup, and what restoring it removes.
type LossDTO struct {
	Backup         BackupDTO `json:"backup"`
	Sales          int       `json:"sales"`
	Voids          int       `json:"voids"`
	DebtEntries    int       `json:"debtEntries"`
	CashEntries    int       `json:"cashEntries"`
	StockMovements int       `json:"stockMovements"`
}

// RestoreDTO says a restore is staged, and whether the application is restarting to apply it now.
type RestoreDTO struct {
	Staged     bool `json:"staged"`
	Restarting bool `json:"restarting"`
}

func backupDTO(app *bootstrap.App, e backups.Entry) BackupDTO {
	age := int64(0)
	if !e.TakenAt.IsZero() {
		age = max(0, int64(app.Now().Sub(e.TakenAt)/time.Second))
	}
	return BackupDTO{Name: e.Name, Reason: string(e.Reason), TakenAt: clock.Format(e.TakenAt), SizeBytes: strconv.FormatInt(e.SizeBytes, 10),
		SchemaVersion: e.SchemaVersion, Outside: e.Outside, AgeSeconds: age}
}

func statusDTO(app *bootstrap.App, st backups.Status) BackupStatusDTO {
	out := BackupStatusDTO{Folder: st.Folder, OutsideStale: st.OutsideStale, OutsideFailed: st.OutsideFailed}
	if st.Last != nil {
		b := backupDTO(app, *st.Last)
		out.Last = &b
	}
	if st.LastOutside != nil {
		b := backupDTO(app, *st.LastOutside)
		out.LastOutside = &b
	}
	if app.Restored != nil {
		out.Restored = &RestoredDTO{From: app.Restored.From, Safety: app.Restored.SafetyBackup}
	}
	return out
}

// List is every backup, newest first. Anyone may see it.
func (b *Backups) List() envelope.Result[[]BackupDTO] {
	return call(b.core, "Backups.List", func(ctx context.Context, app *bootstrap.App) ([]BackupDTO, error) {
		found, err := app.Safety.List(ctx)
		out := make([]BackupDTO, 0, len(found))
		for _, e := range found {
			out = append(out, backupDTO(app, e))
		}
		return out, err
	})
}

// TakeNow takes a backup and copies it outside. Anyone may.
func (b *Backups) TakeNow() envelope.Result[BackupDTO] {
	return call(b.core, "Backups.TakeNow", func(ctx context.Context, app *bootstrap.App) (BackupDTO, error) {
		e, err := app.Safety.TakeNow(ctx)
		return backupDTO(app, e), err
	})
}

// Status is the newest backup and outside copy, and a restore applied at this start.
func (b *Backups) Status() envelope.Result[BackupStatusDTO] {
	return call(b.core, "Backups.Status", func(ctx context.Context, app *bootstrap.App) (BackupStatusDTO, error) {
		st, err := app.Safety.Status(ctx)
		return statusDTO(app, st), err
	})
}

// LossPreview counts what restoring a backup would remove. Owner only.
func (b *Backups) LossPreview(name string) envelope.Result[LossDTO] {
	return call(b.core, "Backups.LossPreview", func(ctx context.Context, app *bootstrap.App) (LossDTO, error) {
		p, err := app.Safety.LossPreview(ctx, name)
		return LossDTO{Backup: backupDTO(app, p.Backup), Sales: p.Loss.Sales, Voids: p.Loss.Voids, DebtEntries: p.Loss.DebtEntries,
			CashEntries: p.Loss.CashEntries, StockMovements: p.Loss.StockMovements}, err
	})
}

// Restore stages a backup — owner PIN, a safety snapshot — and restarts the application to apply it.
func (b *Backups) Restore(name string) envelope.Result[RestoreDTO] {
	return call(b.core, "Backups.Restore", func(ctx context.Context, app *bootstrap.App) (RestoreDTO, error) {
		if _, err := app.Safety.Restore(ctx, name); err != nil {
			return RestoreDTO{}, err
		}
		b.core.mu.RLock()
		restart := b.core.restart
		b.core.mu.RUnlock()
		if restart == nil {
			return RestoreDTO{Staged: true}, nil
		}
		// After this answer reaches the screen: the graph it came from is about to close.
		go func() {
			time.Sleep(300 * time.Millisecond)
			restart()
		}()
		return RestoreDTO{Staged: true, Restarting: true}, nil
	})
}

// RestoreFromFile asks for a backup file, verifies it and lists it; the screen then previews and restores it. Owner only.
// A cancelled dialog returns an empty backup.
func (b *Backups) RestoreFromFile() envelope.Result[BackupDTO] {
	return call(b.core, "Backups.RestoreFromFile", func(ctx context.Context, app *bootstrap.App) (BackupDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return BackupDTO{}, err
		}
		if !app.Owner.Allowed(ctx) {
			return BackupDTO{}, requireOwner(ctx, app, backups.ActImport, "")
		}
		files, err := b.core.dialogs()
		if err != nil {
			return BackupDTO{}, err
		}
		path, err := files.OpenFile(ctx, w.t("backups.open_title"), []Filter{{Name: w.t("backups.filter"), Pattern: "*.db"}})
		if err != nil || path == "" {
			return BackupDTO{}, err
		}
		var e backups.Entry
		err = app.DB.Do(ctx, func(ctx context.Context) error {
			var importErr error
			e, importErr = app.Safety.Import(ctx, path)
			return importErr
		})
		return backupDTO(app, e), err
	})
}

// SaveCopy writes a verified copy of a backup where the owner chooses. Owner only.
func (b *Backups) SaveCopy(name string) envelope.Result[ExportResultDTO] {
	return call(b.core, "Backups.SaveCopy", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return ExportResultDTO{}, err
		}
		if !app.Owner.Allowed(ctx) {
			return ExportResultDTO{}, requireOwner(ctx, app, backups.ActSaveCopy, name)
		}
		files, err := b.core.dialogs()
		if err != nil {
			return ExportResultDTO{}, err
		}
		path, err := files.SaveFile(ctx, w.t("backups.save_title"), name, []Filter{{Name: w.t("backups.filter"), Pattern: "*.db"}})
		if err != nil || path == "" {
			return ExportResultDTO{Cancelled: path == ""}, err
		}
		err = app.DB.Do(ctx, func(ctx context.Context) error { return app.Safety.SaveCopy(ctx, name, path) })
		return ExportResultDTO{Path: path}, err
	})
}

// SetOutsideFolder asks for the folder every backup is copied to — or, with stop, stops copying. Owner only.
func (b *Backups) SetOutsideFolder(stop bool) envelope.Result[BackupStatusDTO] {
	return call(b.core, "Backups.SetOutsideFolder", func(ctx context.Context, app *bootstrap.App) (BackupStatusDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return BackupStatusDTO{}, err
		}
		if !app.Owner.Allowed(ctx) {
			return BackupStatusDTO{}, requireOwner(ctx, app, ActOutsideFolder, "")
		}
		folder := ""
		if !stop {
			files, dialogErr := b.core.dialogs()
			if dialogErr != nil {
				return BackupStatusDTO{}, dialogErr
			}
			if folder, err = files.PickFolder(ctx, w.t("backups.folder_title")); err != nil {
				return BackupStatusDTO{}, err
			}
			if folder == "" {
				st, statusErr := app.Safety.Status(ctx)
				return statusDTO(app, st), statusErr
			}
		}
		err = app.DB.Do(ctx, func(ctx context.Context) error {
			if _, updateErr := app.Settings.Update(ctx, settingsdomain.Update{Printing: settingsdomain.PrintingUpdate{BackupFolder: &folder}}); updateErr != nil {
				return updateErr
			}
			return requireOwner(ctx, app, ActOutsideFolder, folder)
		})
		if err != nil {
			return BackupStatusDTO{}, err
		}
		if folder != "" {
			if _, takeErr := app.Safety.TakeNow(ctx); takeErr != nil { // a first copy at once, so the status is true straight away
				return BackupStatusDTO{}, takeErr
			}
		}
		st, err := app.Safety.Status(ctx)
		return statusDTO(app, st), err
	})
}
