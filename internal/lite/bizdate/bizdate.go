// Package bizdate is the shop's calendar day.
//
// A movement at 01:00 in Damascus is 22:00 UTC the day before. "Today's receipts" means the shop's day, so every
// ledger row carries its business date, computed here from an injected location (L2 §3.7, D-L2.8).
//
// # Never time.LoadLocation
//
// LoadLocation reads the IANA time-zone database, which Windows does not ship: it fails on a shop's Windows
// machine unless the binary embeds Go's time/tzdata, while passing every test on a Mac. Lite uses the machine's own
// zone (time.Local, which Go reads from the Windows registry) and tests use time.FixedZone. TestNoLoadLocationAnywhere scans
// the source so the call cannot creep in.
package bizdate

import "time"

// Layout is the business date's portable CHAR(10) form.
const Layout = "2006-01-02"

// Date returns the calendar day of t in loc.
func Date(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format(Layout)
}
