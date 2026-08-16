package bindings

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	"github.com/mizan-erp/mizan/internal/modules/sales"
)

// StatementNodeDTO is one line of a financial statement, with its children beneath it.
//
// Amounts cross as STRINGS of minor units. JavaScript's number is a float64 and loses integer
// precision above 2^53, which a currency with no decimal places reaches — money crosses as text
// and is formatted, never arithmetic'd, on the frontend.
//
// The tree crosses NESTED rather than flattened with a depth. A screen rendering collapsible
// sections needs the children anyway, and flattening would make it rebuild the tree from depths —
// which is the one place an off-by-one silently reparents a subtotal.
type StatementNodeDTO struct {
	AccountID   string             `json:"accountId"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Depth       int                `json:"depth"`
	Postable    bool               `json:"isPostable"`
	AmountMinor string             `json:"amountMinor"`
	Children    []StatementNodeDTO `json:"children"`
}

// ProfitAndLossDTO is what happened between two dates.
type ProfitAndLossDTO struct {
	RequestedFrom string `json:"requestedFrom"`
	RequestedTo   string `json:"requestedTo"`
	// CoveredFrom and CoveredTo are the whole fiscal periods the figures actually come from.
	// They differ from the requested range when a request lands mid-period, and a screen showing
	// one without the other reports a month's figures under a fortnight's heading.
	CoveredFrom    string `json:"coveredFrom"`
	CoveredTo      string `json:"coveredTo"`
	CoveredPeriods int    `json:"coveredPeriods"`

	Revenue []StatementNodeDTO `json:"revenue"`
	Expense []StatementNodeDTO `json:"expense"`

	RevenueMinor string `json:"revenueMinor"`
	ExpenseMinor string `json:"expenseMinor"`
	// NetProfitMinor is the LEDGER's answer, including rent and wages no sale knows about. Named
	// in full rather than "profit", because the sales screen's gross margin is a different number
	// and a screen labelling either one "profit" is how a shopkeeper concludes they are doing
	// well while losing money.
	NetProfitMinor string `json:"netProfitMinor"`
}

// BalanceSheetDTO is the position as at a date.
type BalanceSheetDTO struct {
	AsAt string `json:"asAt"`

	Asset     []StatementNodeDTO `json:"asset"`
	Liability []StatementNodeDTO `json:"liability"`
	Equity    []StatementNodeDTO `json:"equity"`

	AssetMinor     string `json:"assetMinor"`
	LiabilityMinor string `json:"liabilityMinor"`
	EquityMinor    string `json:"equityMinor"`
	// ResultMinor is the profit not yet closed into retained earnings, already INCLUDED in
	// equityMinor. Carried separately so a screen can show what equity is made of.
	ResultMinor string `json:"resultMinor"`

	// OutOfBalanceMinor is zero when the books are right. It crosses so the screen can say so —
	// a statement that silently rendered a wrong sheet would hide the one fact worth showing.
	OutOfBalanceMinor string `json:"outOfBalanceMinor"`
	Balanced          bool   `json:"balanced"`
}

// AnalysisRowDTO is one row of a sales or spend analysis.
//
// One DTO for both, unlike the Go types. The frontend renders a table, and the two differ only in
// which columns are populated — `costMinor` and `grossMarginMinor` are empty strings on a spend
// row, which a screen already handles because every money field is a string.
type AnalysisRowDTO struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	ID        string `json:"id"`
	Documents int    `json:"documents"`

	QuantityMicro string `json:"quantityMicro"`
	RevenueMinor  string `json:"revenueMinor"`
	CostMinor     string `json:"costMinor"`
	// GrossMarginMinor is revenue less the cost of the goods, and NOT profit. Empty on a spend
	// analysis, where the cost is the purchase.
	GrossMarginMinor   string `json:"grossMarginMinor"`
	MarginPercentMicro string `json:"marginPercentMicro"`
}

// AnalysisDTO is a whole analysis with its total.
type AnalysisDTO struct {
	From  string           `json:"from"`
	To    string           `json:"to"`
	Rows  []AnalysisRowDTO `json:"rows"`
	Total AnalysisRowDTO   `json:"total"`
}

// ValuedLineDTO is one variant's stock in one warehouse.
type ValuedLineDTO struct {
	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	VariantSKU    string `json:"variantSku"`
	ProductName   string `json:"productName"`

	QuantityMicro string `json:"quantityMicro"`
	AvgCostMicro  string `json:"avgCostMicro"`
	ValueMinor    string `json:"valueMinor"`
}

// ValuationDTO is what the stock is worth, and whether the ledger agrees.
type ValuationDTO struct {
	Lines      []ValuedLineDTO `json:"lines"`
	TotalMinor string          `json:"totalMinor"`

	// HasLedger distinguishes "the books agree" from "nobody asked the books". A screen showing
	// a difference of zero in both cases would claim a check that never happened.
	HasLedger       bool   `json:"hasLedger"`
	LedgerMinor     string `json:"ledgerMinor"`
	DifferenceMinor string `json:"differenceMinor"`
	Reconciled      bool   `json:"reconciled"`
}

// SearchResultDTO is one thing found.
type SearchResultDTO struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Subtitle string `json:"subtitle"`
}

// SearchResponseDTO is a whole search, with what failed alongside what was found.
type SearchResponseDTO struct {
	Query   string            `json:"query"`
	Results []SearchResultDTO `json:"results"`
	// Failed names the modules that could not answer, so a screen can say "search is having
	// trouble" rather than "nothing found". They are different answers.
	Failed []string `json:"failed"`
}

// TileDTO is one dashboard figure.
type TileDTO struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Kind   string `json:"kind"`

	AmountMinor string `json:"amountMinor"`
	Count       int    `json:"count"`

	Periodic bool `json:"periodic"`
	Failed   bool `json:"failed"`
}

// DashboardDTO is the home screen.
type DashboardDTO struct {
	From  string    `json:"from"`
	To    string    `json:"to"`
	Tiles []TileDTO `json:"tiles"`
}

// Insight is Phase 8's whole read surface: statements, analyses, valuation, search, dashboard.
//
// # Why one façade and not five
//
// Every method here is a READ, and each is guarded by the view permission of whichever module
// answers — `accounting.PermProfitAndLossView` for the P&L, `sales.PermAnalysisView` for the
// sales analysis, and so on. Splitting them across five façades would put the same permissions in
// five files and give the frontend five imports for one screen area.
//
// It is also the boundary that keeps the rule 8.6 made structural: this file MAPS, and does not
// compute. A DTO built by subtracting two services' figures would be the reporting module Phase 8
// refused, relocated to the API layer.
type Insight struct{ graph }

// insightPolicies declares what each method requires.
//
// Every one is a VIEW permission, which is DoD criterion 10. Nothing in Phase 8 writes, so there
// is no state a wider grant could damage — what these protect is confidentiality, and a till
// operator has no business reading the company's margins.
func insightPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"ProfitAndLoss":   policy.Requires(accounting.PermProfitAndLossView),
		"BalanceSheet":    policy.Requires(accounting.PermBalanceSheetView),
		"SalesByPeriod":   policy.Requires(sales.PermAnalysisView),
		"SalesByProduct":  policy.Requires(sales.PermAnalysisView),
		"SalesByPartner":  policy.Requires(sales.PermAnalysisView),
		"SpendByPeriod":   policy.Requires(purchasing.PermAnalysisView),
		"SpendBySupplier": policy.Requires(purchasing.PermAnalysisView),
		"SpendByProduct":  policy.Requires(purchasing.PermAnalysisView),
		"Valuation":       policy.Requires(inventory.PermValuationView),
		// Search fans out to four modules, and the permission it demands is the one every user
		// of the search box already needs: the catalogue.
		//
		// There is no "any signed-in user" policy, and inventing one for this would be a hole
		// the coverage check could not reason about. Gating it behind all four modules'
		// permissions was the alternative, and it is worse: the box would go silently
		// INCOMPLETE for a user missing one of them rather than being refused, which is the
		// failure nobody reports because it looks like "no results".
		"Search": policy.Requires(catalog.PermCatalogView),
		// The dashboard's figures come from reports with their own permissions, and its tiles
		// mark themselves failed when a source refuses. The board itself needs the lowest bar
		// its tiles share.
		"Dashboard": policy.Requires(sales.PermAnalysisView),
	}
}

// ProfitAndLoss reports revenue and expense over a range.
func (i *Insight) ProfitAndLoss(from, to string) envelope.Result[ProfitAndLossDTO] {
	ctx, app, err := i.guard("ProfitAndLoss")
	if err != nil {
		return envelope.Fail[ProfitAndLossDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ProfitAndLossDTO](err)
	}
	statement, err := app.Accounting.ProfitAndLoss(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[ProfitAndLossDTO](err)
	}

	return envelope.Ok(ProfitAndLossDTO{
		RequestedFrom: statement.RequestedFrom, RequestedTo: statement.RequestedTo,
		CoveredFrom: statement.Covered.From, CoveredTo: statement.Covered.To,
		CoveredPeriods: statement.Covered.Periods,
		Revenue:        statementNodes(statement.Revenue),
		Expense:        statementNodes(statement.Expense),
		RevenueMinor:   minor(statement.RevenueMinor),
		ExpenseMinor:   minor(statement.ExpenseMinor),
		NetProfitMinor: minor(statement.NetProfitMinor),
	})
}

// BalanceSheet reports the position as at a date.
func (i *Insight) BalanceSheet(asAt string) envelope.Result[BalanceSheetDTO] {
	ctx, app, err := i.guard("BalanceSheet")
	if err != nil {
		return envelope.Fail[BalanceSheetDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[BalanceSheetDTO](err)
	}
	sheet, err := app.Accounting.BalanceSheet(ctx, companyID, asAt)
	if err != nil {
		return envelope.Fail[BalanceSheetDTO](err)
	}

	return envelope.Ok(BalanceSheetDTO{
		AsAt:      sheet.AsAt,
		Asset:     statementNodes(sheet.Asset),
		Liability: statementNodes(sheet.Liability),
		Equity:    statementNodes(sheet.Equity),

		AssetMinor:     minor(sheet.AssetMinor),
		LiabilityMinor: minor(sheet.LiabilityMinor),
		EquityMinor:    minor(sheet.EquityMinor),
		ResultMinor:    minor(sheet.ResultMinor),

		OutOfBalanceMinor: minor(sheet.OutOfBalanceMinor),
		Balanced:          sheet.OutOfBalanceMinor == 0,
	})
}

// SalesByPeriod reports what was sold per day or per month.
func (i *Insight) SalesByPeriod(from, to, grouping string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SalesByPeriod")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Sales.SalesByPeriod(ctx, companyID, from, to, sales.Grouping(grouping))
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(salesAnalysis(analysis))
}

// SalesByProduct reports revenue, cost and margin per variant.
func (i *Insight) SalesByProduct(from, to string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SalesByProduct")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Sales.SalesByProduct(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(salesAnalysis(analysis))
}

// SalesByPartner reports revenue, cost and margin per customer.
func (i *Insight) SalesByPartner(from, to string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SalesByPartner")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Sales.SalesByPartner(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(salesAnalysis(analysis))
}

// SpendByPeriod reports what was bought per day or per month.
func (i *Insight) SpendByPeriod(from, to, grouping string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SpendByPeriod")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Purchasing.SpendByPeriod(
		ctx, companyID, from, to, purchasing.Grouping(grouping))
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(spendAnalysis(analysis))
}

// SpendByProduct reports what was bought, per variant.
func (i *Insight) SpendByProduct(from, to string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SpendByProduct")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Purchasing.SpendByProduct(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(spendAnalysis(analysis))
}

// SpendBySupplier reports what was bought from each supplier.
func (i *Insight) SpendBySupplier(from, to string) envelope.Result[AnalysisDTO] {
	ctx, app, companyID, err := i.forCompany("SpendBySupplier")
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	analysis, err := app.Purchasing.SpendBySupplier(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[AnalysisDTO](err)
	}
	return envelope.Ok(spendAnalysis(analysis))
}

// Valuation reports// Valuation reports what the stock is worth and whether the ledger agrees.
func (i *Insight) Valuation() envelope.Result[ValuationDTO] {
	ctx, app, err := i.guard("Valuation")
	if err != nil {
		return envelope.Fail[ValuationDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ValuationDTO](err)
	}
	valuation, err := app.Inventory.Valuation(ctx, companyID)
	if err != nil {
		return envelope.Fail[ValuationDTO](err)
	}

	lines := make([]ValuedLineDTO, 0, len(valuation.Lines))
	for _, line := range valuation.Lines {
		lines = append(lines, ValuedLineDTO{
			WarehouseID: string(line.WarehouseID), WarehouseName: line.WarehouseName,
			ProductID: string(line.ProductID), VariantID: string(line.VariantID),
			VariantSKU: line.VariantSKU, ProductName: line.ProductName,
			QuantityMicro: minor(line.QuantityMicro), AvgCostMicro: minor(line.AvgCostMicro),
			ValueMinor: minor(line.ValueMinor),
		})
	}
	return envelope.Ok(ValuationDTO{
		Lines: lines, TotalMinor: minor(valuation.TotalMinor),
		HasLedger: valuation.HasLedger, LedgerMinor: minor(valuation.LedgerMinor),
		DifferenceMinor: minor(valuation.DifferenceMinor),
		// Reconciled is false when nothing was compared, NOT true. "The books agree" and "nobody
		// asked the books" must not look the same on a screen.
		Reconciled: valuation.HasLedger && valuation.DifferenceMinor == 0,
	})
}

// Search finds products, partners, invoices and bills from one query.
func (i *Insight) Search(query string, limit int) envelope.Result[SearchResponseDTO] {
	ctx, app, err := i.guard("Search")
	if err != nil {
		return envelope.Fail[SearchResponseDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[SearchResponseDTO](err)
	}
	response, err := app.Search.Search(ctx, companyID, query, limit)
	if err != nil {
		return envelope.Fail[SearchResponseDTO](err)
	}

	results := make([]SearchResultDTO, 0, len(response.Results))
	for _, result := range response.Results {
		results = append(results, SearchResultDTO{
			Kind: result.Kind, ID: string(result.ID),
			Label: result.Label, Subtitle: result.Subtitle,
		})
	}
	failed := response.Failed
	if failed == nil {
		// An empty slice, not null: a screen checking `failed.length` must not have to check for
		// null first, and JSON null is what a nil slice becomes.
		failed = []string{}
	}
	return envelope.Ok(SearchResponseDTO{
		Query: response.Query, Results: results, Failed: failed,
	})
}

// Dashboard assembles the home screen over a range.
//
// An empty range means month-to-date, resolved from the injected clock rather than from the
// frontend's idea of today — which is the browser's timezone and can be a day out.
func (i *Insight) Dashboard(from, to string) envelope.Result[DashboardDTO] {
	ctx, app, err := i.guard("Dashboard")
	if err != nil {
		return envelope.Fail[DashboardDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[DashboardDTO](err)
	}
	if from == "" || to == "" {
		from, to = app.MonthToDate()
	}

	board, err := app.Dashboard(ctx, companyID, from, to)
	if err != nil {
		return envelope.Fail[DashboardDTO](err)
	}

	tiles := make([]TileDTO, 0, len(board.Tiles))
	for _, tile := range board.Tiles {
		tiles = append(tiles, TileDTO{
			Key: tile.Key, Source: tile.Source, Kind: tile.Kind,
			AmountMinor: minor(tile.AmountMinor), Count: tile.Count,
			Periodic: tile.Periodic, Failed: tile.Failed,
		})
	}
	return envelope.Ok(DashboardDTO{From: board.From, To: board.To, Tiles: tiles})
}

// ── mapping ─────────────────────────────────────────────────────────────────────

// forCompany is guard plus "which company", which every method here needs and none varies.
//
// Four returns rather than a struct: the alternative was a context type carrying the app, and a
// type invented to shorten four call sites is a type the next reader has to learn.
func (i *Insight) forCompany(
	method string,
) (context.Context, *bootstrap.App, id.ID, error) {
	ctx, app, err := i.guard(method)
	if err != nil {
		return nil, nil, id.ID(""), err
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return nil, nil, id.ID(""), err
	}
	return ctx, app, companyID, nil
}

// salesAnalysis maps a sales analysis, margin and all.
func salesAnalysis(analysis sales.Analysis) AnalysisDTO {
	rows := make([]AnalysisRowDTO, 0, len(analysis.Rows))
	for _, row := range analysis.Rows {
		rows = append(rows, salesRow(row))
	}
	return AnalysisDTO{
		From: analysis.From, To: analysis.To, Rows: rows,
		Total: salesRow(analysis.Total),
	}
}

func salesRow(row sales.Figures) AnalysisRowDTO {
	return AnalysisRowDTO{
		Key: row.Key, Label: row.Label, ID: string(row.ID), Documents: row.Documents,
		QuantityMicro: minor(row.QuantityMicro), RevenueMinor: minor(row.RevenueMinor),
		CostMinor: minor(row.CostMinor), GrossMarginMinor: minor(row.GrossMarginMinor),
		MarginPercentMicro: minor(row.MarginPercentMicro()),
	}
}

// spendAnalysis maps a purchase analysis.
//
// `costMinor`, `grossMarginMinor` and `marginPercentMicro` are left EMPTY rather than zero. A
// purchase has no margin — the cost is the purchase — and "0" on screen would be a claim that it
// broke even, which is a different statement from "this column does not apply here".
func spendAnalysis(analysis purchasing.PurchaseAnalysis) AnalysisDTO {
	rows := make([]AnalysisRowDTO, 0, len(analysis.Rows))
	for _, row := range analysis.Rows {
		rows = append(rows, spendRow(row))
	}
	return AnalysisDTO{
		From: analysis.From, To: analysis.To, Rows: rows,
		Total: spendRow(analysis.Total),
	}
}

func spendRow(row purchasing.Spend) AnalysisRowDTO {
	return AnalysisRowDTO{
		Key: row.Key, Label: row.Label, ID: string(row.ID), Documents: row.Documents,
		QuantityMicro: minor(row.QuantityMicro), RevenueMinor: minor(row.NetMinor),
	}
}

// statementNodes maps a statement tree, keeping its shape.
func statementNodes(nodes []accounting.StatementNode) []StatementNodeDTO {
	out := make([]StatementNodeDTO, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, StatementNodeDTO{
			AccountID: string(node.AccountID), Code: node.Code, Name: node.Name,
			Type: string(node.Type), Depth: node.Depth, Postable: node.IsPostable,
			AmountMinor: minor(node.AmountMinor),
			Children:    statementNodes(node.Children),
		})
	}
	return out
}
