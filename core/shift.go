// SPDX-License-Identifier: MIT

package core

import (
	"math"
	"time"
)

// DaySplit says which day the time of a span past midnight counts for: for the target, the balance, the earnings and
// the overview. Surcharges always look at the clock time of each minute, whichever day it counts for.
type DaySplit string

const (
	// SplitStartDay: the whole span counts for the local date it starts on. This is the default.
	SplitStartDay DaySplit = "start_day"
	// SplitMidnight: the span is cut at local midnight and every day gets its part. The parts keep their real length,
	// so a night with a daylight saving change has 7 or 9 hours from 22:00 to 06:00.
	SplitMidnight DaySplit = "midnight"
)

// Valid reports whether s is empty (the default) or a known split.
func (s DaySplit) Valid() bool { return s == "" || s == SplitStartDay || s == SplitMidnight }

// interval is a stretch of real time.
type interval struct{ start, end time.Time }

func (s interval) minutes() int { return int(math.Round(s.end.Sub(s.start).Minutes())) }

// entryLocation returns the time zone an entry was booked in, or the employer's.
func entryLocation(e Entry, emp Employer) *time.Location {
	if e.TimeZone != "" {
		if loc, err := time.LoadLocation(e.TimeZone); err == nil {
			return loc
		}
	}
	loc, err := emp.Location()
	if err != nil {
		return time.UTC
	}
	return loc
}

// splitAtMidnight cuts a span entry at every local midnight it passes. The parts are copies of the entry with their
// own date, start, end and minutes; the minutes add up to those of the entry. Entries without a span, and spans
// within one day, come back unchanged.
func splitAtMidnight(e Entry, loc *time.Location) []Entry {
	if e.Start == nil || e.End == nil || e.Kind == EntryAdjust {
		return []Entry{e}
	}
	start, end := e.Start.In(loc), e.End.In(loc)
	var out []Entry
	given := 0
	for cur := start; cur.Before(end); {
		date := DateOf(cur)
		next := date.AddDays(1).At(0, loc)
		if !next.Before(end) {
			next = end
		}
		part := e
		s, x := cur.UTC(), next.UTC()
		part.Start, part.End, part.Date = &s, &x, date
		part.Minutes = interval{cur, next}.minutes()
		out = append(out, part)
		given += part.Minutes
		cur = next
	}
	if len(out) <= 1 {
		return []Entry{e}
	}
	// Rounding never changes the sum.
	out[len(out)-1].Minutes += e.Minutes - given
	return out
}

// entriesByDay returns the live entries of the employer by the day they count for: cut at midnight if the employer
// splits, else on the day they start. A timed break inside a work span counts for the day of that span, so a break
// after midnight in a night shift that keeps its start day is taken off that day.
func entriesByDay(d Data, emp Employer) map[Date][]Entry {
	out := map[Date][]Entry{}
	var works []Entry
	for _, e := range d.Entries {
		if e.EmployerID == emp.ID && e.DeletedAt == nil && e.Kind == EntryWork && e.Start != nil && e.End != nil {
			works = append(works, e)
		}
	}
	for _, e := range d.Entries {
		if e.EmployerID != emp.ID || e.DeletedAt != nil {
			continue
		}
		if emp.DaySplit == SplitMidnight {
			for _, p := range splitAtMidnight(e, entryLocation(e, emp)) {
				out[p.Date] = append(out[p.Date], p)
			}
			continue
		}
		if e.Kind == EntryBreak && e.Start != nil && e.End != nil {
			for _, w := range works {
				if w.Date.Before(e.Date) && !e.Start.Before(*w.Start) && !e.Start.After(*w.End) {
					e.Date = w.Date
					break
				}
			}
		}
		out[e.Date] = append(out[e.Date], e)
	}
	return out
}

// subtract returns the parts of s that none of the cuts cover.
func subtract(s interval, cuts []interval) []interval {
	parts := []interval{s}
	for _, c := range cuts {
		var next []interval
		for _, p := range parts {
			if !c.end.After(p.start) || !c.start.Before(p.end) {
				next = append(next, p)
				continue
			}
			if c.start.After(p.start) {
				next = append(next, interval{p.start, c.start})
			}
			if c.end.Before(p.end) {
				next = append(next, interval{c.end, p.end})
			}
		}
		parts = next
	}
	return parts
}

// overlap returns the minutes that s and w share.
func overlap(s, w interval) time.Duration {
	start, end := s.start, s.end
	if w.start.After(start) {
		start = w.start
	}
	if w.end.Before(end) {
		end = w.end
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

// daysOf calls fn with every local date that s touches, from the day before its start (a window that starts the day
// before can reach into it) to the day of its end.
func daysOf(s interval, loc *time.Location, fn func(Date)) {
	last := DateIn(s.end, loc)
	for d := DateIn(s.start, loc).AddDays(-1); !d.After(last); d = d.AddDays(1) {
		fn(d)
	}
}

// windowMinutes returns the minutes of s that fall in the daily clock window from start to end (minutes after local
// midnight; start > end wraps over midnight, such as 20:00 to 06:00).
func windowMinutes(s interval, loc *time.Location, start, end int) time.Duration {
	var total time.Duration
	daysOf(s, loc, func(d Date) {
		from := d.At(start, loc)
		to := d.At(end, loc)
		if end <= start {
			to = d.AddDays(1).At(end, loc)
		}
		total += overlap(s, interval{from, to})
	})
	return total
}

// dayMinutes returns the minutes of s that fall on local dates for which match is true.
func dayMinutes(s interval, loc *time.Location, match func(Date) bool) time.Duration {
	var total time.Duration
	daysOf(s, loc, func(d Date) {
		if match(d) {
			total += overlap(s, interval{d.At(0, loc), d.AddDays(1).At(0, loc)})
		}
	})
	return total
}
