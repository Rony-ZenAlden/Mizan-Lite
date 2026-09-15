package api

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// Sales is the day's sales, their receipts and voids (L4 §10.3).
//
// A sale carries no cost: cost is the owner's (Q-L2.4) and reaches a screen with profit in L6. TestASaleCarriesNoCost
// holds these DTOs so.
type Sales struct{ core *core }

// SaleLineDTO is one line of a receipt.
type SaleLineDTO struct {
	ID              string `json:"id"`
	LineNo          int    `json:"lineNo"`
	ProductID       string `json:"productId"`
	NameAR          string `json:"nameAr"`
	NameEN          string `json:"nameEn"`
	UnitCode        string `json:"unitCode"`
	Quantity        string `json:"quantity"`
	PriceCurrency   string `json:"priceCurrency"`
	UnitPrice       string `json:"unitPrice"`
	GrossLocal      string `json:"grossLocal"`
	GrossUSD        string `json:"grossUsd"`
	DiscountPercent string `json:"discountPercent"`
	DiscountLocal   string `json:"discountLocal"`
	DiscountUSD     string `json:"discountUsd"`
	NetLocal        string `json:"netLocal"`
	NetUSD          string `json:"netUsd"`
}

// SaleDTO is a sale as recorded — its receipt.
type SaleDTO struct {
	ID               string        `json:"id"`
	ReceiptNo        int64         `json:"receiptNo"`
	BusinessDate     string        `json:"businessDate"`
	SoldAt           string        `json:"soldAt"`
	Status           string        `json:"status"`
	Payment          string        `json:"payment"`
	ShopName         string        `json:"shopName"`
	LocalCurrency    string        `json:"localCurrency"`
	Rate             string        `json:"rate"`
	RateRecordedAt   string        `json:"rateRecordedAt"`
	Settlement       string        `json:"settlement"`
	LinesLocal       string        `json:"linesLocal"`
	LinesUSD         string        `json:"linesUsd"`
	DiscountLocal    string        `json:"discountLocal"`
	DiscountUSD      string        `json:"discountUsd"`
	CashNote         string        `json:"cashNote"`
	Rounding         string        `json:"rounding"`
	Total            string        `json:"total"`
	TenderCurrency   string        `json:"tenderCurrency"`
	Tendered         string        `json:"tendered"`
	ChangeCurrency   string        `json:"changeCurrency"`
	Change           string        `json:"change"`
	VoidedAt         string        `json:"voidedAt"`
	VoidBusinessDate string        `json:"voidBusinessDate"`
	VoidReason       string        `json:"voidReason"`
	Lines            []SaleLineDTO `json:"lines"`
	// A credit sale's charge, read from the debt book (L5 §5.5): flat fields, all "" for a cash sale.
	CreditCustomerID   string `json:"creditCustomerId"`
	CreditCustomerName string `json:"creditCustomerName"`
	CreditCurrency     string `json:"creditCurrency"`
	CreditAmount       string `json:"creditAmount"`
	CreditBalanceAfter string `json:"creditBalanceAfter"`
	CreditReversed     bool   `json:"creditReversed"`
	// VoidReturn is the cash a void of this sale hands back, in VoidReturnCurrency (L6 §7.2): shown before the PIN.
	VoidReturn         string `json:"voidReturn"`
	VoidReturnCurrency string `json:"voidReturnCurrency"`
}

// DayTotalsDTO is one currency's side of a day.
type DayTotalsDTO struct {
	Currency  string `json:"currency"`
	Sales     int    `json:"sales"`
	Charged   string `json:"charged"`
	CashIn    string `json:"cashIn"`
	ChangeOut string `json:"changeOut"`
	Voids     int    `json:"voids"`
	// Voided is the value the day's voids took back — a sales figure, not the cash handed back (L6 A-L6.6).
	Voided string `json:"voided"`
	// OnCredit is what the day's credit sales added to debts in this currency (L5 §5.7).
	OnCredit string `json:"onCredit"`
}

// DayDTO is the sales of a business date — those sold and those voided on it — and its totals, local currency first.
type DayDTO struct {
	BusinessDate string         `json:"businessDate"`
	Sales        []SaleDTO      `json:"sales"`
	Totals       []DayTotalsDTO `json:"totals"`
}

// VoidInput voids a whole sale.
type VoidInput struct {
	SaleID string `json:"saleId"`
	Reason string `json:"reason"`
}

// SaleFindingDTO is one thing the sales verifier found.
type SaleFindingDTO struct {
	Code      string `json:"code"`
	SaleID    string `json:"saleId"`
	ReceiptNo int64  `json:"receiptNo"`
}

func (v tillView) line(l salesdomain.Line, local string) SaleLineDTO {
	usd := salesdomain.USD
	dto := SaleLineDTO{
		ID: l.ID.String(), LineNo: l.LineNo, ProductID: l.ProductID.String(), NameAR: l.NameAR, NameEN: l.NameEN,
		UnitCode: l.UnitCode, Quantity: v.quantity(l.QuantityMicro, l.UnitCode), PriceCurrency: l.PriceCurrency,
		UnitPrice:  v.unitPrice(l.UnitPriceMicro, l.PriceCurrency),
		GrossLocal: v.money(l.GrossLocalMinor, local), GrossUSD: v.money(l.GrossUSDMinor, usd),
		DiscountPercent: percentText(l.DiscountPercentMicro),
		DiscountLocal:   v.money(l.DiscountLocalMinor, local), DiscountUSD: v.money(l.DiscountUSDMinor, usd),
		NetLocal: v.money(l.NetLocalMinor(), local), NetUSD: v.money(l.NetUSDMinor(), usd),
	}
	return dto
}

// unitPrice formats a 10⁻⁶ price with at least its currency's decimals.
func (v tillView) unitPrice(micro int64, code string) string {
	return numinput.FormatFixed(micro, 6, v.ref.Currencies[code].Decimals)
}

func (v tillView) sale(s salesdomain.Sale) SaleDTO {
	local, usd := s.LocalCurrency, salesdomain.USD
	dto := SaleDTO{
		ID: s.ID.String(), ReceiptNo: s.ReceiptNo, BusinessDate: s.BusinessDate, SoldAt: clock.Format(s.SoldAt),
		Status: string(s.Status), Payment: string(s.Payment), ShopName: s.ShopName, LocalCurrency: local,
		Rate: fxdomain.FormatRate(s.RateNano), RateRecordedAt: clock.Format(s.RateRecordedAt), Settlement: s.SettlementCurrency,
		LinesLocal: v.money(s.LinesLocalMinor, local), LinesUSD: v.money(s.LinesUSDMinor, usd),
		DiscountLocal: v.money(s.DiscountLocalMinor, local), DiscountUSD: v.money(s.DiscountUSDMinor, usd),
		Rounding: v.money(s.RoundingMinor, s.SettlementCurrency), Total: v.money(s.TotalMinor, s.SettlementCurrency),
		TenderCurrency: s.TenderedCurrency, Tendered: v.money(s.TenderedMinor, s.TenderedCurrency),
		ChangeCurrency: s.ChangeCurrency, Change: v.money(s.ChangeMinor, s.ChangeCurrency),
		VoidBusinessDate: s.VoidBusinessDate, VoidReason: s.VoidReason,
		Lines: make([]SaleLineDTO, 0, len(s.Lines)),
	}
	if s.SettlementCurrency != usd {
		dto.CashNote = v.money(s.CashNoteMinor, local)
	}
	if !s.VoidedAt.IsZero() {
		dto.VoidedAt = clock.Format(s.VoidedAt)
	}
	returnCurrency, returnMinor := s.VoidReturn()
	dto.VoidReturnCurrency, dto.VoidReturn = returnCurrency, v.money(returnMinor, returnCurrency)
	if s.Payment == salesdomain.PaymentCredit && s.Credit.CustomerID != "" {
		c := s.Credit
		dto.CreditCustomerID, dto.CreditCustomerName, dto.CreditCurrency = c.CustomerID.String(), c.CustomerName, c.Currency
		dto.CreditAmount, dto.CreditBalanceAfter, dto.CreditReversed = v.money(c.AmountMinor, c.Currency), v.money(c.BalanceAfterMinor, c.Currency), c.Reversed
	}
	for _, l := range s.Lines {
		dto.Lines = append(dto.Lines, v.line(l, local))
	}
	return dto
}

// List is the sales of a business date (YYYY-MM-DD), today when "".
func (s *Sales) List(businessDate string) envelope.Result[DayDTO] {
	return call(s.core, "Sales.List", func(ctx context.Context, app *bootstrap.App) (DayDTO, error) {
		day, err := app.Sales.Day(ctx, businessDate)
		if err != nil {
			return DayDTO{}, err
		}
		v, err := newTillView(ctx, app)
		if err != nil {
			return DayDTO{}, err
		}
		out := DayDTO{BusinessDate: day.BusinessDate, Sales: make([]SaleDTO, 0, len(day.Sales)), Totals: make([]DayTotalsDTO, 0, len(day.Totals))}
		for _, sale := range day.Sales {
			out.Sales = append(out.Sales, v.sale(sale))
		}
		for code, t := range day.Totals {
			out.Totals = append(out.Totals, DayTotalsDTO{
				Currency: code, Sales: t.Sales, Charged: v.money(t.ChargedMinor, code), CashIn: v.money(t.CashInMinor, code),
				ChangeOut: v.money(t.ChangeOutMinor, code), Voids: t.Voids, Voided: v.money(t.VoidedMinor, code),
				OnCredit: v.money(t.OnCreditMinor, code),
			})
		}
		// Dollars last, whatever the local currency is called.
		sort.Slice(out.Totals, func(i, j int) bool {
			ui, uj := out.Totals[i].Currency == salesdomain.USD, out.Totals[j].Currency == salesdomain.USD
			if ui != uj {
				return uj
			}
			return out.Totals[i].Currency < out.Totals[j].Currency
		})
		return out, nil
	})
}

// Receipt is one sale as recorded.
func (s *Sales) Receipt(saleID string) envelope.Result[SaleDTO] {
	return call(s.core, "Sales.Receipt", func(ctx context.Context, app *bootstrap.App) (SaleDTO, error) {
		parsed, err := id.Parse(saleID)
		if err != nil {
			return SaleDTO{}, salesdomain.ErrNotFound()
		}
		sale, err := app.Sales.Receipt(ctx, parsed)
		if err != nil {
			return SaleDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.sale(sale), err
	})
}

// Void voids a whole sale and returns its stock. Owner only; the reason is required (Q-L4.4).
func (s *Sales) Void(in VoidInput) envelope.Result[SaleDTO] {
	return call(s.core, "Sales.Void", func(ctx context.Context, app *bootstrap.App) (SaleDTO, error) {
		parsed, err := id.Parse(in.SaleID)
		if err != nil {
			return SaleDTO{}, salesdomain.ErrNotFound()
		}
		sale, err := app.Sales.Void(ctx, sales.VoidInput{SaleID: parsed, Reason: in.Reason})
		if err != nil {
			return SaleDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.sale(sale), err
	})
}

// Verify checks every sale against itself and its stock. Outside owner mode it returns lite.owner.required.
func (s *Sales) Verify() envelope.Result[[]SaleFindingDTO] {
	return call(s.core, "Sales.Verify", func(ctx context.Context, app *bootstrap.App) ([]SaleFindingDTO, error) {
		findings, err := app.Sales.Verify(ctx)
		out := make([]SaleFindingDTO, 0, len(findings))
		for _, f := range findings {
			out = append(out, SaleFindingDTO{Code: f.Code, SaleID: f.SaleID.String(), ReceiptNo: f.ReceiptNo})
		}
		return out, err
	})
}
