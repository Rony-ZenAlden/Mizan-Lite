package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
)

var (
	syp = domain.Currency{Code: "SYP", Decimals: 0}
	usd = domain.Currency{Code: "USD", Decimals: 2}
)

func TestMoneyEntries(t *testing.T) {
	e, err := domain.NewMoney(domain.Draft{Kind: domain.KindExpense, Currency: syp, Amount: "٢٥٠٠٠٠", Category: "electricity", FromDrawer: true, Note: " فاتورة "})
	if err != nil || e.AmountMinor != 250_000 || e.Category != "electricity" || !e.FromDrawer || e.Note != "فاتورة" {
		t.Fatalf("expense = %+v, %v", e, err)
	}
	w, err := domain.NewMoney(domain.Draft{Kind: domain.KindWithdrawal, Currency: usd, Amount: "50", Category: "rent", FromDrawer: true})
	if err != nil || w.AmountMinor != 5_000 || w.Category != "" || w.FromDrawer {
		t.Fatalf("a withdrawal keeps no category = %+v, %v", w, err)
	}
	for _, bad := range []struct {
		d    domain.Draft
		code string
	}{
		{domain.Draft{Kind: domain.KindCount, Currency: syp, Amount: "1"}, domain.CodeUnknownKind},
		{domain.Draft{Kind: domain.KindExpense, Currency: syp, Amount: "1", Category: "fun"}, domain.CodeUnknownCategory},
		{domain.Draft{Kind: domain.KindExpense, Currency: syp, Amount: "0", Category: "rent"}, domain.CodeAmountRequired},
		{domain.Draft{Kind: domain.KindDeposit, Currency: syp, Amount: ""}, domain.CodeAmountRequired},
		{domain.Draft{Kind: domain.KindDeposit, Currency: usd, Amount: "1.005"}, domain.CodeAmountDecimals},
		{domain.Draft{Kind: domain.KindDeposit, Currency: syp, Amount: "99999999999999999"}, domain.CodeAmountTooLarge},
		{domain.Draft{Kind: domain.KindDeposit, Currency: syp, Amount: "-5"}, "lite.number.invalid"},
	} {
		if _, err := domain.NewMoney(bad.d); errs.CodeOf(err) != bad.code {
			t.Errorf("%+v: %v, want %s", bad.d, err, bad.code)
		}
	}
}

// TestACountRecordsItsDifference: counted less expected, a shortage below zero; a count of nothing is a count.
func TestACountRecordsItsDifference(t *testing.T) {
	c, err := domain.NewCount(syp, "1250000", 1_300_000, "")
	if err != nil || c.Difference() != -50_000 || c.Kind != domain.KindCount {
		t.Fatalf("count = %+v, %v", c, err)
	}
	if z, err := domain.NewCount(usd, "0", 0, ""); err != nil || z.AmountMinor != 0 || z.Difference() != 0 {
		t.Fatalf("an empty drawer = %+v, %v", z, err)
	}
}

func TestAnEntryIsReversedWithAReasonAndAReversalNever(t *testing.T) {
	e, _ := domain.NewMoney(domain.Draft{Kind: domain.KindDeposit, Currency: usd, Amount: "10"})
	if _, err := domain.Reverse(e, " "); errs.CodeOf(err) != domain.CodeReasonRequired {
		t.Fatalf("no reason: %v", err)
	}
	r, err := domain.Reverse(e, "خطأ")
	if err != nil || r.Kind != domain.KindReversal || r.AmountMinor != 1_000 || r.Currency != "USD" {
		t.Fatalf("reversal = %+v, %v", r, err)
	}
	if _, err := domain.Reverse(r, "x"); errs.CodeOf(err) != domain.CodeNotReversible {
		t.Fatalf("a reversal reversed: %v", err)
	}
}
