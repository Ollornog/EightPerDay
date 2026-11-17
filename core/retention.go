// SPDX-License-Identifier: MIT

package core

import "time"

// Class is a deletion class: data with the same retention.
type Class string

const (
	// ClassTimeRecords: entries and absences with their correction history. German law asks for at least two years
	// (§ 16 Abs. 2 ArbZG, § 17 MiLoG); the default of three years after the end of the year also covers the regular
	// limitation period.
	ClassTimeRecords Class = "time_records"
	// ClassLeaveNotices: the notices about lapsing leave, which decide whether leave lapses.
	ClassLeaveNotices Class = "leave_notices"
)

// Retention says how long a class is kept: YearsAfterYearEnd full calendar years after the end of the year a record
// belongs to. This is a product default, not legal advice: whoever runs the software sets it (tax and commercial law
// can ask for longer).
type Retention struct {
	Class             Class `json:"class"`
	YearsAfterYearEnd int   `json:"years_after_year_end"`
}

// MinimumYears is the lower bound for time records.
const MinimumYears = 2

// DefaultRetention returns the default classes: three years for both.
func DefaultRetention() []Retention {
	return []Retention{
		{Class: ClassTimeRecords, YearsAfterYearEnd: 3},
		{Class: ClassLeaveNotices, YearsAfterYearEnd: 3},
	}
}

// Validate checks the retention: a known class, and at least MinimumYears.
func (r Retention) Validate() error {
	switch r.Class {
	case ClassTimeRecords, ClassLeaveNotices:
	default:
		return invalid("deletion class %q", r.Class)
	}
	if r.YearsAfterYearEnd < MinimumYears || r.YearsAfterYearEnd > 100 {
		return invalid("retention of %d years: at least %d", r.YearsAfterYearEnd, MinimumYears)
	}
	return nil
}

// Cutoff returns the first date that must still be kept on the day today: records dated before it may be purged.
// With three years, on 2026-11-17 everything of 2022 and earlier may go (its year ended more than three years ago),
// so the cutoff is 2023-01-01.
func (r Retention) Cutoff(today Date) Date {
	return NewDate(today.Year-r.YearsAfterYearEnd, time.January, 1)
}
