package bootstrap

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdb "github.com/mizan-erp/mizan/internal/lite/sales/infra/sqlite"
)

// WrapKeepingParams exposes the parameter-carrying wrapper to the external test package.
var WrapKeepingParams = wrapKeepingParams

// TillWithStock builds the till on app's real modules, its stock port wrapped — so a test can fail the transaction after
// a real stock movement was written.
func TillWithStock(app *App, wrap func(sales.Stock) sales.Stock) *sales.Service {
	return sales.NewService(app.DB, salesdb.NewStore(app.DB, clock.System()), salesCatalogue{catalog: app.Catalog},
		wrap(salesStock{stock: app.Stock}), salesRates{fx: app.FX}, salesSettings{settings: app.Settings},
		salesGate{owner: app.Owner}, clock.System(), time.UTC)
}
