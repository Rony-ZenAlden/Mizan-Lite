package settle_test

import (
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/settle"
)

func TestOverAllocationIsRefusedAndUnderAllocationIsNot(t *testing.T) {
	// Allocating more than the payment is arithmetic that cannot be true: the money does not
	// exist. Allocating LESS is a deposit with a balance still to assign, which is ordinary —
	// forcing it to balance would mean inventing an allocation against a document that may not
	// exist yet.
	payment := int64(1_000)

	if err := settle.RequireAllocatable(payment, []settle.Allocation{{AmountMinor: 400}}); err != nil {
		t.Fatalf("a part-allocated payment was refused: %v", err)
	}
	if err := settle.RequireAllocatable(payment, nil); err != nil {
		t.Fatalf("a payment allocated to nothing was refused: %v", err)
	}
	if err := settle.RequireAllocatable(payment, []settle.Allocation{
		{AmountMinor: 600}, {AmountMinor: 400},
	}); err != nil {
		t.Fatalf("an exactly-allocated payment was refused: %v", err)
	}

	err := settle.RequireAllocatable(payment, []settle.Allocation{
		{AmountMinor: 600}, {AmountMinor: 500},
	})
	if !errors.Is(err, settle.ErrOverAllocated) {
		t.Fatalf("err = %v, want ErrOverAllocated", err)
	}
}

func TestAnAllocationOfNothingIsRefused(t *testing.T) {
	for _, amount := range []int64{0, -100} {
		err := settle.RequireAllocatable(1_000, []settle.Allocation{{AmountMinor: amount}})
		if !errors.Is(err, settle.ErrNonPositive) {
			t.Errorf("amount %d: err = %v, want ErrNonPositive", amount, err)
		}
	}
}

func TestADocumentCannotBeOverSettled(t *testing.T) {
	// Not generosity: a keying error that leaves the payable overdrawn and reconciles against
	// nothing.
	if err := settle.RequireSettleable(1_000, 400, 600); err != nil {
		t.Fatalf("settling a document exactly was refused: %v", err)
	}
	if err := settle.RequireSettleable(1_000, 400, 601); !errors.Is(err, settle.ErrOverSettled) {
		t.Fatalf("err = %v, want ErrOverSettled", err)
	}
	// And one already settled takes nothing more.
	if err := settle.RequireSettleable(1_000, 1_000, 1); !errors.Is(err, settle.ErrOverSettled) {
		t.Fatalf("err = %v, want ErrOverSettled", err)
	}
}

func TestOutstandingNeverGoesNegative(t *testing.T) {
	// An over-settled document is a fault to investigate, not a debt owed back — and a negative
	// outstanding would net against another document in any sum, hiding both.
	if got := settle.Outstanding(1_000, 400); got != 600 {
		t.Errorf("outstanding = %d, want 600", got)
	}
	if got := settle.Outstanding(1_000, 1_500); got != 0 {
		t.Errorf("outstanding = %d after over-settlement, want 0", got)
	}
	if !settle.IsSettled(1_000, 1_000) {
		t.Error("a fully settled document reports itself outstanding")
	}
	if settle.IsSettled(1_000, 999) {
		t.Error("a document owing one unit reports itself settled")
	}
}
