// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/Ollornog/EightPerDay/core"
)

// The sheet's pages and what they write: the page itself (GET /), a part of it (older weeks, ?part=weeks), one field
// saved from the page's script (POST /sheet/cell, answered with the weeks it changed), a whole week from the form
// without JavaScript (POST /sheet/week), and the person's preferences (POST /sheet/prefs).

// errChangeReason: a change to an approved week becomes a change request, which needs a reason.
var errChangeReason = errors.New("a change of an approved week needs a reason")

// sheetPage shows the sheet, or the start form without an employer.
func (h *handler) sheetPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	employers := activeEmployers(cat)
	if len(employers) == 0 {
		h.render(w, r, http.StatusOK, "start", view{Title: h.t(r, "home.title"), Data: newEmployerData(r)})
		return
	}
	s, err := h.sheetFor(r, cat, r.URL.Query())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	if r.URL.Query().Get("part") == "weeks" {
		h.renderPart(w, r, "sheet", "sheet-part", s)
		return
	}
	h.render(w, r, http.StatusOK, "sheet", view{Title: h.t(r, "list.title"), Data: s, Wide: true, Sheet: true})
}

// sheetFor builds the sheet a request asks for: the person's preferences, a view the address names for this request
// alone (the journal a narrow screen switches to), and the weeks of from, before and to.
func (h *handler) sheetFor(r *http.Request, cat core.Catalog, q map[string][]string) (*sheet, error) {
	ctx := r.Context()
	prefs := cat.PreferencesOf(accountFrom(ctx).ID)
	auto := !slices.Contains(sheetViews, prefs.View)
	if v := first(q, "view"); slices.Contains(sheetViews, v) {
		prefs.View = v
	} else if auto && r.Header.Get("Sec-CH-UA-Mobile") == "?1" {
		prefs.View = viewJournal
	}
	employers := activeEmployers(cat)
	today := h.today(&employers[0])
	rng, earliest, err := h.sheetBounds(ctx, cat, q, today)
	if err != nil {
		return nil, err
	}
	s, err := h.buildSheet(ctx, h.lang(r), cat, prefs, rng, today)
	if err != nil {
		return nil, err
	}
	s.AutoView = auto
	s.HasOlder = rng.from.After(earliest)
	s.Older = maxDate(earliest, rng.from.AddDays(-7*weeksPerLoad))
	return s, nil
}

func first(q map[string][]string, name string) string {
	if v := q[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// cellAnswer is what POST /sheet/cell answers: the weeks from the one changed to the last one shown, rendered in the
// view of the page, or what went wrong.
type cellAnswer struct {
	HTML   string `json:"html,omitempty"`
	Error  string `json:"error,omitempty"`
	Reason bool   `json:"reason,omitempty"`
	Done   string `json:"done,omitempty"`
}

// saveCell saves one field of the sheet.
func (h *handler) saveCell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	answer := func(status int, a cellAnswer) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(a)
	}
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.log.Error("request failed", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
		answer(http.StatusInternalServerError, cellAnswer{Error: h.t(r, "error.internal.text")})
		return
	}
	date, err := core.ParseDate(r.PostFormValue("date"))
	if err != nil {
		answer(http.StatusBadRequest, cellAnswer{Error: h.t(r, "track.error.invalid", "date")})
		return
	}
	done, err := h.applyCell(r, cat, date, r.PostFormValue("col"), r.PostFormValue("value"), strings.TrimSpace(r.PostFormValue("reason")))
	// What the person has to fix is an answer like any other (200): the page shows it at the field.
	switch {
	case errors.Is(err, errChangeReason):
		answer(http.StatusOK, cellAnswer{Reason: true, Error: h.t(r, "list.error.reason")})
		return
	case err != nil:
		if text := h.cellError(r, err); text != "" {
			answer(http.StatusOK, cellAnswer{Error: text})
			return
		}
		h.log.Error("request failed", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
		answer(http.StatusInternalServerError, cellAnswer{Error: h.t(r, "error.internal.text")})
		return
	}
	// The weeks from the one changed to the last one shown: a change moves the balance of every week after it.
	if cat, err = h.ownCatalog(ctx); err != nil {
		answer(http.StatusInternalServerError, cellAnswer{Error: h.t(r, "error.internal.text")})
		return
	}
	q := map[string][]string{"view": {r.PostFormValue("view")}, "from": {date.String()}, "to": {r.PostFormValue("to")}}
	s, err := h.sheetFor(r, cat, q)
	if err != nil {
		h.log.Error("request failed", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
		answer(http.StatusInternalServerError, cellAnswer{Error: h.t(r, "error.internal.text")})
		return
	}
	s.Weeks = slices.DeleteFunc(s.Weeks, func(wk sheetWeek) bool { return wk.To.Before(date) })
	var out bytes.Buffer
	if err := h.pages[h.lang(r)]["sheet"].ExecuteTemplate(&out, "sheet-part", s); err != nil {
		h.log.Error("template failed", slog.String("page", "sheet"), slog.String("error", err.Error()))
		answer(http.StatusInternalServerError, cellAnswer{Error: h.t(r, "error.internal.text")})
		return
	}
	answer(http.StatusOK, cellAnswer{HTML: out.String(), Done: done})
}

// cellError is the sentence for a value the person has to fix, or "" for a failure that is not theirs.
func (h *handler) cellError(r *http.Request, err error) string {
	var parse *core.ParseError
	if errors.As(err, &parse) {
		return h.t(r, "sheet.error.value", parse.Error())
	}
	return h.storeError(r, err)
}

// saveWeek saves the fields of a week from the form without JavaScript: only those that changed (w: carries what the
// page showed), so a week saved again books nothing twice and a day changed elsewhere keeps its change.
func (h *handler) saveWeek(w http.ResponseWriter, r *http.Request) {
	cat, err := h.ownCatalog(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.back(w, r, err, "")
		return
	}
	type field struct {
		date       core.Date
		col, value string
	}
	var fields []field
	for key, values := range r.PostForm {
		rest, found := strings.CutPrefix(key, "c:")
		if !found || len(values) != 1 {
			continue
		}
		raw, col, ok := strings.Cut(rest, ":")
		date, err := core.ParseDate(raw)
		if !ok || err != nil {
			continue
		}
		if strings.TrimSpace(values[0]) == strings.TrimSpace(r.PostFormValue("w:"+rest)) {
			continue
		}
		fields = append(fields, field{date, col, values[0]})
	}
	// The line of a day last, so the fields of the table come first where both are sent.
	slices.SortFunc(fields, func(a, b field) int {
		if c := a.date.Compare(b.date); c != 0 {
			return c
		}
		return strings.Compare(a.col, b.col)
	})
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	done := "saved"
	for _, f := range fields {
		d, err := h.applyCell(r, cat, f.date, f.col, f.value, reason)
		if errors.Is(err, errChangeReason) {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "list.error.reason"))
			return
		}
		if err != nil {
			if text := h.cellError(r, err); text != "" {
				h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), text)
				return
			}
			h.failed(w, r, err)
			return
		}
		if d == "requested" {
			done = d
		}
		if cat, err = h.ownCatalog(r.Context()); err != nil {
			h.failed(w, r, err)
			return
		}
	}
	h.back(w, r, nil, done)
}

// savePrefs keeps the person's view, detail and the employers whose projects stand open.
func (h *handler) savePrefs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	person := accountFrom(ctx).ID
	p := cat.PreferencesOf(person)
	if v := r.PostFormValue("view"); slices.Contains(sheetViews, v) {
		p.View = v
	}
	if v := r.PostFormValue("detail"); slices.Contains(sheetDetails, v) {
		p.Detail = v
	}
	if id := r.PostFormValue("expand"); id != "" {
		if _, ok := cat.Employer(id); !ok {
			h.back(w, r, core.ErrNotFound, "")
			return
		}
		if i := slices.Index(p.Expanded, id); i >= 0 {
			p.Expanded = slices.Delete(p.Expanded, i, i+1)
		} else {
			p.Expanded = append(p.Expanded, id)
		}
	}
	// Employers that are gone leave the list.
	p.Expanded = slices.DeleteFunc(p.Expanded, func(id string) bool { _, ok := cat.Employer(id); return !ok })
	h.back(w, r, h.options.Store.Put(ctx, p), "")
}

// sheetDayContext is what a day's fields need to know: the employers, their keys and projects, the day's own employer.
type sheetDayContext struct {
	employers []core.Employer
	keys      map[string]string
	projects  []core.Project
	def       core.Employer
	data      core.Data
}

// dayContext reads the day for its fields: the records of the day and the employer the day belongs to (the first with a
// target that day, else the first with a schedule, else the first).
func (h *handler) dayContext(r *http.Request, cat core.Catalog, date core.Date) (sheetDayContext, error) {
	c := sheetDayContext{employers: activeEmployers(cat)}
	if len(c.employers) == 0 {
		return c, core.ErrNotFound
	}
	c.keys = core.EmployerKeys(c.employers)
	data, err := core.Load(r.Context(), h.options.Store, accountFrom(r.Context()).ID, date, date)
	if err != nil {
		return c, err
	}
	data.Catalog = cat
	c.data = data
	var primary *core.Employer
	for i, e := range c.employers {
		c.projects = append(c.projects, activeProjects(cat, e.ID)...)
		if _, ok := accountStart(cat, e.ID, date); ok && primary == nil {
			primary = &c.employers[i]
		}
		days, err := core.Days(data, e.ID, date, date)
		if err != nil {
			return c, err
		}
		if c.def.ID == "" && len(days) == 1 && days[0].Target > 0 {
			c.def = e
		}
	}
	if c.def.ID == "" {
		c.def = c.employers[0]
		if primary != nil {
			c.def = *primary
		}
	}
	return c, nil
}

// applyCell saves what a field of the sheet says: col is e-<employer> (the employer's hours or code of the day),
// p-<project> (the hours on a project), note (the note of the day) or line (the whole day, as the journal writes it).
// It returns "requested" when a change became a change request.
func (h *handler) applyCell(r *http.Request, cat core.Catalog, date core.Date, col, value, reason string) (string, error) {
	c, err := h.dayContext(r, cat, date)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	find := func(id string) (core.Employer, error) {
		for _, e := range c.employers {
			if e.ID == id {
				if !core.MayBook(e, core.Actor{Person: accountFrom(r.Context()).ID}) {
					return e, errForbidden
				}
				return e, nil
			}
		}
		return core.Employer{}, core.ErrNotFound
	}
	switch {
	case col == "note":
		return "", h.applyNote(r, cat, c, date, value, reason)
	case col == "line":
		return h.applyLine(r, cat, c, date, value, reason)
	case strings.HasPrefix(col, "e-"):
		emp, err := find(strings.TrimPrefix(col, "e-"))
		if err != nil {
			return "", err
		}
		line, err := core.ParseDay(value, core.DayOptions{Default: emp.ID})
		if err != nil {
			return "", err
		}
		if line.Note != "" {
			return "", &core.ParseError{Token: line.Note, Msg: "not hours and not a code"}
		}
		v, _ := valueOf(line.Groups, emp.ID)
		return h.writeDay(r, cat, c, emp, date, v, reason)
	case strings.HasPrefix(col, "p-"):
		p := projectOf(cat, strings.TrimPrefix(col, "p-"))
		if p.ID == "" {
			return "", core.ErrNotFound
		}
		emp, err := find(p.EmployerID)
		if err != nil {
			return "", err
		}
		line, err := core.ParseDay(value, core.DayOptions{Default: emp.ID})
		if err != nil {
			return "", err
		}
		minutes := 0
		for _, g := range line.Groups {
			if g.Code != nil {
				return "", &core.ParseError{Msg: "a project takes hours, not a code"}
			}
			minutes += g.Minutes
		}
		if line.Note != "" || minutes > 24*60 {
			return "", &core.ParseError{Token: line.Note, Msg: "not hours"}
		}
		if _, locked := cat.PeriodAt(emp.ID, date); locked {
			return "", core.ErrLocked
		}
		return "", h.setProjectDay(r, cat, emp, date, p.ID, minutes)
	}
	return "", core.ErrNotFound
}

// valueOf joins the groups of a line that hold an employer's own hours of the day into one value: the hours summed,
// the code, the half of the day when one group alone says it, what was typed for a span. ok is false when the line
// says nothing about the employer's own hours.
func valueOf(groups []core.DayGroup, employerID string) (dayValue, bool) {
	var v dayValue
	ok, n := false, 0
	var typed []string
	for _, g := range groups {
		if g.EmployerID != employerID || g.ProjectID != "" {
			continue
		}
		ok, n = true, n+1
		v.minutes += g.Minutes
		if g.Code != nil {
			v.code = g.Code
		}
		if g.Part != "" {
			v.part = g.Part
		}
		if g.Typed != "" {
			typed = append(typed, g.Typed)
		}
	}
	if n > 1 {
		v.part = ""
	}
	if len(typed) == 1 && n == 1 {
		v.typed = typed[0]
	}
	if v.minutes == 0 {
		v.part, v.typed = "", ""
	}
	return v, ok
}

// refersTo reports whether a line books anything on the employer.
func refersTo(groups []core.DayGroup, employerID string) bool {
	return slices.ContainsFunc(groups, func(g core.DayGroup) bool { return g.EmployerID == employerID })
}

// currentValue is what an employer's day holds now, as a field would write it back.
func currentValue(data core.Data, emp core.Employer, date core.Date) dayValue {
	var v dayValue
	for _, e := range data.Entries {
		if isManualDay(e, emp.ID) && e.Date == date {
			v.minutes += e.Minutes
			v.part, v.typed = e.Part, e.Typed
		}
	}
	for _, a := range data.Absences {
		if a.DeletedAt == nil && a.EmployerID == emp.ID && a.From == date && a.To == date && core.CodedAbsence(a.Type) &&
			a.Status == core.StatusApproved {
			v.code = &core.DayCode{Absence: a.Type, HalfDay: a.HalfDay}
		}
	}
	return v
}

func sameValue(a, b dayValue) bool {
	codes := (a.code == nil && b.code == nil) || (a.code != nil && b.code != nil && *a.code == *b.code)
	return codes && a.minutes == b.minutes && a.part == b.part && a.typed == b.typed
}

// writeDay makes an employer's day hold the value: at once, or through a change request where the organization
// approved the week (with the reason; without one errChangeReason).
func (h *handler) writeDay(r *http.Request, cat core.Catalog, c sheetDayContext, emp core.Employer, date core.Date, v dayValue,
	reason string) (string, error) {
	if sameValue(v, currentValue(c.data, emp, date)) {
		return "", nil
	}
	period, inPeriod := cat.PeriodAt(emp.ID, date)
	if !inPeriod {
		return "", h.setDay(r, cat, emp, date, v, "", false)
	}
	_, approval, err := h.approvalOrg(r.Context(), emp)
	if err != nil {
		return "", err
	}
	if !approval || period.Status != core.SubmissionApproved {
		return "", core.ErrLocked
	}
	if reason == "" {
		return "", errChangeReason
	}
	note := dayNoteOf(c.data, emp.ID, date)
	return "requested", h.requestChange(r, cat, emp, period, []core.ProposedDay{v.proposed(date, note)}, reason)
}

// dayNoteOf is the note an employer's day holds: in its details, or on its hours from the former list.
func dayNoteOf(data core.Data, employerID string, date core.Date) string {
	for _, i := range data.Catalog.DayInfos {
		if i.EmployerID == employerID && i.Date == date && i.Note != "" {
			return i.Note
		}
	}
	for _, e := range data.Entries {
		if isManualDay(e, employerID) && e.Date == date && e.Note != "" {
			return e.Note
		}
	}
	return ""
}

// applyNote makes the note of the day the text: the day's own employer keeps it, the notes of the other employers of
// that day go (the field showed them all, so the text holds them).
func (h *handler) applyNote(r *http.Request, cat core.Catalog, c sheetDayContext, date core.Date, note, reason string) error {
	if len(note) > 2000 {
		return &core.ParseError{Msg: "the note is longer than 2000 characters"}
	}
	current := map[string]string{}
	var joined []string
	for _, e := range c.employers {
		if n := dayNoteOf(c.data, e.ID, date); n != "" {
			current[e.ID] = n
			joined = append(joined, n)
		}
	}
	if note == strings.Join(joined, " · ") {
		return nil
	}
	for _, e := range c.employers {
		want := ""
		if e.ID == c.def.ID {
			want = note
		}
		if current[e.ID] == want {
			continue
		}
		if _, locked := cat.PeriodAt(e.ID, date); locked {
			return core.ErrLocked
		}
		if err := h.setDayNote(r, cat, e, date, want); err != nil {
			return err
		}
	}
	return nil
}

// applyLine makes the day hold what its line says: every employer's own hours or code, its hours on projects, and the
// note. What the line no longer says goes; employers whose day is handed in keep it.
func (h *handler) applyLine(r *http.Request, cat core.Catalog, c sheetDayContext, date core.Date, text, reason string) (string, error) {
	line, err := core.ParseDay(text, core.DayOptions{Default: c.def.ID, Keys: c.keys, Projects: c.projects})
	if err != nil {
		return "", err
	}
	done := ""
	codes := map[string]map[core.Date]dayCode{}
	for _, emp := range c.employers {
		codes[emp.ID] = h.dayCodes(r, cat, emp, c.data)
	}
	for _, emp := range c.employers {
		// A day handed in, or held by an absence the line cannot write, is not the line's to change.
		if period, ok := cat.PeriodAt(emp.ID, date); (ok && period.Status != core.SubmissionApproved) || codes[emp.ID][date].fixed {
			if refersTo(line.Groups, emp.ID) {
				return "", core.ErrLocked
			}
			continue
		}
		v, _ := valueOf(line.Groups, emp.ID)
		d, err := h.writeDay(r, cat, c, emp, date, v, reason)
		if err != nil {
			return "", err
		}
		if d != "" {
			done = d
		}
		want := map[string]int{}
		for _, g := range line.Groups {
			if g.EmployerID == emp.ID && g.ProjectID != "" {
				want[g.ProjectID] += g.Minutes
			}
		}
		have := map[string]int{}
		for _, e := range c.data.Entries {
			if isProjectDay(e, emp.ID) && e.Date == date {
				have[e.ProjectID] += e.Minutes
			}
		}
		for id, m := range have {
			if _, ok := want[id]; !ok && m > 0 {
				want[id] = 0
			}
		}
		for id, m := range want {
			if have[id] == m {
				continue
			}
			if _, locked := cat.PeriodAt(emp.ID, date); locked {
				return "", core.ErrLocked
			}
			if err := h.setProjectDay(r, cat, emp, date, id, m); err != nil {
				return "", err
			}
		}
	}
	return done, h.applyNote(r, cat, c, date, line.Note, reason)
}

// renderPart writes one template of a page alone, for a part the page loads into itself.
func (h *handler) renderPart(w http.ResponseWriter, r *http.Request, page, name string, data any) {
	var out bytes.Buffer
	if err := h.pages[h.lang(r)][page].ExecuteTemplate(&out, name, data); err != nil {
		h.log.Error("template failed", slog.String("page", page), slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Bytes())
}

func maxDate(a, b core.Date) core.Date {
	if b.After(a) {
		return b
	}
	return a
}
