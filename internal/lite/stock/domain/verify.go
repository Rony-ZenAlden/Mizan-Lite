package domain

import (
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Verifier finding codes. They double as i18n keys.
const (
	FindingBadStart      = "lite.stock.verify.bad_start"
	FindingChainBroken   = "lite.stock.verify.chain_broken"
	FindingLevelMismatch = "lite.stock.verify.level_mismatch"
	FindingUnpaired      = "lite.stock.verify.unpaired"
	FindingBadReversal   = "lite.stock.verify.bad_reversal"
	FindingNoMovements   = "lite.stock.verify.level_without_movements"
	FindingNoLevel       = "lite.stock.verify.movements_without_level"
)

// Finding is one thing the verifier found wrong. It is a report, never a repair (D-L2.14): a correction that erased
// a discrepancy would erase the evidence of what caused it.
type Finding struct {
	Code       string
	ProductID  id.ID
	MovementID id.ID // zero when the finding is about the product, not one row
}

// Verifier checks the ledger against itself and against the levels (L2 §7). Movements are added in ledger order —
// by product, then by place — one at a time, so a long ledger is never held in memory whole.
type Verifier struct {
	levels   map[id.ID]Level
	seen     map[id.ID]bool
	previous Movement
	started  bool
	receipts map[id.ID]Movement
	// reversals are checked once every receipt has been seen: a reversal and its receipt share a product, so the
	// receipt comes first in ledger order — but a reversal pointing at another product's row would not.
	reversals []Movement
	pairs     map[id.ID][]Movement
	findings  []Finding
}

// NewVerifier starts a check of levels.
func NewVerifier(levels []Level) *Verifier {
	v := &Verifier{
		levels: map[id.ID]Level{}, seen: map[id.ID]bool{}, receipts: map[id.ID]Movement{}, pairs: map[id.ID][]Movement{},
	}
	for _, l := range levels {
		v.levels[l.ProductID] = l
	}
	return v
}

// Add checks the next movement in ledger order.
func (v *Verifier) Add(m Movement) {
	if !v.started || v.previous.ProductID != m.ProductID {
		v.closeProduct()
		if m.Seq != 1 || m.OnHandBeforeMicro != 0 || m.AvgCostBeforeMicro != 0 {
			v.find(FindingBadStart, m.ProductID, m.ID)
		}
	} else if m.Seq != v.previous.Seq+1 ||
		m.OnHandBeforeMicro != v.previous.OnHandAfterMicro || m.AvgCostBeforeMicro != v.previous.AvgCostAfterMicro {
		v.find(FindingChainBroken, m.ProductID, m.ID)
	}
	v.started = true
	v.previous = m
	v.seen[m.ProductID] = true

	switch m.Kind {
	case KindReceipt:
		v.receipts[m.ID] = m
	case KindReceiptReversal:
		v.reversals = append(v.reversals, m)
	case KindPackageOut, KindContentIn:
		v.pairs[m.PairID] = append(v.pairs[m.PairID], m)
	}
}

// closeProduct compares the product just walked with its level.
func (v *Verifier) closeProduct() {
	if !v.started {
		return
	}
	last := v.previous
	level, ok := v.levels[last.ProductID]
	switch {
	case !ok:
		v.find(FindingNoLevel, last.ProductID, "")
	case level.OnHandMicro != last.OnHandAfterMicro || level.AvgCostMicro != last.AvgCostAfterMicro ||
		level.LastMovementID != last.ID:
		v.find(FindingLevelMismatch, last.ProductID, last.ID)
	}
}

// Findings finishes the check and returns what it found, in a stable order.
func (v *Verifier) Findings() []Finding {
	v.closeProduct()
	v.started = false
	for productID := range v.levels {
		if !v.seen[productID] {
			v.find(FindingNoMovements, productID, "")
		}
	}
	for _, r := range v.reversals {
		receipt, ok := v.receipts[r.ReversesID]
		if !ok || receipt.ProductID != r.ProductID || receipt.QuantityMicro != -r.QuantityMicro {
			v.find(FindingBadReversal, r.ProductID, r.ID)
		}
	}
	for _, rows := range v.pairs {
		if len(rows) != 2 || rows[0].Kind == rows[1].Kind {
			for _, m := range rows {
				v.find(FindingUnpaired, m.ProductID, m.ID)
			}
		}
	}
	sort.SliceStable(v.findings, func(i, j int) bool {
		a, b := v.findings[i], v.findings[j]
		if a.ProductID != b.ProductID {
			return a.ProductID < b.ProductID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.MovementID < b.MovementID
	})
	out := v.findings
	v.findings = nil
	return out
}

func (v *Verifier) find(code string, productID, movementID id.ID) {
	v.findings = append(v.findings, Finding{Code: code, ProductID: productID, MovementID: movementID})
}
