package domain

import (
	"math/big"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Verifier finding codes. They double as i18n keys.
const (
	FindingLinesDoNotAdd    = "lite.sales.verify.lines_do_not_add"
	FindingRoundingWrong    = "lite.sales.verify.rounding_wrong"
	FindingChangeWrong      = "lite.sales.verify.change_wrong"
	FindingStockMissing     = "lite.sales.verify.stock_missing"
	FindingStockWithoutSale = "lite.sales.verify.stock_without_sale"
	FindingReceiptNumberGap = "lite.sales.verify.receipt_number_gap"
	// A credit sale's charge in the debt book (L5 §9.2).
	FindingCreditWithoutCharge = "lite.sales.verify.credit_without_charge"
	FindingChargeWrong         = "lite.sales.verify.charge_wrong"
	FindingChargeWithoutCredit = "lite.sales.verify.charge_without_credit"
)

// Finding is one thing the sales verifier found wrong. A report, never a repair (D-L4.15).
type Finding struct {
	Code      string
	SaleID    id.ID
	ReceiptNo int64
}

// StockMovement is what the verifier needs of a sale's stock movement, supplied by the stock module.
type StockMovement struct {
	Kind          string // "sale" or "sale_void"
	ProductID     id.ID
	QuantityMicro int64 // signed
	SaleID        id.ID
	SaleLineID    id.ID
}

// Verify checks every sale against itself, the stock that moved with it (L4 §8.3) and — for a credit sale — its charge
// in the debt book (L5 §9.2). sales carry their lines; local and usd are the two currencies' decimals.
func Verify(sales []Sale, movements []StockMovement, charges []Charge, local, usd Currency) []Finding {
	var out []Finding
	find := func(code string, s Sale) {
		out = append(out, Finding{Code: code, SaleID: s.ID, ReceiptNo: s.ReceiptNo})
	}

	byLine := map[id.ID]map[string]StockMovement{}
	for _, m := range movements {
		if byLine[m.SaleLineID] == nil {
			byLine[m.SaleLineID] = map[string]StockMovement{}
		}
		byLine[m.SaleLineID][m.Kind] = m
	}
	knownLines := map[id.ID]Sale{}
	chargesOf := map[id.ID][]Charge{}
	for _, ch := range charges {
		chargesOf[ch.SaleID] = append(chargesOf[ch.SaleID], ch)
	}
	knownSales := map[id.ID]Sale{}

	sorted := append([]Sale(nil), sales...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ReceiptNo < sorted[j].ReceiptNo })
	for i, s := range sorted {
		knownSales[s.ID] = s
		if s.ReceiptNo != int64(i+1) {
			find(FindingReceiptNumberGap, s)
		}
		var local64, usd64 int64
		for _, l := range s.Lines {
			knownLines[l.ID] = s
			local64 += l.NetLocalMinor()
			usd64 += l.NetUSDMinor()
			sale, sold := byLine[l.ID]["sale"]
			void, voided := byLine[l.ID]["sale_void"]
			// An open-priced line takes nothing off a shelf: the stock record matches the sale when there is NONE.
			if l.OpenPrice {
				if sold || voided {
					find(FindingStockMissing, s)
				}
				continue
			}
			if !sold || sale.ProductID != l.ProductID || sale.QuantityMicro != -l.QuantityMicro || sale.SaleID != s.ID ||
				voided != (s.Status == StatusVoided) || (voided && (void.QuantityMicro != l.QuantityMicro || void.SaleID != s.ID)) {
				find(FindingStockMissing, s)
			}
		}
		if local64 != s.LinesLocalMinor || usd64 != s.LinesUSDMinor {
			find(FindingLinesDoNotAdd, s)
		}

		settleUSD := s.SettlementCurrency == usd.Code
		before := s.LinesLocalMinor - s.DiscountLocalMinor
		if settleUSD {
			before = s.LinesUSDMinor - s.DiscountUSDMinor
		}
		if settleUSD && s.RoundingMinor != 0 || !settleUSD && s.TotalMinor != roundToNote(big.NewRat(before, 1), s.CashNoteMinor) ||
			s.TotalMinor != before+s.RoundingMinor {
			find(FindingRoundingWrong, s)
		}

		c := Context{Local: local, USD: usd, Rate: Rate{Nano: s.RateNano}}
		cur := func(code string) Currency {
			if code == usd.Code {
				return usd
			}
			return local
		}
		if s.Payment == PaymentCredit {
			// Change is never given on credit; exactly one charge, in the settled currency, of the total less what was paid
			// now, reversed if and only if the sale is voided.
			mine := chargesOf[s.ID]
			switch {
			case len(mine) == 0:
				find(FindingCreditWithoutCharge, s)
			case len(mine) > 1 || s.ChangeMinor != 0 || mine[0].Currency != s.SettlementCurrency ||
				mine[0].AmountMinor != DebtOf(s.TotalMinor, cur(s.SettlementCurrency), cur(s.TenderedCurrency), s.TenderedMinor, s.RateNano, s.CashNoteMinor) ||
				mine[0].AmountMinor <= 0 || mine[0].Reversed != (s.Status == StatusVoided):
				find(FindingChargeWrong, s)
			}
			continue
		}
		if len(chargesOf[s.ID]) > 0 {
			find(FindingChargeWithoutCredit, s)
		}
		exact := new(big.Rat).Sub(inMinor(s.TenderedMinor, cur(s.TenderedCurrency), cur(s.ChangeCurrency), c),
			inMinor(s.TotalMinor, cur(s.SettlementCurrency), cur(s.ChangeCurrency), c))
		note := int64(1)
		if s.ChangeCurrency != usd.Code {
			note = s.CashNoteMinor
		}
		// The change was rounded once from the exact change: to the nearest note (or cent), so never by more than half.
		diff := new(big.Rat).Sub(new(big.Rat).SetInt64(s.ChangeMinor), exact)
		if exact.Sign() < 0 || new(big.Rat).Abs(diff).Cmp(big.NewRat(note, 2)) > 0 {
			find(FindingChangeWrong, s)
		}
	}
	for saleID, mine := range chargesOf {
		if _, ok := knownSales[saleID]; !ok {
			out = append(out, Finding{Code: FindingChargeWithoutCredit, SaleID: mine[0].SaleID})
		}
	}
	for lineID, kinds := range byLine {
		if _, ok := knownLines[lineID]; !ok {
			for _, m := range kinds {
				out = append(out, Finding{Code: FindingStockWithoutSale, SaleID: m.SaleID})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ReceiptNo != out[j].ReceiptNo {
			return out[i].ReceiptNo < out[j].ReceiptNo
		}
		return out[i].Code < out[j].Code
	})
	return out
}
