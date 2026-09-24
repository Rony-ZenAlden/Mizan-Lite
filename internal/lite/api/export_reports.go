package api

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/sheets"
)

// Dollars only (0.10.1): a dollars-only shop's reports are read in dollars — the screens hide every pound column — and
// its exports are the same reports. dropAt takes a table's pound columns out; each builder names its own.

// dropAt returns row without the entries at the given indexes, which are ascending. An index past the row's end is
// ignored: a sheet's blank or note rows are shorter than its data rows.
func dropAt[T any](row []T, at ...int) []T {
	out := make([]T, 0, len(row))
	next := 0
	for i, c := range row {
		for next < len(at) && at[next] < i {
			next++
		}
		if next < len(at) && at[next] == i {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dollarsOnly takes the pound columns at out of a table and a sheet in a dollars-only shop, and leaves both alone in
// every other. The table's columns and the sheet's are given apart: a sheet carries a unit column the table does not.
func (w words) dollarsOnly(t documents.Table, tableAt []int, sh sheets.Sheet, sheetAt []int) (documents.Table, sheets.Sheet) {
	if !w.usdOnly() {
		return t, sh
	}
	t.Headings = dropAt(t.Headings, tableAt...)
	t.Widths = dropAt(t.Widths, tableAt...)
	for i := range t.Rows {
		t.Rows[i] = dropAt(t.Rows[i], tableAt...)
	}
	if t.Total != nil {
		t.Total = dropAt(t.Total, tableAt...)
	}
	for i := range t.Footer {
		t.Footer[i] = dropAt(t.Footer[i], tableAt...)
	}
	for i := range sh.Rows {
		sh.Rows[i] = dropAt(sh.Rows[i], sheetAt...)
	}
	return t, sh
}

// statementRow is one line of a two-reading statement: a label, dollars, pounds.
type statementRow struct {
	label      string
	usd, local string
	bold       bool
	margin     bool // both readings are percentages
}

func (w words) twoReadings(rows []statementRow, local string) (documents.Table, sheets.Sheet, []sheets.Cell) {
	t := documents.Table{Headings: []string{w.t("reports.col.line"), w.t("reports.col.usd"), w.t("reports.col.local")}, Widths: []int{30, 14, 16}}
	head := headings(w.t("reports.col.line"), w.t("reports.col.usd"), w.t("reports.col.local"))
	sh := sheets.Sheet{Frozen: 1, Rows: [][]sheets.Cell{head}}
	for _, r := range rows {
		usd, loc := documents.M(w.money(r.usd, "USD")), documents.M(w.money(r.local, local))
		if r.margin {
			usd, loc = documents.M(percent(w, r.usd)), documents.M(percent(w, r.local))
		}
		usd.Bold, loc.Bold = r.bold, r.bold
		t.Rows = append(t.Rows, []documents.Cell{{Text: r.label, Bold: r.bold}, usd, loc})
		label := sheets.Text(r.label)
		a, b := num(r.usd), num(r.local)
		if r.bold {
			label, a, b = label.Bolded(), a.Bolded(), b.Bolded()
		}
		sh.Rows = append(sh.Rows, []sheets.Cell{label, a, b})
	}
	t, sh = w.dollarsOnly(t, []int{2}, sh, []int{2})
	return t, sh, sh.Rows[0]
}

func percent(w words, v string) string {
	if v == "" {
		return ""
	}
	return w.t("reports.margin", "margin", w.fig(v))
}

func nonZero(values ...string) bool {
	for _, v := range values {
		if !isZeroFigure(v) {
			return true
		}
	}
	return false
}

func (w words) dayRows(d DayReportDTO) []statementRow {
	p := d.Profit
	rows := []statementRow{{label: w.t("reports.sales_count", "count", w.fig(strconv.Itoa(p.Sales))), usd: p.RevenueUSD, local: p.RevenueLocal}}
	if nonZero(p.DiscountUSD, p.DiscountLocal) {
		rows = append(rows, statementRow{label: w.t("reports.sale_discounts"), usd: p.DiscountUSD, local: p.DiscountLocal})
	}
	// Rounding to a paper note is the pound's alone: a dollars-only statement has no line for it, as its screen has none.
	if nonZero(p.RoundingLocal) && !w.usdOnly() {
		rows = append(rows, statementRow{label: w.t("reports.rounding"), usd: "0.00", local: p.RoundingLocal})
	}
	rows = append(rows,
		statementRow{label: w.t("reports.cost"), usd: p.CostUSD, local: p.CostLocal},
		statementRow{label: w.t("reports.gross_profit"), usd: p.ProfitUSD, local: p.ProfitLocal, bold: true})
	if p.MarginUSD != "" || p.MarginLocal != "" {
		rows = append(rows, statementRow{label: w.t("reports.col.margin"), usd: p.MarginUSD, local: p.MarginLocal, margin: true})
	}
	rows = append(rows, statementRow{label: w.t("reports.losses"), usd: d.Losses.Out.USD, local: d.Losses.Out.Local})
	for _, l := range []struct {
		key string
		a   AmountDTO
	}{{"reports.losses.spoiled", d.Losses.Spoiled}, {"reports.losses.own_use", d.Losses.OwnUse}, {"reports.losses.other", d.Losses.Other},
		{"reports.losses.shortfall", d.Losses.Shortfall}, {"reports.losses.surplus", d.Losses.Surplus}} {
		if nonZero(l.a.USD, l.a.Local) {
			rows = append(rows, statementRow{label: w.t(l.key), usd: l.a.USD, local: l.a.Local})
		}
	}
	rows = append(rows, statementRow{label: w.t("reports.bad_debts"), usd: d.BadDebts.USD, local: d.BadDebts.Local},
		statementRow{label: w.t("reports.expenses"), usd: d.Expenses.USD, local: d.Expenses.Local})
	for _, c := range d.Categories {
		rows = append(rows, statementRow{label: w.t("cash.category." + c.Category), usd: c.Amount.USD, local: c.Amount.Local})
	}
	return append(rows, statementRow{label: w.t("reports.net_profit"), usd: d.NetUSD, local: d.NetLocal, bold: true})
}

func (w words) takings(d DayReportDTO) (documents.Table, sheets.Sheet) {
	heads := []string{w.t("reports.col.line")}
	for _, k := range d.Takings {
		heads = append(heads, w.cur(k.Currency))
	}
	t := documents.Table{Headings: heads}
	sh := sheets.Sheet{Name: w.t("reports.takings"), Frozen: 1, Rows: [][]sheets.Cell{headings(heads...)}}
	line := func(label string, pick func(TakingsDTO) string) {
		cells := []documents.Cell{documents.T(label)}
		row := []sheets.Cell{sheets.Text(label)}
		for _, k := range d.Takings {
			cells = append(cells, documents.M(w.money(pick(k), k.Currency)))
			row = append(row, num(pick(k)))
		}
		t.Rows = append(t.Rows, cells)
		sh.Rows = append(sh.Rows, row)
	}
	first := func(key string, n func(TakingsDTO) int) string {
		total := 0
		for _, k := range d.Takings {
			total += n(k)
		}
		return w.t(key, "count", w.fig(strconv.Itoa(total)))
	}
	line(first("reports.takings.sales", func(k TakingsDTO) int { return k.Sales }), func(k TakingsDTO) string { return k.Charged })
	line(first("reports.takings.credit", func(k TakingsDTO) int { return k.CreditSales }), func(k TakingsDTO) string { return k.Credit })
	line(w.t("reports.takings.discounts"), func(k TakingsDTO) string { return k.Discounts })
	line(w.t("reports.takings.rounding"), func(k TakingsDTO) string { return k.Rounding })
	line(first("reports.takings.voids", func(k TakingsDTO) int { return k.Voids }), func(k TakingsDTO) string { return k.Voided })
	line(w.t("reports.takings.collected"), func(k TakingsDTO) string { return k.Collected })
	line(w.t("reports.takings.refunded"), func(k TakingsDTO) string { return k.Refunded })
	line(w.t("reports.takings.written_off"), func(k TakingsDTO) string { return k.WrittenOff })
	return t, sh
}

func (w words) dayExport(d DayReportDTO) exportable {
	x := exportable{title: w.t("doc.report_day"), subtitle: w.date(d.Date)}
	if d.Profit.UnknownLines > 0 {
		text := w.t("reports.unknown_cost", "count", w.fig(strconv.Itoa(d.Profit.UnknownLines)),
			"usd", w.fig(documents.Group(d.Profit.UnknownUSD)), "local", w.fig(documents.Group(d.Profit.UnknownLocal)), "currency", w.short(d.LocalCurrency))
		if w.usdOnly() {
			text = w.t("reports.unknown_cost_usd", "count", w.fig(strconv.Itoa(d.Profit.UnknownLines)), "usd", w.fig(documents.Group(d.Profit.UnknownUSD)))
		}
		x.blocks = append(x.blocks, documents.Paragraph{Bold: true, Text: text})
	}
	if d.Unconverted > 0 {
		x.blocks = append(x.blocks, documents.Paragraph{Bold: true, Text: w.t("reports.unconverted", "count", w.fig(strconv.Itoa(d.Unconverted)))})
	}
	table, sh, _ := w.twoReadings(w.dayRows(d), d.LocalCurrency)
	sh.Name = w.t("doc.sheet_statement")
	takings, takingsSheet := w.takings(d)
	x.blocks = append(x.blocks, table)
	// The note says at what rates the pound column was converted; a dollars-only statement has no pound column.
	if !w.usdOnly() {
		note := w.t("reports.rate_note_range")
		if d.Rate != "" {
			note = w.t("reports.rate_note", "rate", w.fig(documents.Group(d.Rate)))
		}
		x.blocks = append(x.blocks, documents.Paragraph{Text: note, Small: true})
		sh.Rows = append(sh.Rows, []sheets.Cell{}, []sheets.Cell{sheets.Text(note)})
	}
	x.blocks = append(x.blocks, documents.Heading{Text: w.t("reports.takings")}, takings)
	x.sheets = []sheets.Sheet{sh, takingsSheet}
	return x
}

func (w words) monthExport(m MonthReportDTO) exportable {
	x := exportable{title: w.t("doc.report_month"), subtitle: w.date(m.Month)}
	heads := []string{w.t("reports.col.date"), w.t("reports.col.sales"), w.t("reports.col.revenue_usd"), w.t("reports.col.gross_usd"),
		w.t("reports.col.net_usd"), w.t("reports.col.gross_local"), w.t("reports.col.net_local")}
	t := documents.Table{Headings: heads, Widths: []int{13, 7, 12, 12, 12, 14, 14}}
	sh := sheets.Sheet{Name: w.t("doc.sheet_days"), Frozen: 1, Rows: [][]sheets.Cell{headings(heads...)}}
	row := func(date string, d DayReportDTO, bold bool) {
		cells := []documents.Cell{{Text: date, Bold: bold}, {Text: w.fig(strconv.Itoa(d.Profit.Sales)), End: true, Bold: bold},
			{Text: w.money(d.Profit.RevenueUSD, "USD"), End: true, Bold: bold}, {Text: w.money(d.Profit.ProfitUSD, "USD"), End: true, Bold: bold},
			{Text: w.money(d.NetUSD, "USD"), End: true, Bold: bold}, {Text: w.money(d.Profit.ProfitLocal, m.LocalCurrency), End: true, Bold: bold},
			{Text: w.money(d.NetLocal, m.LocalCurrency), End: true, Bold: bold}}
		t.Rows = append(t.Rows, cells)
		sh.Rows = append(sh.Rows, []sheets.Cell{sheets.Text(date), count(d.Profit.Sales), num(d.Profit.RevenueUSD), num(d.Profit.ProfitUSD),
			num(d.NetUSD), num(d.Profit.ProfitLocal), num(d.NetLocal)})
	}
	for _, d := range m.Days {
		row(w.date(d.Date), d, false)
	}
	t.Total = nil
	row(w.t("reports.month_total"), m.Total, true)
	t, sh = w.dollarsOnly(t, []int{5, 6}, sh, []int{5, 6})
	totalTable, totalSheet, _ := w.twoReadings(w.dayRows(m.Total), m.LocalCurrency)
	totalSheet.Name = w.t("doc.sheet_totals")
	x.blocks = []documents.Block{t, documents.Heading{Text: w.t("reports.month_total")}, totalTable}
	x.sheets = []sheets.Sheet{sh, totalSheet}
	return x
}

func (w words) productsExport(p ProductsReportDTO) exportable {
	x := exportable{title: w.t("doc.report_products"), subtitle: w.rangeText(p.From, p.To)}
	heads := []string{w.t("reports.col.product"), w.t("reports.col.quantity"), w.t("reports.col.revenue_usd"), w.t("reports.col.cost_usd"),
		w.t("reports.col.gross_usd"), w.t("reports.col.margin"), w.t("reports.col.gross_local"), w.t("reports.col.no_cost")}
	// Eight columns on a portrait page: the amounts carry no currency name — each heading names it — so none wraps.
	t := documents.Table{Headings: heads, Widths: []int{15, 16, 12, 12, 12, 9, 14, 10}}
	sheetHeads := append([]string{heads[0], heads[1], w.t("doc.col_unit")}, heads[2:]...)
	sh := sheets.Sheet{Name: w.t("doc.sheet_products"), Frozen: 1, Rows: [][]sheets.Cell{headings(sheetHeads...)}}
	for _, r := range p.Rows {
		noCost := ""
		if r.UnknownLines > 0 {
			noCost = w.t("reports.no_cost_lines", "count", w.fig(strconv.Itoa(r.UnknownLines)), "usd", w.fig(documents.Group(r.UnknownUSD)))
		}
		t.Rows = append(t.Rows, []documents.Cell{documents.T(w.name(r.NameAR, r.NameEN)), documents.M(w.fig(r.Quantity) + " " + w.unit(r.UnitCode)),
			documents.M(w.amount(r.RevenueUSD)), documents.M(w.amount(r.CostUSD)), documents.M(w.amount(r.ProfitUSD)),
			documents.M(percent(w, r.MarginUSD)), documents.M(w.amount(r.ProfitLocal)), documents.T(noCost)})
		sh.Rows = append(sh.Rows, []sheets.Cell{sheets.Text(w.name(r.NameAR, r.NameEN)), num(r.Quantity), sheets.Text(w.unit(r.UnitCode)), num(r.RevenueUSD),
			num(r.CostUSD), num(r.ProfitUSD), num(r.MarginUSD), num(r.ProfitLocal), sheets.Text(noCost)})
	}
	recon := w.t("reports.reconciling_local", "discount", w.fig(documents.Group(p.DiscountLocal)), "rounding", w.fig(documents.Group(p.RoundingLocal)), "currency", w.short(p.LocalCurrency))
	reconciling := w.t("reports.reconciling")
	if w.usdOnly() {
		reconciling = w.t("reports.reconciling_usd") // no pound rounding to reconcile
	}
	t.Rows = append(t.Rows, []documents.Cell{documents.T(reconciling), documents.T(""), documents.M(w.amount(p.DiscountUSD)),
		documents.T(""), documents.T(""), documents.T(""), documents.T(recon), documents.T("")})
	t.Total = []documents.Cell{documents.T(w.t("reports.products_total", "count", w.fig(strconv.Itoa(p.Total.Sales)))), documents.T(""),
		documents.M(w.amount(p.Total.RevenueUSD)), documents.M(w.amount(p.Total.CostUSD)), documents.M(w.amount(p.Total.ProfitUSD)),
		documents.M(percent(w, p.Total.MarginUSD)), documents.M(w.amount(p.Total.ProfitLocal)), documents.T("")}
	reconcilingRow := []sheets.Cell{sheets.Text(reconciling), {}, {}, num(p.DiscountUSD), {}, {}, {}, num(p.DiscountLocal), sheets.Text(w.t("reports.rounding")), num(p.RoundingLocal)}
	if w.usdOnly() {
		reconcilingRow = reconcilingRow[:4] // the discount in dollars; the pounds' discount and rounding are not a dollars-only shop's
	}
	sh.Rows = append(sh.Rows, reconcilingRow,
		[]sheets.Cell{sheets.Text(w.t("reports.products_total", "count", strconv.Itoa(p.Total.Sales))).Bolded(), {}, {}, num(p.Total.RevenueUSD).Bolded(),
			num(p.Total.CostUSD).Bolded(), num(p.Total.ProfitUSD).Bolded(), num(p.Total.MarginUSD).Bolded(), num(p.Total.ProfitLocal).Bolded()})
	// Dollars only: the pound profit column goes, and the pound reconciliation with it.
	t, sh = w.dollarsOnly(t, []int{6}, sh, []int{7})
	x.blocks = []documents.Block{t, documents.Paragraph{Text: w.t("reports.products_hint"), Small: true}}
	x.sheets = []sheets.Sheet{sh}
	return x
}

func (w words) stockExport(s StockReportDTO) exportable {
	x := exportable{title: w.t("doc.report_stock"), subtitle: w.rangeText(s.From, s.To)}
	cur := s.LocalCurrency
	valueHeads := []string{w.t("reports.col.product"), w.t("reports.col.on_hand"), w.t("reports.col.average_cost"), w.t("reports.col.value_usd"), w.t("reports.col.value_local")}
	value := documents.Table{Headings: valueHeads, Widths: []int{30, 18, 17, 16, 19}}
	valueSheet := sheets.Sheet{Name: w.t("doc.sheet_value"), Frozen: 1, Rows: [][]sheets.Cell{headings(append(valueHeads[:2:2], append([]string{w.t("doc.col_unit")}, valueHeads[2:]...)...)...)}}
	for _, l := range s.Lines {
		value.Rows = append(value.Rows, []documents.Cell{documents.T(w.name(l.NameAR, l.NameEN)), documents.M(w.fig(l.OnHand) + " " + w.unit(l.UnitCode)),
			documents.M(w.money(l.AverageCost, "USD")), documents.M(w.money(l.ValueUSD, "USD")), documents.M(w.money(l.ValueLocal, cur))})
		valueSheet.Rows = append(valueSheet.Rows, []sheets.Cell{sheets.Text(w.name(l.NameAR, l.NameEN)), num(l.OnHand), sheets.Text(w.unit(l.UnitCode)),
			num(l.AverageCost), num(l.ValueUSD), num(l.ValueLocal)})
	}
	value.Total = []documents.Cell{documents.T(w.t("receipt.total")), documents.T(""), documents.T(""), documents.M(w.money(s.TotalUSD, "USD")), documents.M(w.money(s.TotalLocal, cur))}
	valueSheet.Rows = append(valueSheet.Rows, []sheets.Cell{sheets.Text(w.t("receipt.total")).Bolded(), {}, {}, {}, num(s.TotalUSD).Bolded(), num(s.TotalLocal).Bolded()})
	value, valueSheet = w.dollarsOnly(value, []int{4}, valueSheet, []int{5})
	c := s.Reconciliation
	reconRows := []struct{ key, v string }{{"reports.stock.opening", c.Opening}, {"reports.stock.received", c.Received}, {"reports.stock.sold", c.Sold},
		{"reports.stock.losses", c.Losses}, {"reports.stock.gains", c.Gains}, {"reports.stock.revaluation", c.Revaluation}, {"reports.stock.packages", c.Packages},
		{"reports.stock.negative", c.NegativeStock}, {"reports.stock.rounding", c.Rounding}, {"reports.stock.closing", c.Closing}}
	var pairs []documents.Pair
	reconSheet := sheets.Sheet{Name: w.t("doc.sheet_movements"), Rows: [][]sheets.Cell{headings(w.t("reports.col.line"), w.t("reports.col.value_usd"))}, Frozen: 1}
	for _, r := range reconRows {
		if (r.key == "reports.stock.negative" || r.key == "reports.stock.rounding") && isZeroFigure(r.v) {
			continue
		}
		bold := r.key == "reports.stock.closing"
		pairs = append(pairs, documents.Pair{Label: w.t(r.key), Value: documents.T(w.money(r.v, "USD")), Bold: bold})
		reconSheet.Rows = append(reconSheet.Rows, []sheets.Cell{sheets.Text(w.t(r.key)), num(r.v)})
	}
	shelfHeads := []string{w.t("reports.col.product"), w.t("reports.col.on_hand"), w.t("reports.col.average_cost"), w.t("reports.col.price"),
		w.t("reports.col.expected_usd"), w.t("reports.col.expected_local")}
	shelf := documents.Table{Headings: shelfHeads, Widths: []int{24, 16, 15, 14, 14, 17}}
	shelfSheet := sheets.Sheet{Name: w.t("doc.sheet_shelf"), Frozen: 1, Rows: [][]sheets.Cell{headings(append(shelfHeads, w.t("doc.col_price_currency"), w.t("reports.shelf.priced_below_cost"))...)}}
	for _, l := range s.Shelf {
		name := w.name(l.NameAR, l.NameEN)
		if l.BelowCost {
			name += " — " + w.t("reports.shelf.priced_below_cost")
		}
		shelf.Rows = append(shelf.Rows, []documents.Cell{documents.T(name), documents.M(w.fig(l.OnHand) + " " + w.unit(l.UnitCode)), documents.M(w.money(l.AverageCost, "USD")),
			documents.M(w.money(l.Price, l.PriceCurrency)), documents.M(w.money(l.ProfitUSD, "USD")), documents.M(w.money(l.ProfitLocal, cur))})
		below := ""
		if l.BelowCost {
			below = w.t("reports.shelf.priced_below_cost")
		}
		shelfSheet.Rows = append(shelfSheet.Rows, []sheets.Cell{sheets.Text(w.name(l.NameAR, l.NameEN)), num(l.OnHand), num(l.AverageCost), num(l.Price),
			num(l.ProfitUSD), num(l.ProfitLocal), sheets.Text(l.PriceCurrency), sheets.Text(below)})
	}
	shelf.Total = []documents.Cell{documents.T(w.t("receipt.total")), documents.T(""), documents.T(""), documents.T(""), documents.M(w.money(s.ShelfTotalUSD, "USD")), documents.M(w.money(s.ShelfTotalLocal, cur))}
	shelf, shelfSheet = w.dollarsOnly(shelf, []int{5}, shelfSheet, []int{5})
	left := documents.Table{Headings: []string{w.t("reports.col.product"), w.t("doc.col_reason")}}
	leftSheet := sheets.Sheet{Name: w.t("doc.sheet_left_out"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("reports.col.product"), w.t("doc.col_reason"))}}
	for _, l := range s.LeftOut {
		left.Rows = append(left.Rows, []documents.Cell{documents.T(w.name(l.NameAR, l.NameEN)), documents.T(w.t("reports.left_out." + l.Reason))})
		leftSheet.Rows = append(leftSheet.Rows, []sheets.Cell{sheets.Text(w.name(l.NameAR, l.NameEN)), sheets.Text(w.t("reports.left_out." + l.Reason))})
	}
	x.blocks = []documents.Block{documents.Heading{Text: w.t("reports.stock.value_on", "date", w.date(s.To))}, value,
		documents.Heading{Text: w.t("reports.stock.movements", "from", w.date(c.From), "to", w.date(c.To))}, documents.Pairs{Rows: pairs},
		documents.Heading{Text: w.t("reports.shelf.title")}, shelf}
	if len(s.LeftOut) > 0 {
		x.blocks = append(x.blocks, documents.Heading{Text: w.t("reports.shelf.left_out", "count", w.fig(strconv.Itoa(len(s.LeftOut))))}, left)
	}
	x.sheets = []sheets.Sheet{valueSheet, reconSheet, shelfSheet, leftSheet}
	return x
}

func (w words) drawerExport(d DrawerDTO) exportable {
	x := exportable{title: w.t("doc.report_drawer"), subtitle: w.date(d.Date)}
	termsSheet := sheets.Sheet{Name: w.t("doc.sheet_drawer"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("cash.currency"), w.t("reports.col.line"), w.t("cash.amount"))}}
	for _, c := range d.Currencies {
		opening := w.t("cash.opening_never")
		if c.OpeningCountDate != "" {
			opening = w.t("cash.opening_counted", "date", w.date(c.OpeningCountDate))
		}
		rows := []struct {
			label, value string
			bold         bool
		}{{opening, c.Opening, false}, {w.t("cash.plus", "term", w.t("cash.term.cash_sales")), c.CashSalesIn, false},
			{w.t("cash.plus", "term", w.t("cash.term.credit_paid")), c.CreditPaidIn, false}, {w.t("cash.plus", "term", w.t("cash.term.repayments")), c.RepaymentsIn, false},
			{w.t("cash.plus", "term", w.t("cash.term.deposits")), c.DepositsIn, false}, {w.t("cash.less", "term", w.t("cash.term.change")), c.ChangeOut, false},
			{w.t("cash.less", "term", w.t("cash.term.refunds")), c.RefundsOut, false}, {w.t("cash.less", "term", w.t("cash.term.void_returns")), c.VoidReturns, false},
			{w.t("cash.less", "term", w.t("cash.term.expenses")), c.ExpensesOut, false}, {w.t("cash.less", "term", w.t("cash.term.withdrawals")), c.WithdrawalsOut, false},
			{w.t("cash.expected"), c.Expected, true}}
		if c.Counted {
			rows = append(rows, struct {
				label, value string
				bold         bool
			}{w.t("doc.counted"), c.Count, false}, struct {
				label, value string
				bold         bool
			}{w.t("cash.difference", "expected", w.fig(documents.Group(c.CountExpected))), c.Difference, true})
		}
		var pairs []documents.Pair
		for _, r := range rows {
			if isZeroFigure(r.value) && !r.bold && r.label != opening {
				continue
			}
			pairs = append(pairs, documents.Pair{Label: r.label, Value: documents.T(w.money(r.value, c.Currency)), Bold: r.bold})
			termsSheet.Rows = append(termsSheet.Rows, []sheets.Cell{sheets.Text(w.cur(c.Currency)), sheets.Text(r.label), num(r.value)})
		}
		if !c.Counted {
			pairs = append(pairs, documents.Pair{Label: w.t("cash.not_counted")})
		}
		x.blocks = append(x.blocks, documents.Heading{Text: w.cur(c.Currency)}, documents.Pairs{Rows: pairs})
	}
	book := documents.Table{Headings: []string{w.t("cash.col.time"), w.t("cash.col.kind"), w.t("cash.col.amount"), w.t("cash.col.detail")}, Widths: []int{12, 10, 12, 26}}
	bookSheet := sheets.Sheet{Name: w.t("cash.book"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("cash.col.time"), w.t("cash.col.kind"), w.t("cash.currency"), w.t("cash.col.amount"),
		w.t("cash.category"), w.t("cash.note"), w.t("doc.reversed"))}}
	for _, e := range d.Entries {
		detail := w.user(e.Note)
		if e.Category != "" {
			detail = w.t("cash.category."+e.Category) + " " + detail
		}
		if e.Kind == "count" {
			detail = w.t("cash.count.detail", "expected", w.fig(documents.Group(e.Expected)), "difference", w.fig(documents.Group(e.Difference)))
		}
		kind := w.t("cash.kind." + e.Kind)
		reversed := ""
		if e.Reversed {
			reversed = w.t("doc.reversed")
			kind += " — " + reversed
		}
		book.Rows = append(book.Rows, []documents.Cell{documents.T(w.when(e.OccurredAt)), documents.T(kind), documents.M(w.money(e.Amount, e.Currency)), documents.T(detail)})
		category := ""
		if e.Category != "" {
			category = w.t("cash.category." + e.Category)
		}
		bookSheet.Rows = append(bookSheet.Rows, []sheets.Cell{sheets.Text(w.when(e.OccurredAt)), sheets.Text(w.t("cash.kind." + e.Kind)), sheets.Text(e.Currency),
			num(e.Amount), sheets.Text(category), sheets.Text(e.Note), sheets.Text(reversed)})
	}
	x.blocks = append(x.blocks, documents.Heading{Text: w.t("cash.book")})
	if len(book.Rows) == 0 {
		x.blocks = append(x.blocks, documents.Paragraph{Text: w.t("cash.book_empty"), Small: true})
	} else {
		x.blocks = append(x.blocks, book)
	}
	x.sheets = []sheets.Sheet{termsSheet, bookSheet}
	return x
}

func (w words) entryDetails(e EntryDTO) string {
	if e.Tendered == "" {
		return ""
	}
	return w.t("statement.cash", "tendered", w.money(e.Tendered, e.TenderedCurrency), "change", w.money(e.Change, e.ChangeCurrency), "rate", w.fig(documents.Group(e.Rate)))
}

func (w words) statementExport(name string, statements []StatementDTO) exportable {
	x := exportable{title: w.t("statement.title", "name", w.user(name)), subtitle: w.now()}
	if len(statements) == 0 {
		x.blocks = []documents.Block{documents.Paragraph{Text: w.t("statement.empty")}}
		x.sheets = []sheets.Sheet{{Name: w.t("doc.sheet_statement"), Rows: [][]sheets.Cell{{sheets.Text(w.t("statement.empty"))}}}}
		return x
	}
	heads := []string{w.t("statement.col.when"), w.t("statement.col.kind"), w.t("statement.col.amount"), w.t("statement.col.balance"), w.t("statement.col.details")}
	for _, st := range statements {
		pairs := []documents.Pair{{Label: w.t("statement.balance"), Value: documents.T(w.money(st.Balance, st.Currency)), Bold: true}}
		if st.OwedSince != "" {
			pairs = append(pairs, documents.Pair{Label: w.t("customers.col.owed_since"), Value: documents.T(w.date(st.OwedSince))})
		}
		if st.LastPayment != "" {
			pairs = append(pairs, documents.Pair{Label: w.t("customers.col.last_payment"), Value: documents.T(w.date(st.LastPayment))})
		}
		t := documents.Table{Headings: heads, Widths: []int{20, 12, 16, 16, 26}}
		sh := sheets.Sheet{Name: w.cur(st.Currency), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("statement.col.when"), w.t("statement.col.kind"), w.t("statement.col.amount"),
			w.t("statement.col.balance"), w.t("doc.col_tendered"), w.t("doc.col_tendered_currency"), w.t("receipt.change"), w.t("doc.col_change_currency"),
			w.t("doc.col_rate"), w.t("doc.note"), w.t("doc.reversed"))}}
		for _, e := range st.Entries {
			kind := w.t("debt.kind." + e.Kind)
			reversed := ""
			if e.Reversed {
				reversed = w.t("doc.reversed")
				kind += " — " + reversed
			}
			detail := w.entryDetails(e)
			if e.Note != "" {
				detail += " " + w.user(e.Note)
			}
			t.Rows = append(t.Rows, []documents.Cell{documents.T(w.when(e.OccurredAt)), documents.T(kind), documents.M(w.money(e.Amount, e.Currency)),
				documents.M(w.money(e.BalanceAfter, e.Currency)), documents.T(detail)})
			sh.Rows = append(sh.Rows, []sheets.Cell{sheets.Text(w.when(e.OccurredAt)), sheets.Text(w.t("debt.kind." + e.Kind)), num(e.Amount), num(e.BalanceAfter),
				num(e.Tendered), sheets.Text(e.TenderedCurrency), num(e.Change), sheets.Text(e.ChangeCurrency), num(e.Rate), sheets.Text(e.Note), sheets.Text(reversed)})
		}
		x.blocks = append(x.blocks, documents.Heading{Text: w.cur(st.Currency)}, documents.Pairs{Rows: pairs}, t)
		x.sheets = append(x.sheets, sh)
	}
	return x
}

func (w words) ledgerExport(out OutstandingDTO, entries []EntryDTO, from, to string) exportable {
	x := exportable{title: w.t("doc.report_ledger"), subtitle: w.rangeText(from, to)}
	owingHeads := []string{w.t("customers.col.name"), w.t("customers.col.phone"), w.t("cash.currency"), w.t("customers.col.balances"),
		w.t("customers.col.owed_since"), w.t("customers.col.last_payment")}
	owing := documents.Table{Headings: owingHeads, Widths: []int{20, 15, 13, 16, 13, 13}}
	owingSheet := sheets.Sheet{Name: w.t("doc.sheet_owing"), Frozen: 1, Rows: [][]sheets.Cell{headings(owingHeads...)}}
	for _, c := range out.Customers {
		for _, b := range c.Balances {
			owing.Rows = append(owing.Rows, []documents.Cell{documents.T(w.user(c.Name)), documents.T(w.fig(c.Phone)), documents.T(w.cur(b.Currency)),
				documents.M(w.money(b.Balance, b.Currency)), documents.T(w.date(b.OwedSince)), documents.T(w.date(b.LastPayment))})
			owingSheet.Rows = append(owingSheet.Rows, []sheets.Cell{sheets.Text(c.Name), sheets.Text(c.Phone), sheets.Text(b.Currency), num(b.Balance),
				sheets.Text(shopDate(b.OwedSince)), sheets.Text(shopDate(b.LastPayment))})
		}
	}
	bookHeads := []string{w.t("statement.col.when"), w.t("customers.col.name"), w.t("statement.col.kind"), w.t("statement.col.amount"), w.t("statement.col.balance"), w.t("statement.col.details")}
	book := documents.Table{Headings: bookHeads, Widths: []int{20, 18, 11, 16, 16, 21}}
	bookSheet := sheets.Sheet{Name: w.t("doc.sheet_debt_book"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("statement.col.when"), w.t("customers.col.name"), w.t("statement.col.kind"),
		w.t("cash.currency"), w.t("statement.col.amount"), w.t("statement.col.balance"), w.t("doc.col_tendered"), w.t("doc.col_tendered_currency"), w.t("receipt.change"),
		w.t("doc.col_change_currency"), w.t("doc.col_rate"), w.t("doc.note"))}}
	for _, e := range entries {
		book.Rows = append(book.Rows, []documents.Cell{documents.T(w.when(e.OccurredAt)), documents.T(w.user(e.CustomerName)), documents.T(w.t("debt.kind." + e.Kind)),
			documents.M(w.money(e.Amount, e.Currency)), documents.M(w.money(e.BalanceAfter, e.Currency)), documents.T(w.entryDetails(e))})
		bookSheet.Rows = append(bookSheet.Rows, []sheets.Cell{sheets.Text(w.when(e.OccurredAt)), sheets.Text(e.CustomerName), sheets.Text(w.t("debt.kind." + e.Kind)),
			sheets.Text(e.Currency), num(e.Amount), num(e.BalanceAfter), num(e.Tendered), sheets.Text(e.TenderedCurrency), num(e.Change), sheets.Text(e.ChangeCurrency),
			num(e.Rate), sheets.Text(e.Note)})
	}
	x.blocks = []documents.Block{documents.Heading{Text: w.t("doc.sheet_owing")}, owing, documents.Paragraph{Text: w.t("doc.never_totalled"), Small: true},
		documents.Heading{Text: w.t("doc.sheet_debt_book")}, book}
	x.sheets = []sheets.Sheet{owingSheet, bookSheet}
	return x
}

func (w words) salesExport(rows []historySale, from, to string) exportable {
	x := exportable{title: w.t("doc.report_sales"), subtitle: w.rangeText(from, to)}
	heads := []string{w.t("doc.col_receipt"), w.t("statement.col.when"), w.t("sales.col.status"), w.t("receipt.total"), w.t("receipt.tendered"),
		w.t("receipt.change"), w.t("doc.col_customer")}
	t := documents.Table{Headings: heads, Widths: []int{7, 17, 9, 12, 12, 10, 13}}
	salesSheet := sheets.Sheet{Name: w.t("doc.sheet_sales"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("doc.col_receipt"), w.t("statement.col.when"), w.t("sales.col.status"),
		w.t("doc.col_payment"), w.t("doc.col_settlement"), w.t("receipt.total"), w.t("receipt.tendered"), w.t("doc.col_tendered_currency"), w.t("receipt.change"),
		w.t("doc.col_change_currency"), w.t("doc.col_customer"), w.t("receipt.debt_added"), w.t("doc.col_cost_usd"), w.t("doc.col_void_date"), w.t("doc.void_reason"),
		w.t("doc.handed_back"), w.t("doc.col_rate"))}}
	linesSheet := sheets.Sheet{Name: w.t("doc.sheet_lines"), Frozen: 1, Rows: [][]sheets.Cell{headings(w.t("doc.col_receipt"), w.t("reports.col.product"), w.t("doc.col_quantity"),
		w.t("doc.col_unit"), w.t("reports.col.price"), w.t("doc.col_price_currency"), w.t("doc.col_discount_percent"), w.t("doc.col_net_local"), w.t("doc.col_net_usd"),
		w.t("doc.col_cost_usd"), w.t("doc.col_cost_local"))}}
	for _, h := range rows {
		s := h.sale
		status := w.t("sales.status." + s.Status)
		if s.Payment == "credit" {
			status += " · " + w.t("doc.payment.credit")
		}
		t.Rows = append(t.Rows, []documents.Cell{documents.M(w.fig(strconv.FormatInt(s.ReceiptNo, 10))), documents.T(w.when(s.SoldAt)), documents.T(status),
			documents.M(w.money(s.Total, s.Settlement)), documents.M(w.money(s.Tendered, s.TenderCurrency)), documents.M(w.money(s.Change, s.ChangeCurrency)),
			documents.T(w.user(s.CreditCustomerName))})
		voidReturn := ""
		if s.Status == "voided" {
			voidReturn = s.VoidReturn
		}
		salesSheet.Rows = append(salesSheet.Rows, []sheets.Cell{count(int(s.ReceiptNo)), sheets.Text(w.when(s.SoldAt)), sheets.Text(w.t("sales.status." + s.Status)),
			sheets.Text(w.t("doc.payment." + s.Payment)), sheets.Text(s.Settlement), num(s.Total), num(s.Tendered), sheets.Text(s.TenderCurrency), num(s.Change),
			sheets.Text(s.ChangeCurrency), sheets.Text(s.CreditCustomerName), num(s.CreditAmount), num(h.costUSD), sheets.Text(shopDate(s.VoidBusinessDate)), sheets.Text(s.VoidReason),
			num(voidReturn), num(s.Rate)})
		for i, l := range s.Lines {
			costUSD, costLocal := sheets.Text(w.t("reports.col.no_cost")), sheets.Text("")
			if i < len(h.lineCostKnown) && h.lineCostKnown[i] {
				costUSD, costLocal = num(h.lineCostUSD[i]), num(h.lineCostLocal[i])
			}
			linesSheet.Rows = append(linesSheet.Rows, []sheets.Cell{count(int(s.ReceiptNo)), sheets.Text(w.name(l.NameAR, l.NameEN)), num(l.Quantity), sheets.Text(w.unit(l.UnitCode)),
				num(l.UnitPrice), sheets.Text(l.PriceCurrency), num(l.DiscountPercent), num(l.NetLocal), num(l.NetUSD), costUSD, costLocal})
		}
	}
	x.blocks = []documents.Block{t}
	x.sheets = []sheets.Sheet{salesSheet, linesSheet}
	return x
}
