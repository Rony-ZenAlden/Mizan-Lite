package domain

import (
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Unknown is what was sold with no recorded cost: shown apart, in neither revenue, cost nor margin (Q-L6.2).
type Unknown struct {
	Lines         int
	QuantityMicro int64
	NetUSDMinor   int64
	NetLocalMinor int64
}

func (u *Unknown) add(o Unknown, sign int64) {
	u.Lines += int(sign) * o.Lines
	u.QuantityMicro += sign * o.QuantityMicro
	u.NetUSDMinor += sign * o.NetUSDMinor
	u.NetLocalMinor += sign * o.NetLocalMinor
}

// Open is what was sold as an open-priced item — the carrier bag, the bunch of parsley — at a price typed at the till
// (2026-09-23). Like Unknown it is in neither revenue, cost nor margin, because it has no cost to set against it; unlike
// Unknown it is not a gap for the shop to fill, so it is counted apart. Lumping the two together would make every misc
// sale read as a product whose cost the owner forgot to enter.
type Open = Unknown

// Profit is revenue, cost and gross profit in dollars and in pounds at each sale's own rate (L6 §3.1).
type Profit struct {
	// Sales is the sales rung up less the sales voided.
	Sales        int
	RevenueUSD   int64
	CostUSD      int64
	RevenueLocal int64
	CostLocal    int64
	// DiscountUSD and DiscountLocal are the whole-sale discounts, already taken off revenue.
	DiscountUSD   int64
	DiscountLocal int64
	// RoundingLocal is the cash rounding of pounds sales, already in RevenueLocal and never in dollars.
	RoundingLocal int64
	Unknown       Unknown
	// Open is the open-priced items sold, apart from both revenue and Unknown.
	Open Open
}

// ProfitUSD is revenue less cost in dollars.
func (p Profit) ProfitUSD() int64 { return p.RevenueUSD - p.CostUSD }

// ProfitLocal is revenue less cost in pounds.
func (p Profit) ProfitLocal() int64 { return p.RevenueLocal - p.CostLocal }

// Add adds o, or takes it away with sign −1.
func (p *Profit) Add(o Profit, sign int64) {
	p.Sales += int(sign) * o.Sales
	p.RevenueUSD += sign * o.RevenueUSD
	p.CostUSD += sign * o.CostUSD
	p.RevenueLocal += sign * o.RevenueLocal
	p.CostLocal += sign * o.CostLocal
	p.DiscountUSD += sign * o.DiscountUSD
	p.DiscountLocal += sign * o.DiscountLocal
	p.RoundingLocal += sign * o.RoundingLocal
	p.Unknown.add(o.Unknown, sign)
	p.Open.add(o.Open, sign)
}

// SaleProfit is one sale's figures, read from what it stored and never re-priced (D-L6.1). A line of unknown cost is
// left out; the whole-sale discount and the rounding are the sale's and stay in (L6 §3.2, H6).
func SaleProfit(s Sale, pair Pair) Profit {
	p := Profit{Sales: 1, DiscountUSD: s.DiscountUSDMinor, DiscountLocal: s.DiscountLocalMinor}
	if s.SettlementCurrency == pair.Local.Code {
		p.RoundingLocal = s.RoundingMinor
	}
	for _, l := range s.Lines {
		if l.OpenPrice {
			p.Open.add(Open{Lines: 1, QuantityMicro: l.QuantityMicro, NetUSDMinor: l.NetUSDMinor, NetLocalMinor: l.NetLocalMinor}, 1)
			continue
		}
		if !l.CostKnown {
			p.Unknown.add(Unknown{Lines: 1, QuantityMicro: l.QuantityMicro, NetUSDMinor: l.NetUSDMinor, NetLocalMinor: l.NetLocalMinor}, 1)
			continue
		}
		p.RevenueUSD += l.NetUSDMinor
		p.RevenueLocal += l.NetLocalMinor
		p.CostUSD += l.CostUSDMinor
		p.CostLocal += l.CostLocalMinor
	}
	p.RevenueUSD -= p.DiscountUSD
	p.RevenueLocal += p.RoundingLocal - p.DiscountLocal
	return p
}

// ProfitOn is a business day's gross profit: the sales rung up that day less the sales voided that day (D-L6.3).
func ProfitOn(sales []Sale, date string, pair Pair) Profit {
	var p Profit
	for _, s := range sales {
		if s.BusinessDate == date {
			p.Add(SaleProfit(s, pair), 1)
		}
		if s.Voided && s.VoidBusinessDate == date {
			p.Add(SaleProfit(s, pair), -1)
		}
	}
	return p
}

// ProductRow is one product's sales over a range, from line nets, voided lines taken back on their void's day.
type ProductRow struct {
	ProductID     id.ID
	NameAR        string
	NameEN        string
	UnitCode      string
	UnitDecimals  int
	QuantityMicro int64
	RevenueUSD    int64
	CostUSD       int64
	RevenueLocal  int64
	CostLocal     int64
	Unknown       Unknown
	// Open is this product's sales when it is an open-priced item; such a product has nothing but Open.
	Open Open
}

// ProfitUSD is the product's profit in dollars.
func (r ProductRow) ProfitUSD() int64 { return r.RevenueUSD - r.CostUSD }

// ProfitLocal is the product's profit in pounds.
func (r ProductRow) ProfitLocal() int64 { return r.RevenueLocal - r.CostLocal }

// Products is per-product figures over a range, and the one reconciling row that makes them add up to the range's
// revenue exactly (D-L6.4).
type Products struct {
	From, To string
	Rows     []ProductRow
	// DiscountUSD and DiscountLocal are the whole-sale discounts, taken off; RoundingLocal the pounds cash rounding.
	DiscountUSD   int64
	DiscountLocal int64
	RoundingLocal int64
	Total         Profit
}

// ProductsOver builds the per-product report, ordered by dollar profit, largest first.
func ProductsOver(sales []Sale, from, to string, pair Pair, products map[id.ID]Product) Products {
	out := Products{From: from, To: to}
	rows := map[id.ID]*ProductRow{}
	take := func(s Sale, sign int64) {
		p := SaleProfit(s, pair)
		out.Total.Add(p, sign)
		out.DiscountUSD += sign * p.DiscountUSD
		out.DiscountLocal += sign * p.DiscountLocal
		out.RoundingLocal += sign * p.RoundingLocal
		for _, l := range s.Lines {
			r, ok := rows[l.ProductID]
			if !ok {
				r = &ProductRow{ProductID: l.ProductID, NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode}
				if known, found := products[l.ProductID]; found {
					r.NameAR, r.NameEN, r.UnitCode, r.UnitDecimals = known.NameAR, known.NameEN, known.UnitCode, known.UnitDecimals
				}
				rows[l.ProductID] = r
			}
			if l.OpenPrice {
				r.Open.add(Open{Lines: 1, QuantityMicro: l.QuantityMicro, NetUSDMinor: l.NetUSDMinor, NetLocalMinor: l.NetLocalMinor}, sign)
				continue
			}
			if !l.CostKnown {
				r.Unknown.add(Unknown{Lines: 1, QuantityMicro: l.QuantityMicro, NetUSDMinor: l.NetUSDMinor, NetLocalMinor: l.NetLocalMinor}, sign)
				continue
			}
			r.QuantityMicro += sign * l.QuantityMicro
			r.RevenueUSD += sign * l.NetUSDMinor
			r.RevenueLocal += sign * l.NetLocalMinor
			r.CostUSD += sign * l.CostUSDMinor
			r.CostLocal += sign * l.CostLocalMinor
		}
	}
	for _, s := range sales {
		if s.BusinessDate >= from && s.BusinessDate <= to {
			take(s, 1)
		}
		if s.Voided && s.VoidBusinessDate >= from && s.VoidBusinessDate <= to {
			take(s, -1)
		}
	}
	for _, r := range rows {
		out.Rows = append(out.Rows, *r)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.ProfitUSD() != b.ProfitUSD() {
			return a.ProfitUSD() > b.ProfitUSD()
		}
		return a.ProductID < b.ProductID
	})
	return out
}
