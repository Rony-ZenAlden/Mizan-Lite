package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// LossLineDTO is one piece of stock lost: what, when, why — damaged, expired, spoiled, own_use, gift, other, or shortfall
// for a count that found less — how much, and what it cost (0.10.0).
type LossLineDTO struct {
	MovementID   string    `json:"movementId"`
	BusinessDate string    `json:"businessDate"`
	ProductID    string    `json:"productId"`
	NameAR       string    `json:"nameAr"`
	NameEN       string    `json:"nameEn"`
	UnitCode     string    `json:"unitCode"`
	Reason       string    `json:"reason"`
	Quantity     string    `json:"quantity"`
	Value        AmountDTO `json:"value"`
	Note         string    `json:"note"`
}

// LossTotalDTO is one reason's losses over the period.
type LossTotalDTO struct {
	Reason string    `json:"reason"`
	Lines  int       `json:"lines"`
	Value  AmountDTO `json:"value"`
}

// ArrivalDamageDTO is goods that arrived damaged from a supplier, which the supplier did not charge for.
type ArrivalDamageDTO struct {
	BusinessDate string `json:"businessDate"`
	PurchaseNo   int64  `json:"purchaseNo"`
	SupplierName string `json:"supplierName"`
	ProductID    string `json:"productId"`
	NameAR       string `json:"nameAr"`
	NameEN       string `json:"nameEn"`
	UnitCode     string `json:"unitCode"`
	Damaged      string `json:"damaged"`
	Currency     string `json:"currency"`
	Value        string `json:"value"`
}

// LossReportDTO is a period's losses at cost — dollars at each movement's own cost, pounds at the rate of its day — and
// the goods that arrived damaged.
type LossReportDTO struct {
	From          string             `json:"from"`
	To            string             `json:"to"`
	LocalCurrency string             `json:"localCurrency"`
	Lines         []LossLineDTO      `json:"lines"`
	ByReason      []LossTotalDTO     `json:"byReason"`
	Total         AmountDTO          `json:"total"`
	Arrival       []ArrivalDamageDTO `json:"arrival"`
}

// Losses is a period's stock lost, at cost — the month to date when empty. Owner only: it returns lite.owner.required
// outside owner mode while the PIN switch is on.
func (r *Reports) Losses(in RangeInput) envelope.Result[LossReportDTO] {
	return call(r.core, "Reports.Losses", func(ctx context.Context, app *bootstrap.App) (LossReportDTO, error) {
		return lossReportDTO(ctx, app, in)
	})
}

// lossReportDTO builds the DTO the screen receives.
func lossReportDTO(ctx context.Context, app *bootstrap.App, in RangeInput) (LossReportDTO, error) {
	report, err := app.Reports.Losses(ctx, in.From, in.To)
	if err != nil {
		return LossReportDTO{}, err
	}
	v, err := newReportView(ctx, app)
	if err != nil {
		return LossReportDTO{}, err
	}
	return v.losses(report), nil
}

func (v reportView) losses(report domain.LossReport) LossReportDTO {
	dto := LossReportDTO{From: report.From, To: report.To, LocalCurrency: v.pair.Local.Code, Lines: make([]LossLineDTO, 0, len(report.Lines)),
		ByReason: make([]LossTotalDTO, 0, len(report.ByReason)), Total: v.amount(report.Total),
		Arrival: make([]ArrivalDamageDTO, 0, len(report.Arrival))}
	for _, l := range report.Lines {
		dto.Lines = append(dto.Lines, LossLineDTO{MovementID: l.MovementID.String(), BusinessDate: l.BusinessDate,
			ProductID: l.ProductID.String(), NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode, Reason: l.Reason,
			Quantity: numinput.FormatFixed(l.QuantityMicro, 6, l.UnitDecimals), Value: v.amount(l.Value), Note: l.Note})
	}
	for _, t := range report.ByReason {
		dto.ByReason = append(dto.ByReason, LossTotalDTO{Reason: t.Reason, Lines: t.Lines, Value: v.amount(t.Value)})
	}
	if v.shop.USDOnly() {
		// A dollars-only shop reads its losses in dollars (0.10.1). Without the local currency the screen has no pound
		// column to draw, and the pound figures are not sent at all.
		dto.LocalCurrency = ""
		dto.Total.Local = ""
		for i := range dto.Lines {
			dto.Lines[i].Value.Local = ""
		}
		for i := range dto.ByReason {
			dto.ByReason[i].Value.Local = ""
		}
	}
	for _, a := range report.Arrival {
		dto.Arrival = append(dto.Arrival, ArrivalDamageDTO{BusinessDate: a.BusinessDate, PurchaseNo: a.PurchaseNo,
			SupplierName: a.SupplierName, ProductID: a.ProductID.String(), NameAR: a.NameAR, NameEN: a.NameEN, UnitCode: a.UnitCode,
			Damaged: v.quantity(a.DamagedMicro, a.UnitCode), Currency: a.Currency, Value: v.money(a.ValueMinor, a.Currency)})
	}
	return dto
}
