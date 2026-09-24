package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/tafqeet"
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

// logoLines is how tall the logo stands at the head of a document, in body lines.
const logoLines = 4

// header is the shop's lines at the top of every receipt and voucher: its logo when it has one, its name, and where to
// find it.
func (w words) header(shop string, copyNo int, subtitle string) []documents.Block {
	var blocks []documents.Block
	if w.logo != nil {
		blocks = append(blocks, documents.Image{Picture: w.logo, MaxLines: logoLines})
	}
	blocks = append(blocks, documents.Title{Text: w.user(shop), Subtitle: subtitle, Center: true})
	blocks = append(blocks, w.whereToFind(true)...)
	if copyNo > 1 {
		blocks = append(blocks, documents.Stamp{Text: w.t("doc.copy", "number", w.fig(strconv.Itoa(copyNo)))})
	}
	return append(blocks, documents.Rule{Dashed: true})
}

// whereToFind is the shop's city, address and phone, each a small line — set once under Settings > Store information.
func (w words) whereToFind(center bool) []documents.Block {
	var blocks []documents.Block
	r := w.settings.Receipt
	var place []string
	for _, part := range []string{r.City, r.Address} {
		if part != "" {
			place = append(place, w.user(part))
		}
	}
	if len(place) > 0 {
		blocks = append(blocks, documents.Paragraph{Text: strings.Join(place, " · "), Small: true, Center: center})
	}
	if r.Phone != "" {
		blocks = append(blocks, documents.Paragraph{Text: w.t("doc.phone", "phone", w.fig(r.Phone)), Small: true, Center: center})
	}
	return blocks
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
	if sale.Cartons != "" {
		pairs = append(pairs, documents.Pair{Label: w.t("invoice.cartons"), Value: documents.F(sale.Cartons)})
	}
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
	blocks = append(blocks, documents.Pairs{Rows: pairs})
	if said := w.inWords(sale.Total, settle); said != "" {
		blocks = append(blocks, documents.Paragraph{Text: said, Small: true, Center: true})
	}
	blocks = append(blocks, documents.Rule{Dashed: true})
	blocks = append(blocks, w.rateLine(sale.Rate, sale.LocalCurrency, true)...)
	if credit {
		blocks = append(blocks, documents.Signature{Label: w.t("doc.customer_signature")})
	}
	return documents.Document{Direction: w.dir, Blocks: append(blocks, w.footer()...)}
}

// inWords is an amount as the invoice states it — "فقط … لا غير" — in the figure the shop reads first: the new pound of
// a dual reading. "" for a currency or figure that cannot be written, which a document then leaves out rather than
// print words that do not say the figure.
func (w words) inWords(amount, currency string) string {
	fresh, _, _ := moneyfmt.SplitDual(amount)
	c, ok := tafqeet.ForCode(currency)
	if !ok || fresh == "" {
		return ""
	}
	write := tafqeet.Arabic
	if w.english() {
		write = tafqeet.English
	}
	said, err := write(fresh, c)
	if err != nil {
		return ""
	}
	return said
}

// invoiceParty is who an invoice is made out to: a credit sale's customer, or nobody for a cash sale.
type invoiceParty struct{ Name, Phone, City string }

// invoicePartyOf reads the customer of a credit sale — the name as the sale recorded it, the phone and city as they are
// now, since those are how to reach them.
func invoicePartyOf(ctx context.Context, app *bootstrap.App, sale SaleDTO) (invoiceParty, error) {
	if sale.CreditCustomerID == "" {
		return invoiceParty{}, nil
	}
	customerID, err := parseCustomerID(sale.CreditCustomerID)
	if err != nil {
		return invoiceParty{}, err
	}
	c, err := app.Customers.Customer(ctx, customerID)
	if err != nil {
		return invoiceParty{}, err
	}
	return invoiceParty{Name: sale.CreditCustomerName, Phone: c.Phone, City: c.City}, nil
}

// invoiceColumns are the model invoice's columns, start edge first (2026-09-24): the line's total, the item, how many,
// the unit, the cartons, a note, the price. Widths are relative.
var invoiceColumns = []int{14, 28, 9, 9, 7, 12, 15}

// invoiceDocument is a sale's A4 invoice, laid out as the owner's model invoice (2026-09-24): the shop at the head —
// its logo, or its name in type — the customer and the date, a ruled table of the lines, and under it the cartons, the
// total and the amount in words beside the net.
func (w words) invoiceDocument(sale SaleDTO, party invoiceParty) documents.Document {
	settle := sale.Settlement
	var blocks []documents.Block
	if w.logo != nil {
		blocks = append(blocks, documents.Image{Picture: w.logo, MaxLines: logoLines + 1})
	}
	blocks = append(blocks, documents.Title{Text: w.user(w.settings.ShopName), Subtitle: w.t("invoice.kind"), Center: true})
	blocks = append(blocks, w.whereToFind(true)...)
	if sale.Status == string(salesdomain.StatusVoided) {
		blocks = append(blocks, documents.Stamp{Text: w.t("doc.voided")})
	}

	customer := w.t("invoice.cash_customer")
	if party.Name != "" {
		customer = w.user(party.Name)
	}
	start := []documents.Field{
		{Label: w.t("invoice.customer"), Value: documents.T(customer)},
		{Label: w.t("invoice.number"), Value: documents.F(strconv.FormatInt(sale.ReceiptNo, 10))},
	}
	if party.City != "" {
		start = append(start, documents.Field{Label: w.t("invoice.city"), Value: documents.T(w.user(party.City))})
	}
	end := []documents.Field{{Label: w.t("invoice.date"), Value: documents.F(w.dayOf(sale.SoldAt))}}
	if party.Phone != "" {
		end = append(end, documents.Field{Label: w.t("invoice.phone"), Value: documents.F(party.Phone)})
	}
	blocks = append(blocks, documents.Fields{Start: start, End: end})

	figure := func(s string) documents.Cell { return documents.F(documents.Group(s)) }
	rows := make([][]documents.Cell, 0, len(sale.Lines))
	for _, l := range sale.Lines {
		net := l.NetLocal
		if settle == tender.USD {
			net = l.NetUSD
		}
		price := figure(l.UnitPrice)
		if l.PriceCurrency != settle {
			// A dollar price on a pound invoice says it is dollars; the line's total is already in pounds.
			price = documents.M(w.money(l.UnitPrice, l.PriceCurrency))
		}
		note := ""
		if l.DiscountPercent != "" {
			note = w.t("receipt.line_discount", "percent", w.fig(l.DiscountPercent))
		}
		rows = append(rows, []documents.Cell{figure(net), documents.T(w.name(l.NameAR, l.NameEN)), figure(l.Quantity),
			{Text: w.unit(l.UnitCode), Center: true}, {Text: w.fig(l.Cartons), Center: true}, documents.T(note), price})
	}

	gross, discount := sale.LinesLocal, sale.DiscountLocal
	if settle == tender.USD {
		gross, discount = sale.LinesUSD, sale.DiscountUSD
	}
	var footer [][]documents.Cell
	if sale.Cartons != "" {
		footer = append(footer, []documents.Cell{{Text: w.t("invoice.cartons"), Span: 4}, {Text: w.fig(sale.Cartons), Center: true}, {Span: 2}})
	}
	footer = append(footer, []documents.Cell{{Text: w.t("invoice.gross"), Span: 5}, {Text: w.money(gross, settle), End: true, Span: 2}})
	if !isZeroFigure(discount) {
		footer = append(footer, []documents.Cell{{Text: w.t("receipt.discount"), Span: 5}, {Text: w.money(discount, settle), End: true, Span: 2}})
	}
	if !isZeroFigure(sale.Rounding) {
		footer = append(footer, []documents.Cell{{Text: w.t("receipt.rounding", "note", w.fig(documents.Group(sale.CashNote))), Span: 5},
			{Text: w.money(sale.Rounding, settle), End: true, Span: 2}})
	}
	footer = append(footer, []documents.Cell{{Text: w.inWords(sale.Total, settle), Span: 5}, {Text: w.t("invoice.net"), Center: true},
		{Text: w.money(sale.Total, settle), End: true}})
	blocks = append(blocks, documents.Table{Grid: true, Widths: invoiceColumns, Rows: rows, Footer: footer,
		Headings: []string{w.t("invoice.col.total"), w.t("invoice.col.item"), w.t("invoice.col.quantity"), w.t("invoice.col.unit"),
			w.t("invoice.col.cartons"), w.t("invoice.col.notes"), w.t("invoice.col.price")}})

	if sale.Payment == string(salesdomain.PaymentCredit) && sale.CreditCustomerID != "" {
		blocks = append(blocks, documents.Pairs{Rows: []documents.Pair{
			{Label: w.t("receipt.paid_now"), Value: documents.T(w.money(sale.Tendered, sale.TenderCurrency))},
			{Label: w.t("receipt.debt_added"), Value: documents.T(w.money(sale.CreditAmount, sale.CreditCurrency))},
			{Label: w.t("receipt.balance_after"), Value: documents.T(w.money(sale.CreditBalanceAfter, sale.CreditCurrency))},
		}}, documents.Signature{Label: w.t("doc.customer_signature")})
	}
	blocks = append(blocks, w.rateLine(sale.Rate, sale.LocalCurrency, false)...)
	blocks = append(blocks, w.footer()...)
	return documents.Document{Direction: w.dir, Blocks: blocks, Footer: "-{page}-",
		Meta: documents.Meta{Title: w.t("invoice.title", "number", strconv.FormatInt(sale.ReceiptNo, 10)), Author: w.settings.ShopName,
			Created: w.app.Now().UTC().Format("D:20060102150405")}}
}

// dayOf is a stored timestamp's day in the shop's time — an invoice is dated, not timed.
func (w words) dayOf(stamp string) string {
	at, ok := clock.ParseTimestamp(stamp)
	if !ok {
		return stamp
	}
	return at.In(w.tz).Format("02/01/2006")
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
	blocks = append(blocks, documents.Pairs{Rows: pairs}, documents.Rule{Dashed: true})
	blocks = append(blocks, w.rateLine(fxdomain.FormatRate(e.Cash.RateNano), v.local, true)...)
	blocks = append(blocks, documents.Signature{Label: w.t("doc.receiver_signature")})
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

// rateLine is a printout's exchange-rate line — none in a dollars-only shop, which prints no rate (0.10.0).
func (w words) rateLine(rate, local string, center bool) []documents.Block {
	if moneyfmt.Parse(w.settings.MoneyDisplay) == moneyfmt.USD {
		return nil
	}
	return []documents.Block{documents.Paragraph{Text: w.rate(rate, local), Small: true, Center: center}}
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
