package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/guide"
	guidepdf "github.com/mizan-erp/mizan/internal/lite/guide/pdf"
	"github.com/mizan-erp/mizan/internal/lite/notices"
	"github.com/mizan-erp/mizan/internal/lite/support"
)

// ActSupportDatabase is putting a copy of the database in a support file — the owner's: it holds every customer's debts.
const ActSupportDatabase = "support.database"

// AboutDTO is the About screen (L8 §9.1): what is running, where its data is, its guides and its licences.
type AboutDTO struct {
	Version       string   `json:"version"`
	SchemaVersion int64    `json:"schemaVersion"`
	Platform      string   `json:"platform"`
	Arch          string   `json:"arch"`
	DataDir       string   `json:"dataDir"`
	LogsDir       string   `json:"logsDir"`
	BackupsDir    string   `json:"backupsDir"`
	Guides        []string `json:"guides"`
	Notices       string   `json:"notices"`
}

// About describes the running application. Anyone may read it.
func (a *App) About() envelope.Result[AboutDTO] {
	return call(a.core, "App.About", func(_ context.Context, app *bootstrap.App) (AboutDTO, error) {
		return AboutDTO{Version: a.core.version, SchemaVersion: app.SchemaVersion, Platform: runtime.GOOS, Arch: runtime.GOARCH,
			DataDir: app.Paths.Data, LogsDir: app.Paths.Logs, BackupsDir: app.Paths.Backups, Guides: guide.Names(), Notices: notices.Text()}, nil
	})
}

// SaveGuide writes one of the shipped guides where the person chooses. Anyone may.
func (a *App) SaveGuide(name string) envelope.Result[ExportResultDTO] {
	return call(a.core, "App.SaveGuide", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		body, err := guidepdf.Guide(name)
		if err != nil {
			return ExportResultDTO{}, err
		}
		w, err := newWords(ctx, app)
		if err != nil {
			return ExportResultDTO{}, err
		}
		return a.core.saveBytes(ctx, w.t("about.guide_save_title"), guide.FileName(name), Filter{Name: w.t("doc.filter_pdf"), Pattern: "*.pdf"}, body)
	})
}

// diagnostics is the support file's diagnostics.json. It carries no PIN, no PIN hash and no recovery code: nothing here reads
// the owner's credential (TestTheSupportFileHoldsNoSecret).
type diagnostics struct {
	Version       string        `json:"version"`
	Platform      string        `json:"platform"`
	Arch          string        `json:"arch"`
	GoVersion     string        `json:"goVersion"`
	SchemaVersion int64         `json:"schemaVersion"`
	TimeZone      string        `json:"timeZone"`
	GeneratedAt   string        `json:"generatedAt"`
	Settings      any           `json:"settings"`
	Backups       any           `json:"backups"`
	BackupList    any           `json:"backupList"`
	PrintJobs     any           `json:"printJobs"`
	Verifiers     []findingNote `json:"verifiers"`
	Problems      []string      `json:"problems,omitempty"`
}

type findingNote struct {
	Verifier string `json:"verifier"`
	Code     string `json:"code"`
	Detail   string `json:"detail,omitempty"`
}

// SaveSupportFile writes the support file (L8 §9.2) where the owner chooses: the last week's logs and the diagnostics, and —
// only with includeDatabase, and only in owner mode — a verified copy of the database. Nothing is sent anywhere.
func (a *App) SaveSupportFile(includeDatabase bool) envelope.Result[ExportResultDTO] {
	return call(a.core, "App.SaveSupportFile", func(ctx context.Context, app *bootstrap.App) (ExportResultDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return ExportResultDTO{}, err
		}
		if includeDatabase && !app.Owner.Allowed(ctx) {
			return ExportResultDTO{}, requireOwner(ctx, app, ActSupportDatabase, "")
		}
		now := app.Now()
		d := diagnostics{Version: a.core.version, Platform: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(),
			SchemaVersion: app.SchemaVersion, TimeZone: app.Location().String(), GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z")}
		note := func(what string, err error) {
			if err != nil {
				d.Problems = append(d.Problems, what+": "+errs.CodeOf(err))
			}
		}
		current, err := app.Settings.Get(ctx)
		note("settings", err)
		d.Settings = current
		status, err := app.Safety.Status(ctx)
		note("backup status", err)
		d.Backups = status
		list, err := app.Safety.List(ctx)
		note("backup list", err)
		d.BackupList = list
		jobs, err := app.Printing.Jobs(ctx, 50)
		note("print jobs", err)
		d.PrintJobs = jobs
		salesFindings, err := app.Sales.VerifyUnguarded(ctx)
		note("sales verifier", err)
		for _, f := range salesFindings {
			d.Verifiers = append(d.Verifiers, findingNote{Verifier: "sales", Code: f.Code, Detail: string(f.SaleID)})
		}
		stockFindings, err := app.Stock.VerifyUnguarded(ctx)
		note("stock verifier", err)
		for _, f := range stockFindings {
			d.Verifiers = append(d.Verifiers, findingNote{Verifier: "stock", Code: f.Code})
		}
		debtFindings, err := app.Customers.VerifyUnguarded(ctx)
		note("debt verifier", err)
		for _, f := range debtFindings {
			d.Verifiers = append(d.Verifiers, findingNote{Verifier: "customers", Code: f.Code})
		}
		body, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return ExportResultDTO{}, errs.Wrap(err, errs.CategoryInternal, support.CodeWriteFailed, "encoding the diagnostics")
		}
		parts := []support.Part{{Name: "diagnostics.json", Body: body}}
		logs, err := support.RecentLogs(app.Paths.Logs, now)
		if err != nil {
			return ExportResultDTO{}, err
		}
		parts = append(parts, logs...)
		if includeDatabase {
			taken, takeErr := app.Safety.TakeNow(ctx)
			if takeErr != nil {
				return ExportResultDTO{}, takeErr
			}
			db, readErr := os.ReadFile(filepath.Join(app.Paths.Backups, taken.Name))
			if readErr != nil {
				return ExportResultDTO{}, errs.Wrap(readErr, errs.CategoryInternal, support.CodeWriteFailed, "reading the database copy")
			}
			parts = append(parts, support.Part{Name: "database/" + taken.Name, Body: db})
		}
		zipped, err := support.Zip(parts, now)
		if err != nil {
			return ExportResultDTO{}, err
		}
		name := "Mizan Lite support " + now.In(app.Location()).Format("2006-01-02 1504") + ".zip"
		return a.core.saveBytes(ctx, w.t("about.support_save_title"), name, Filter{Name: w.t("about.support_filter"), Pattern: "*.zip"}, zipped)
	})
}

// saveBytes asks where to save and writes the file atomically; a closed dialog writes nothing.
func (c *core) saveBytes(ctx context.Context, title, name string, filter Filter, body []byte) (ExportResultDTO, error) {
	files, err := c.dialogs()
	if err != nil {
		return ExportResultDTO{}, err
	}
	path, err := files.SaveFile(ctx, title, name, []Filter{filter})
	if err != nil {
		return ExportResultDTO{}, errs.Wrap(err, errs.CategoryConflict, CodeSaveFailed, "the save dialog failed")
	}
	if path == "" {
		return ExportResultDTO{Cancelled: true}, nil
	}
	if ext := strings.TrimPrefix(filter.Pattern, "*"); !strings.HasSuffix(strings.ToLower(path), ext) {
		path += ext
	}
	if err = writeAtomically(path, body); err != nil {
		return ExportResultDTO{}, err
	}
	return ExportResultDTO{Path: path, Bytes: len(body)}, nil
}
