// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/web/auth"
)

// homeData is the week of one employer, in the view its mode asks for.
type homeData struct {
	Employers   []core.Employer
	Employer    core.Employer
	Org         string // the name of the organization of a managed employer
	Mode        core.Mode
	Modes       []core.Mode
	Today       core.Date
	Week        core.Date
	WeekEnd     core.Date
	Prev, Next  core.Date
	ThisWeek    bool
	Days        []dayView
	Total       core.Total
	HasTarget   bool
	HasSchedule bool
	Account     core.OvertimeAccount
	Leave       core.LeaveAccount
	HasLeave    bool
	Confirmed   bool // every plan day of the week up to today is confirmed
	PlanDays    int  // plan days up to today
	WeekMinutes int  // week_sum: the booked total of the week
	Recent      []core.Total
	Projects    []projectRow
	AllProjects []core.Project
	Absences    []absenceView
	Types       []core.AbsenceType
	Timer       *timerView
	CanSettings bool
	Back        string
	// Approval: the organization of the managed employer approves periods and leave.
	Approval    bool
	Submissions []submissionView
	// WeekLocked: the week is handed in or approved; its records cannot change.
	WeekLocked bool
}

type dayView struct {
	core.Day
	Weekday  int
	IsToday  bool
	Future   bool
	Entries  []entryView
	Absences []string
	// Input is the hours typed for the day in the mode schedule (the manual day value).
	Input string
	// Locked: the day lies in a period that is handed in or approved.
	Locked bool
}

type entryView struct {
	ID       string
	Version  int
	Kind     core.EntryKind
	Start    string
	End      string
	Minutes  int
	Project  string
	Note     string
	Tags     string
	FromPlan bool
}

type projectRow struct {
	Project core.Project
	Minutes int
	Input   string
}

type absenceView struct {
	core.Absence
	Name string
}

type timerView struct {
	auth.Timer
	Project string
	Elapsed int // minutes so far
	Since   string
}

func (h *handler) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	emp, ok := h.pickEmployer(w, r, cat)
	if !ok {
		h.render(w, r, http.StatusOK, "start", view{Title: h.t(r, "home.title"), Data: newEmployerData(r)})
		return
	}
	data, err := h.week(r, cat, *emp)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "home", view{Title: h.t(r, "home.title"), Data: data})
}

// week gathers what the week view of an employer shows. The numbers all come from the core library.
func (h *handler) week(r *http.Request, cat core.Catalog, emp core.Employer) (homeData, error) {
	ctx := r.Context()
	account := accountFrom(ctx)
	today := h.today(&emp)
	week := weekOf(r, emp, today)
	end := week.AddDays(6)
	d := homeData{
		Employers: activeEmployers(cat), Employer: emp, Today: today, Week: week, WeekEnd: end,
		Prev: week.AddDays(-7), Next: week.AddDays(7), ThisWeek: today.Within(week, end),
		Mode: core.ModeAt(cat, emp.ID, week), Modes: core.Modes, Types: cat.AbsenceTypesFor(emp.ID),
		AllProjects: projectsOf(cat, emp.ID),
		Back:        "/?e=" + emp.ID + "&w=" + week.String(),
	}
	if d.ThisWeek {
		d.Mode = core.ModeAt(cat, emp.ID, today)
	}
	if emp.Managed() {
		org, err := h.options.Store.Organization(ctx, emp.Organization)
		if err == nil {
			d.Org = org.Name
		}
	}
	memberships, err := h.options.Store.Memberships(ctx, emp.Organization, account.ID)
	if err != nil {
		return d, err
	}
	d.CanSettings = core.MayChangeSettings(emp, core.ActorFor(account.ID, emp.Organization, memberships, today))

	// One load covers the week, the weeks before it, the year for the leave account and the time account since the
	// first schedule.
	from := core.NewDate(week.Year, time.January, 1)
	start := from
	for _, s := range cat.Schedules {
		if s.EmployerID == emp.ID {
			d.HasSchedule = true
			if s.ValidFrom.Before(start) {
				start = s.ValidFrom
			}
		}
	}
	from = minDate(start, minDate(from, week.AddDays(-7*8)))
	to := core.NewDate(end.Year, time.December, 31)
	data, err := core.Load(ctx, h.options.Store, account.ID, from, to)
	if err != nil {
		return d, err
	}
	days, err := core.Days(data, emp.ID, week, end)
	if err != nil {
		return d, err
	}
	// The balance of a week counts up to today: a day that has not come yet is not short of anything.
	var sofar []core.Day
	for _, day := range days {
		if !day.Date.After(today) {
			sofar = append(sofar, day)
		}
	}
	d.Total = core.Sum(sofar)
	byDay := map[core.Date][]entryView{}
	manual := map[core.Date]int{}
	for _, e := range data.Entries {
		if e.EmployerID != emp.ID || e.DeletedAt != nil || !e.Date.Within(week, end) {
			continue
		}
		byDay[e.Date] = append(byDay[e.Date], h.entryView(cat, e, emp))
		if e.Precision == core.PrecisionDay && e.Kind == core.EntryWork && e.Source == core.SourceManual && e.Start == nil && e.ProjectID == "" {
			manual[e.Date] += e.Minutes
		}
		if e.Precision == core.PrecisionWeek && e.Kind == core.EntryWork {
			d.WeekMinutes += e.Minutes
		}
	}
	names := map[string]string{}
	for i, t := range d.Types {
		names[t.Key] = h.absenceName(r, t)
		d.Types[i].Name = names[t.Key]
	}
	for _, day := range days {
		v := dayView{Day: day, Weekday: int(day.Date.Weekday()), IsToday: day.Date == today, Future: day.Date.After(today),
			Entries: byDay[day.Date], Locked: cat.SubmittedLocked(emp.ID, day.Date)}
		if v.Locked {
			d.WeekLocked = true
		}
		if minutes := manual[day.Date]; minutes > 0 {
			v.Input = formatMinutes(minutes)
		}
		for _, key := range day.Absences {
			v.Absences = append(v.Absences, names[key])
		}
		if day.HasTarget {
			d.HasTarget = true
		}
		if day.FromPlan && !day.Date.After(today) {
			d.PlanDays++
		}
		d.Days = append(d.Days, v)
	}
	d.Confirmed = d.PlanDays > 0
	for _, day := range days {
		if day.FromPlan && !day.Date.After(today) && !day.Confirmed {
			d.Confirmed = false
		}
	}
	for _, a := range data.Absences {
		if a.DeletedAt == nil && (a.EmployerID == emp.ID || (a.EmployerID == "" && !emp.Managed())) &&
			!a.To.Before(week) && !a.From.After(end) {
			d.Absences = append(d.Absences, absenceView{Absence: a, Name: names[a.Type]})
		}
	}
	if _, on, err := h.approvalOrg(ctx, emp); err != nil {
		return d, err
	} else if on {
		d.Approval = true
		d.Submissions = submissionsOf(cat, emp.ID, names, data.Absences)
	}
	if d.HasSchedule {
		if d.Account, err = core.Overtime(data, emp.ID, start, minDate(today, end)); err != nil {
			return d, err
		}
		if d.Leave, err = core.Leave(data, emp.ID, week.Year); err != nil {
			return d, err
		}
		d.HasLeave = d.Leave.Entitlement > 0 || d.Leave.Taken > 0
	}
	if d.Mode == core.ModeWeekSum {
		earlier, err := core.Days(data, emp.ID, week.AddDays(-7*8), week.AddDays(-1))
		if err != nil {
			return d, err
		}
		d.Recent = core.Totals(earlier, core.PeriodWeek, emp.WeekStart)
		slices.Reverse(d.Recent)
	}
	if d.Mode == core.ModeProjectsOnly || d.Mode == core.ModeExact {
		for _, p := range d.AllProjects {
			minutes := d.Total.Projects[p.ID]
			row := projectRow{Project: p, Minutes: minutes}
			if minutes > 0 {
				row.Input = formatMinutes(minutes)
			}
			d.Projects = append(d.Projects, row)
		}
	}
	if timer, err := h.options.Auth.RunningTimer(ctx, account.ID); err != nil {
		return d, err
	} else if timer != nil {
		loc, _ := emp.Location()
		d.Timer = &timerView{Timer: *timer, Project: projectName(cat, timer.ProjectID),
			Elapsed: int(h.now().Sub(timer.Started).Minutes()), Since: timer.Started.In(loc).Format("15:04")}
	}
	return d, nil
}

func (h *handler) entryView(cat core.Catalog, e core.Entry, emp core.Employer) entryView {
	v := entryView{ID: e.ID, Version: e.Version, Kind: e.Kind, Minutes: e.Minutes, Project: projectName(cat, e.ProjectID),
		Note: e.Note, Tags: strings.Join(e.Tags, " "), FromPlan: e.Source == core.SourcePlan}
	loc, err := emp.Location()
	if err != nil {
		loc = time.UTC
	}
	if e.Start != nil && e.End != nil {
		v.Start, v.End = e.Start.In(loc).Format("15:04"), e.End.In(loc).Format("15:04")
	}
	return v
}

func minDate(a, b core.Date) core.Date {
	if a.Before(b) {
		return a
	}
	return b
}

// newEmployer is the form for a new employer, also the start page of a new account.
type newEmployer struct {
	Modes []core.Mode
	Kinds []core.EmployerKind
}

func newEmployerData(*http.Request) newEmployer {
	return newEmployer{Modes: core.Modes, Kinds: []core.EmployerKind{core.KindEmployer, core.KindClient, core.KindPrivate}}
}

// absenceName is the name of an absence type in the language of the page: the built-in ones are translated, one's
// own keep the name they were given.
func (h *handler) absenceName(r *http.Request, t core.AbsenceType) string {
	if text, ok := h.catalog[h.lang(r)]["absence."+t.Key]; ok {
		return text
	}
	return t.Name
}
