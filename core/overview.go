// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Actor is who acts on the data: a person, or an administrator of an organization.
type Actor struct {
	Person       string
	Organization string
	// Admin: the person administers time tracking of Organization.
	Admin bool
}

// MayChangeSettings reports whether the actor may change the employer and its settings (mode, schedule, pay rules,
// holidays, period closes): the owner for a private employer, an administrator of the organization for a managed one.
// The store does not know who acts; the embedding application asks this before it calls Put or Delete.
func MayChangeSettings(e Employer, a Actor) bool {
	if e.Managed() {
		return a.Admin && a.Organization != "" && a.Organization == e.Organization
	}
	return a.Person != "" && a.Person == e.Owner
}

// MayBook reports whether the actor may book or correct entries and absences of the employer: only its person, for
// both holders. A managed employer keeps every correction in the history.
func MayBook(e Employer, a Actor) bool { return a.Person != "" && a.Person == e.Owner }

// MayErase reports whether the actor may erase the employer's records: only the person, and only private ones.
// Managed records leave only through the retention purge of their organization.
func MayErase(e Employer, a Actor) bool { return !e.Managed() && a.Person != "" && a.Person == e.Owner }

// HintCode names a hint of the personal overview.
type HintCode string

const (
	// HintDayOver10h: the work of all employments on one working day is more than ten hours (§ 3 ArbZG).
	HintDayOver10h HintCode = "day_over_10h"
	// HintWeekOver48h: more than 48 hours in a week; allowed only if it evens out to eight hours per working day
	// within six months (§ 3 ArbZG).
	HintWeekOver48h HintCode = "week_over_48h"
	// HintRestUnder11h: less than eleven hours between the end of one working day and the start of the next (§ 5 ArbZG).
	HintRestUnder11h HintCode = "rest_under_11h"
)

// Hint is a note for the person, never for an organization.
type Hint struct {
	Code    HintCode `json:"code"`
	Date    Date     `json:"date"`
	Minutes int      `json:"minutes"`
	Text    string   `json:"text"`
}

// OverviewDay is one day over all employers of the person.
type OverviewDay struct {
	Date Date `json:"date"`
	// Worked sums all employers. Employment sums only those of kind employer, which German law adds up (§ 2 Abs. 1
	// ArbZG), and only where the time is known per day (not in the mode week_sum).
	Worked      int            `json:"worked"`
	Employment  int            `json:"employment"`
	PerEmployer map[string]int `json:"per_employer"`
}

// Overview is the person's own view over all their employers, managed and private.
type Overview struct {
	Days  []OverviewDay `json:"days"`
	Weeks []Total       `json:"weeks"`
	Hints []Hint        `json:"hints"`
}

// ErrNotPersonal: the overview was asked for data of more than one person. It is for the person alone.
var ErrNotPersonal = fmt.Errorf("%w: the overview is for one person only", ErrInvalid)

// PersonalOverview sums the days of every employer of one person and adds hints where the work of all employments
// together goes past the limits of the German Working Hours Act: hours of several employers count together (§ 2 Abs. 1
// ArbZG). Only employers of kind employer count for the hints; clients and private work do not fall under the act.
// Days of the mode week_sum carry no daily hint, as their time is not known per day. The hints are information for the
// person; they are not legal advice and must never be shown to an organization, so the overview refuses data of more
// than one owner (such as the result of Store.Managed).
func PersonalOverview(d Data, from, to Date, weekStart time.Weekday) (Overview, error) {
	if len(d.Catalog.Employers) == 0 {
		return Overview{}, nil
	}
	person := d.Catalog.Employers[0].Owner
	for _, e := range d.Catalog.Employers {
		if e.Owner != person {
			return Overview{}, ErrNotPersonal
		}
	}
	ids := make([]string, 0, len(d.Catalog.Employers))
	for _, e := range d.Catalog.Employers {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)

	byDate := map[Date]*OverviewDay{}
	var out Overview
	for _, date := range DaysIn(from, to) {
		out.Days = append(out.Days, OverviewDay{Date: date, PerEmployer: map[string]int{}})
	}
	for i := range out.Days {
		byDate[out.Days[i].Date] = &out.Days[i]
	}
	dailyKnown := map[Date]bool{}
	var all []Day
	for _, id := range ids {
		emp, _ := d.Catalog.Employer(id)
		days, err := Days(d, id, from, to)
		if err != nil {
			return Overview{}, err
		}
		for _, day := range days {
			o := byDate[day.Date]
			o.Worked += day.Worked
			o.PerEmployer[id] = day.Worked
			if emp.Kind == KindEmployer && day.Mode != ModeWeekSum {
				o.Employment += day.Worked
				dailyKnown[day.Date] = true
			}
			all = append(all, Day{Date: day.Date, Worked: boolInt(emp.Kind == KindEmployer) * day.Worked})
		}
	}
	for _, o := range out.Days {
		if dailyKnown[o.Date] && o.Employment > 10*60 {
			out.Hints = append(out.Hints, Hint{Code: HintDayOver10h, Date: o.Date, Minutes: o.Employment,
				Text: "More than 10 hours of work for your employers on this day (§ 3 ArbZG)."})
		}
	}
	slices.SortStableFunc(all, func(a, b Day) int { return a.Date.Compare(b.Date) })
	for _, w := range Totals(all, PeriodWeek, weekStart) {
		total := Total{From: w.From, To: w.To, Days: w.From.DaysUntil(w.To) + 1}
		for _, o := range out.Days {
			if o.Date.Within(w.From, w.To) {
				total.Worked += o.Worked
			}
		}
		out.Weeks = append(out.Weeks, total)
		if w.Worked > 48*60 {
			out.Hints = append(out.Hints, Hint{Code: HintWeekOver48h, Date: w.From, Minutes: w.Worked,
				Text: "More than 48 hours for your employers this week; on average it must stay at 8 hours per working day (§ 3 ArbZG)."})
		}
	}
	out.Hints = append(out.Hints, restHints(d, from, to)...)
	return out, nil
}

// restHints compares the last end of a day with the first start of the next, over the spans of all employments.
func restHints(d Data, from, to Date) []Hint {
	type span struct{ start, end time.Time }
	byDate := map[Date][]span{}
	for _, e := range d.Entries {
		emp, ok := d.Catalog.Employer(e.EmployerID)
		if !ok || emp.Kind != KindEmployer || e.Kind != EntryWork || e.DeletedAt != nil || e.Start == nil || e.End == nil {
			continue
		}
		byDate[e.Date] = append(byDate[e.Date], span{*e.Start, *e.End})
	}
	var out []Hint
	for date := from; date.Before(to); date = date.AddDays(1) {
		today, next := byDate[date], byDate[date.AddDays(1)]
		if len(today) == 0 || len(next) == 0 {
			continue
		}
		lastEnd, firstStart := today[0].end, next[0].start
		for _, s := range today {
			if s.end.After(lastEnd) {
				lastEnd = s.end
			}
		}
		for _, s := range next {
			if s.start.Before(firstStart) {
				firstStart = s.start
			}
		}
		if rest := firstStart.Sub(lastEnd); rest < 11*time.Hour {
			out = append(out, Hint{Code: HintRestUnder11h, Date: date.AddDays(1), Minutes: int(rest.Minutes()),
				Text: "Less than 11 hours of rest before this working day (§ 5 ArbZG)."})
		}
	}
	return out
}

// PrivatePart returns the export without anything of managed employers: what the person takes along when leaving.
// The managed records stay with the organization; the person's full export (ExportOwner) still shows them, as a copy.
func (x Export) PrivatePart() Export {
	managed := map[string]bool{}
	for _, e := range x.Catalog.Employers {
		if e.Managed() {
			managed[e.ID] = true
		}
	}
	var out Export
	out.Format, out.ExportedAt, out.Owner = x.Format, x.ExportedAt, x.Owner
	for _, r := range catalogRecords(x.Catalog) {
		if !managed[r.Meta().EmployerID] {
			out.Catalog.Add(r)
		}
	}
	kept := map[string]bool{}
	for _, e := range x.Entries {
		if !managed[e.EmployerID] {
			out.Entries = append(out.Entries, e)
			kept[e.ID] = true
		}
	}
	for _, a := range x.Absences {
		if !managed[a.EmployerID] {
			out.Absences = append(out.Absences, a)
			kept[a.ID] = true
		}
	}
	for _, r := range x.Revisions {
		if kept[r.RecordID] {
			out.Revisions = append(out.Revisions, r)
		}
	}
	return out
}

func catalogRecords(c Catalog) []Record {
	var out []Record
	add := func(r Record) { out = append(out, r) }
	for _, v := range c.Employers {
		add(v)
	}
	for _, v := range c.ModeChanges {
		add(v)
	}
	for _, v := range c.Schedules {
		add(v)
	}
	for _, v := range c.PayRules {
		add(v)
	}
	for _, v := range c.Clients {
		add(v)
	}
	for _, v := range c.Projects {
		add(v)
	}
	for _, v := range c.Activities {
		add(v)
	}
	for _, v := range c.Tags {
		add(v)
	}
	for _, v := range c.AbsenceTypes {
		add(v)
	}
	for _, v := range c.Confirmations {
		add(v)
	}
	for _, v := range c.Adjustments {
		add(v)
	}
	for _, v := range c.PeriodCloses {
		add(v)
	}
	for _, v := range c.Holidays {
		add(v)
	}
	for _, v := range c.LeaveNotices {
		add(v)
	}
	for _, v := range c.AbsencePays {
		add(v)
	}
	for _, v := range c.Surcharges {
		add(v)
	}
	for _, v := range c.Submissions {
		add(v)
	}
	for _, v := range c.Incomes {
		add(v)
	}
	for _, v := range c.DayInfos {
		add(v)
	}
	return out
}

// ImportPrivate takes over the private part of an export into a store under the owner, for moving to another
// instance. Catalog records keep their IDs; entries and absences are created anew (their history starts with the
// import, the old history stays in the export file). Managed records are never imported.
func ImportPrivate(ctx context.Context, s Store, x Export, owner string) error {
	p := x.PrivatePart()
	change := Change{By: owner, Reason: "import"}
	// Closed periods last: before them, the entries of the period can still be written.
	records := catalogRecords(p.Catalog)
	slices.SortStableFunc(records, func(a, b Record) int {
		return boolInt(a.Meta().Kind == RecPeriodClose) - boolInt(b.Meta().Kind == RecPeriodClose)
	})
	var closes []Record
	for _, r := range records {
		if r.Meta().Kind == RecPeriodClose {
			closes = append(closes, r)
			continue
		}
		if err := s.Put(ctx, withOwner(r, owner)); err != nil {
			return fmt.Errorf("import %s %s: %w", r.Meta().Kind, r.Meta().ID, err)
		}
	}
	for _, e := range p.Entries {
		if e.DeletedAt != nil {
			continue
		}
		e.Owner, e.Version, e.Source = owner, 0, SourceImport
		if _, err := s.CreateEntry(ctx, e, change); err != nil {
			return fmt.Errorf("import entry %s: %w", e.ID, err)
		}
	}
	for _, a := range p.Absences {
		if a.DeletedAt != nil {
			continue
		}
		a.Owner, a.Version = owner, 0
		if _, err := s.CreateAbsence(ctx, a, change); err != nil {
			return fmt.Errorf("import absence %s: %w", a.ID, err)
		}
	}
	for _, r := range closes {
		if err := s.Put(ctx, withOwner(r, owner)); err != nil {
			return fmt.Errorf("import period close %s: %w", r.Meta().ID, err)
		}
	}
	return nil
}

func withOwner(r Record, owner string) Record {
	switch v := r.(type) {
	case Employer:
		v.Owner = owner
		return v
	case ModeChange:
		v.Owner = owner
		return v
	case Schedule:
		v.Owner = owner
		return v
	case PayRule:
		v.Owner = owner
		return v
	case Client:
		v.Owner = owner
		return v
	case Project:
		v.Owner = owner
		return v
	case Activity:
		v.Owner = owner
		return v
	case Tag:
		v.Owner = owner
		return v
	case AbsenceType:
		v.Owner = owner
		return v
	case Confirmation:
		v.Owner = owner
		return v
	case Adjustment:
		v.Owner = owner
		return v
	case PeriodClose:
		v.Owner = owner
		return v
	case Holiday:
		v.Owner = owner
		return v
	case LeaveNotice:
		v.Owner = owner
		return v
	case AbsencePay:
		v.Owner = owner
		return v
	case Surcharge:
		v.Owner = owner
		return v
	case Submission:
		v.Owner = owner
		return v
	case Income:
		v.Owner = owner
		return v
	case DayInfo:
		v.Owner = owner
		return v
	}
	return r
}
