package quantity_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/quantity"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

func TestUnitAndQuantityAccessors(t *testing.T) {
	u := kg(t)
	if u.Code() != "KG" || u.Category() != "weight" || !u.AllowsFractional() || u.String() != "KG" {
		t.Error("unit accessors")
	}
	if (quantity.Unit{}).String() != "<nil>" {
		t.Error("zero unit String")
	}

	q, _ := quantity.Parse(u, "2")
	if !q.Unit().Equal(u) || !q.Valid() || (quantity.Quantity{}).Valid() {
		t.Error("quantity accessors")
	}
}

func TestQuantityErrorBranches(t *testing.T) {
	a, _ := quantity.Parse(kg(t), "2")
	g, _ := quantity.Parse(gram(t), "5")

	if _, err := a.Sub(g); !errors.Is(err, quantity.ErrUnitMismatch) {
		t.Errorf("Sub mismatch: want ErrUnitMismatch, got %v", err)
	}
	if _, err := a.Cmp(g); !errors.Is(err, quantity.ErrUnitMismatch) {
		t.Errorf("Cmp mismatch: want ErrUnitMismatch, got %v", err)
	}
	if _, err := (quantity.Quantity{}).Cmp(a); !errors.Is(err, quantity.ErrNoUnit) {
		t.Errorf("Cmp zero-value: want ErrNoUnit, got %v", err)
	}

	// Conversion overflow: a huge kg amount into mg (×10^6) exceeds int64.
	huge, _ := quantity.FromMicro(kg(t), math.MaxInt64)
	if _, err := huge.ConvertTo(mg(t), round.HalfAwayFromZero); !errors.Is(err, quantity.ErrOverflow) {
		t.Errorf("ConvertTo overflow: want ErrOverflow, got %v", err)
	}

	// Converting within a category into a non-fractional unit that would yield a
	// fraction is rejected: 1 piece → dozen = 0.0833… dozen, and dozen is whole-only.
	dozen := mustUnit(t, "DOZEN", "count", 12*quantity.FactorScale, false)
	onePiece, _ := quantity.Parse(pcs(t), "1")
	if _, err := onePiece.ConvertTo(dozen, round.HalfAwayFromZero); !errors.Is(err, quantity.ErrFractionNotAllowed) {
		t.Errorf("1 pcs → dozen: want ErrFractionNotAllowed, got %v", err)
	}
}
