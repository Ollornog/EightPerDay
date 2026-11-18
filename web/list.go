// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// The start page is a list of days and weeks, the way people keep their hours in a spreadsheet: one number per day
// typed into its row, a short code instead of it for leave, sickness or a day off, and the balance carried from week
// to week (core.Ledger). Everything else of a mode lives on the week page (/week).

// listData is the list of one employer around one week.
type listData struct {
	Employers []core.Employer
	Employer  core.Employer
	Org       string
	Mode      core.Mode
	Today     core.Date
	// Weeks are the week before the anchor week and the anchor week, in this order.
	Weeks      []listWeek
	Prev, Next core.Date
	ThisWeek   bool
	Gaps       []gapView
	HasTarget  bool
	HasAccount bool
	Account    int
	Leave      core.LeaveAccount
	HasLeave   bool
	CanCorrect bool
	Back       string
	Month      monthGrid
	Year       yearGrid
	// Corrections of the time account, the newest first.
	Corrections []core.Adjustment
}

// listWeekView hands a week and its list to the template of a week.
type listWeekView struct {
	Week listWeek
	List listData
}

var days31 = func() []int {
	out := make([]int, 31)
	for i := range out {
		out[i] = i + 1
	}
	return out
}()

type listWeek struct {
	core.LedgerWeek
	Number int // the ISO week
	Rows   []listRow
	Locked bool
}

type listRow struct {
	Date    core.Date
	Day     core.Day
	Weekday int
	IsToday bool
	// Counted: the day lies up to today, so its difference counts.
	Counted bool
	Weekend bool
	// Value is what the field holds: the hours typed for the day, or the code of an absence booked from the row.
	Value       string
	Placeholder string
	Note        string
	// Code is the short code of what the day holds besides work: leave, sickness, a public holiday.
	Code string
	// Fixed: the day cannot be typed over in the row (handed in, or an absence of several days or of every employer).
	Fixed bool
	Focus bool
}

type gapView struct {
	core.WeekGap
	Number       int
	MissingCount int
}

type monthGrid struct {
	Month time.Month
	Year  int
	Weeks [][]gridCell
}

type yearGrid struct {
	Year   int
	Months []yearMonth
}

type yearMonth struct {
	Month time.Month
	Days  []gridCell
}

type gridCell struct {
	Date    core.Date
	Day     int
	In      bool // the cell belongs to the month (the month grid also shows days of the months around it)
	Code    string
	Hours   string
	Weekend bool
	Today   bool
}

// list shows the start page: the list of the chosen employer, or the start form without one.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
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
	data, err := h.listOf(r, cat, *emp)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "list", view{Title: h.t(r, "list.title"), Data: data, Wide: true})
}

// accountStart is the day an employer's time account starts: its first schedule, or from when nothing counts.
func accountStart(cat core.Catalog, employerID string, from core.Date) (core.Date, bool) {
	start, found := from, false
	for _, s := range cat.Schedules {
		if s.EmployerID == employerID && (!found || s.ValidFrom.Before(start)) {
			start, found = s.ValidFrom, true
		}
	}
	return start, found
}

func (h *handler) listOf(r *http.Request, cat core.Catalog, emp core.Employer) (listData, error) {
	ctx := r.Context()
	account := accountFrom(ctx)
	today := h.today(&emp)
	anchor := weekOf(r, emp, today)
	prev := anchor.AddDays(-7)
	end := anchor.AddDays(6)
	d := listData{Employers: activeEmployers(cat), Employer: emp, Today: today, Prev: prev, Next: anchor.AddDays(7),
		ThisWeek: today.Within(anchor, end), Mode: core.ModeAt(cat, emp.ID, minDate(today, end)),
		Back: "/?e=" + emp.ID + "&w=" + anchor.String()}
	if emp.Managed() {
		if org, err := h.options.Store.Organization(ctx, emp.Organization); err == nil {
			d.Org = org.Name
		}
	}
	memberships, err := h.options.Store.Memberships(ctx, emp.Organization, account.ID)
	if err != nil {
		return d, err
	}
	d.CanCorrect = core.MayChangeSettings(emp, core.ActorFor(account.ID, emp.Organization, memberships, today))
	for _, a := range cat.Adjustments {
		if a.EmployerID == emp.ID && a.Kind == core.AdjustOvertime {
			d.Corrections = append(d.Corrections, a)
		}
	}
	slices.SortStableFunc(d.Corrections, func(a, b core.Adjustment) int { return b.Date.Compare(a.Date) })

	gridYear := anchor.Year
	if d.ThisWeek {
		gridYear = today.Year
	}
	gapsFrom := today.AddDays(-7 * 8)
	start, hasSchedule := accountStart(cat, emp.ID, prev)
	from := minDate(minDate(start, prev), minDate(gapsFrom, core.NewDate(min(gridYear, today.Year), time.January, 1)))
	to := core.NewDate(max(gridYear, today.Year), time.December, 31)
	if end.After(to) {
		to = end
	}
	data, err := core.Load(ctx, h.options.Store, account.ID, from, to)
	if err != nil {
		return d, err
	}
	weeks, err := core.Ledger(data, emp.ID, start, prev, anchor, today)
	if err != nil {
		return d, err
	}
	codes := h.dayCodes(r, cat, emp, data)
	manual, notes := manualDays(data, emp.ID)
	focused := false
	for i, wk := range weeks {
		_, number := toTime(wk.From).ISOWeek()
		lw := listWeek{LedgerWeek: wk, Number: number}
		for _, day := range wk.Days {
			row := listRow{Date: day.Date, Day: day, Weekday: int(day.Date.Weekday()), IsToday: day.Date == today,
				Counted: !day.Date.After(today), Weekend: day.Date.Weekday() == time.Saturday || day.Date.Weekday() == time.Sunday}
			if day.HasTarget {
				d.HasTarget = true
			}
			c := codes[day.Date]
			row.Code = c.text
			row.Note = notes[day.Date]
			switch {
			case cat.SubmittedLocked(emp.ID, day.Date):
				row.Fixed, lw.Locked = true, true
				row.Value = h.dayText(r, manual[day.Date], c)
			case c.fixed:
				row.Fixed = true
				row.Value = c.text
			case manual[day.Date] > 0:
				row.Value = hoursText(h.lang(r), manual[day.Date])
			case c.replaceable:
				row.Value = c.text
				row.Note = c.note
			}
			if row.Value == "" && day.Actual > 0 {
				row.Placeholder = hoursText(h.lang(r), day.Actual)
			} else if row.Value == "" && c.text != "" {
				row.Placeholder = c.text
			}
			working := day.Target > 0 || (!day.HasTarget && !row.Weekend)
			if i == len(weeks)-1 && !focused && working && !row.Fixed && row.Value == "" && day.Holiday == "" && day.Actual == 0 {
				row.Focus, focused = true, true
			}
			lw.Rows = append(lw.Rows, row)
		}
		d.Weeks = append(d.Weeks, lw)
	}

	_, approval, err := h.approvalOrg(ctx, emp)
	if err != nil {
		return d, err
	}
	gaps, err := core.WeekGaps(data, emp.ID, start, gapsFrom, today, approval)
	if err != nil {
		return d, err
	}
	for _, g := range gaps {
		_, number := toTime(g.From).ISOWeek()
		d.Gaps = append(d.Gaps, gapView{WeekGap: g, Number: number, MissingCount: len(g.Missing)})
	}
	if hasSchedule {
		d.HasAccount = true
		acc, err := core.Overtime(data, emp.ID, start, today)
		if err != nil {
			return d, err
		}
		d.Account = acc.Balance
		if d.Leave, err = core.Leave(data, emp.ID, today.Year); err != nil {
			return d, err
		}
		d.HasLeave = d.Leave.Entitlement > 0 || d.Leave.Taken > 0
	}
	if d.Month, d.Year, err = h.grids(r, data, emp, codes, gridYear, anchor, today); err != nil {
		return d, err
	}
	return d, nil
}

// dayText is what a handed-in day shows instead of its field.
func (h *handler) dayText(r *http.Request, minutes int, c dayCode) string {
	if minutes > 0 {
		return hoursText(h.lang(r), minutes)
	}
	return c.text
}

// manualDays returns the hours typed for a day (a manual day entry without span or project) and their notes.
func manualDays(data core.Data, employerID string) (map[core.Date]int, map[core.Date]string) {
	minutes, notes := map[core.Date]int{}, map[core.Date]string{}
	for _, e := range data.Entries {
		if isManualDay(e, employerID) {
			minutes[e.Date] += e.Minutes
			if e.Note != "" {
				notes[e.Date] = e.Note
			}
		}
	}
	return minutes, notes
}

func isManualDay(e core.Entry, employerID string) bool {
	return e.EmployerID == employerID && e.DeletedAt == nil && e.Kind == core.EntryWork && e.Precision == core.PrecisionDay &&
		e.Source == core.SourceManual && e.Start == nil && e.ProjectID == ""
}

// dayCode is the short code of a day: what it shows, and whether the row may replace it.
type dayCode struct {
	text string
	note string
	// replaceable: one approved absence of this employer for this day alone, of a type with a code, which the row
	// booked or could have.
	replaceable bool
	// fixed: an absence the row cannot replace (several days, every employer, a type without a code, or requested).
	fixed bool
}

// dayCodes gives the short code of every day that has an absence or a public holiday.
func (h *handler) dayCodes(r *http.Request, cat core.Catalog, emp core.Employer, data core.Data) map[core.Date]dayCode {
	out := map[core.Date]dayCode{}
	types := cat.AbsenceTypesFor(emp.ID)
	for _, a := range data.Absences {
		if a.DeletedAt != nil || a.Status == core.StatusRejected || !(a.EmployerID == emp.ID || (a.EmployerID == "" && !emp.Managed())) {
			continue
		}
		text := h.absenceCode(r, types, a.Type)
		if a.HalfDay {
			text = "½" + text
		}
		if a.Status == core.StatusRequested {
			text += "?"
		}
		single := a.From == a.To && a.EmployerID == emp.ID && core.CodedAbsence(a.Type) && a.Status == core.StatusApproved
		for day := a.From; !day.After(a.To); day = day.AddDays(1) {
			c := out[day]
			if c.text != "" {
				c.text += " "
			}
			c.text += text
			if single {
				c.replaceable, c.note = true, a.Note
			} else {
				c.fixed = true
			}
			out[day] = c
		}
	}
	return out
}

// absenceCode is the short code of an absence type in the language of the page; an own type shows its first letter.
func (h *handler) absenceCode(r *http.Request, types []core.AbsenceType, key string) string {
	if text, ok := h.catalog[h.lang(r)]["code."+key]; ok {
		return text
	}
	for _, t := range types {
		if t.Key == key && t.Name != "" {
			first, _ := firstRune(t.Name)
			return strings.ToUpper(first)
		}
	}
	return "?"
}

func firstRune(s string) (string, bool) {
	for _, c := range s {
		return string(c), true
	}
	return "", false
}

// grids builds the month of the anchor week (or of today) and the whole year as grids of codes.
func (h *handler) grids(r *http.Request, data core.Data, emp core.Employer, codes map[core.Date]dayCode, year int, anchor,
	today core.Date) (monthGrid, yearGrid, error) {
	first, last := core.NewDate(year, time.January, 1), core.NewDate(year, time.December, 31)
	days, err := core.Days(data, emp.ID, first, last)
	if err != nil {
		return monthGrid{}, yearGrid{}, err
	}
	lang := h.lang(r)
	cells := map[core.Date]gridCell{}
	for _, day := range days {
		c := gridCell{Date: day.Date, Day: day.Date.Day, In: true, Code: codes[day.Date].text, Today: day.Date == today,
			Weekend: day.Date.Weekday() == time.Saturday || day.Date.Weekday() == time.Sunday}
		if day.Holiday != "" && c.Code == "" {
			c.Code = h.t(r, "code.public")
		}
		if day.Actual > 0 && day.Worked > 0 {
			c.Hours = hoursText(lang, day.Worked)
		}
		cells[day.Date] = c
	}
	y := yearGrid{Year: year}
	for m := time.January; m <= time.December; m++ {
		ym := yearMonth{Month: m}
		for day := core.NewDate(year, m, 1); day.Month == m; day = day.AddDays(1) {
			ym.Days = append(ym.Days, cells[day])
		}
		y.Months = append(y.Months, ym)
	}
	month := anchor
	if today.Within(anchor, anchor.AddDays(6)) {
		month = today
	}
	if month.Year != year {
		month = core.NewDate(year, month.Month, 1)
	}
	mg := monthGrid{Month: month.Month, Year: year}
	start := month.StartOfMonth().StartOfWeek(emp.WeekStart)
	for day := start; day.Month == month.Month || day.Before(month.StartOfMonth()); day = day.AddDays(7) {
		var row []gridCell
		for i := range 7 {
			cell, ok := cells[day.AddDays(i)]
			if !ok {
				cell = gridCell{Date: day.AddDays(i), Day: day.AddDays(i).Day}
			}
			cell.In = cell.Date.Month == month.Month
			row = append(row, cell)
		}
		mg.Weeks = append(mg.Weeks, row)
	}
	return mg, y, nil
}

// decimalHours writes minutes as hours the way they are typed: 8,5 or 7,75 where that is exact, 7:40 where not.
func hoursText(lang string, minutes int) string {
	sign := ""
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	if minutes%3 != 0 {
		return sign + formatMinutes(minutes)
	}
	text := strconv.FormatFloat(float64(minutes)/60, 'f', -1, 64)
	if lang == "de" {
		text = strings.ReplaceAll(text, ".", ",")
	}
	return sign + text
}

// signedDecimal writes a difference with its sign, such as +0,5 or -1; zero has none.
func signedDecimal(lang string, minutes int) string {
	if minutes > 0 {
		return "+" + hoursText(lang, minutes)
	}
	return hoursText(lang, minutes)
}

// dayValue is what a day's field holds: hours, a short code, or nothing.
type dayValue struct {
	minutes int
	code    *core.DayCode
}

// parseDayValue reads a day's field: hours without a sign (8, 8,5, 7:30, 7h30) or a short code (U, K, F, ½U).
func parseDayValue(input string) (dayValue, error) {
	s := strings.TrimSpace(input)
	if code, ok := core.ParseDayCode(s); ok {
		return dayValue{code: &code}, nil
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return dayValue{}, core.ErrInvalid
	}
	minutes, err := parseHours(s)
	if err != nil || minutes > 24*60 {
		return dayValue{}, core.ErrInvalid
	}
	return dayValue{minutes: minutes}, nil
}

// halfMark and dayCodePattern follow core.ParseDayCode; a test keeps dayPattern in step with parseDayValue.
const (
	halfMark       = `(?:½|/2|0[.,]5|[.,]5)`
	codeLetters    = `(?:[uUvVkKsSfFoOcC]|[zZ][aA])`
	dayCodePattern = `(?:` + halfMark + `\s*` + codeLetters + `|` + codeLetters + `(?:\s*` + halfMark + `)?)`
	// dayPattern is the pattern of a day's field in the list: hours without a sign, or a short code.
	dayPattern = `\s*(?:` + durationBody + `|` + dayCodePattern + `)\s*`
)

// listDays saves the rows of a week of the list: hours, a code, or nothing, and the notes. Only rows whose field
// changed are touched: a week saved again books nothing twice, and a day changed elsewhere since the page was loaded
// keeps that change unless its row was typed over.
func (h *handler) listDays(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.back(w, r, err, "")
		return
	}
	for key, values := range r.PostForm {
		raw, found := strings.CutPrefix(key, "v-")
		if !found || len(values) != 1 {
			continue
		}
		date, err := core.ParseDate(raw)
		if err != nil {
			continue
		}
		value, note := values[0], strings.TrimSpace(r.PostFormValue("n-"+raw))
		if strings.TrimSpace(value) == strings.TrimSpace(r.PostFormValue("was-"+raw)) && note == strings.TrimSpace(r.PostFormValue("wasn-"+raw)) {
			continue
		}
		parsed, err := parseDayValue(value)
		if err != nil {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "list.error.value", value))
			return
		}
		if parsed.code == nil && parsed.minutes == 0 && note != "" {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "list.error.note"))
			return
		}
		if err := h.setDay(r, cat, emp, date, parsed, note); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	h.back(w, r, nil, "saved")
}

// setDay makes a day hold what its row says (the store refuses a day handed in for approval): the hours as the one manual entry of the day, or the code as an absence
// of the day alone; whatever the row booked before and no longer says goes, through the history.
func (h *handler) setDay(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, v dayValue, note string) error {
	ctx := r.Context()
	owner := accountFrom(ctx).ID
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: owner, EmployerID: emp.ID, From: date, To: date})
	if err != nil {
		return err
	}
	absences, err := h.options.Store.Absences(ctx, core.AbsenceFilter{Owner: owner, EmployerID: emp.ID, From: date, To: date})
	if err != nil {
		return err
	}
	keep := ""
	for _, a := range absences {
		if a.DeletedAt != nil || a.EmployerID != emp.ID || a.From != date || a.To != date || !core.CodedAbsence(a.Type) ||
			a.Status != core.StatusApproved {
			continue
		}
		if v.code != nil && keep == "" && a.Type == v.code.Absence && a.HalfDay == v.code.HalfDay {
			keep = a.ID
			if a.Note != note && !healthType(cat, emp, a.Type) {
				a.Note = note
				if _, err := h.options.Store.UpdateAbsence(ctx, a, change(r, "")); err != nil {
					return err
				}
			}
			continue
		}
		if err := h.options.Store.DeleteAbsence(ctx, owner, a.ID, a.Version, change(r, "")); err != nil {
			return err
		}
	}
	minutes := v.minutes
	if v.code != nil {
		minutes = 0
	}
	if err := h.setManualDay(r, cat, emp, date, entries, minutes, note); err != nil {
		return err
	}
	if v.code == nil || keep != "" {
		return nil
	}
	a := core.Absence{Owner: owner, EmployerID: emp.ID, Type: v.code.Absence, From: date, To: date, HalfDay: v.code.HalfDay}
	if !healthType(cat, emp, a.Type) {
		a.Note = note
	}
	return h.createAbsence(r, cat, emp, a)
}

func healthType(cat core.Catalog, emp core.Employer, key string) bool {
	for _, t := range cat.AbsenceTypesFor(emp.ID) {
		if t.Key == key {
			return t.Health
		}
	}
	return false
}

// setManualDay keeps one manual entry for the day with the minutes and the note; zero removes it.
func (h *handler) setManualDay(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, entries []core.Entry, minutes int,
	note string) error {
	ctx := r.Context()
	for _, e := range entries {
		if !isManualDay(e, emp.ID) {
			continue
		}
		if minutes == 0 {
			if err := h.options.Store.DeleteEntry(ctx, e.Owner, e.ID, e.Version, change(r, "")); err != nil {
				return err
			}
			continue
		}
		if e.Minutes != minutes || e.Note != note {
			e.Minutes, e.Note = minutes, note
			if _, err := h.options.Store.UpdateEntry(ctx, e, change(r, "")); err != nil {
				return err
			}
		}
		minutes = 0 // one entry holds the day; any other one goes
	}
	if minutes == 0 {
		return nil
	}
	loc, _ := emp.Location()
	return h.book(r, cat, []core.Entry{{Owner: emp.Owner, EmployerID: emp.ID, Kind: core.EntryWork, Date: date, TimeZone: loc.String(),
		Minutes: minutes, Precision: core.PrecisionDay, Source: core.SourceManual, Note: note}}, "")
}

// addCorrection books a correction of the time account (a payout, a cap) with its reason, for whoever may change the
// employer's settings.
func (h *handler) addCorrection(w http.ResponseWriter, r *http.Request) {
	cat, err := h.ownCatalog(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	emp, ok := cat.Employer(r.PostFormValue("employer"))
	if !ok || emp.Archived {
		h.back(w, r, core.ErrNotFound, "")
		return
	}
	if !h.mayCorrect(r.Context(), emp) {
		h.back(w, r, errForbidden, "")
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	minutes, err := parseHours(r.PostFormValue("minutes"))
	if err != nil || minutes == 0 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
		return
	}
	if r.PostFormValue("direction") == "less" && minutes > 0 {
		minutes = -minutes
	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if reason == "" {
		h.back(w, r, errReasonNeeded, "")
		return
	}
	a := core.Adjustment{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, Date: date, Kind: core.AdjustOvertime,
		Minutes: minutes, Reason: reason}
	h.back(w, r, h.options.Store.Put(r.Context(), a), "corrected")
}

// removeCorrection takes a correction back.
func (h *handler) removeCorrection(w http.ResponseWriter, r *http.Request) {
	cat, err := h.ownCatalog(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	for _, a := range cat.Adjustments {
		if a.ID != r.PathValue("id") {
			continue
		}
		emp, ok := cat.Employer(a.EmployerID)
		if !ok || !h.mayCorrect(r.Context(), emp) {
			h.back(w, r, errForbidden, "")
			return
		}
		h.back(w, r, h.options.Store.Delete(r.Context(), core.RecAdjustment, a.Owner, a.ID), "removed")
		return
	}
	h.back(w, r, core.ErrNotFound, "")
}

func (h *handler) mayCorrect(ctx context.Context, emp core.Employer) bool {
	person := accountFrom(ctx).ID
	memberships, err := h.options.Store.Memberships(ctx, emp.Organization, person)
	if err != nil {
		return false
	}
	return core.MayChangeSettings(emp, core.ActorFor(person, emp.Organization, memberships, h.today(&emp)))
}

// shortDate writes a day without its year: 16.11. or 11-16.
func shortDate(lang string, d core.Date) string {
	if lang == "de" {
		return toTime(d).Format("02.01.")
	}
	return toTime(d).Format("01-02")
}
