// SPDX-License-Identifier: MIT

package web

import (
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// employerData is the settings page of an employer: its data, its mode over time, its schedules, pay rules and
// projects. For a managed employer only the organization's administrators change the settings (core.MayChangeSettings).
type employerData struct {
	Employer    core.Employer
	Owner       string // the person whose employer it is; differs from the viewer when an administrator edits a member's
	OwnerName   string
	Org         string
	CanChange   bool
	Today       core.Date
	Mode        core.Mode
	Modes       []core.Mode
	ModeChanges []core.ModeChange
	Schedules   []scheduleView
	PayRules    []core.PayRule
	AbsencePays []core.AbsencePay
	Surcharges  []core.Surcharge
	Types       []core.AbsenceType
	Projects    []core.Project
	Kinds       []core.EmployerKind
	Weekdays    []int
	Bases       []core.PayBasis
	// NoticeApplies: the employer is German (core.LeaveNoticeApplies), so the page shows its notices on lapsing
	// leave; CanNotice: the viewer stands for the employer and records them (core.MayRecordLeaveNotice).
	NoticeApplies bool
	CanNotice     bool
	Notices       []core.LeaveNotice
	Back          string
}

type scheduleView struct {
	core.Schedule
	Hours [7]string
	Week  int
}

// weekdays in the order of a German week, Monday first.
var weekdays = []int{1, 2, 3, 4, 5, 6, 0}

// employerTarget finds the employer of a settings address and checks who acts: the person on a private employer, an
// administrator of the organization on a managed one, and the person reads the settings of their managed employer.
// The actor it returns decides what the viewer may change (core.MayChangeSettings, core.MayRecordLeaveNotice).
func (h *handler) employerTarget(w http.ResponseWriter, r *http.Request) (core.Catalog, core.Employer, string, core.Actor, bool) {
	ctx := r.Context()
	account := accountFrom(ctx)
	owner := r.URL.Query().Get("owner")
	if owner == "" {
		owner = r.PostFormValue("owner")
	}
	if owner == "" {
		owner = account.ID
	}
	cat, err := h.options.Store.Catalog(ctx, owner)
	if err != nil {
		h.failed(w, r, err)
		return cat, core.Employer{}, owner, core.Actor{}, false
	}
	emp, found := cat.Employer(r.PathValue("id"))
	if !found {
		h.notFound(w, r)
		return cat, emp, owner, core.Actor{}, false
	}
	memberships, err := h.options.Store.Memberships(ctx, emp.Organization, account.ID)
	if err != nil {
		h.failed(w, r, err)
		return cat, emp, owner, core.Actor{}, false
	}
	actor := core.ActorFor(account.ID, emp.Organization, memberships, h.today(&emp))
	// Someone else's employer: only an administrator of its organization, never a private one.
	if owner != account.ID && !core.MayChangeSettings(emp, actor) {
		h.notFound(w, r)
		return cat, emp, owner, actor, false
	}
	return cat, emp, owner, actor, true
}

func (h *handler) employerPage(w http.ResponseWriter, r *http.Request) {
	cat, emp, owner, actor, ok := h.employerTarget(w, r)
	if !ok {
		return
	}
	// An administrator reading a member's settings: into the member's access log.
	if err := h.logAccess(r.Context(), accountFrom(r.Context()).ID, emp.Organization, owner, emp.ID, core.AccessRead, "employer settings",
		core.Date{}, core.Date{}); err != nil {
		h.failed(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "employer", view{Title: emp.Name, Data: h.employerData(r, cat, emp, owner, actor)})
}

func (h *handler) employerData(r *http.Request, cat core.Catalog, emp core.Employer, owner string, actor core.Actor) employerData {
	today := h.today(&emp)
	d := employerData{Employer: emp, Owner: owner, CanChange: core.MayChangeSettings(emp, actor),
		NoticeApplies: core.LeaveNoticeApplies(emp), CanNotice: core.MayRecordLeaveNotice(emp, actor), Today: today, Mode: core.ModeAt(cat, emp.ID, today),
		Modes: core.Modes, Weekdays: weekdays, Projects: projectsOf(cat, emp.ID),
		Kinds: []core.EmployerKind{core.KindEmployer, core.KindClient, core.KindPrivate},
		Bases: []core.PayBasis{core.PayHourly, core.PayPerProject, core.PayPerAssignment, core.PayFlatPeriod,
			core.PayStandbyPassive, core.PayStandbyActive},
		Back: "/employers/" + emp.ID}
	if owner != accountFrom(r.Context()).ID {
		d.Back += "?owner=" + owner
		if a, err := h.directory().Account(r.Context(), owner); err == nil {
			d.OwnerName = a.DisplayName
		}
	}
	if emp.Managed() {
		if org, err := h.options.Store.Organization(r.Context(), emp.Organization); err == nil {
			d.Org = org.Name
		}
	}
	for _, m := range cat.ModeChanges {
		if m.EmployerID == emp.ID {
			d.ModeChanges = append(d.ModeChanges, m)
		}
	}
	slices.SortFunc(d.ModeChanges, func(a, b core.ModeChange) int { return a.ValidFrom.Compare(b.ValidFrom) })
	for _, s := range cat.Schedules {
		if s.EmployerID != emp.ID {
			continue
		}
		v := scheduleView{Schedule: s, Week: s.WeekMinutes()}
		for i, plan := range s.Days {
			v.Hours[i] = formatMinutes(plan.Minutes)
		}
		d.Schedules = append(d.Schedules, v)
	}
	slices.SortFunc(d.Schedules, func(a, b scheduleView) int { return b.ValidFrom.Compare(a.ValidFrom) })
	for _, p := range cat.PayRules {
		if p.EmployerID == emp.ID {
			d.PayRules = append(d.PayRules, p)
		}
	}
	slices.SortFunc(d.PayRules, func(a, b core.PayRule) int { return b.ValidFrom.Compare(a.ValidFrom) })
	for _, p := range cat.AbsencePays {
		if p.EmployerID == emp.ID {
			d.AbsencePays = append(d.AbsencePays, p)
		}
	}
	slices.SortFunc(d.AbsencePays, func(a, b core.AbsencePay) int { return b.ValidFrom.Compare(a.ValidFrom) })
	for _, s := range cat.Surcharges {
		if s.EmployerID == emp.ID {
			d.Surcharges = append(d.Surcharges, s)
		}
	}
	slices.SortFunc(d.Surcharges, func(a, b core.Surcharge) int { return b.ValidFrom.Compare(a.ValidFrom) })
	d.Types = cat.AbsenceTypesFor(emp.ID)
	for i, t := range d.Types {
		d.Types[i].Name = h.absenceName(r, t)
	}
	for _, n := range cat.LeaveNotices {
		if n.EmployerID == emp.ID {
			d.Notices = append(d.Notices, n)
		}
	}
	slices.SortFunc(d.Notices, func(a, b core.LeaveNotice) int { return b.NotifiedAt.Compare(a.NotifiedAt) })
	return d
}

// createEmployer makes a private employer of the person, with its mode from today on.
func (h *handler) createEmployer(w http.ResponseWriter, r *http.Request) {
	account := accountFrom(r.Context())
	emp := core.Employer{ID: core.NewID(), Owner: account.ID, Name: strings.TrimSpace(r.PostFormValue("name")),
		Kind: core.EmployerKind(r.PostFormValue("kind")), Holder: core.HolderPrivate, Mode: core.Mode(r.PostFormValue("mode")),
		TimeZone: core.DefaultTimeZone, Region: r.PostFormValue("region"), WeekStart: time.Monday}
	if err := emp.Validate(); err != nil {
		h.back(w, r, err, "")
		return
	}
	if err := h.options.Store.Put(r.Context(), emp); err != nil {
		h.back(w, r, err, "")
		return
	}
	// A schedule from the form, when the mode needs one: hours per weekday from the start form.
	if minutes, err := parseHours(r.PostFormValue("weekday_hours")); err == nil && minutes > 0 {
		s := core.Schedule{ID: core.NewID(), Owner: account.ID, EmployerID: emp.ID, ValidFrom: h.today(&emp).StartOfMonth()}
		for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
			s.Days[d].Minutes = minutes
		}
		if days, err := strconv.ParseFloat(strings.ReplaceAll(r.PostFormValue("leave_days"), ",", "."), 64); err == nil && days >= 0 {
			s.LeaveDays = days
		}
		if err := h.options.Store.Put(r.Context(), s); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	h.rememberEmployer(w, r, emp.ID)
	http.Redirect(w, r, "/?e="+emp.ID+"&done=created", http.StatusSeeOther)
}

// changeEmployer acts on the settings of an employer; each action checks MayChangeSettings first.
func (h *handler) changeEmployer(w http.ResponseWriter, r *http.Request) {
	cat, emp, owner, actor, ok := h.employerTarget(w, r)
	if !ok {
		return
	}
	// The notice on lapsing leave has a right of its own; everything else is a setting.
	allowed := core.MayChangeSettings(emp, actor)
	if r.PathValue("action") == "notice" {
		allowed = core.MayRecordLeaveNotice(emp, actor)
	}
	if !allowed {
		h.back(w, r, errForbidden, "")
		return
	}
	ctx := r.Context()
	var err error
	switch r.PathValue("action") {
	case "details":
		emp.Name = strings.TrimSpace(r.PostFormValue("name"))
		emp.Kind = core.EmployerKind(r.PostFormValue("kind"))
		emp.Region = r.PostFormValue("region")
		emp.DaySplit = core.DaySplit(r.PostFormValue("day_split"))
		emp.StandbyWork = core.StandbyWork(r.PostFormValue("standby_work"))
		if emp.StandbyWork == core.StandbyWorkActive {
			emp.StandbyWork = "" // the default
		}
		if tz := strings.TrimSpace(r.PostFormValue("time_zone")); tz != "" {
			emp.TimeZone = tz
		}
		if err = emp.Validate(); err == nil {
			err = h.options.Store.Put(ctx, emp)
		}
	case "mode":
		var from core.Date
		if from, err = formDate(r, "from"); err == nil {
			m := core.ModeChange{ID: core.NewID(), Owner: owner, EmployerID: emp.ID, ValidFrom: from, Mode: core.Mode(r.PostFormValue("mode"))}
			if err = m.Validate(); err == nil {
				err = h.options.Store.Put(ctx, m)
			}
		}
	case "schedule":
		var s core.Schedule
		if s, err = scheduleFromForm(r, owner, emp.ID); err == nil {
			err = h.options.Store.Put(ctx, s)
		}
	case "pay":
		var p core.PayRule
		if p, err = payRuleFromForm(r, owner, emp.ID); err == nil {
			err = h.options.Store.Put(ctx, p)
		}
	case "absence-pay":
		var p core.AbsencePay
		if p, err = absencePayFromForm(r, owner, emp.ID); err == nil {
			err = h.options.Store.Put(ctx, p)
		}
	case "surcharge":
		var s core.Surcharge
		if s, err = surchargeFromForm(r, owner, emp.ID); err == nil {
			err = h.options.Store.Put(ctx, s)
		}
	case "project":
		p := core.Project{ID: core.NewID(), Owner: owner, EmployerID: emp.ID, Name: strings.TrimSpace(r.PostFormValue("name")),
			Billable: r.PostFormValue("billable") == "on"}
		if err = p.Validate(); err == nil {
			err = h.options.Store.Put(ctx, p)
		}
	case "archive-project":
		for _, p := range cat.Projects {
			if p.ID == r.PostFormValue("project") && p.EmployerID == emp.ID {
				p.Archived = true
				err = h.options.Store.Put(ctx, p)
			}
		}
	case "delete":
		var kind core.Kind
		switch r.PostFormValue("kind") {
		case "schedule":
			kind = core.RecSchedule
		case "pay":
			kind = core.RecPayRule
		case "mode":
			kind = core.RecModeChange
		case "absence-pay":
			kind = core.RecAbsencePay
		case "surcharge":
			kind = core.RecSurcharge
		default:
			h.notFound(w, r)
			return
		}
		err = h.options.Store.Delete(ctx, kind, owner, r.PostFormValue("record"))
	case "notice":
		var n core.LeaveNotice
		if n, err = h.noticeFromForm(r, owner, emp); err == nil {
			err = h.options.Store.Put(ctx, n)
		}
	case "archive":
		if emp.Managed() {
			err = errForbidden // a membership ends in the organization, not here
		} else {
			emp.Archived = true
			err = h.options.Store.Put(ctx, emp)
		}
		if err == nil {
			http.Redirect(w, r, "/?done=archived", http.StatusSeeOther)
			return
		}
	default:
		h.notFound(w, r)
		return
	}
	if err == nil {
		// An administrator changing a member's settings: into the member's access log.
		err = h.logAccess(ctx, accountFrom(ctx).ID, emp.Organization, owner, emp.ID, core.AccessChange,
			"employer settings: "+r.PathValue("action"), core.Date{}, core.Date{})
	}
	h.back(w, r, err, "saved")
}

// noticeFromForm reads the employer's notice on lapsing leave: the year it is about (this year if empty), the day it
// was given (not after today) and its wording. The day is kept as noon UTC, so it reads as the same day everywhere.
func (h *handler) noticeFromForm(r *http.Request, owner string, emp core.Employer) (core.LeaveNotice, error) {
	day, err := formDate(r, "notified")
	if err != nil {
		return core.LeaveNotice{}, err
	}
	today := h.today(&emp)
	if day.After(today) {
		return core.LeaveNotice{}, fmt.Errorf("%w: a notice given after today", core.ErrInvalid)
	}
	year := today.Year
	if raw := strings.TrimSpace(r.PostFormValue("year")); raw != "" {
		if year, err = strconv.Atoi(raw); err != nil {
			return core.LeaveNotice{}, core.ErrInvalid
		}
	}
	n := core.LeaveNotice{ID: core.NewID(), Owner: owner, EmployerID: emp.ID, Year: year,
		NotifiedAt: time.Date(day.Year, day.Month, day.Day, 12, 0, 0, 0, time.UTC), Text: strings.TrimSpace(r.PostFormValue("text"))}
	return n, n.Validate()
}

// scheduleFromForm reads hours per weekday, optional start, end and break, the overtime basis and the leave days.
func scheduleFromForm(r *http.Request, owner, employerID string) (core.Schedule, error) {
	from, err := formDate(r, "from")
	if err != nil {
		return core.Schedule{}, err
	}
	s := core.Schedule{ID: core.NewID(), Owner: owner, EmployerID: employerID, ValidFrom: from,
		OvertimeBasis: core.OvertimeBasis(r.PostFormValue("basis"))}
	for _, day := range weekdays {
		suffix := strconv.Itoa(day)
		minutes, err := parseHours(r.PostFormValue("hours-" + suffix))
		if err != nil || minutes < 0 {
			return s, core.ErrInvalid
		}
		s.Days[day].Minutes = minutes
		start, okStart := parseClock(r.PostFormValue("start-" + suffix))
		end, okEnd := parseClock(r.PostFormValue("end-" + suffix))
		if okStart && okEnd {
			s.Days[day].Start, s.Days[day].End = &start, &end
			if pause, err := parseHours(r.PostFormValue("break-" + suffix)); err == nil && pause > 0 {
				s.Days[day].Break = pause
			}
		}
	}
	if raw := strings.TrimSpace(r.PostFormValue("leave_days")); raw != "" {
		days, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || days < 0 || math.IsNaN(days) {
			return s, core.ErrInvalid
		}
		s.LeaveDays = days
	}
	return s, s.Validate()
}

// payRuleFromForm reads a pay rule; the amount (and the amount for half a day) is typed in the currency (12,50) and
// stored in cents.
func payRuleFromForm(r *http.Request, owner, employerID string) (core.PayRule, error) {
	from, err := formDate(r, "from")
	if err != nil {
		return core.PayRule{}, err
	}
	amount, err := parseMoney(r.PostFormValue("amount"))
	if err != nil {
		return core.PayRule{}, core.ErrInvalid
	}
	p := core.PayRule{ID: core.NewID(), Owner: owner, EmployerID: employerID, ProjectID: r.PostFormValue("project"), ValidFrom: from,
		Basis: core.PayBasis(r.PostFormValue("basis")), Amount: amount, Currency: strings.ToUpper(strings.TrimSpace(r.PostFormValue("currency"))),
		Unit: r.PostFormValue("unit")}
	if p.Currency == "" {
		p.Currency = "EUR"
	}
	if raw := strings.TrimSpace(r.PostFormValue("half_amount")); raw != "" {
		if p.HalfAmount, err = parseMoney(raw); err != nil {
			return p, core.ErrInvalid
		}
	}
	if raw := r.PostFormValue("salaries"); raw != "" {
		if p.Salaries, err = strconv.Atoi(raw); err != nil {
			return p, core.ErrInvalid
		}
	}
	if raw := strings.TrimSpace(r.PostFormValue("to")); raw != "" {
		if p.ValidTo, err = core.ParseDate(raw); err != nil {
			return p, err
		}
	}
	return p, p.Validate()
}

// absencePayFromForm reads a rule that pays an absence type (or public holidays): at the target minutes or at the
// average of the reference weeks.
func absencePayFromForm(r *http.Request, owner, employerID string) (core.AbsencePay, error) {
	from, err := formDate(r, "from")
	if err != nil {
		return core.AbsencePay{}, err
	}
	p := core.AbsencePay{ID: core.NewID(), Owner: owner, EmployerID: employerID, Type: r.PostFormValue("type"),
		Method: core.AbsencePayMethod(r.PostFormValue("method")), ValidFrom: from}
	if raw := strings.TrimSpace(r.PostFormValue("weeks")); raw != "" && p.Method == core.PayAverage {
		if p.Weeks, err = strconv.Atoi(raw); err != nil {
			return p, core.ErrInvalid
		}
	}
	return p, p.Validate()
}

// surchargeFromForm reads a surcharge: the kind, the percentage (25 or 12,5), the night window and an hourly base for
// salaried staff.
func surchargeFromForm(r *http.Request, owner, employerID string) (core.Surcharge, error) {
	from, err := formDate(r, "from")
	if err != nil {
		return core.Surcharge{}, err
	}
	percent, err := parseMoney(r.PostFormValue("percent")) // two decimals: 25,5 % are 2550 basis points
	if err != nil {
		return core.Surcharge{}, core.ErrInvalid
	}
	s := core.Surcharge{ID: core.NewID(), Owner: owner, EmployerID: employerID, Kind: core.SurchargeKind(r.PostFormValue("kind")),
		ValidFrom: from, BasisPoints: int(percent)}
	if s.Kind == core.SurchargeNight {
		start, okStart := parseClock(r.PostFormValue("window_start"))
		end, okEnd := parseClock(r.PostFormValue("window_end"))
		if !okStart || !okEnd {
			return s, core.ErrInvalid
		}
		s.WindowStart, s.WindowEnd = start, end
	}
	if raw := strings.TrimSpace(r.PostFormValue("base")); raw != "" {
		if s.BaseAmount, err = parseMoney(raw); err != nil {
			return s, core.ErrInvalid
		}
		s.Currency = strings.ToUpper(strings.TrimSpace(r.PostFormValue("currency")))
		if s.Currency == "" {
			s.Currency = "EUR"
		}
	}
	return s, s.Validate()
}

// parseMoney reads an amount such as 12,50 or 1200 into cents.
func parseMoney(input string) (int64, error) {
	s := strings.TrimSpace(strings.ReplaceAll(input, " ", ""))
	if s == "" {
		return 0, core.ErrInvalid
	}
	whole, fraction, _ := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || units < 0 || units > 100_000_000 || len(fraction) > 2 {
		return 0, core.ErrInvalid
	}
	cents := int64(0)
	if fraction != "" {
		if len(fraction) == 1 {
			fraction += "0"
		}
		if cents, err = strconv.ParseInt(fraction, 10, 64); err != nil {
			return 0, core.ErrInvalid
		}
	}
	return units*100 + cents, nil
}

// formatMoney writes cents as an amount with two decimals and the currency.
func formatMoney(lang string, cents int64, currency string) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	separator := "."
	if lang == "de" {
		separator = ","
	}
	return sign + strconv.FormatInt(cents/100, 10) + separator + leftPad(int(cents%100)) + " " + currency
}
