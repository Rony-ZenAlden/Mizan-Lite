package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
)

// Reports is the owner's reports: a day, a month, products and stock (L6 §10.1). Every figure crosses as a string Go
// formatted, dollars first and pounds beside them; each report says which rate its pounds were converted at.
type Reports struct{ core *core }

// Cash is the cash drawer and the cash book (L6 §7). The drawer and the count are the counter's; money entries and
// reversals are the owner's.
type Cash struct{ core *core }

// AmountDTO is a figure in both readings. Unconverted counts figures missing from a reading for want of a rate.
type AmountDTO struct {
	USD         string `json:"usd"`
	Local       string `json:"local"`
	Unconverted int    `json:"unconverted"`
}

// ReturnsDTO is what came back over the counter: the refund handed out, the cost put back on the shelf, and the profit
// that actually cost the shop — the margin, not the refund (2026-09-20).
type ReturnsDTO struct {
	Count  int       `json:"count"`
	Refund AmountDTO `json:"refund"`
	Cost   AmountDTO `json:"cost"`
	// Profit is what the day's profit lost: the refund less the cost put back.
	Profit AmountDTO `json:"profit"`
	// Unknown counts returned lines whose cost was never recorded, so a screen says so rather than implying the shop
	// knows what the return cost it.
	Unknown int `json:"unknown"`
}

// ProfitDTO is revenue, cost and gross profit in dollars and in pounds at each sale's rate; lines of unknown cost apart.
type ProfitDTO struct {
	Sales         int    `json:"sales"`
	RevenueUSD    string `json:"revenueUsd"`
	CostUSD       string `json:"costUsd"`
	ProfitUSD     string `json:"profitUsd"`
	MarginUSD     string `json:"marginUsd"`
	RevenueLocal  string `json:"revenueLocal"`
	CostLocal     string `json:"costLocal"`
	ProfitLocal   string `json:"profitLocal"`
	MarginLocal   string `json:"marginLocal"`
	DiscountUSD   string `json:"discountUsd"`
	DiscountLocal string `json:"discountLocal"`
	RoundingLocal string `json:"roundingLocal"`
	UnknownLines  int    `json:"unknownLines"`
	UnknownUSD    string `json:"unknownUsd"`
	UnknownLocal  string `json:"unknownLocal"`
	// OpenLines, OpenUSD and OpenLocal are the open-priced items sold (2026-09-23): no cost by design, so like Unknown
	// outside revenue and margin — and apart from it, because they are not a cost anyone forgot to enter.
	OpenLines int    `json:"openLines"`
	OpenUSD   string `json:"openUsd"`
	OpenLocal string `json:"openLocal"`
}

// LossesDTO is stock that left without a sale, and what counts found, at cost.
type LossesDTO struct {
	Spoiled   AmountDTO `json:"spoiled"`
	OwnUse    AmountDTO `json:"ownUse"`
	Other     AmountDTO `json:"other"`
	Shortfall AmountDTO `json:"shortfall"`
	Surplus   AmountDTO `json:"surplus"`
	Out       AmountDTO `json:"out"`
}

// CategoryDTO is an expense category's total.
type CategoryDTO struct {
	Category string    `json:"category"`
	Amount   AmountDTO `json:"amount"`
}

// TakingsDTO is what the till did in one currency.
type TakingsDTO struct {
	Currency    string `json:"currency"`
	Sales       int    `json:"sales"`
	Charged     string `json:"charged"`
	CreditSales int    `json:"creditSales"`
	Credit      string `json:"credit"`
	Discounts   string `json:"discounts"`
	Rounding    string `json:"rounding"`
	Voids       int    `json:"voids"`
	Voided      string `json:"voided"`
	Collected   string `json:"collected"`
	Refunded    string `json:"refunded"`
	WrittenOff  string `json:"writtenOff"`
}

// DayReportDTO is a day's statement, or a range's totals.
type DayReportDTO struct {
	Date          string    `json:"date"`
	LocalCurrency string    `json:"localCurrency"`
	Profit        ProfitDTO `json:"profit"`
	Losses        LossesDTO `json:"losses"`
	BadDebts      AmountDTO `json:"badDebts"`
	// Returns is what came back over the counter this day (2026-09-20).
	Returns ReturnsDTO `json:"returns"`
	// Expenses is every expense of the day; DailyExpenses and PeriodicExpenses tell the day's small change apart from
	// rent and the bills, so a day the rent was paid does not read as a disaster.
	Expenses         AmountDTO     `json:"expenses"`
	DailyExpenses    AmountDTO     `json:"dailyExpenses"`
	PeriodicExpenses AmountDTO     `json:"periodicExpenses"`
	Categories       []CategoryDTO `json:"categories"`
	NetUSD           string        `json:"netUsd"`
	NetLocal         string        `json:"netLocal"`
	Unconverted      int           `json:"unconverted"`
	Takings          []TakingsDTO  `json:"takings"`
	// Rate is the rate of the day losses and bad debts were converted at; "" before any rate, and for a range.
	Rate string `json:"rate"`
}

// MonthReportDTO is a month: a row per day with anything in it, and the totals.
type MonthReportDTO struct {
	Month         string         `json:"month"`
	From          string         `json:"from"`
	To            string         `json:"to"`
	LocalCurrency string         `json:"localCurrency"`
	Days          []DayReportDTO `json:"days"`
	Total         DayReportDTO   `json:"total"`
}

// RangeInput is a range of business dates; empty is the month to date.
type RangeInput struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProductRowDTO is one product's sales over a range.
type ProductRowDTO struct {
	ProductID       string `json:"productId"`
	NameAR          string `json:"nameAr"`
	NameEN          string `json:"nameEn"`
	UnitCode        string `json:"unitCode"`
	Quantity        string `json:"quantity"`
	RevenueUSD      string `json:"revenueUsd"`
	CostUSD         string `json:"costUsd"`
	ProfitUSD       string `json:"profitUsd"`
	MarginUSD       string `json:"marginUsd"`
	RevenueLocal    string `json:"revenueLocal"`
	CostLocal       string `json:"costLocal"`
	ProfitLocal     string `json:"profitLocal"`
	MarginLocal     string `json:"marginLocal"`
	UnknownLines    int    `json:"unknownLines"`
	UnknownQuantity string `json:"unknownQuantity"`
	UnknownUSD      string `json:"unknownUsd"`
	UnknownLocal    string `json:"unknownLocal"`
	// OpenLines, OpenUSD and OpenLocal are an open-priced item's sales (2026-09-23): no cost by design, so outside
	// revenue and profit like Unknown, but said as what they are rather than as a cost the shop forgot to enter.
	OpenLines int    `json:"openLines"`
	OpenUSD   string `json:"openUsd"`
	OpenLocal string `json:"openLocal"`
}

// ProductsReportDTO is per-product figures and the reconciling row that makes them the range's revenue.
type ProductsReportDTO struct {
	From          string          `json:"from"`
	To            string          `json:"to"`
	LocalCurrency string          `json:"localCurrency"`
	Rows          []ProductRowDTO `json:"rows"`
	DiscountUSD   string          `json:"discountUsd"`
	DiscountLocal string          `json:"discountLocal"`
	RoundingLocal string          `json:"roundingLocal"`
	Total         ProfitDTO       `json:"total"`
}

// StockLineDTO is a product's stock on a date and its value.
type StockLineDTO struct {
	ProductID   string `json:"productId"`
	NameAR      string `json:"nameAr"`
	NameEN      string `json:"nameEn"`
	UnitCode    string `json:"unitCode"`
	OnHand      string `json:"onHand"`
	AverageCost string `json:"averageCost"`
	ValueUSD    string `json:"valueUsd"`
	ValueLocal  string `json:"valueLocal"`
}

// ReconciliationDTO is a period's movements at cost, from opening to closing value; NegativeStock and Rounding are the named
// differences.
type ReconciliationDTO struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Opening       string `json:"opening"`
	Received      string `json:"received"`
	Sold          string `json:"sold"`
	Losses        string `json:"losses"`
	Gains         string `json:"gains"`
	Revaluation   string `json:"revaluation"`
	Packages      string `json:"packages"`
	NegativeStock string `json:"negativeStock"`
	Rounding      string `json:"rounding"`
	Closing       string `json:"closing"`
}

// ShelfLineDTO is a product's expected profit at today's price.
type ShelfLineDTO struct {
	ProductID     string `json:"productId"`
	NameAR        string `json:"nameAr"`
	NameEN        string `json:"nameEn"`
	UnitCode      string `json:"unitCode"`
	OnHand        string `json:"onHand"`
	AverageCost   string `json:"averageCost"`
	Price         string `json:"price"`
	PriceCurrency string `json:"priceCurrency"`
	ProfitUSD     string `json:"profitUsd"`
	ProfitLocal   string `json:"profitLocal"`
	BelowCost     bool   `json:"belowCost"`
}

// LeftOutDTO is a product the shelf profit does not count, and why: no_stock, no_cost, inactive, no_rate.
type LeftOutDTO struct {
	ProductID string `json:"productId"`
	NameAR    string `json:"nameAr"`
	NameEN    string `json:"nameEn"`
	Reason    string `json:"reason"`
}

// StockReportDTO is the stock on the range's last date, the range's movements reconciled, and the profit on the shelf now.
type StockReportDTO struct {
	From            string            `json:"from"`
	To              string            `json:"to"`
	LocalCurrency   string            `json:"localCurrency"`
	Lines           []StockLineDTO    `json:"lines"`
	TotalUSD        string            `json:"totalUsd"`
	TotalLocal      string            `json:"totalLocal"`
	ValueRate       string            `json:"valueRate"`
	BelowZero       []StockLineDTO    `json:"belowZero"`
	UnknownCost     []StockLineDTO    `json:"unknownCost"`
	Reconciliation  ReconciliationDTO `json:"reconciliation"`
	Shelf           []ShelfLineDTO    `json:"shelf"`
	ShelfTotalUSD   string            `json:"shelfTotalUsd"`
	ShelfTotalLocal string            `json:"shelfTotalLocal"`
	// RetailTotalUSD and RetailTotalLocal value the same shelf at its SELLING prices, against TotalUSD/TotalLocal above
	// which value it at cost (the owner's request, 2026-09-17). Retail less the shelf profit is the cost.
	RetailTotalUSD   string       `json:"retailTotalUsd"`
	RetailTotalLocal string       `json:"retailTotalLocal"`
	ShelfRate        string       `json:"shelfRate"`
	BelowCost        int          `json:"belowCost"`
	LeftOut          []LeftOutDTO `json:"leftOut"`
}

// reportView formats report figures in the shop's currencies.
type reportView struct {
	tillView
	pair domain.Pair
}

func newReportView(ctx context.Context, app *bootstrap.App) (reportView, error) {
	v, err := newTillView(ctx, app)
	if err != nil {
		return reportView{}, err
	}
	pair, err := app.Reports.Pair(ctx)
	return reportView{tillView: v, pair: pair}, err
}

func (v reportView) usd(minor int64) string   { return v.money(minor, v.pair.USD.Code) }
func (v reportView) local(minor int64) string { return v.money(minor, v.pair.Local.Code) }

func (v reportView) amount(c domain.Converted) AmountDTO {
	return AmountDTO{USD: v.usd(c.USD), Local: v.local(c.Local), Unconverted: c.Unconverted}
}

func margin(profit, revenue int64) string {
	m, _ := domain.Margin(profit, revenue)
	return m
}

func (v reportView) profit(p domain.Profit) ProfitDTO {
	return ProfitDTO{
		Sales: p.Sales, RevenueUSD: v.usd(p.RevenueUSD), CostUSD: v.usd(p.CostUSD), ProfitUSD: v.usd(p.ProfitUSD()), MarginUSD: margin(p.ProfitUSD(), p.RevenueUSD),
		RevenueLocal: v.local(p.RevenueLocal), CostLocal: v.local(p.CostLocal), ProfitLocal: v.local(p.ProfitLocal()),
		MarginLocal: margin(p.ProfitLocal(), p.RevenueLocal), DiscountUSD: v.usd(p.DiscountUSD), DiscountLocal: v.local(p.DiscountLocal),
		RoundingLocal: v.local(p.RoundingLocal), UnknownLines: p.Unknown.Lines, UnknownUSD: v.usd(p.Unknown.NetUSDMinor), UnknownLocal: v.local(p.Unknown.NetLocalMinor),
		OpenLines: p.Open.Lines, OpenUSD: v.usd(p.Open.NetUSDMinor), OpenLocal: v.local(p.Open.NetLocalMinor),
	}
}

func (v reportView) day(d domain.Day, withRate bool) DayReportDTO {
	dto := DayReportDTO{
		Date: d.Date, LocalCurrency: v.pair.Local.Code, Profit: v.profit(d.Profit),
		Losses: LossesDTO{Spoiled: v.amount(d.Losses.Spoiled), OwnUse: v.amount(d.Losses.OwnUse), Other: v.amount(d.Losses.Other),
			Shortfall: v.amount(d.Losses.Shortfall), Surplus: v.amount(d.Losses.Surplus), Out: v.amount(d.Losses.Out())},
		BadDebts: v.amount(d.BadDebts),
		Returns: ReturnsDTO{Count: d.Returns.Count, Refund: v.amount(d.Returns.Refund), Cost: v.amount(d.Returns.Cost),
			Profit: v.amount(d.Returns.Profit()), Unknown: d.Returns.Unknown},
		Expenses: v.amount(d.Expenses.Total), DailyExpenses: v.amount(d.Expenses.Daily),
		PeriodicExpenses: v.amount(d.Expenses.Periodic), Categories: make([]CategoryDTO, 0, len(d.Categories)),
		NetUSD: v.usd(d.NetUSD()), NetLocal: v.local(d.NetLocal()), Unconverted: d.Unconverted(), Takings: make([]TakingsDTO, 0, len(d.Takings)),
	}
	if withRate && d.RateFound {
		dto.Rate = rateText(v.shop, d.Rate.Nano)
	}
	for _, c := range d.Categories {
		dto.Categories = append(dto.Categories, CategoryDTO{Category: c.Category, Amount: v.amount(c.Amount)})
	}
	for _, t := range d.Takings {
		// A dollars-only shop takes no pounds, and a column of pound noughts under "in the drawer" records nothing
		// (0.10.1). A day that did take pounds — the day the shop went over — still lists them: that is a record.
		if v.shop.USDOnly() && t.Currency != v.pair.USD.Code && idle(t) {
			continue
		}
		m := func(minor int64) string { return v.money(minor, t.Currency) }
		dto.Takings = append(dto.Takings, TakingsDTO{Currency: t.Currency, Sales: t.Sales, Charged: m(t.ChargedMinor), CreditSales: t.CreditSales,
			Credit: m(t.CreditMinor), Discounts: m(t.DiscountMinor), Rounding: m(t.RoundingMinor), Voids: t.Voids, Voided: m(t.VoidedMinor),
			Collected: m(t.CollectedMinor), Refunded: m(t.RefundedMinor), WrittenOff: m(t.WrittenOffMinor)})
	}
	return dto
}

// idle reports whether a currency's takings for the day are nothing at all.
func idle(t domain.Takings) bool {
	return t.Sales == 0 && t.ChargedMinor == 0 && t.CreditSales == 0 && t.CreditMinor == 0 && t.DiscountMinor == 0 &&
		t.RoundingMinor == 0 && t.Voids == 0 && t.VoidedMinor == 0 && t.CollectedMinor == 0 && t.RefundedMinor == 0 &&
		t.WrittenOffMinor == 0
}

// Day is a business day's statement ("" for today). Owner only.
func (r *Reports) Day(date string) envelope.Result[DayReportDTO] {
	return call(r.core, "Reports.Day", func(ctx context.Context, app *bootstrap.App) (DayReportDTO, error) {
		return dayReportDTO(ctx, app, date)
	})
}

// dayReportDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func dayReportDTO(ctx context.Context, app *bootstrap.App, date string) (DayReportDTO, error) {
	d, err := app.Reports.Day(ctx, date)
	if err != nil {
		return DayReportDTO{}, err
	}
	v, err := newReportView(ctx, app)
	return v.day(d, true), err
}

// Month is a calendar month ("" for this one), YYYY-MM. Owner only.
func (r *Reports) Month(month string) envelope.Result[MonthReportDTO] {
	return call(r.core, "Reports.Month", func(ctx context.Context, app *bootstrap.App) (MonthReportDTO, error) {
		return monthReportDTO(ctx, app, month)
	})
}

// monthReportDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func monthReportDTO(ctx context.Context, app *bootstrap.App, month string) (MonthReportDTO, error) {
	m, err := app.Reports.Month(ctx, month)
	if err != nil {
		return MonthReportDTO{}, err
	}
	v, err := newReportView(ctx, app)
	if err != nil {
		return MonthReportDTO{}, err
	}
	dto := MonthReportDTO{Month: m.Month, From: m.From, To: m.To, LocalCurrency: v.pair.Local.Code, Days: make([]DayReportDTO, 0, len(m.Days)), Total: v.day(m.Total, false)}
	for _, d := range m.Days {
		dto.Days = append(dto.Days, v.day(d, true))
	}
	return dto, nil
}

// Period is the profit of any span of days — a week, a year, the last thirty days, or a range the shop typed (the owner's
// request, 2026-09-17). It answers in the same shape as a month, because a month is one.
func (r *Reports) Period(in RangeInput) envelope.Result[MonthReportDTO] {
	return call(r.core, "Reports.Period", func(ctx context.Context, app *bootstrap.App) (MonthReportDTO, error) {
		m, err := app.Reports.Period(ctx, in.From, in.To)
		if err != nil {
			return MonthReportDTO{}, err
		}
		v, err := newReportView(ctx, app)
		if err != nil {
			return MonthReportDTO{}, err
		}
		dto := MonthReportDTO{Month: m.Month, From: m.From, To: m.To, LocalCurrency: v.pair.Local.Code,
			Days: make([]DayReportDTO, 0, len(m.Days)), Total: v.day(m.Total, false)}
		for _, d := range m.Days {
			dto.Days = append(dto.Days, v.day(d, true))
		}
		return dto, nil
	})
}

// Products is per-product profit over a range. Owner only.
func (r *Reports) Products(in RangeInput) envelope.Result[ProductsReportDTO] {
	return call(r.core, "Reports.Products", func(ctx context.Context, app *bootstrap.App) (ProductsReportDTO, error) {
		return productsReportDTO(ctx, app, in)
	})
}

// productsReportDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func productsReportDTO(ctx context.Context, app *bootstrap.App, in RangeInput) (ProductsReportDTO, error) {
	p, err := app.Reports.Products(ctx, in.From, in.To)
	if err != nil {
		return ProductsReportDTO{}, err
	}
	v, err := newReportView(ctx, app)
	if err != nil {
		return ProductsReportDTO{}, err
	}
	dto := ProductsReportDTO{From: p.From, To: p.To, LocalCurrency: v.pair.Local.Code, Rows: make([]ProductRowDTO, 0, len(p.Rows)),
		DiscountUSD: v.usd(p.DiscountUSD), DiscountLocal: v.local(p.DiscountLocal), RoundingLocal: v.local(p.RoundingLocal), Total: v.profit(p.Total)}
	for _, row := range p.Rows {
		dto.Rows = append(dto.Rows, ProductRowDTO{
			ProductID: row.ProductID.String(), NameAR: row.NameAR, NameEN: row.NameEN, UnitCode: row.UnitCode,
			Quantity: v.quantity(row.QuantityMicro, row.UnitCode), RevenueUSD: v.usd(row.RevenueUSD), CostUSD: v.usd(row.CostUSD),
			ProfitUSD: v.usd(row.ProfitUSD()), MarginUSD: margin(row.ProfitUSD(), row.RevenueUSD), RevenueLocal: v.local(row.RevenueLocal),
			CostLocal: v.local(row.CostLocal), ProfitLocal: v.local(row.ProfitLocal()), MarginLocal: margin(row.ProfitLocal(), row.RevenueLocal),
			UnknownLines: row.Unknown.Lines, UnknownQuantity: v.quantity(row.Unknown.QuantityMicro, row.UnitCode),
			UnknownUSD: v.usd(row.Unknown.NetUSDMinor), UnknownLocal: v.local(row.Unknown.NetLocalMinor),
			OpenLines: row.Open.Lines, OpenUSD: v.usd(row.Open.NetUSDMinor), OpenLocal: v.local(row.Open.NetLocalMinor),
		})
	}
	return dto, nil
}

func cost(micro int64) string { return numinput.FormatFixed(micro, 6, 2) }

func (v reportView) stockLine(l domain.StockLine) StockLineDTO {
	return StockLineDTO{ProductID: l.Product.ID.String(), NameAR: l.Product.NameAR, NameEN: l.Product.NameEN, UnitCode: l.Product.UnitCode,
		OnHand: v.quantity(l.OnHandMicro, l.Product.UnitCode), AverageCost: cost(l.AvgCostMicro), ValueUSD: v.usd(l.ValueUSD), ValueLocal: v.local(l.ValueLocal)}
}

func (v reportView) stockLines(lines []domain.StockLine) []StockLineDTO {
	out := make([]StockLineDTO, 0, len(lines))
	for _, l := range lines {
		out = append(out, v.stockLine(l))
	}
	return out
}

// Stock is the stock on the range's last date, the range's movements reconciled, and the shelf's expected profit. Owner only.
func (r *Reports) Stock(in RangeInput) envelope.Result[StockReportDTO] {
	return call(r.core, "Reports.Stock", func(ctx context.Context, app *bootstrap.App) (StockReportDTO, error) {
		return stockReportDTO(ctx, app, in)
	})
}

// stockReportDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func stockReportDTO(ctx context.Context, app *bootstrap.App, in RangeInput) (StockReportDTO, error) {
	s, err := app.Reports.Stock(ctx, in.From, in.To)
	if err != nil {
		return StockReportDTO{}, err
	}
	v, err := newReportView(ctx, app)
	if err != nil {
		return StockReportDTO{}, err
	}
	c := s.Reconciliation
	dto := StockReportDTO{
		From: c.From, To: c.To, LocalCurrency: v.pair.Local.Code, Lines: v.stockLines(s.Value.Lines), TotalUSD: v.usd(s.Value.TotalUSD),
		TotalLocal: v.local(s.Value.TotalLocal), BelowZero: v.stockLines(s.Value.BelowZero), UnknownCost: v.stockLines(s.Value.UnknownCost),
		Reconciliation: ReconciliationDTO{From: c.From, To: c.To, Opening: v.usd(c.Opening), Received: v.usd(c.Received), Sold: v.usd(c.Sold),
			Losses: v.usd(c.Losses), Gains: v.usd(c.Gains), Revaluation: v.usd(c.Revaluation), Packages: v.usd(c.Packages),
			NegativeStock: v.usd(c.NegativeStock), Rounding: v.usd(c.Rounding), Closing: v.usd(c.Closing)},
		Shelf: make([]ShelfLineDTO, 0, len(s.Shelf.Lines)), ShelfTotalUSD: v.usd(s.Shelf.TotalUSD), ShelfTotalLocal: v.local(s.Shelf.TotalLocal),
		RetailTotalUSD: v.usd(s.Shelf.RetailTotalUSD), RetailTotalLocal: v.local(s.Shelf.RetailTotalLocal),
		BelowCost: s.Shelf.BelowCost, LeftOut: make([]LeftOutDTO, 0, len(s.Shelf.LeftOut)),
	}
	if s.Value.RateFound {
		dto.ValueRate = rateText(v.shop, s.Value.Rate.Nano)
	}
	if s.Shelf.RateFound {
		dto.ShelfRate = rateText(v.shop, s.Shelf.Rate.Nano)
	}
	for _, l := range s.Shelf.Lines {
		p := l.Product
		dto.Shelf = append(dto.Shelf, ShelfLineDTO{ProductID: p.ID.String(), NameAR: p.NameAR, NameEN: p.NameEN, UnitCode: p.UnitCode,
			OnHand: v.quantity(l.OnHandMicro, p.UnitCode), AverageCost: cost(l.AvgCostMicro), Price: numinput.FormatFixed(p.PriceMicro, 6, v.ref.Currencies[p.PriceCurrency].Decimals),
			PriceCurrency: p.PriceCurrency, ProfitUSD: v.usd(l.ProfitUSD), ProfitLocal: v.local(l.ProfitLocal), BelowCost: l.BelowCost})
	}
	for _, l := range s.Shelf.LeftOut {
		dto.LeftOut = append(dto.LeftOut, LeftOutDTO{ProductID: l.Product.ID.String(), NameAR: l.Product.NameAR, NameEN: l.Product.NameEN, Reason: l.Reason})
	}
	return dto, nil
}

// CashEntryDTO is a line of the cash book.
type CashEntryDTO struct {
	ID           string `json:"id"`
	Seq          int64  `json:"seq"`
	BusinessDate string `json:"businessDate"`
	OccurredAt   string `json:"occurredAt"`
	Kind         string `json:"kind"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	// Expected and Difference are a count's; "" otherwise.
	Expected     string `json:"expected"`
	Difference   string `json:"difference"`
	Category     string `json:"category"`
	FromDrawer   bool   `json:"fromDrawer"`
	Note         string `json:"note"`
	ReversesKind string `json:"reversesKind"`
	Reversed     bool   `json:"reversed"`
	Reversible   bool   `json:"reversible"`
}

// DrawerCurrencyDTO is what should be in the drawer in one currency, how it was made up, and the day's count.
type DrawerCurrencyDTO struct {
	Currency         string `json:"currency"`
	Opening          string `json:"opening"`
	OpeningCountDate string `json:"openingCountDate"`
	CashSalesIn      string `json:"cashSalesIn"`
	CreditPaidIn     string `json:"creditPaidIn"`
	RepaymentsIn     string `json:"repaymentsIn"`
	DepositsIn       string `json:"depositsIn"`
	ChangeOut        string `json:"changeOut"`
	RefundsOut       string `json:"refundsOut"`
	VoidReturns      string `json:"voidReturns"`
	ExpensesOut      string `json:"expensesOut"`
	WithdrawalsOut   string `json:"withdrawalsOut"`
	// ReturnsOut is cash handed back over the counter for returned goods (2026-09-20). Until 0.10.0 it was in Expected and
	// on no line of its own, so a day with a cash return did not add up on the screen.
	ReturnsOut string `json:"returnsOut"`
	// SuppliersOut is money paid to suppliers out of the drawer, SuppliersIn their refunds into it (0.10.0).
	SuppliersOut  string `json:"suppliersOut"`
	SuppliersIn   string `json:"suppliersIn"`
	Expected      string `json:"expected"`
	Counted       bool   `json:"counted"`
	Count         string `json:"count"`
	CountExpected string `json:"countExpected"`
	Difference    string `json:"difference"`
	CountedAt     string `json:"countedAt"`
}

// DrawerDTO is a day's drawer. OwnerView is false at the counter: expenses are then shown with withdrawals as money taken
// out, and the cash book lists only counts.
type DrawerDTO struct {
	Date       string              `json:"date"`
	Today      string              `json:"today"`
	OwnerView  bool                `json:"ownerView"`
	Currencies []DrawerCurrencyDTO `json:"currencies"`
	Entries    []CashEntryDTO      `json:"entries"`
	Categories []string            `json:"categories"`
}

// CashRecordInput is an expense, a withdrawal or a deposit.
type CashRecordInput struct {
	Kind       string `json:"kind"`
	Currency   string `json:"currency"`
	Amount     string `json:"amount"`
	Category   string `json:"category"`
	FromDrawer bool   `json:"fromDrawer"`
	// Recurrence is "" or "once" for the day's small change, "monthly" for rent and the bills (2026-09-20).
	Recurrence string `json:"recurrence"`
	Note       string `json:"note"`
}

// CashCountInput is a closing count in one currency.
type CashCountInput struct {
	Currency string `json:"currency"`
	Counted  string `json:"counted"`
	Note     string `json:"note"`
}

// ReverseCashInput reverses a cash book entry.
type ReverseCashInput struct {
	EntryID string `json:"entryId"`
	Reason  string `json:"reason"`
}

func (v tillView) cashEntry(e domain.CashEntry, owner bool) CashEntryDTO {
	dto := CashEntryDTO{ID: e.ID.String(), Seq: e.Seq, BusinessDate: e.BusinessDate, OccurredAt: clock.Format(e.OccurredAt), Kind: e.Kind,
		Currency: e.Currency, Amount: v.money(e.AmountMinor, e.Currency), Category: e.Category, FromDrawer: e.FromDrawer, Note: e.Note,
		Reversed: e.Reversed, Reversible: owner && !e.Reversed && e.Kind != domain.CashReversal}
	if e.Kind == domain.CashCount {
		dto.Expected, dto.Difference = v.money(e.ExpectedMinor, e.Currency), v.money(e.AmountMinor-e.ExpectedMinor, e.Currency)
	}
	if e.Reverses != nil {
		dto.ReversesKind = e.Reverses.Kind
	}
	return dto
}

// Drawer is a day's drawer ("" for today). Anyone at the counter may see it.
func (c *Cash) Drawer(date string) envelope.Result[DrawerDTO] {
	return call(c.core, "Cash.Drawer", func(ctx context.Context, app *bootstrap.App) (DrawerDTO, error) {
		return drawerDTO(ctx, app, date)
	})
}

// drawerDTO builds the DTO the screen receives — the one exports and printouts are made from (L7 D-L7.6).
func drawerDTO(ctx context.Context, app *bootstrap.App, date string) (DrawerDTO, error) {
	d, err := app.Reports.Drawer(ctx, date)
	if err != nil {
		return DrawerDTO{}, err
	}
	v, err := newTillView(ctx, app)
	if err != nil {
		return DrawerDTO{}, err
	}
	dto := DrawerDTO{Date: d.Date, Today: app.Reports.Today(), OwnerView: d.OwnerView, Currencies: make([]DrawerCurrencyDTO, 0, len(d.Currencies)),
		Entries: make([]CashEntryDTO, 0, len(d.Entries)), Categories: cashbookdomain.Categories}
	for _, t := range d.Currencies {
		m := func(minor int64) string { return v.money(minor, t.Currency) }
		cur := DrawerCurrencyDTO{Currency: t.Currency, Opening: m(t.Opening), CashSalesIn: m(t.CashSalesIn), CreditPaidIn: m(t.CreditPaidIn),
			RepaymentsIn: m(t.RepaymentsIn), DepositsIn: m(t.DepositsIn), ChangeOut: m(t.ChangeOut), RefundsOut: m(t.RefundsOut),
			VoidReturns: m(t.VoidReturns), ExpensesOut: m(t.ExpensesOut), WithdrawalsOut: m(t.WithdrawalOut), ReturnsOut: m(t.ReturnsOut),
			SuppliersOut: m(t.SuppliersOut), SuppliersIn: m(t.SuppliersIn), Expected: m(t.Expected)}
		if t.OpeningCount != nil {
			cur.OpeningCountDate = t.OpeningCount.BusinessDate
		}
		if diff, counted := t.Difference(); counted {
			cur.Counted, cur.Count, cur.CountExpected, cur.Difference = true, m(t.Count.AmountMinor), m(t.Count.ExpectedMinor), m(diff)
			cur.CountedAt = clock.Format(t.Count.OccurredAt)
		}
		dto.Currencies = append(dto.Currencies, cur)
	}
	for _, e := range d.Entries {
		dto.Entries = append(dto.Entries, v.cashEntry(e, d.OwnerView))
	}
	return dto, nil
}

func (c *Cash) entryAct(method string, fn func(ctx context.Context, app *bootstrap.App) (cashbookdomain.Entry, error)) envelope.Result[CashEntryDTO] {
	return call(c.core, method, func(ctx context.Context, app *bootstrap.App) (CashEntryDTO, error) {
		e, err := fn(ctx, app)
		if err != nil {
			return CashEntryDTO{}, err
		}
		v, err := newTillView(ctx, app)
		fact := domain.CashEntry{ID: e.ID, Seq: e.Seq, BusinessDate: e.BusinessDate, OccurredAt: e.OccurredAt, Kind: string(e.Kind), Currency: e.Currency,
			AmountMinor: e.AmountMinor, ExpectedMinor: e.ExpectedMinor, Category: e.Category, FromDrawer: e.FromDrawer, Note: e.Note}
		return v.cashEntry(fact, false), err
	})
}

// Record writes an expense, a withdrawal or a deposit. Owner only.
func (c *Cash) Record(in CashRecordInput) envelope.Result[CashEntryDTO] {
	return c.entryAct("Cash.Record", func(ctx context.Context, app *bootstrap.App) (cashbookdomain.Entry, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return cashbookdomain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency); err != nil {
			return cashbookdomain.Entry{}, err
		}
		return app.Cashbook.Record(ctx, cashbook.RecordInput{Kind: cashbookdomain.Kind(in.Kind), Currency: in.Currency,
			Amount:   shop.Base(in.Amount, in.Currency),
			Category: in.Category, FromDrawer: in.FromDrawer, Recurrence: in.Recurrence, Note: in.Note})
	})
}

// Count records what the drawer holds in one currency. Anyone at the counter may.
func (c *Cash) Count(in CashCountInput) envelope.Result[CashEntryDTO] {
	return c.entryAct("Cash.Count", func(ctx context.Context, app *bootstrap.App) (cashbookdomain.Entry, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return cashbookdomain.Entry{}, err
		}
		if err = localCurrencyOff(shop, "currency", in.Currency); err != nil {
			return cashbookdomain.Entry{}, err
		}
		return app.Cashbook.Count(ctx, cashbook.CountInput{Currency: in.Currency, Counted: shop.Base(in.Counted, in.Currency), Note: in.Note})
	})
}

// Reverse undoes a cash book entry, with a reason. Owner only.
func (c *Cash) Reverse(in ReverseCashInput) envelope.Result[CashEntryDTO] {
	return c.entryAct("Cash.Reverse", func(ctx context.Context, app *bootstrap.App) (cashbookdomain.Entry, error) {
		entryID, err := id.Parse(in.EntryID)
		if err != nil {
			return cashbookdomain.Entry{}, cashbookdomain.ErrEntryNotFound()
		}
		return app.Cashbook.Reverse(ctx, entryID, in.Reason)
	})
}
