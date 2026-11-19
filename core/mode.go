// SPDX-License-Identifier: MIT

package core

import "time"

// MaterializePlan turns the plan days of the employer from from to to into entries, for the switch from the mode plan
// to the mode exact. Only days that the plan fills (Day.FromPlan) get entries, and every day keeps its minutes: with
// start, end and break from the schedule as a span and a break where they add up to the planned minutes, otherwise
// as a day entry. Deviations (adjust entries) stay as they are. The caller stores the result and then adds a
// ModeChange; nothing else changes.
func MaterializePlan(d Data, employerID string, from, to Date) ([]Entry, error) {
	days, err := Days(d, employerID, from, to)
	if err != nil {
		return nil, err
	}
	emp, _ := d.Catalog.Employer(employerID)
	loc, err := emp.Location()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, day := range days {
		if !day.FromPlan {
			continue
		}
		adjust, breaks := 0, 0
		for _, e := range d.Entries {
			if e.EmployerID == employerID && e.Date == day.Date && e.DeletedAt == nil {
				switch e.Kind {
				case EntryAdjust:
					adjust += e.Minutes
				case EntryBreak:
					breaks += e.Minutes
				}
			}
		}
		// The minutes of the plan alone: what the day counted before deviations, booked breaks and standby.
		planned := day.Worked - day.Standby - adjust + breaks // standby stays in DayInfo
		base := Entry{Owner: emp.Owner, EmployerID: employerID, Kind: EntryWork, Date: day.Date, TimeZone: loc.String(),
			Precision: PrecisionDay, Minutes: planned, Source: SourcePlan}
		s, _ := ScheduleAt(d.Catalog, employerID, day.Date)
		plan := s.Days[day.Date.Weekday()]
		if plan.Start != nil && *plan.End-*plan.Start-plan.Break == planned {
			start := day.Date.At(*plan.Start, loc).UTC()
			// The end follows from the minutes, not from the clock: on a daylight saving day the span keeps them.
			end := start.Add(time.Duration(planned+plan.Break) * time.Minute)
			base.Start, base.End, base.Precision, base.Minutes = &start, &end, PrecisionSpan, planned+plan.Break
			out = append(out, base)
			if plan.Break > 0 {
				out = append(out, Entry{Owner: emp.Owner, EmployerID: employerID, Kind: EntryBreak, Date: day.Date,
					TimeZone: loc.String(), Precision: PrecisionDay, Minutes: plan.Break, Source: SourcePlan})
			}
			continue
		}
		out = append(out, base)
	}
	return out, nil
}

// SpreadWeek splits an entry that holds a whole week (precision week) into day entries, in proportion to the target
// of each weekday in the schedule valid at the start of the week (Monday to Friday evenly without one). The sum stays
// the same; the caller stores the parts and deletes the week entry, both through the history.
func SpreadWeek(cat Catalog, e Entry) ([]Entry, error) {
	if e.Precision != PrecisionWeek {
		return nil, invalid("only a week entry can be spread")
	}
	weights := make([]int, 7)
	if s, ok := ScheduleAt(cat, e.EmployerID, e.Date); ok && s.WeekMinutes() > 0 {
		for i := range 7 {
			weights[i] = s.Days[e.Date.AddDays(i).Weekday()].Minutes
		}
	} else {
		for i := range 7 {
			wd := e.Date.AddDays(i).Weekday()
			weights[i] = boolInt(wd != time.Saturday && wd != time.Sunday)
		}
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return nil, invalid("no working day to spread to")
	}
	var out []Entry
	given, acc := 0, 0
	for i, w := range weights {
		if w == 0 {
			continue
		}
		acc += w
		share := (e.Minutes*acc+total/2)/total - given
		given += share
		part := e
		part.ID, part.Version, part.Start, part.End = "", 0, nil, nil
		part.Date, part.Minutes, part.Precision = e.Date.AddDays(i), share, PrecisionDay
		out = append(out, part)
	}
	return out, nil
}
