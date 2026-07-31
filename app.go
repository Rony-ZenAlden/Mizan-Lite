package main

import (
	"context"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/buildinfo"
)

// App is the Wails-bound application surface.
//
// Deliberately thin: resolve context, call the graph, map the result (ARCHITECTURE_v1 §5.4).
// The object graph is built by internal/bootstrap before Wails starts, so this struct holds a
// reference and constructs nothing.
type App struct {
	app *bootstrap.App
}

// NewApp wraps an already-built graph.
func NewApp(built *bootstrap.App) *App { return &App{app: built} }

func (a *App) startup(ctx context.Context) {
	slog.InfoContext(ctx, "mizan window ready", slog.String("version", buildinfo.Version))
}

// shutdown tears the graph down when the window closes.
func (a *App) shutdown(ctx context.Context) {
	if err := a.app.Shutdown(ctx); err != nil {
		slog.WarnContext(ctx, "shutdown completed with errors", slog.Any("error", err))
	}
}

// Health returns build metadata.
//
// The first consumer of the result envelope, which proves the whole boundary before any
// module has a screen: every binding returns Result[T], and a failure crosses as a code plus
// parameters — never English prose (§5.4, §22.2).
func (a *App) Health() envelope.Result[buildinfo.Info] {
	return envelope.Ok(buildinfo.Current())
}

// CurrencyDTO is a currency as the frontend sees it.
//
// Domain types are never exposed to JavaScript (§5.4): this flat shape decouples the UI from
// domain refactoring and prevents internal fields leaking into the bridge.
type CurrencyDTO struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Symbol         string `json:"symbol"`
	DecimalPlaces  int    `json:"decimalPlaces"`
	SymbolPosition string `json:"symbolPosition"`
}

// Currencies lists the configured currencies, named in the caller's language.
//
// The end-to-end proof that the graph is genuinely wired: this one call goes through the
// per-request context, the settings-bound locale, the currency module, and the i18n
// translation resolver.
func (a *App) Currencies() envelope.Result[[]CurrencyDTO] {
	ctx := a.app.Context()

	infos, err := a.app.Currency.List(ctx, false)
	if err != nil {
		return envelope.Fail[[]CurrencyDTO](err)
	}

	out := make([]CurrencyDTO, 0, len(infos))
	for _, info := range infos {
		out = append(out, CurrencyDTO{
			Code:           info.Code,
			Name:           info.Name,
			Symbol:         info.Symbol,
			DecimalPlaces:  int(info.DecimalPlaces),
			SymbolPosition: info.SymbolPosition,
		})
	}
	return envelope.Ok(out)
}
