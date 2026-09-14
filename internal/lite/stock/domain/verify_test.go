package domain_test

import (
	"sort"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// ledger is a consistent history built through the domain: oil opened into loose oil, ghee received and one
// receipt reversed, labneh sold and the sale voided, labneh written off, and a cost corrected.
type ledger struct {
	levels    map[string]domain.Level
	movements []domain.Movement
}

func (l *ledger) record(name string, m domain.Movement, after domain.Level) {
	l.levels[name] = after
	l.movements = append(l.movements, m)
}

func buildLedger(t *testing.T) *ledger {
	t.Helper()
	l := &ledger{levels: map[string]domain.Level{}}
	must := func(m domain.Movement, after domain.Level, err error) (domain.Movement, domain.Level) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return m, after
	}

	tin, tinLevel := must(domain.Opening(domain.Level{ProductID: newID(t)}, stamp(t), 3*u, usd(95*u), ""))
	l.record("tin", tin, tinLevel)
	loose, looseLevel := must(domain.Opening(domain.Level{ProductID: newID(t)}, stamp(t), 2*u, usd(6*u), ""))
	l.record("loose", loose, looseLevel)
	opened, err := domain.OpenPackage(l.levels["tin"], l.levels["loose"],
		domain.Package{ContentProductID: looseLevel.ProductID, ContentQuantityMicro: 16 * u}, u, stamp(t), stamp(t), newID(t))
	if err != nil {
		t.Fatal(err)
	}
	l.record("tin", opened.Out, opened.PackageLevel)
	l.record("loose", opened.In, opened.ContentLv)

	ghee, gheeLevel := must(domain.Receive(domain.Level{ProductID: newID(t)}, stamp(t), 12*u, usd(17*u), ""))
	l.record("ghee", ghee, gheeLevel)
	receipt, received := must(domain.Receive(l.levels["ghee"], stamp(t), 5*u, usd(18*u), ""))
	l.record("ghee", receipt, received)
	reversal, reversed := must(domain.ReverseReceipt(l.levels["ghee"], receipt, stamp(t), ""))
	l.record("ghee", reversal, reversed)

	labneh, labnehLevel := must(domain.Opening(domain.Level{ProductID: newID(t)}, stamp(t), 10*u, usd(2*u), ""))
	l.record("labneh", labneh, labnehLevel)
	sold, afterSale := must(domain.Sale(l.levels["labneh"], stamp(t), 3*u, newID(t), newID(t)))
	l.record("labneh", sold, afterSale)
	voided, afterVoid := must(domain.SaleVoid(l.levels["labneh"], sold, stamp(t)))
	l.record("labneh", voided, afterVoid)
	spoiled, afterSpoil := must(domain.Adjust(l.levels["labneh"], stamp(t), -2*u, domain.ReasonExpired, ""))
	l.record("labneh", spoiled, afterSpoil)
	corrected, afterCorrection := must(domain.CorrectCost(l.levels["labneh"], stamp(t), 2_500_000, "السعر الصحيح"))
	l.record("labneh", corrected, afterCorrection)
	return l
}

// verify runs the verifier as the store feeds it: every movement ordered by product, then place.
func (l *ledger) verify() []domain.Finding {
	levels := make([]domain.Level, 0, len(l.levels))
	for _, level := range l.levels {
		levels = append(levels, level)
	}
	ordered := append([]domain.Movement(nil), l.movements...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ProductID != ordered[j].ProductID {
			return ordered[i].ProductID < ordered[j].ProductID
		}
		return ordered[i].Seq < ordered[j].Seq
	})
	v := domain.NewVerifier(levels)
	for _, m := range ordered {
		v.Add(m)
	}
	return v.Findings()
}

func (l *ledger) index(t *testing.T, kind domain.Kind) int {
	t.Helper()
	for i, m := range l.movements {
		if m.Kind == kind {
			return i
		}
	}
	t.Fatalf("no %s in the ledger", kind)
	return -1
}

func (l *ledger) remove(i int) { l.movements = append(l.movements[:i], l.movements[i+1:]...) }

func expectOnly(t *testing.T, findings []domain.Finding, code string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("nothing found; want %s", code)
	}
	for _, f := range findings {
		if f.Code != code {
			t.Fatalf("findings = %+v; want only %s", findings, code)
		}
	}
}

func TestAConsistentLedgerHasNoFindings(t *testing.T) {
	if findings := buildLedger(t).verify(); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings := domain.NewVerifier(nil).Findings(); len(findings) != 0 {
		t.Fatalf("an empty ledger: %+v", findings)
	}
}

func TestTheVerifierFindsARowAlteredOutOfTurn(t *testing.T) {
	l := buildLedger(t)
	i := l.index(t, domain.KindAdjustment)
	// Edited as the schema would still accept it — after = before + quantity — so only the chain can tell.
	l.movements[i].OnHandBeforeMicro += u
	l.movements[i].QuantityMicro -= u
	expectOnly(t, l.verify(), domain.FindingChainBroken)
}

func TestTheVerifierFindsARowRemoved(t *testing.T) {
	l := buildLedger(t)
	i := l.index(t, domain.KindAdjustment)
	m := l.movements[i]
	l.remove(i)
	// The correction after it now follows nothing it recorded.
	findings := l.verify()
	expectOnly(t, findings, domain.FindingChainBroken)
	if findings[0].ProductID != m.ProductID {
		t.Fatalf("found on the wrong product: %+v", findings)
	}
}

func TestTheVerifierFindsAPlaceSkipped(t *testing.T) {
	l := buildLedger(t)
	// Same state, wrong place: a row inserted and removed again leaves a gap only the sequence shows.
	i := l.index(t, domain.KindCostCorrection)
	l.movements[i].Seq++
	l.levels["labneh"] = domain.Level{
		ProductID: l.levels["labneh"].ProductID, OnHandMicro: l.levels["labneh"].OnHandMicro,
		AvgCostMicro: l.levels["labneh"].AvgCostMicro, LastMovementID: l.levels["labneh"].LastMovementID,
		LastSeq: l.movements[i].Seq, RowVersion: 1,
	}
	expectOnly(t, l.verify(), domain.FindingChainBroken)
}

func TestTheVerifierFindsALedgerThatDoesNotStartFromNothing(t *testing.T) {
	l := buildLedger(t)
	i := l.index(t, domain.KindOpening)
	l.movements[i].OnHandBeforeMicro = u
	l.movements[i].OnHandAfterMicro += u
	findings := l.verify()
	if len(findings) == 0 || findings[0].Code != domain.FindingBadStart {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestTheVerifierFindsALevelWrittenWithoutItsMovement(t *testing.T) {
	for name, plant := range map[string]func(*domain.Level){
		"on hand":       func(l *domain.Level) { l.OnHandMicro += u },
		"average":       func(l *domain.Level) { l.AvgCostMicro++ },
		"last movement": func(l *domain.Level) { l.LastMovementID = newID(t) },
	} {
		t.Run(name, func(t *testing.T) {
			l := buildLedger(t)
			level := l.levels["ghee"]
			plant(&level)
			l.levels["ghee"] = level
			expectOnly(t, l.verify(), domain.FindingLevelMismatch)
		})
	}
}

func TestTheVerifierFindsHalfAnOpening(t *testing.T) {
	for _, kind := range []domain.Kind{domain.KindPackageOut, domain.KindContentIn} {
		t.Run(string(kind), func(t *testing.T) {
			l := buildLedger(t)
			i := l.index(t, kind)
			removed := l.movements[i]
			// Remove the row and write its product's level as if it never happened, so only the pairing is wrong.
			l.remove(i)
			for name, level := range l.levels {
				if level.ProductID == removed.ProductID {
					level.OnHandMicro, level.AvgCostMicro = removed.OnHandBeforeMicro, removed.AvgCostBeforeMicro
					level.LastSeq, level.LastMovementID = removed.Seq-1, previousID(l, removed)
					l.levels[name] = level
				}
			}
			expectOnly(t, l.verify(), domain.FindingUnpaired)
		})
	}
}

func previousID(l *ledger, m domain.Movement) id.ID {
	for _, other := range l.movements {
		if other.ProductID == m.ProductID && other.Seq == m.Seq-1 {
			return other.ID
		}
	}
	return ""
}

func TestTheVerifierFindsAReversalOfTheWrongThing(t *testing.T) {
	l := buildLedger(t)
	i := l.index(t, domain.KindReceiptReversal)
	l.movements[i].ReversesID = l.movements[l.index(t, domain.KindOpening)].ID
	expectOnly(t, l.verify(), domain.FindingBadReversal)
}

func TestTheVerifierFindsOrphans(t *testing.T) {
	l := buildLedger(t)
	delete(l.levels, "labneh")
	expectOnly(t, l.verify(), domain.FindingNoLevel)

	l = buildLedger(t)
	l.levels["ghost"] = domain.Level{ProductID: newID(t), OnHandMicro: u, LastSeq: 1, LastMovementID: newID(t)}
	expectOnly(t, l.verify(), domain.FindingNoMovements)
}

func TestTheVerifierFindsAVoidOfTheWrongThing(t *testing.T) {
	l := buildLedger(t)
	void := l.index(t, domain.KindSaleVoid)
	// Point the void at the opening instead of its sale.
	l.movements[void].ReversesID = l.movements[l.index(t, domain.KindOpening)].ID
	expectOnly(t, l.verify(), domain.FindingBadReversal)

	l = buildLedger(t)
	void = l.index(t, domain.KindSaleVoid)
	l.movements[void].SaleLineID = newID(t) // a void of another line's sale
	expectOnly(t, l.verify(), domain.FindingBadReversal)
}
