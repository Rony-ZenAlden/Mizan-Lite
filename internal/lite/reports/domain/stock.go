package domain

import (
	"math/big"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// StockLine is one product's stock on a date and what it was worth.
type StockLine struct {
	Product      Product
	OnHandMicro  int64
	AvgCostMicro int64
	ValueUSD     int64
	ValueLocal   int64
	LocalUnrated bool
}

// StockValue is the stock on a business date: each product's last ledger row on or before it (D-L6.9).
type StockValue struct {
	Date       string
	Lines      []StockLine
	TotalUSD   int64
	TotalLocal int64
	// BelowZero lists products sold beyond their stock: they count as no value.
	BelowZero []StockLine
	// UnknownCost lists products on hand with no cost recorded.
	UnknownCost []StockLine
	Rate        Rate
	RateFound   bool
}

func productOf(products map[id.ID]Product, productID id.ID) Product {
	if p, ok := products[productID]; ok {
		return p
	}
	return Product{ID: productID}
}

// ValueOn values the stock from each product's last row on or before a date; the pounds reading at the rate of that
// day, rounded once per product. Negative stock has no value (L6 §6.2).
func ValueOn(last []Movement, date string, rates []Rate, pair Pair, products map[id.ID]Product) StockValue {
	v := StockValue{Date: date}
	v.Rate, v.RateFound = RateOfDay(rates, date)
	for _, m := range last {
		line := StockLine{Product: productOf(products, m.ProductID), OnHandMicro: m.OnHandAfterMicro, AvgCostMicro: m.AvgCostAfterMicro}
		switch {
		case m.OnHandAfterMicro < 0:
			v.BelowZero = append(v.BelowZero, line)
			continue
		case m.OnHandAfterMicro == 0:
			continue
		case m.AvgCostAfterMicro == 0:
			v.UnknownCost = append(v.UnknownCost, line)
			continue
		}
		c := rowValue(m.OnHandAfterMicro, m.AvgCostAfterMicro, v.Rate.Nano, v.RateFound, pair)
		line.ValueUSD, line.ValueLocal, line.LocalUnrated = c.USD, c.Local, c.Unconverted > 0
		v.TotalUSD += c.USD
		v.TotalLocal += c.Local
		v.Lines = append(v.Lines, line)
	}
	sort.Slice(v.Lines, func(i, j int) bool {
		if v.Lines[i].ValueUSD != v.Lines[j].ValueUSD {
			return v.Lines[i].ValueUSD > v.Lines[j].ValueUSD
		}
		return v.Lines[i].Product.ID < v.Lines[j].Product.ID
	})
	return v
}

// Reconciliation is a period's stock movements at cost in dollars, from the value at its start to the value at its end
// (D-L6.10). Every figure is signed as it moves the value, so:
//
//	Closing = Opening + Received + Sold + Losses + Gains + Revaluation + Packages + NegativeStock + Rounding
//
// Each class is the exact change in value of its rows — on hand × average after, less before — summed and rounded once,
// so the classes telescope to the exact change. What remains is named: NegativeStock, the value that stock below zero
// does not have, and Rounding, bounded by half a cent per rounded figure.
type Reconciliation struct {
	From, To      string
	Opening       int64
	Received      int64
	Sold          int64
	Losses        int64
	Gains         int64
	Revaluation   int64
	Packages      int64
	NegativeStock int64
	Rounding      int64
	Closing       int64
	// Rounded is how many rounded figures Rounding is bounded by: |Rounding| ≤ Rounded × ½ cent.
	Rounded int
}

// exactValue is Σ on hand × average over the products, in dollars at 10⁻¹², and the part below zero (its negation).
func exactValue(last []Movement) (total, negative *big.Int) {
	total, negative = new(big.Int), new(big.Int)
	for _, m := range last {
		v := mul(m.OnHandAfterMicro, m.AvgCostAfterMicro)
		total.Add(total, v)
		if m.OnHandAfterMicro < 0 {
			negative.Sub(negative, v)
		}
	}
	return total, negative
}

// Reconcile reconciles a period from the last rows before it, the rows in it and the last rows at its end.
func Reconcile(from, to string, before, rows, end []Movement, pair Pair) Reconciliation {
	r := Reconciliation{From: from, To: to}
	cents := func(v *big.Int) int64 { return roundDiv(new(big.Int).Mul(v, pow10(pair.USD.Decimals)), pow10(12)) }
	displayed := func(last []Movement) int64 {
		var sum int64
		for _, m := range last {
			if m.OnHandAfterMicro > 0 {
				sum += cents(mul(m.OnHandAfterMicro, m.AvgCostAfterMicro))
				r.Rounded++
			}
		}
		return sum
	}
	r.Opening, r.Closing = displayed(before), displayed(end)

	classes := map[*int64]*big.Int{}
	class := func(dst *int64) *big.Int {
		if classes[dst] == nil {
			classes[dst] = new(big.Int)
		}
		return classes[dst]
	}
	for _, m := range rows {
		delta := new(big.Int).Sub(mul(m.OnHandAfterMicro, m.AvgCostAfterMicro), mul(m.OnHandBeforeMicro, m.AvgCostBeforeMicro))
		var dst *int64
		switch m.Kind {
		case MoveOpening, MoveReceipt, MoveReceiptReversal:
			dst = &r.Received
		case MoveSale, MoveSaleVoid:
			dst = &r.Sold
		case MoveAdjustment, MoveCount:
			dst = &r.Losses
			if m.QuantityMicro > 0 {
				dst = &r.Gains
			}
		case MoveCostCorrection:
			dst = &r.Revaluation
		default: // package_out, content_in
			dst = &r.Packages
		}
		class(dst).Add(class(dst), delta)
	}
	for dst, v := range classes {
		*dst = cents(v)
		r.Rounded++
	}
	_, negBefore := exactValue(before)
	_, negEnd := exactValue(end)
	r.NegativeStock = cents(new(big.Int).Sub(negEnd, negBefore))
	r.Rounded++
	r.Rounding = r.Closing - r.Opening - r.Received - r.Sold - r.Losses - r.Gains - r.Revaluation - r.Packages - r.NegativeStock
	return r
}

// ShelfLine is the profit a product's stock would earn at today's price (Q3).
type ShelfLine struct {
	Product      Product
	OnHandMicro  int64
	AvgCostMicro int64
	ProfitUSD    int64
	ProfitLocal  int64
	// BelowCost is true when the price is under the average cost: the line is counted, and named.
	BelowCost bool
}

// Left-out reasons.
const (
	LeftNoStock  = "no_stock"
	LeftNoCost   = "no_cost"
	LeftInactive = "inactive"
	LeftNoRate   = "no_rate"
)

// LeftOut is a product the shelf profit does not count, and why.
type LeftOut struct {
	Product Product
	Reason  string
}

// Shelf is the expected profit on the shelf: on hand × (price in dollars − average cost) for every active product with
// stock and a cost, the pounds price at the rate in force (D-L6.11).
type Shelf struct {
	Lines      []ShelfLine
	TotalUSD   int64
	TotalLocal int64
	BelowCost  int
	LeftOut    []LeftOut
	Rate       Rate
	RateFound  bool
}

// ShelfProfit computes the shelf from each product's newest row.
func ShelfProfit(last []Movement, products []Product, rates []Rate, pair Pair) Shelf {
	s := Shelf{}
	s.Rate, s.RateFound = InForce(rates)
	levels := map[id.ID]Movement{}
	for _, m := range last {
		levels[m.ProductID] = m
	}
	for _, p := range products {
		m, moved := levels[p.ID]
		switch {
		case !p.Active:
			s.LeftOut = append(s.LeftOut, LeftOut{Product: p, Reason: LeftInactive})
			continue
		case !moved || m.OnHandAfterMicro <= 0:
			s.LeftOut = append(s.LeftOut, LeftOut{Product: p, Reason: LeftNoStock})
			continue
		case m.AvgCostAfterMicro == 0:
			s.LeftOut = append(s.LeftOut, LeftOut{Product: p, Reason: LeftNoCost})
			continue
		case p.PriceCurrency != pair.USD.Code && !s.RateFound:
			s.LeftOut = append(s.LeftOut, LeftOut{Product: p, Reason: LeftNoRate})
			continue
		}
		// Exact margin per unit, in dollars at 10⁻⁶ × the rate's 10⁹ scale for a pounds price: (price − cost).
		var perUnit, den *big.Int
		if p.PriceCurrency == pair.USD.Code {
			perUnit, den = big.NewInt(p.PriceMicro-m.AvgCostAfterMicro), big.NewInt(1)
		} else {
			perUnit = new(big.Int).Sub(mul(p.PriceMicro, 1_000_000_000), mul(m.AvgCostAfterMicro, s.Rate.Nano))
			den = big.NewInt(s.Rate.Nano)
		}
		exact := new(big.Int).Mul(perUnit, big.NewInt(m.OnHandAfterMicro)) // dollars at 10⁻¹² × den
		line := ShelfLine{Product: p, OnHandMicro: m.OnHandAfterMicro, AvgCostMicro: m.AvgCostAfterMicro, BelowCost: perUnit.Sign() < 0}
		line.ProfitUSD = roundDiv(new(big.Int).Mul(exact, pow10(pair.USD.Decimals)), new(big.Int).Mul(den, pow10(12)))
		if s.RateFound {
			line.ProfitLocal = roundDiv(new(big.Int).Mul(new(big.Int).Mul(exact, big.NewInt(s.Rate.Nano)), pow10(pair.Local.Decimals)),
				new(big.Int).Mul(den, pow10(21)))
		}
		if line.BelowCost {
			s.BelowCost++
		}
		s.TotalUSD += line.ProfitUSD
		s.TotalLocal += line.ProfitLocal
		s.Lines = append(s.Lines, line)
	}
	sort.Slice(s.Lines, func(i, j int) bool {
		if s.Lines[i].ProfitUSD != s.Lines[j].ProfitUSD {
			return s.Lines[i].ProfitUSD > s.Lines[j].ProfitUSD
		}
		return s.Lines[i].Product.ID < s.Lines[j].Product.ID
	})
	return s
}
