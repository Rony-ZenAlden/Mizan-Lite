package domain

import (
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Fiscal statuses (§20.4). Posting into anything but `open` is rejected — in Phase 2, which is
// the first code to read these.
const (
	StatusOpen   = "open"
	StatusClosed = "closed"
	StatusLocked = "locked"
)

// PeriodsPerYear is fixed at twelve calendar months.
//
// Not configurable: 4-4-5 retail calendars and 13-period schemes exist, but supporting one
// means the period generator, every report's date grouping, and the year-end close all become
// variable. That is a Phase 2 decision to make deliberately with an accountant, not a
// parameter to leave dangling here.
const PeriodsPerYear = 12

// FiscalYear is an accounting year for a company.
type FiscalYear struct {
	ID        id.ID
	CompanyID id.ID
	Code      string
	Start     time.Time // business dates (§7.5): a date, not an instant
	End       time.Time
	Status    string
	Periods   []FiscalPeriod
}

// FiscalPeriod is one month within a fiscal year.
type FiscalPeriod struct {
	ID           id.ID
	FiscalYearID id.ID
	Sequence     int
	Start        time.Time
	End          time.Time
	Status       string
}

// NewIDFunc supplies identifiers to the generator.
//
// Injected rather than called directly because id.New returns an error (entropy can fail) and
// the domain must not decide what to do about that — the caller does.
type NewIDFunc func() (id.ID, error)

// GenerateFiscalYear builds a fiscal year and its twelve monthly periods.
//
// Periods are GENERATED, never hand-entered. A boundary one day out means a Phase 2 posting
// that belongs to no period, or to two — and generation makes that unrepresentable rather than
// merely validated.
//
// startMonth/startYear name the first month. A July start therefore runs July of startYear
// through June of startYear+1, and the code is taken from the year the fiscal year ENDS in,
// which is the convention accountants use when a year straddles a boundary.
//
// Month lengths and leap years come from time.Date's normalisation, not from day arithmetic:
// AddDate(0, 1, 0) on 31 January would give 3 March, so each period is built from the first of
// its month and ended one day before the first of the next.
func GenerateFiscalYear(
	newID NewIDFunc, companyID id.ID, startYear int, startMonth time.Month,
) (FiscalYear, error) {
	if companyID.IsZero() {
		return FiscalYear{}, errs.Validation(CodeInvalidFiscalYear, "a fiscal year needs a company")
	}
	if startMonth < time.January || startMonth > time.December {
		return FiscalYear{}, errs.Validation(CodeInvalidFiscalYear, "the start month is out of range").
			WithParam("month", strconv.Itoa(int(startMonth)))
	}

	yearID, err := newID()
	if err != nil {
		return FiscalYear{}, err
	}

	start := time.Date(startYear, startMonth, 1, 0, 0, 0, 0, time.UTC)
	// The day before the same month next year: the exact end of a twelve-month span.
	end := start.AddDate(1, 0, 0).AddDate(0, 0, -1)

	// A year beginning in any month but January ends in the following calendar year, and that
	// is the year it is named for.
	code := "FY" + strconv.Itoa(end.Year())

	fy := FiscalYear{
		ID:        yearID,
		CompanyID: companyID,
		Code:      code,
		Start:     start,
		End:       end,
		Status:    StatusOpen,
		Periods:   make([]FiscalPeriod, 0, PeriodsPerYear),
	}

	for i := 0; i < PeriodsPerYear; i++ {
		periodID, idErr := newID()
		if idErr != nil {
			return FiscalYear{}, idErr
		}
		periodStart := start.AddDate(0, i, 0)
		periodEnd := start.AddDate(0, i+1, 0).AddDate(0, 0, -1)

		fy.Periods = append(fy.Periods, FiscalPeriod{
			ID:           periodID,
			FiscalYearID: yearID,
			Sequence:     i + 1,
			Start:        periodStart,
			End:          periodEnd,
			Status:       StatusOpen,
		})
	}
	return fy, nil
}

// Tiles reports whether the periods cover the year exactly: no gap, no overlap, and the ends
// meeting the year's own boundaries.
//
// Exported because it is the property Phase 2 depends on, and a property worth asserting at
// runtime as well as in tests (§30's invariant checks).
func (f FiscalYear) Tiles() bool {
	if len(f.Periods) == 0 {
		return false
	}
	if !f.Periods[0].Start.Equal(f.Start) {
		return false
	}
	if !f.Periods[len(f.Periods)-1].End.Equal(f.End) {
		return false
	}
	for i := 1; i < len(f.Periods); i++ {
		wantStart := f.Periods[i-1].End.AddDate(0, 0, 1)
		if !f.Periods[i].Start.Equal(wantStart) {
			return false
		}
	}
	return true
}
