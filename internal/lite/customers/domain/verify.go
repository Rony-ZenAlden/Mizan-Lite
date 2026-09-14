package domain

import (
	"math/big"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// Stable finding codes. They double as i18n keys.
const (
	FindingChainBroken = "lite.customers.verify.chain_broken"
	FindingBadReversal = "lite.customers.verify.bad_reversal"
	FindingCashWrong   = "lite.customers.verify.cash_wrong"
)

// Finding is one thing the debt book's verifier found wrong. A report, never a repair (D-L2.14).
type Finding struct {
	Code       string
	CustomerID id.ID
	Currency   string
	EntryID    id.ID
}

// Verify checks the debt book against itself (L5 §9.1): every chain continuous from zero, every reversal the opposite of
// what it names, every payment and refund worth what it settled at its rate within the roundings it was allowed.
func Verify(entries []Entry, currencies []Currency) []Finding {
	var out []Finding
	cur := map[string]Currency{}
	for _, c := range currencies {
		cur[c.Code] = c
	}
	byID := map[id.ID]Entry{}
	type chainKey struct {
		customer id.ID
		currency string
	}
	chains := map[chainKey][]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
		k := chainKey{e.CustomerID, e.Currency}
		chains[k] = append(chains[k], e)
	}
	find := func(code string, e Entry) {
		out = append(out, Finding{Code: code, CustomerID: e.CustomerID, Currency: e.Currency, EntryID: e.ID})
	}

	for _, chain := range chains {
		sort.Slice(chain, func(i, j int) bool { return chain[i].Seq < chain[j].Seq })
		var before int64
		for i, e := range chain {
			if e.Seq != int64(i+1) || e.BalanceBeforeMinor != before || e.BalanceAfterMinor != e.BalanceBeforeMinor+e.AmountMinor {
				find(FindingChainBroken, e)
			}
			before = e.BalanceAfterMinor
		}
	}

	for _, e := range entries {
		switch e.Kind {
		case KindReversal:
			target, ok := byID[e.ReversesID]
			if !ok || target.Kind == KindReversal || target.CustomerID != e.CustomerID || target.Currency != e.Currency ||
				target.AmountMinor != -e.AmountMinor {
				find(FindingBadReversal, e)
			}
		case KindPayment, KindRefund:
			if !cashAddsUp(e, cur) {
				find(FindingCashWrong, e)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CustomerID != out[j].CustomerID {
			return out[i].CustomerID < out[j].CustomerID
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// cashAddsUp holds a payment's or refund's cash to what it settled: the tender less the change, in the debt's currency,
// within half the note (or cent) each of them was rounded to, and half the debt's minor unit.
func cashAddsUp(e Entry, cur map[string]Currency) bool {
	debt, okD := cur[e.Currency]
	tendered, okT := cur[e.Cash.TenderedCurrency]
	change, okC := cur[e.Cash.ChangeCurrency]
	if !okD || !okT || !okC || e.Cash.RateNano <= 0 {
		return false
	}
	rate := e.Cash.RateNano
	net := new(big.Rat).Sub(tender.Convert(e.Cash.TenderedMinor, tc(tendered), tc(debt), rate),
		tender.Convert(e.Cash.ChangeMinor, tc(change), tc(debt), rate))
	settled := e.AmountMinor
	if settled < 0 {
		settled = -settled
	}
	half := big.NewRat(1, 2)
	tolerance := new(big.Rat).Add(half, new(big.Rat).Mul(half,
		tender.Convert(tender.Increment(tc(tendered), e.Cash.CashNoteMinor), tc(tendered), tc(debt), rate)))
	tolerance.Add(tolerance, new(big.Rat).Mul(half, tender.Convert(tender.Increment(tc(change), e.Cash.CashNoteMinor), tc(change), tc(debt), rate)))
	diff := new(big.Rat).Sub(net, new(big.Rat).SetInt64(settled))
	return new(big.Rat).Abs(diff).Cmp(tolerance) <= 0
}
