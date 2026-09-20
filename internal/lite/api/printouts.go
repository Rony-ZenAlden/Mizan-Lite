package api

import (
	"strconv"

	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// isZeroFigure is a figure Go formatted that is zero ("0", "0.00", "-0") or absent: a comparison of text, deciding whether a line
// is printed — never arithmetic.
func isZeroFigure(s string) bool {
	for _, r := range s {
		if r != '0' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

// header is the shop's lines at the top of every receipt and voucher.
func (w words) header(shop string, copyNo int, subtitle string) []documents.Block {
	blocks := []documents.Block{documents.Title{Text: w.user(shop), Subtitle: subtitle, Center: true}}
	r := w.settings.Receipt
	if r.Address != "" {
		blocks = append(blocks, documents.Paragraph{Text: w.user(r.Address), Small: true, Center: true})
	}
	if r.Phone != "" {
		blocks = append(blocks, documents.Paragraph{Text: w.fig(r.Phone), Small: true, Center: true})
	}
	if copyNo > 1 {
		blocks = append(blocks, documents.Stamp{Text: w.t("doc.copy", "number", w.fig(strconv.Itoa(copyNo)))})
	}
	return append(blocks, documents.Rule{Dashed: true})
}

func (w words) footer() []documents.Block {
	text := w.user(w.settings.Receipt.Footer)
	if text == "" {
		text = w.t("receipt.thanks")
	}
	return []documents.Block{documents.Space{Lines: 0.5}, documents.Paragraph{Text: text, Center: true}}
}

// receiptDocument is a sale's receipt, built from the receipt the screen shows (D-L7.6).
func (w words) receiptDocument(sale SaleDTO, copyNo int) documents.Document {
	settle := sale.Settlement
	blocks := w.header(sale.ShopName, copyNo, w.t("receipt.number", "number", w.fig(strconv.FormatInt(sale.ReceiptNo, 10)))+" · "+w.when(sale.SoldAt))
	if sale.Status == string(salesdomain.StatusVoided) {
		blocks = append(blocks, documents.Stamp{Text: w.t("doc.voided")},
			documents.Pairs{Rows: []documents.Pair{
				{Label: w.t("doc.void_reason"), Value: documents.T(w.user(sale.VoidReason))},
				{Label: w.t("doc.handed_back"), Value: documents.T(w.money(sale.VoidReturn, sale.VoidReturnCurrency))},
			}}, documents.Rule{Dashed: true})
	}
	for _, l := range sale.Lines {
		net := l.NetLocal
		if settle == tender.USD {
			net = l.NetUSD
		}
		label := w.fig(l.Quantity) + " " + w.unit(l.UnitCode) + " × " + w.money(l.UnitPrice, l.PriceCurrency)
		if l.DiscountPercent != "" {
			label += " · " + w.t("receipt.line_discount", "percent", w.fig(l.DiscountPercent))
		}
		blocks = append(blocks, documents.Paragraph{Text: w.name(l.NameAR, l.NameEN), Bold: true},
			documents.Pairs{Rows: []documents.Pair{{Label: label, Value: documents.T(w.money(net, settle))}}})
	}
	blocks = append(blocks, documents.Rule{Dashed: true})
	var pairs []documents.Pair
	discount := sale.DiscountLocal
	if settle == tender.USD {
		discount = sale.DiscountUSD
	}
	if !isZeroFigure(discount) {
		pairs = append(pairs, documents.Pair{Label: w.t("receipt.discount"), Value: documents.T(w.money(discount, settle))})
	}
	if !isZeroFigure(sale.Rounding) {
		pairs = append(pairs, documents.Pair{Label: w.t("receipt.rounding", "note", w.fig(documents.Group(sale.CashNote))), Value: documents.T(w.money(sale.Rounding, settle))})
	}
	pairs = append(pairs, documents.Pair{Label: w.t("receipt.total"), Value: documents.T(w.money(sale.Total, settle)), Bold: true})
	credit := sale.Payment == string(salesdomain.PaymentCredit)
	if credit {
		pairs = append(pairs, documents.Pair{Label: w.t("receipt.paid_now"), Value: documents.T(w.money(sale.Tendered, sale.TenderCurrency))})
		if sale.CreditCustomerID != "" {
			pairs = append(pairs,
				documents.Pair{Label: w.t("receipt.on_credit", "name", w.user(sale.CreditCustomerName))},
				documents.Pair{Label: w.t("receipt.debt_added"), Value: documents.T(w.money(sale.CreditAmount, sale.CreditCurrency))},
				documents.Pair{Label: w.t("receipt.balance_after"), Value: documents.T(w.money(sale.CreditBalanceAfter, sale.CreditCurrency))})
		}
	} else {
		pairs = append(pairs,
			documents.Pair{Label: w.t("receipt.tendered"), Value: documents.T(w.money(sale.Tendered, sale.TenderCurrency))},
			documents.Pair{Label: w.t("receipt.change"), Value: documents.T(w.money(sale.Change, sale.ChangeCurrency))})
	}
	blocks = append(blocks, documents.Pairs{Rows: pairs}, documents.Rule{Dashed: true},
		documents.Paragraph{Text: w.rate(sale.Rate, sale.LocalCurrency), Small: true, Center: true})
	if credit {
		blocks = append(blocks, documents.Signature{Label: w.t("doc.customer_signature")})
	}
	return documents.Document{Direction: w.dir, Blocks: append(blocks, w.footer()...)}
}

// voucherDocument is a debt payment's or refund's voucher (سند قبض، سند صرف).
func (w words) voucherDocument(v debtView, e customersdomain.Entry, number int64, numbered bool, copyNo int) documents.Document {
	title := w.t("doc.voucher_payment")
	if e.Kind == customersdomain.KindRefund {
		title = w.t("doc.voucher_refund")
	}
	no := "—"
	if numbered {
		no = w.fig(strconv.FormatInt(number, 10))
	}
	blocks := w.header(w.settings.ShopName, copyNo, title+" · "+w.t("doc.voucher_number", "number", no))
	settled := e.AmountMinor
	if settled < 0 {
		settled = -settled
	}
	dto := v.entry(e, false)
	pairs := []documents.Pair{
		{Label: w.t("doc.date"), Value: documents.T(w.when(dto.OccurredAt))},
		{Label: w.t("doc.customer"), Value: documents.T(w.user(e.CustomerName))},
		{Label: w.t("doc.settled"), Value: documents.T(w.money(v.money(settled, e.Currency), e.Currency)), Bold: true},
	}
	handed := w.t("doc.handed_over")
	if e.Kind == customersdomain.KindRefund {
		handed = w.t("doc.paid_out")
	}
	pairs = append(pairs, documents.Pair{Label: handed, Value: documents.T(w.money(dto.Tendered, dto.TenderedCurrency))})
	if !isZeroFigure(dto.Change) {
		pairs = append(pairs, documents.Pair{Label: w.t("receipt.change"), Value: documents.T(w.money(dto.Change, dto.ChangeCurrency))})
	}
	pairs = append(pairs,
		documents.Pair{Label: w.t("doc.balance_before"), Value: documents.T(w.money(v.money(e.BalanceBeforeMinor, e.Currency), e.Currency))},
		documents.Pair{Label: w.t("receipt.balance_after"), Value: documents.T(w.money(dto.BalanceAfter, e.Currency))})
	if e.Note != "" {
		pairs = append(pairs, documents.Pair{Label: w.t("doc.note"), Value: documents.T(w.user(e.Note))})
	}
	blocks = append(blocks, documents.Pairs{Rows: pairs}, documents.Rule{Dashed: true},
		documents.Paragraph{Text: w.rate(fxdomain.FormatRate(e.Cash.RateNano), v.local), Small: true, Center: true},
		documents.Signature{Label: w.t("doc.receiver_signature")})
	return documents.Document{Direction: w.dir, Blocks: append(blocks, w.footer()...)}
}

// testDocument is the page that shows a printer prints Arabic, Latin and figures at the paper's edges.
func (w words) testDocument(printer string) documents.Document {
	r := w.settings.Receipt
	blocks := w.header(w.settings.ShopName, 1, w.t("doc.test_title"))
	blocks = append(blocks,
		documents.Paragraph{Text: w.t("doc.test_arabic")},
		documents.Paragraph{Text: "Mizan Lite — " + w.fig("0123456789")},
		documents.Pairs{Rows: []documents.Pair{
			{Label: w.t("doc.test_printer"), Value: documents.T(w.user(printer))},
			{Label: w.t("doc.test_paper"), Value: documents.T(w.fig(strconv.Itoa(r.PaperMM)) + " mm")},
			{Label: w.t("doc.test_path"), Value: documents.T(w.t("printers.path." + r.Path))},
			{Label: w.t("doc.date"), Value: documents.T(w.now())},
			{Label: w.t("receipt.total"), Value: documents.T(w.money("76000", "SYP")), Bold: true},
		}},
		documents.Rule{},
	)
	return documents.Document{Direction: w.dir, Blocks: append(blocks, w.footer()...)}
}

// rate is the exchange-rate line of a printout: 1 USD = 15,000 ل.س, each amount isolated.
func (w words) rate(rate, local string) string {
	return w.t("doc.rate", "usd", w.money("1", "USD"), "local", w.money(rate, local))
}

// labelDocument is a shelf label or price tag: the product's name, its price, and a Code 128 symbol of its barcode
// (the owner's request, 2026-09-20).
//
// It is built for the receipt printer the shop already owns, not for a dedicated label printer: a 40 mm or 80 mm roll
// cut short is what a pantry shop has. The symbol is drawn from the barcode the CATALOGUE holds, so what the label
// scans as is what the till looks up.
func (w words) labelDocument(p ProductDTO, copies int) documents.Document {
	var blocks []documents.Block
	for i := range copies {
		if i > 0 {
			blocks = append(blocks, documents.Rule{Dashed: true})
		}
		blocks = append(blocks,
			documents.Paragraph{Text: w.user(w.productName(p)), Center: true, Bold: true},
			documents.Paragraph{Text: documents.Money(p.Price, w.t("currency.short."+p.PriceCurrency)), Center: true, Bold: true})
		if p.ConvertedPrice != "" {
			blocks = append(blocks, documents.Paragraph{
				Text:  documents.Money(p.ConvertedPrice, w.t("currency.short."+p.ConvertedCurrency)),
				Small: true, Center: true})
		}
		// A product with no barcode still gets a label: the price is the point of it. The symbol is simply absent
		// rather than made up, because a made-up symbol scans as a product the shop does not have.
		if symbol, ok := documents.BarcodeOf(p.Barcode, p.Barcode, 3.5); ok {
			blocks = append(blocks, symbol)
		}
	}
	return documents.Document{Direction: w.dir, Blocks: blocks}
}

// productName is the product's name in the reader's language, falling back to the Arabic every product has.
func (w words) productName(p ProductDTO) string {
	if w.loc == "en" && p.NameEN != "" {
		return p.NameEN
	}
	return p.NameAR
}

// zReportDocument is the end-of-day statement on receipt paper (the owner's request, 2026-09-20).
//
// Built from the very DayReportDTO the Reports screen shows, so the paper and the screen cannot disagree (D-L7.6).
// A shop with no printer reads it on the screen; a shop with one can put it in the till drawer at close.
func (w words) zReportDocument(day DayReportDTO, shopName string) documents.Document {
	blocks := w.header(shopName, 1, w.t("zreport.title"))
	blocks = append(blocks, documents.Pairs{Rows: []documents.Pair{
		{Label: w.t("doc.date"), Value: documents.T(w.fig(day.Date))},
		{Label: w.t("zreport.printed"), Value: documents.T(w.now())},
	}}, documents.Rule{})

	local := func(a AmountDTO) documents.Cell { return documents.T(w.money(a.Local, day.LocalCurrency)) }

	blocks = append(blocks,
		documents.Heading{Text: w.t("zreport.sales")},
		documents.Pairs{Rows: []documents.Pair{
			{Label: w.t("reports.sales_count"), Value: documents.T(w.fig(strconv.Itoa(day.Profit.Sales)))},
			{Label: w.t("reports.revenue"), Value: documents.T(w.money(day.Profit.RevenueLocal, day.LocalCurrency))},
			{Label: w.t("reports.cost"), Value: documents.T(w.money(day.Profit.CostLocal, day.LocalCurrency))},
			{Label: w.t("reports.profit"), Value: documents.T(w.money(day.Profit.ProfitLocal, day.LocalCurrency)), Bold: true},
		}})

	// Returns, where any came back — a line that is absent on a day nothing was returned rather than a printed zero.
	if day.Returns.Count > 0 {
		blocks = append(blocks, documents.Rule{Dashed: true},
			documents.Heading{Text: w.t("zreport.returns")},
			documents.Pairs{Rows: []documents.Pair{
				{Label: w.t("zreport.returns_count"), Value: documents.T(w.fig(strconv.Itoa(day.Returns.Count)))},
				{Label: w.t("zreport.returns_refund"), Value: local(day.Returns.Refund)},
				{Label: w.t("zreport.returns_profit"), Value: local(day.Returns.Profit)},
			}})
	}

	rows := []documents.Pair{}
	if !isZeroFigure(day.DailyExpenses.Local) {
		rows = append(rows, documents.Pair{Label: w.t("zreport.expenses_daily"), Value: local(day.DailyExpenses)})
	}
	if !isZeroFigure(day.PeriodicExpenses.Local) {
		rows = append(rows, documents.Pair{Label: w.t("zreport.expenses_periodic"), Value: local(day.PeriodicExpenses)})
	}
	if len(rows) > 0 {
		rows = append(rows, documents.Pair{Label: w.t("reports.expenses"), Value: local(day.Expenses), Bold: true})
		blocks = append(blocks, documents.Rule{Dashed: true},
			documents.Heading{Text: w.t("zreport.expenses")}, documents.Pairs{Rows: rows})
	}

	if !isZeroFigure(day.Losses.Out.Local) || !isZeroFigure(day.BadDebts.Local) {
		blocks = append(blocks, documents.Rule{Dashed: true}, documents.Pairs{Rows: []documents.Pair{
			{Label: w.t("reports.losses"), Value: local(day.Losses.Out)},
			{Label: w.t("reports.bad_debts"), Value: local(day.BadDebts)},
		}})
	}

	blocks = append(blocks, documents.Rule{}, documents.Pairs{Rows: []documents.Pair{
		{Label: w.t("reports.net"), Value: documents.T(w.money(day.NetLocal, day.LocalCurrency)), Bold: true},
	}})

	// What is in the drawer, per currency, which is what the person closing up is actually counting against.
	if len(day.Takings) > 0 {
		takings := make([]documents.Pair, 0, len(day.Takings))
		for _, t := range day.Takings {
			takings = append(takings, documents.Pair{
				Label: w.t("currency." + t.Currency), Value: documents.T(w.money(t.Charged, t.Currency))})
		}
		blocks = append(blocks, documents.Rule{Dashed: true},
			documents.Heading{Text: w.t("zreport.takings")}, documents.Pairs{Rows: takings})
	}
	if day.Unconverted > 0 {
		blocks = append(blocks, documents.Paragraph{Text: w.t("reports.unconverted"), Small: true})
	}
	return documents.Document{Direction: w.dir, Blocks: append(blocks, w.footer()...)}
}
