// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"slices"
	"time"
)

// Day is the result of one day of one employer. All values are minutes.
type Day struct {
	Date       Date   `json:"date"`
	EmployerID string `json:"employer_id"`
	Mode       Mode   `json:"mode"`
	// HasTarget: the mode has a target on this day, so Balance means something.
	HasTarget bool `json:"has_target"`
	// Target is the target of the schedule for the weekday, before holidays and absences.
	Target int `json:"target"`
	// Holiday names the public or own holiday of the day.
	Holiday string `json:"holiday,omitempty"`
	// AdjustedTarget is the target after holidays and absences that reduce it.
	AdjustedTarget int `json:"adjusted_target"`
	// Worked is work minus breaks plus deviations; in the mode plan the schedule's time unless work was booked.
	Worked int `json:"worked"`
	Breaks int `json:"breaks"`
	// Credited is absent time that counts as worked (absence types that fill the target).
	Credited int `json:"credited"`
	// Actual is Worked plus Credited.
	Actual int `json:"actual"`
	// Balance is Actual minus AdjustedTarget, zero in modes without a target.
	Balance int `json:"balance"`
	// FromPlan: Worked comes from the schedule (mode plan), not from entries.
	FromPlan bool `json:"from_plan,omitempty"`
	// Confirmed: the person confirmed the day (mode plan).
	Confirmed bool `json:"confirmed,omitempty"`
	// Absences lists the absence type keys of the day.
	Absences []string `json:"absences,omitempty"`
	// Projects holds the booked minutes per project ID ("" for none), work minus nothing.
	Projects map[string]int `json:"projects,omitempty"`
}

// employerView bundles what the day calculation of one employer needs, sorted once.
type employerView struct {
	emp       Employer
	modes     []ModeChange
	schedules []Schedule
	holidays  *holidayCalendar
	types     []AbsenceType
	confirms  []Confirmation
	entries   map[Date][]Entry
	absences  []Absence
}

func newEmployerView(d Data, employerID string) (*employerView, error) {
	emp, ok := d.Catalog.Employer(employerID)
	if !ok {
		return nil, fmt.Errorf("employer %q: %w", employerID, ErrNotFound)
	}
	v := &employerView{emp: emp, holidays: newHolidayCalendar(d.Catalog, emp), types: d.Catalog.AbsenceTypesFor(employerID)}
	for _, m := range d.Catalog.ModeChanges {
		if m.EmployerID == employerID {
			v.modes = append(v.modes, m)
		}
	}
	slices.SortStableFunc(v.modes, func(a, b ModeChange) int { return a.ValidFrom.Compare(b.ValidFrom) })
	for _, s := range d.Catalog.Schedules {
		if s.EmployerID == employerID {
			v.schedules = append(v.schedules, s)
		}
	}
	slices.SortStableFunc(v.schedules, func(a, b Schedule) int { return a.ValidFrom.Compare(b.ValidFrom) })
	for _, c := range d.Catalog.Confirmations {
		if c.EmployerID == employerID {
			v.confirms = append(v.confirms, c)
		}
	}
	v.entries = entriesByDay(d, emp)
	for _, a := range d.Absences {
		if appliesTo(emp, a.EmployerID) && a.DeletedAt == nil && a.Status == StatusApproved {
			v.absences = append(v.absences, a)
		}
	}
	slices.SortStableFunc(v.absences, func(a, b Absence) int { return a.From.Compare(b.From) })
	return v, nil
}

// ModeAt returns the mode of the employer on the date.
func ModeAt(cat Catalog, employerID string, d Date) Mode {
	emp, _ := cat.Employer(employerID)
	mode, best := emp.Mode, Date{}
	for _, m := range cat.ModeChanges {
		if m.EmployerID == employerID && !m.ValidFrom.After(d) && !m.ValidFrom.Before(best) {
			mode, best = m.Mode, m.ValidFrom
		}
	}
	return mode
}

func (v *employerView) modeAt(d Date) Mode {
	mode := v.emp.Mode
	for _, m := range v.modes {
		if m.ValidFrom.After(d) {
			break
		}
		mode = m.Mode
	}
	return mode
}

// ScheduleAt returns the schedule valid on the date, if any.
func ScheduleAt(cat Catalog, employerID string, d Date) (Schedule, bool) {
	var out Schedule
	found := false
	for _, s := range cat.Schedules {
		if s.EmployerID == employerID && !s.ValidFrom.After(d) && (!found || !s.ValidFrom.Before(out.ValidFrom)) {
			out, found = s, true
		}
	}
	return out, found
}

func (v *employerView) scheduleAt(d Date) (Schedule, bool) {
	var out Schedule
	found := false
	for _, s := range v.schedules {
		if s.ValidFrom.After(d) {
			break
		}
		out, found = s, true
	}
	return out, found
}

func (v *employerView) confirmed(d Date) bool {
	for _, c := range v.confirms {
		if d.Within(c.From, c.To) {
			return true
		}
	}
	return false
}

// absentPart is the share of one day (in thousandths) that one absence takes.
type absentPart struct {
	absence  Absence
	typ      AbsenceType
	fraction int
}

// absentParts returns the absences of the day with their fractions, capped at a full day in the order the absences
// start.
func (v *employerView) absentParts(d Date) []absentPart {
	var out []absentPart
	left := 1000
	for _, a := range v.absences {
		if !d.Within(a.From, a.To) || left == 0 {
			continue
		}
		t, ok := findAbsenceType(v.types, a.Type)
		if !ok {
			continue
		}
		f := 1000
		if a.HalfDay {
			f = 500
		}
		f = min(f, left)
		left -= f
		out = append(out, absentPart{a, t, f})
	}
	return out
}

// absent returns the absence fractions of the day in thousandths, by effect, and the type keys.
func (v *employerView) absent(d Date) (fills, reduces, info int, keys []string) {
	for _, p := range v.absentParts(d) {
		switch p.typ.Effect {
		case FillsTarget:
			fills += p.fraction
		case ReducesTarget:
			reduces += p.fraction
		default:
			info += p.fraction
		}
		keys = append(keys, p.absence.Type)
	}
	return fills, reduces, info, keys
}

func part(minutes, thousandths int) int { return (minutes*thousandths + 500) / 1000 }

// day calculates one day; see Day for the meaning of the fields.
func (v *employerView) day(d Date) Day {
	mode := v.modeAt(d)
	out := Day{Date: d, EmployerID: v.emp.ID, Mode: mode, HasTarget: mode.HasTarget()}

	var work, breaks, adjust int
	booked := false
	for _, e := range v.entries[d] {
		switch e.Kind {
		case EntryWork:
			work += e.Minutes
			booked = true
			if out.Projects == nil {
				out.Projects = map[string]int{}
			}
			out.Projects[e.ProjectID] += e.Minutes
		case EntryBreak:
			breaks += e.Minutes
		case EntryAdjust:
			adjust += e.Minutes
		}
	}
	out.Breaks = breaks

	name, holiday := v.holidays.on(d)
	out.Holiday = name
	fills, reduces, info, keys := v.absent(d)
	out.Absences = keys

	if !mode.HasTarget() {
		out.Worked = max(0, work-breaks+adjust)
		out.Actual = out.Worked
		return out
	}

	sched, ok := v.scheduleAt(d)
	if ok {
		out.Target = sched.Days[d.Weekday()].Minutes
	}
	available := out.Target - part(out.Target, holiday)
	out.AdjustedTarget = available - part(available, reduces)
	out.Credited = part(available, fills)

	switch {
	case mode == ModePlan && !booked:
		// The plan counts as worked, less every absent part of the day.
		planned := available - part(available, min(1000, fills+reduces+info))
		out.Worked = max(0, planned-breaks+adjust)
		out.FromPlan = planned > 0
		out.Confirmed = v.confirmed(d)
	default:
		out.Worked = max(0, work-breaks+adjust)
		if mode == ModePlan {
			out.Confirmed = v.confirmed(d)
		}
	}
	out.Actual = out.Worked + out.Credited
	out.Balance = out.Actual - out.AdjustedTarget
	return out
}

// Days calculates every day from from to to for one employer.
func Days(d Data, employerID string, from, to Date) ([]Day, error) {
	if to.Before(from) {
		return nil, invalid("range ends before it starts")
	}
	v, err := newEmployerView(d, employerID)
	if err != nil {
		return nil, err
	}
	var out []Day
	for day := from; !day.After(to); day = day.AddDays(1) {
		out = append(out, v.day(day))
	}
	return out, nil
}

// Period is the length of a total.
type Period string

const (
	PeriodDay   Period = "day"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
	PeriodYear  Period = "year"
)

// Total sums days. Balance only sums days with a target.
type Total struct {
	From           Date           `json:"from"`
	To             Date           `json:"to"`
	Target         int            `json:"target"`
	AdjustedTarget int            `json:"adjusted_target"`
	Worked         int            `json:"worked"`
	Breaks         int            `json:"breaks"`
	Credited       int            `json:"credited"`
	Actual         int            `json:"actual"`
	Balance        int            `json:"balance"`
	Days           int            `json:"days"`
	Projects       map[string]int `json:"projects,omitempty"`
}

func (t *Total) add(d Day) {
	if t.Days == 0 || d.Date.Before(t.From) {
		t.From = d.Date
	}
	if t.Days == 0 || d.Date.After(t.To) {
		t.To = d.Date
	}
	t.Days++
	t.Target += d.Target
	t.AdjustedTarget += d.AdjustedTarget
	t.Worked += d.Worked
	t.Breaks += d.Breaks
	t.Credited += d.Credited
	t.Actual += d.Actual
	if d.HasTarget {
		t.Balance += d.Balance
	}
	for p, m := range d.Projects {
		if t.Projects == nil {
			t.Projects = map[string]int{}
		}
		t.Projects[p] += m
	}
}

// Sum adds up days.
func Sum(days []Day) Total {
	var t Total
	for _, d := range days {
		t.add(d)
	}
	return t
}

// periodStart returns the first day of the period that holds d.
func periodStart(d Date, p Period, weekStart time.Weekday) Date {
	switch p {
	case PeriodWeek:
		return d.StartOfWeek(weekStart)
	case PeriodMonth:
		return d.StartOfMonth()
	case PeriodYear:
		return NewDate(d.Year, time.January, 1)
	}
	return d
}

// Totals groups days by period, in order. Weeks start on weekStart; a period cut by the range only sums the days in it.
func Totals(days []Day, p Period, weekStart time.Weekday) []Total {
	var out []Total
	var key Date
	for _, d := range days {
		k := periodStart(d.Date, p, weekStart)
		if len(out) == 0 || k != key {
			out = append(out, Total{})
			key = k
		}
		out[len(out)-1].add(d)
	}
	return out
}
