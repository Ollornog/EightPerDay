// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"time"
)

// Date is a calendar day without a time and without a time zone, the local date a time belongs to. The zero value is
// "no date". Dates compare with Compare, Before and After; arithmetic goes through AddDays.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate returns the normalised date, so NewDate(2026, 2, 30) is 2026-03-02.
func NewDate(year int, month time.Month, day int) Date {
	return DateOf(time.Date(year, month, day, 12, 0, 0, 0, time.UTC))
}

// DateOf returns the date of t in its own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// DateIn returns the date of t in loc.
func DateIn(t time.Time, loc *time.Location) Date { return DateOf(t.In(loc)) }

// ParseDate reads a date in the form 2006-01-02.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("date %q: want YYYY-MM-DD", s)
	}
	return DateOf(t), nil
}

// MustDate is ParseDate for tests and constants; it panics on a malformed date.
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// IsZero reports whether d is the zero date.
func (d Date) IsZero() bool { return d == Date{} }

func (d Date) noon() time.Time { return time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC) }

// String returns the date as 2006-01-02, or "" for the zero date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.noon().Format(time.DateOnly)
}

// AddDays returns the date n days later (or earlier for negative n).
func (d Date) AddDays(n int) Date { return DateOf(d.noon().AddDate(0, 0, n)) }

// AddMonths returns the date n months later, normalised like time.AddDate.
func (d Date) AddMonths(n int) Date { return DateOf(d.noon().AddDate(0, n, 0)) }

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday { return d.noon().Weekday() }

// Compare returns -1, 0 or +1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Year != o.Year:
		return sign(d.Year - o.Year)
	case d.Month != o.Month:
		return sign(int(d.Month) - int(o.Month))
	default:
		return sign(d.Day - o.Day)
	}
}

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After reports whether d is later than o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// Within reports whether from <= d <= to. A zero to means open-ended.
func (d Date) Within(from, to Date) bool {
	return !d.Before(from) && (to.IsZero() || !d.After(to))
}

// DaysUntil returns the number of days from d to o (negative if o is earlier).
func (d Date) DaysUntil(o Date) int {
	return int(o.noon().Sub(d.noon()).Round(time.Hour).Hours() / 24)
}

// At returns the instant of the clock time (minutes after midnight) on d in loc. Clock times that do not exist on a
// daylight saving day are moved forward as time.Date does.
func (d Date) At(minutes int, loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, minutes, 0, 0, loc)
}

// StartOfWeek returns the first day of the week that holds d, for weeks starting on start.
func (d Date) StartOfWeek(start time.Weekday) Date {
	back := (int(d.Weekday()) - int(start) + 7) % 7
	return d.AddDays(-back)
}

// StartOfMonth returns the first day of d's month.
func (d Date) StartOfMonth() Date { return Date{d.Year, d.Month, 1} }

// MarshalText writes the date as 2006-01-02.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText reads 2006-01-02, and the empty string as the zero date.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	v, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// DaysIn lists every date from from to to, both included.
func DaysIn(from, to Date) []Date {
	var out []Date
	for d := from; !d.After(to); d = d.AddDays(1) {
		out = append(out, d)
	}
	return out
}
