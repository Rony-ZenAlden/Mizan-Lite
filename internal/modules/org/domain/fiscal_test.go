package domain_test

import (
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/org/domain"
)

func TestGenerateFiscalYearProducesTwelvePeriods(t *testing.T) {
	companyID, _ := id.New()

	fy, err := domain.GenerateFiscalYear(id.New, companyID, 2026, time.January)
	if err != nil {
		t.Fatalf("GenerateFiscalYear: %v", err)
	}
	if len(fy.Periods) != domain.PeriodsPerYear {
		t.Fatalf("periods = %d, want %d", len(fy.Periods), domain.PeriodsPerYear)
	}
	if fy.Code != "FY2026" {
		t.Errorf("code = %q, want FY2026", fy.Code)
	}
	if got := fy.Start.Format("2006-01-02"); got != "2026-01-01" {
		t.Errorf("start = %s", got)
	}
	if got := fy.End.Format("2006-01-02"); got != "2026-12-31" {
		t.Errorf("end = %s", got)
	}
}

// TestFiscalPeriodsTileTheYear is the guarantee Phase 2 depends on.
//
// A boundary one day out means a posting that belongs to no period, or to two. Generation is
// supposed to make that unrepresentable — this asserts it, and the mutation drill (moving a
// period end by a day) must fail here.
func TestFiscalPeriodsTileTheYear(t *testing.T) {
	companyID, _ := id.New()

	for _, start := range []time.Month{time.January, time.April, time.July, time.October} {
		fy, err := domain.GenerateFiscalYear(id.New, companyID, 2026, start)
		if err != nil {
			t.Fatalf("start %v: %v", start, err)
		}
		if !fy.Tiles() {
			t.Errorf("periods do not tile the year for a %v start", start)
		}
		// Assert the gap/overlap property directly too, so a broken Tiles() cannot hide it.
		for i := 1; i < len(fy.Periods); i++ {
			gap := fy.Periods[i].Start.Sub(fy.Periods[i-1].End)
			if gap != 24*time.Hour {
				t.Errorf("%v start, period %d: gap of %v between periods, want exactly one day",
					start, i+1, gap)
			}
		}
	}
}

// TestFiscalYearHandlesMonthLengthsAndLeapYears pins the reason generation goes through
// time.Date rather than day arithmetic: AddDate(0,1,0) on 31 January yields 3 March.
func TestFiscalYearHandlesMonthLengthsAndLeapYears(t *testing.T) {
	companyID, _ := id.New()

	fy, err := domain.GenerateFiscalYear(id.New, companyID, 2028, time.January) // 2028 is a leap year
	if err != nil {
		t.Fatal(err)
	}
	february := fy.Periods[1]
	if got := february.End.Format("2006-01-02"); got != "2028-02-29" {
		t.Errorf("February 2028 ends %s, want 2028-02-29", got)
	}
	january := fy.Periods[0]
	if got := january.End.Format("2006-01-02"); got != "2028-01-31" {
		t.Errorf("January ends %s, want 2028-01-31", got)
	}
}

// TestFiscalYearWrapsIntoTheNextCalendarYear covers the non-January start: a July 2026 year
// runs to June 2027 and is NAMED for the year it ends in, which is the accounting convention.
func TestFiscalYearWrapsIntoTheNextCalendarYear(t *testing.T) {
	companyID, _ := id.New()

	fy, err := domain.GenerateFiscalYear(id.New, companyID, 2026, time.July)
	if err != nil {
		t.Fatal(err)
	}
	if got := fy.Start.Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("start = %s", got)
	}
	if got := fy.End.Format("2006-01-02"); got != "2027-06-30" {
		t.Errorf("end = %s", got)
	}
	if fy.Code != "FY2027" {
		t.Errorf("code = %q, want FY2027 (named for the year it ends in)", fy.Code)
	}
}

func TestGenerateFiscalYearRejectsBadInput(t *testing.T) {
	companyID, _ := id.New()

	if _, err := domain.GenerateFiscalYear(id.New, "", 2026, time.January); err == nil {
		t.Error("a fiscal year with no company was accepted")
	}
	if _, err := domain.GenerateFiscalYear(id.New, companyID, 2026, time.Month(13)); err == nil {
		t.Error("an out-of-range start month was accepted")
	}
}
