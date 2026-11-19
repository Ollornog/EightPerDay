// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Ollornog/EightPerDay/core"
)

// Booking a day: the hours typed for it, a short code instead of them, both for half a day of a code, the hours on a
// project, and the change request that a change to an approved week becomes. The sheet (sheet.go) writes through
// these; core.ParseDay reads what was typed.

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

// proposedTextIn is what a change request asks a day to hold, as a day's field writes it.
func (h *handler) proposedTextIn(lang string, types []core.AbsenceType, p core.ProposedDay) string {
	var words []string
	switch {
	case p.Absence != "" && p.HalfDay:
		words = append(words, "½"+h.absenceCodeIn(lang, types, p.Absence))
	case p.Absence != "":
		words = append(words, h.absenceCodeIn(lang, types, p.Absence))
	}
	if p.Minutes > 0 {
		words = append(words, h.hoursOfPartIn(lang, p.Minutes, p.Part))
	}
	if len(words) == 0 {
		return "–"
	}
	return strings.Join(words, " ")
}

// hoursOfPart writes hours with their half of the day as a day's field reads them back: 4 NM, 3,5 AM.
func (h *handler) hoursOfPart(r *http.Request, minutes int, part core.DayPart) string {
	return h.hoursOfPartIn(h.lang(r), minutes, part)
}

func (h *handler) hoursOfPartIn(lang string, minutes int, part core.DayPart) string {
	text := hoursText(lang, minutes)
	if part != "" {
		text += " " + h.catalog[lang]["part."+string(part)]
	}
	return text
}

// dayTextIn is what a handed-in day shows instead of its field.
func (h *handler) dayTextIn(lang string, minutes int, part core.DayPart, c dayCode) string {
	if minutes > 0 {
		return h.hoursOfPartIn(lang, minutes, part)
	}
	return c.text
}

// manualDays returns the hours typed for a day (a manual day entry without span or project) with their half of the
// day, and their notes.
func manualDays(data core.Data, employerID string) (map[core.Date]int, map[core.Date]core.DayPart, map[core.Date]string) {
	minutes, parts, notes := map[core.Date]int{}, map[core.Date]core.DayPart{}, map[core.Date]string{}
	for _, e := range data.Entries {
		if isManualDay(e, employerID) {
			minutes[e.Date] += e.Minutes
			parts[e.Date] = e.Part
			if e.Note != "" {
				notes[e.Date] = e.Note
			}
		}
	}
	return minutes, parts, notes
}

func isManualDay(e core.Entry, employerID string) bool {
	return e.EmployerID == employerID && e.DeletedAt == nil && core.DayTotal(e)
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
	// half: the replaceable absence takes half the day, so the row may hold hours next to it.
	half bool
}

// dayCodes gives the short code of every day that has an absence.
func (h *handler) dayCodes(r *http.Request, cat core.Catalog, emp core.Employer, data core.Data) map[core.Date]dayCode {
	return h.dayCodesIn(h.lang(r), cat, emp, data)
}

func (h *handler) dayCodesIn(lang string, cat core.Catalog, emp core.Employer, data core.Data) map[core.Date]dayCode {
	out := map[core.Date]dayCode{}
	types := cat.AbsenceTypesFor(emp.ID)
	for _, a := range data.Absences {
		if a.DeletedAt != nil || a.Status == core.StatusRejected || !(a.EmployerID == emp.ID || (a.EmployerID == "" && !emp.Managed())) {
			continue
		}
		text := h.absenceCodeIn(lang, types, a.Type)
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
				c.replaceable, c.note, c.half = true, a.Note, a.HalfDay
			} else {
				c.fixed = true
			}
			out[day] = c
		}
	}
	return out
}

// absenceCodeIn is the short code of an absence type in the language of the page; an own type shows its first letter.
func (h *handler) absenceCodeIn(lang string, types []core.AbsenceType, key string) string {
	if text, ok := h.catalog[lang]["code."+key]; ok {
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

// dayValue is what a day's field of an employer holds: hours (with their half of the day and what was typed for them),
// a short code, half a day of a code with hours, or nothing.
type dayValue struct {
	minutes int
	part    core.DayPart
	code    *core.DayCode
	typed   string
}

// proposed is the value of a row as a change request asks for it.
func (v dayValue) proposed(date core.Date, note string) core.ProposedDay {
	p := core.ProposedDay{Date: date, Minutes: v.minutes, Part: v.part, Note: note}
	if v.code != nil {
		p.Absence, p.HalfDay = v.code.Absence, v.code.HalfDay
	}
	return p
}

// requestChange asks the organization to change days of an approved period (a correction with the proposed values,
// core.Submit). An open change request of the period takes the new days in, a day asked for again its new value.
// The approved values count until the organization decides.
func (h *handler) requestChange(r *http.Request, cat core.Catalog, emp core.Employer, period core.Submission, days []core.ProposedDay,
	reason string) error {
	ctx := r.Context()
	org, on, err := h.approvalOrg(ctx, emp)
	if err != nil {
		return err
	}
	if !on {
		return core.ErrLocked
	}
	if open, ok := cat.OpenCorrection(period.ID); ok {
		merged := open.Proposal
		for _, d := range days {
			replaced := false
			for i := range merged {
				if merged[i].Date == d.Date {
					merged[i], replaced = d, true
				}
			}
			if !replaced {
				merged = append(merged, d)
			}
		}
		slices.SortFunc(merged, func(a, b core.ProposedDay) int { return a.Date.Compare(b.Date) })
		open.Proposal, open.Note, open.SubmittedAt = merged, reason, h.now().UTC()
		if err := open.Validate(); err != nil {
			return err
		}
		return h.options.Store.Put(ctx, open)
	}
	slices.SortFunc(days, func(a, b core.ProposedDay) int { return a.Date.Compare(b.Date) })
	s, err := core.Submit(org, core.Data{Catalog: cat}, core.SubmitRequest{Kind: core.SubmitCorrection, EmployerID: emp.ID,
		Supersedes: period.ID, Note: reason, Proposal: days, At: h.now()})
	if err != nil {
		return err
	}
	return h.options.Store.Put(ctx, s)
}

// applyChange books the values of an approved change request on the member's days and approves the period again as
// corrected, in the name of the administrator who decided; the member's access log shows the change.
func (h *handler) applyChange(r *http.Request, org core.Organization, actor core.Actor, emp core.Employer, c core.Submission) error {
	ctx := r.Context()
	data, err := h.options.Store.Managed(ctx, org.ID, c.From, c.To)
	if err != nil {
		return err
	}
	for _, p := range c.Proposal {
		v := dayValue{minutes: p.Minutes, part: p.Part}
		if p.Absence != "" {
			v.code = &core.DayCode{Absence: p.Absence, HalfDay: p.HalfDay}
		}
		if err := h.setDay(r, data.Catalog, emp, p.Date, v, "change request "+c.ID, true); err != nil {
			return err
		}
		if err := h.setDayNote(r, data.Catalog, emp, p.Date, p.Note); err != nil {
			return err
		}
	}
	if data, err = h.options.Store.Managed(ctx, org.ID, c.From, c.To); err != nil {
		return err
	}
	s, err := core.Submit(org, data, core.SubmitRequest{Kind: core.SubmitPeriod, EmployerID: emp.ID, From: c.From, To: c.To,
		Note: c.Note, At: h.now()})
	if err != nil {
		return err
	}
	if s, err = core.Decide(s, emp, actor, true, "", h.now()); err != nil {
		return err
	}
	if err := h.options.Store.Put(ctx, s); err != nil {
		return err
	}
	return h.logAccess(ctx, actor.Person, org.ID, emp.Owner, emp.ID, core.AccessChange, "change request applied", c.From, c.To)
}

// setDay makes an employer's day hold what its field says (the store refuses a day handed in for approval): the hours
// (with their half of the day and what was typed for them) as the one manual entry of the day, the code as an absence
// of the day alone, or both for half a day of a code with hours; whatever the field booked before and no longer says
// goes, through the history. Notes stay where they are (setDayNote). direct books an absence as approved without
// asking (an approved change request).
func (h *handler) setDay(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, v dayValue, reason string,
	direct bool) error {
	ctx := r.Context()
	owner := emp.Owner
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
			continue
		}
		if err := h.options.Store.DeleteAbsence(ctx, owner, a.ID, a.Version, change(r, reason)); err != nil {
			return err
		}
	}
	if err := h.setManualDay(r, cat, emp, date, entries, v, reason); err != nil {
		return err
	}
	if v.code == nil || keep != "" {
		return nil
	}
	a := core.Absence{Owner: owner, EmployerID: emp.ID, Type: v.code.Absence, From: date, To: date, HalfDay: v.code.HalfDay}
	if direct {
		a.Status = core.StatusApproved
		_, err := h.options.Store.CreateAbsence(ctx, a, change(r, reason))
		return err
	}
	return h.createAbsence(r, cat, emp, a)
}

// setManualDay keeps one manual entry for the day with the minutes, their half of the day and what was typed for them;
// zero removes it. Its note stays.
func (h *handler) setManualDay(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, entries []core.Entry, v dayValue,
	reason string) error {
	ctx := r.Context()
	minutes, part, typed := v.minutes, v.part, v.typed
	for _, e := range entries {
		if !isManualDay(e, emp.ID) {
			continue
		}
		if minutes == 0 {
			if err := h.options.Store.DeleteEntry(ctx, e.Owner, e.ID, e.Version, change(r, reason)); err != nil {
				return err
			}
			continue
		}
		if e.Minutes != minutes || e.Part != part || e.Typed != typed {
			e.Minutes, e.Part, e.Typed = minutes, part, typed
			if _, err := h.options.Store.UpdateEntry(ctx, e, change(r, reason)); err != nil {
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
		Minutes: minutes, Part: part, Typed: typed, Precision: core.PrecisionDay, Source: core.SourceManual}}, reason)
}

// setProjectDay keeps the hours of a day on a project in one entry; zero removes them.
func (h *handler) setProjectDay(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, projectID string, minutes int) error {
	ctx := r.Context()
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: emp.Owner, EmployerID: emp.ID, From: date, To: date})
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !isProjectDay(e, emp.ID) || e.ProjectID != projectID || e.Date != date {
			continue
		}
		if minutes == 0 {
			if err := h.options.Store.DeleteEntry(ctx, e.Owner, e.ID, e.Version, change(r, "")); err != nil {
				return err
			}
			continue
		}
		if e.Minutes != minutes {
			e.Minutes = minutes
			if _, err := h.options.Store.UpdateEntry(ctx, e, change(r, "")); err != nil {
				return err
			}
		}
		minutes = 0
	}
	if minutes == 0 {
		return nil
	}
	loc, _ := emp.Location()
	return h.book(r, cat, []core.Entry{{Owner: emp.Owner, EmployerID: emp.ID, Kind: core.EntryWork, Date: date, TimeZone: loc.String(),
		Minutes: minutes, ProjectID: projectID, Precision: core.PrecisionDay, Source: core.SourceManual}}, "")
}

// setDayNote makes the note of an employer's day the text: in the details of the day (core.DayInfo.Note), which keep
// a note with or without hours. Notes the former list kept on the hours of the day go into it.
func (h *handler) setDayNote(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, note string) error {
	ctx := r.Context()
	info := core.DayInfo{ID: core.DayInfoID(emp.ID, date), Owner: emp.Owner, EmployerID: emp.ID, Date: date}
	for _, i := range cat.DayInfos {
		if i.ID == info.ID {
			info = i
		}
	}
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: emp.Owner, EmployerID: emp.ID, From: date, To: date})
	if err != nil {
		return err
	}
	for _, e := range entries {
		if isManualDay(e, emp.ID) && e.Date == date && e.Note != "" {
			e.Note = ""
			if _, err := h.options.Store.UpdateEntry(ctx, e, change(r, "")); err != nil {
				return err
			}
		}
	}
	if info.Note == note {
		return nil
	}
	info.Note = note
	if info == (core.DayInfo{ID: info.ID, Owner: info.Owner, EmployerID: info.EmployerID, Date: info.Date}) {
		return h.options.Store.Delete(ctx, core.RecDayInfo, info.Owner, info.ID)
	}
	return h.options.Store.Put(ctx, info)
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
