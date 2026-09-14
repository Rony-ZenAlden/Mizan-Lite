package domain

import (
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stamp is what a checkout supplies that a quote does not have: ids, the receipt number, the moment, the shop's name.
type Stamp struct {
	SaleID       id.ID
	LineIDs      []id.ID
	ReceiptNo    int64
	BusinessDate string
	SoldAt       time.Time
	ShopName     string
	Payment      Payment
}

// Assemble turns a quote into the sale checkout records — the receipt, fully snapshotted (L4 §7.2).
func Assemble(q Quote, local Currency, st Stamp) Sale {
	s := Sale{
		ID: st.SaleID, ReceiptNo: st.ReceiptNo, BusinessDate: st.BusinessDate, SoldAt: st.SoldAt, Status: StatusPosted,
		Payment: st.Payment, LocalCurrency: local.Code, RateID: q.Rate.ID, RateNano: q.Rate.Nano, RateRecordedAt: q.Rate.RecordedAt,
		SettlementCurrency: q.Settlement.Code, LinesLocalMinor: q.LinesLocalMinor, LinesUSDMinor: q.LinesUSDMinor,
		DiscountLocalMinor: q.DiscountLocalMinor, DiscountUSDMinor: q.DiscountUSDMinor, CashNoteMinor: q.CashNoteMinor,
		RoundingMinor: q.RoundingMinor, TotalMinor: q.TotalMinor, TenderedCurrency: q.Tender.Code, TenderedMinor: q.TenderedMinor,
		ChangeCurrency: q.Change.Code, ChangeMinor: q.ChangeMinor, CostUSDMinor: q.CostUSDMinor, ShopName: st.ShopName, RowVersion: 1,
	}
	for i, pl := range q.Lines {
		line := pl.Line
		line.ID = st.LineIDs[i]
		s.Lines = append(s.Lines, line)
	}
	return s
}
