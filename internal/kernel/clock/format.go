package clock

import "time"

// TimestampLayout is the portable CHAR(24) timestamp form required by the database type
// contract (ARCHITECTURE_v1 §8.1): ISO-8601, UTC, millisecond precision, fixed width.
//
// Fixed width is the point. Because every timestamp is exactly 24 characters in UTC, a
// lexical comparison is a chronological one — which is what lets a plain string column
// be ordered and range-queried identically on SQLite, PostgreSQL, MySQL, and SQL Server
// without a portable date type.
const TimestampLayout = "2006-01-02T15:04:05.000Z"

// Format renders t in the portable CHAR(24) form, converting to UTC first.
//
// This lives in the kernel so there is exactly one definition of "how Mizan writes a
// timestamp". A second copy in some platform package is how two subtly different formats
// end up in one database.
func Format(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}

// ParseTimestamp reads a timestamp written by Format. The returned time is in UTC.
func ParseTimestamp(s string) (time.Time, bool) {
	t, err := time.Parse(TimestampLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// DateLayout is the portable CHAR(10) calendar-date form (ARCHITECTURE_v1 §8.1).
//
// A separate type from a timestamp because a date is not an instant: an exchange rate is
// valid "on 2026-07-30" everywhere, not from a particular moment, and storing it as a
// timestamp would make the answer depend on the reader's time zone.
const DateLayout = "2006-01-02"

// FormatDate renders the calendar date of t in UTC.
func FormatDate(t time.Time) string { return t.UTC().Format(DateLayout) }

// ParseDate reads a date written by FormatDate, at midnight UTC.
func ParseDate(s string) (time.Time, bool) {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
