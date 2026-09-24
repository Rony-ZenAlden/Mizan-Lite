package domain

import (
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// The owner's spoilage screen (0.10.0, 2026-09-24): what the shop lost over a period — written off as damaged, expired
// or spoiled, given away or used, found short at a count — at what each piece cost, and the goods that arrived damaged
// from a supplier, which the shop never paid for.

// LossShortfall is the reason a line carries when a count found less than the books held.
const LossShortfall = "shortfall"

// LossReasons are the reasons a loss line may carry, in the order the report reads them: the three the spoilage screen
// records first.
var LossReasons = []string{ReasonDamaged, ReasonExpired, ReasonSpoiled, ReasonOwnUse, ReasonGift, ReasonOther, LossShortfall}

// LossLine is one piece of stock lost: what went, when, why, and what it cost — dollars at the movement's own cost, the
// pounds at the rate of its day.
type LossLine struct {
	MovementID    id.ID
	BusinessDate  string
	ProductID     id.ID
	NameAR        string
	NameEN        string
	UnitCode      string
	UnitDecimals  int
	Reason        string
	QuantityMicro int64
	Value         Converted
	Note          string
}

// LossTotal is one reason's losses over the period.
type LossTotal struct {
	Reason string
	Lines  int
	Value  Converted
}

// ArrivalDamage is goods that arrived damaged from a supplier (0.10.0). The supplier does not charge for them (the
// owner's answer, 2026-09-24), so they cost the shop nothing — they are here to be seen, not added to the losses.
type ArrivalDamage struct {
	BusinessDate string
	PurchaseNo   int64
	SupplierName string
	ProductID    id.ID
	NameAR       string
	NameEN       string
	UnitCode     string
	DamagedMicro int64
	Currency     string
	// ValueMinor is what the damaged units would have cost at the purchase's unit price, in its currency.
	ValueMinor int64
}

// LossReport is a period's losses, newest first, and each reason's total.
type LossReport struct {
	From, To string
	Lines    []LossLine
	ByReason []LossTotal
	Total    Converted
	Arrival  []ArrivalDamage
}

// LossesBetween reads a period's losses out of its stock movements: every adjustment that took stock out, and every count
// that found less. An adjustment that put stock in, and a count that found more, are not losses and are not here — the
// day's statement counts them as a surplus.
func LossesBetween(movements []Movement, products map[id.ID]Product, rates []Rate, pair Pair, from, to string, arrival []ArrivalDamage) LossReport {
	r := LossReport{From: from, To: to, Arrival: arrival}
	totals := map[string]*LossTotal{}
	for _, m := range movements {
		if m.BusinessDate < from || m.BusinessDate > to || m.QuantityMicro >= 0 {
			continue
		}
		var reason string
		switch m.Kind {
		case MoveAdjustment:
			reason = m.Reason
		case MoveCount:
			reason = LossShortfall
		default:
			continue
		}
		rate, found := RateOfDay(rates, m.BusinessDate)
		line := LossLine{MovementID: m.ID, BusinessDate: m.BusinessDate, ProductID: m.ProductID, Reason: reason,
			QuantityMicro: -m.QuantityMicro, Note: m.Note}
		line.Value = rowValue(line.QuantityMicro, m.UnitCostMicro, rate.Nano, found, pair)
		if p, ok := products[m.ProductID]; ok {
			line.NameAR, line.NameEN, line.UnitCode, line.UnitDecimals = p.NameAR, p.NameEN, p.UnitCode, p.UnitDecimals
		}
		r.Lines = append(r.Lines, line)
		t := totals[reason]
		if t == nil {
			t = &LossTotal{Reason: reason}
			totals[reason] = t
		}
		t.Lines++
		t.Value.Add(line.Value)
		r.Total.Add(line.Value)
	}
	// Newest day first, as a book is read from the top; one day's lines as the stock book lists them.
	sort.SliceStable(r.Lines, func(i, j int) bool { return r.Lines[i].BusinessDate > r.Lines[j].BusinessDate })
	for _, reason := range LossReasons {
		if t := totals[reason]; t != nil {
			r.ByReason = append(r.ByReason, *t)
		}
	}
	sort.SliceStable(r.Arrival, func(i, j int) bool { return r.Arrival[i].BusinessDate > r.Arrival[j].BusinessDate })
	return r
}
