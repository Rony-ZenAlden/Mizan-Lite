package partner_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/partner"
)

// ── the ageing analysis ─────────────────────────────────────────────────────────

func TestAgeingPutsEachDebtInTheBandAConversationIsAbout(t *testing.T) {
	// Four buckets, because that is what every collections conversation uses. A debt due
	// tomorrow and one due last March are not the same problem, and a single "outstanding"
	// figure cannot say which one a business has.
	items := []partner.OpenItem{
		{DueDate: "2026-10-15", OutstandingMinor: 1_000}, // not yet due
		{DueDate: "2026-09-20", OutstandingMinor: 2_000}, // ~20 days over
		{DueDate: "2026-08-20", OutstandingMinor: 3_000}, // ~50 days over
		{DueDate: "2026-07-20", OutstandingMinor: 4_000}, // ~80 days over
		{DueDate: "2026-01-20", OutstandingMinor: 5_000}, // long over
	}

	bands := partner.Age(items, "2026-10-10")

	if bands.CurrentMinor != 1_000 {
		t.Errorf("current = %d, want 1000", bands.CurrentMinor)
	}
	if bands.Days30Minor != 2_000 {
		t.Errorf("30 days = %d, want 2000", bands.Days30Minor)
	}
	if bands.Days60Minor != 3_000 {
		t.Errorf("60 days = %d, want 3000", bands.Days60Minor)
	}
	if bands.Days90Minor != 4_000 {
		t.Errorf("90 days = %d, want 4000", bands.Days90Minor)
	}
	if bands.OlderMinor != 5_000 {
		t.Errorf("older = %d, want 5000", bands.OlderMinor)
	}

	// The bands account for everything. A debt that fell between two buckets would vanish from
	// the report while remaining owed.
	total := bands.CurrentMinor + bands.Days30Minor + bands.Days60Minor +
		bands.Days90Minor + bands.OlderMinor
	if total != 15_000 {
		t.Errorf("the bands sum to %d, want the whole 15000", total)
	}
}

func TestAnItemWithNoDueDateIsAgedFromWhenItWasRaised(t *testing.T) {
	// # Why this test uses a RECENT date
	//
	// An empty due date sorts before every real one, so an OLD undated item lands in "older"
	// whether or not the fallback exists — a drill removing it passed against a test using
	// January.
	//
	// The fallback is load-bearing for a RECENT one: an invoice raised five days ago with no
	// terms is five days over, not a year over. Ageing it as ancient would put a fresh debt at
	// the top of a collections list and bury the genuinely old ones beneath it.
	recent := []partner.OpenItem{{Date: "2026-10-05", OutstandingMinor: 900}}

	bands := partner.Age(recent, "2026-10-10")
	if bands.Days30Minor != 900 {
		t.Errorf("a debt raised five days ago aged as %+v, want it under 30 days", bands)
	}
	if bands.OlderMinor != 0 {
		t.Errorf("a debt raised five days ago was aged as ancient: %d", bands.OlderMinor)
	}

	// And one genuinely old is still old.
	old := []partner.OpenItem{{Date: "2026-01-05", OutstandingMinor: 500}}
	if partner.Age(old, "2026-10-10").OlderMinor != 500 {
		t.Error("an undated debt from January was not aged as old")
	}
}

func TestAgeingIsAsAtADateAndNotAsAtNow(t *testing.T) {
	// An ageing analysis printed for a month end must say what it said at that month end. One
	// that quietly re-ages itself on reopening is a report nobody can file.
	items := []partner.OpenItem{{DueDate: "2026-09-01", OutstandingMinor: 1_000}}

	atSeptember := partner.Age(items, "2026-09-15")
	atDecember := partner.Age(items, "2026-12-15")

	if atSeptember.Days30Minor != 1_000 {
		t.Errorf("in September it should be under 30 days over, got %+v", atSeptember)
	}
	if atDecember.OlderMinor != 1_000 {
		t.Errorf("in December it should be long over, got %+v", atDecember)
	}
}

// ── the balance ─────────────────────────────────────────────────────────────────

func TestABalanceRefusesWhenTheLedgersAreMissing(t *testing.T) {
	// A service built without the ports serves partners and refuses balances. Answering zero
	// would be worse than refusing: a partner who owes 40,000 would appear to owe nothing, and
	// nothing about the answer would look wrong.
	//
	// # The first version of this test passed for the wrong reason
	//
	// It asserted only that AN error came back — and one did, because the partner identifier was
	// invented and the lookup failed first. A drill removing the guard entirely left it green.
	//
	// It now uses a REAL partner, so the lookup succeeds and the only thing that can refuse is
	// the guard, and it names the code rather than accepting any failure.
	f := newFixture(t)
	created := f.create(t, "ACME", "Acme Supplies", true, true)

	_, err := f.svc.BalanceOf(f.ctx, f.companyID, created.ID)
	if code := errs.CodeOf(err); code != partner.CodeLedgerMissing {
		t.Fatalf("code = %q, want %q — a service with no ledgers answered a balance",
			code, partner.CodeLedgerMissing)
	}
}
