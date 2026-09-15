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
