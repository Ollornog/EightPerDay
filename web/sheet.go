// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// The start page is the sheet: every employer of a person over the weeks around today, one endless list that opens at
// this week, older weeks above it and two weeks to plan below. One view model (this file) holds the weeks, days,
// columns, sums, balance, earnings, hours not booked on projects and the state of each week; three views render it
// (templates/sheet.html): a table (days as rows, employers and their projects as columns), a journal (one line per
// day) and a grid per week (employers and projects as rows, days as columns). The detail decides which columns there
// are; nothing hidden is lost, it is summed (ADR-4).

// The views and the levels of detail of the sheet.
const (
	viewTable   = "table"
	viewJournal = "journal"
	viewGrid    = "grid"

	detailAuto      = "auto"
	detailHours     = "hours"
	detailProjects  = "projects"
	detailEmployers = "employers"
)

var (
	sheetViews   = []string{viewTable, viewJournal, viewGrid}
	sheetDetails = []string{detailAuto, detailHours, detailProjects, detailEmployers}
	// employerMarks are the badge variants that tell employers apart next to their key letter (never the colour alone).
	employerMarks = []string{"primary", "info", "secondary", "warning", "success", "outline"}
)

// weeksBefore is how many weeks before this week the sheet shows at first, weeksAhead how many after it (to plan), and
// weeksPerLoad how many older weeks each load adds.
const (
	weeksBefore  = 3
	weeksAhead   = 2
	weeksPerLoad = 8
)

type sheet struct {
	View   string
	Detail string // the detail chosen
	Level  string // the detail it comes to (auto resolved)
	// AutoView: nobody chose a view; a narrow screen switches to the journal.
	AutoView  bool
	Today     core.Date
	ThisWeek  core.Date
	Employers []*sheetEmployer
	Primary   *sheetEmployer
	Columns   []sheetColumn
	// Summed: the detail shows fewer employers than there are, and the others' hours are summed into the day.
	Summed   bool
	Weeks    []sheetWeek
	HasOlder bool
	Older    core.Date // the first week of the next load up
	First    core.Date // the first week shown
	Last     core.Date // the last week shown
	Gaps     []sheetGap
	Stats    sheetStats
	Currency string
	// HasTarget: some employer has a schedule, so target, difference and balance mean something.
	HasTarget bool
	// FocusDate and FocusCol name the field that has the focus when the page opens: the first empty working day of this
	// week, in the column of the day's own employer.
	FocusDate core.Date
	FocusCol  string
}

// sheetEmployer is an employer as the sheet shows it, with what the view model read for it.
type sheetEmployer struct {
	core.Employer
	Index    int
	Key      string
	Mark     string
	Projects []core.Project
	// HasTarget: the employer has a schedule. Approval: its organization approves weeks.
	HasTarget, Approval bool
	Expanded            bool
	start               core.Date
	days                map[core.Date]core.Day
	codes               map[core.Date]dayCode
	manual              map[core.Date]int
	parts               map[core.Date]core.DayPart
	typed               map[core.Date]string
	entryNotes          map[core.Date]string
	onProjects          map[core.Date]map[string]int
	weeks               map[core.Date]core.LedgerWeek
}

// sheetColumn is a column of the table and a row of the grid: an employer's hours of the day, or one of its projects.
type sheetColumn struct {
	Key      string // e-<employer> or p-<project>
	Employer *sheetEmployer
	Project  *core.Project
	// Expander: the employer's column can open and close its projects next to it.
	Expander bool
	// First: the first column of an employer.
	First bool
}

type sheetStats struct {
	Done, Target int
	Balance      int
	HasBalance   bool
	Earned       int64
}

type sheetGap struct {
	core.WeekGap
	Number   int
	Employer string
	Missing  int
}

type sheetWeek struct {
	From, To core.Date
	Number   int
	Current  bool
	Future   bool
	Days     []sheetDay
	// Sums are the week's minutes per column.
	Sums                []int
	Total, Target, Diff int
	Balance             int
	HasBalance          bool
	Earned              int64
	Status              []weekStatus
	// Approved: a change of the week becomes a change request, which needs a reason. Editable: some field takes a value.
	Approved, Editable bool
	Reports            []*sheetEmployer
}

// weekStatus is the state of one employer's week: handed in, approved, a change asked for.
type weekStatus struct {
	Employer *sheetEmployer
	State    string // locked, approved, requested
}

type sheetDay struct {
	Date                   core.Date
	Weekday                int
	Today, Weekend, Future bool
	Holiday                string
	Cells                  []sheetCell
	Total, Target, Diff    int
	HasTotal, HasDiff      bool
	Others                 int
	Note                   string
	NoteFixed              bool
	Line                   string
	LineFixed              bool
	Chips                  []sheetChip
	Default                *sheetEmployer
}

// sheetCell is one field of a day: what it shows, what it gives back to be changed, and what is around it.
type sheetCell struct {
	Column      *sheetColumn
	Shown       string
	Raw         string
	Placeholder string
	Open        int
	Fixed       bool
	Proposed    string
	HasProposal bool
	Approved    bool
}

// sheetChip is a part of a day's line as the journal shows it while the line has no focus.
type sheetChip struct {
	Employer *sheetEmployer
	Text     string
	Open     bool
	Code     bool
}

// sheetRange is the weeks a sheet shows.
type sheetRange struct{ from, to core.Date }

// buildSheet reads everything the sheet shows for the weeks of the range. focusWeek, when set, is the week whose
// first empty working day takes the focus.
func (h *handler) buildSheet(ctx context.Context, lang string, cat core.Catalog, prefs core.Preferences, rng sheetRange,
	today core.Date) (*sheet, error) {
	employers := activeEmployers(cat)
	s := &sheet{View: prefs.View, Detail: prefs.Detail, Today: today, First: rng.from, Last: rng.to}
	if !slices.Contains(sheetViews, s.View) {
		s.View, s.AutoView = viewTable, true
	}
	if !slices.Contains(sheetDetails, s.Detail) {
		s.Detail = detailAuto
	}
	if len(employers) == 0 {
		return s, nil
	}
	weekStart := employers[0].WeekStart
	s.ThisWeek = today.StartOfWeek(weekStart)
	keys := core.EmployerKeys(employers)
	keyOf := map[string]string{}
	for k, id := range keys {
		keyOf[id] = k
	}
	end := rng.to.AddDays(6)
	from := rng.from
	gapsFrom := today.AddDays(-7 * 8)
	from = minDate(from, gapsFrom)
	for i, e := range employers {
		se := &sheetEmployer{Employer: e, Index: i, Key: keyOf[e.ID], Mark: employerMarks[i%len(employerMarks)],
			Projects: activeProjects(cat, e.ID), Expanded: slices.Contains(prefs.Expanded, e.ID)}
		se.start, se.HasTarget = accountStart(cat, e.ID, rng.from)
		_, on, err := h.approvalOrg(ctx, e)
		if err != nil {
			return nil, err
		}
		se.Approval = on
		from = minDate(minDate(from, se.start), core.EarningsDataFrom(cat, e.ID, rng.from))
		s.Employers = append(s.Employers, se)
		if se.HasTarget {
			s.HasTarget = true
			if s.Primary == nil {
				s.Primary = se
			}
		}
	}
	if s.Primary == nil {
		s.Primary = s.Employers[0]
	}
	data, err := core.Load(ctx, h.options.Store, accountFrom(ctx).ID, from, end)
	if err != nil {
		return nil, err
	}
	data.Catalog = cat
	s.Currency = employerCurrency(cat, employers[0].ID)

	// The detail: one employer without projects shows its hours, one with projects them as columns, several employers
	// each in a column of its own with their projects to open.
	s.Level = s.Detail
	if s.Level == detailAuto {
		switch {
		case len(s.Employers) > 1:
			s.Level = detailEmployers
		case len(s.Primary.Projects) > 0:
			s.Level = detailProjects
		default:
			s.Level = detailHours
		}
	}
	switch s.Level {
	case detailHours, detailProjects:
		s.Columns = append(s.Columns, sheetColumn{Key: "e-" + s.Primary.ID, Employer: s.Primary, First: true})
		if s.Level == detailProjects {
			for i := range s.Primary.Projects {
				s.Columns = append(s.Columns, sheetColumn{Key: "p-" + s.Primary.Projects[i].ID, Employer: s.Primary, Project: &s.Primary.Projects[i]})
			}
		}
		s.Summed = len(s.Employers) > 1
	default:
		for _, e := range s.Employers {
			s.Columns = append(s.Columns, sheetColumn{Key: "e-" + e.ID, Employer: e, First: true, Expander: len(e.Projects) > 0})
			if e.Expanded {
				for i := range e.Projects {
					s.Columns = append(s.Columns, sheetColumn{Key: "p-" + e.Projects[i].ID, Employer: e, Project: &e.Projects[i]})
				}
			}
		}
	}

	for _, e := range s.Employers {
		days, err := core.Days(data, e.ID, rng.from, end)
		if err != nil {
			return nil, err
		}
		e.days = map[core.Date]core.Day{}
		for _, d := range days {
			e.days[d.Date] = d
		}
		e.codes = h.dayCodesIn(lang, cat, e.Employer, data)
		e.manual, e.parts, e.entryNotes = manualDays(data, e.ID)
		e.typed, e.onProjects = map[core.Date]string{}, map[core.Date]map[string]int{}
		for _, x := range data.Entries {
			if x.EmployerID != e.ID || x.DeletedAt != nil {
				continue
			}
			switch {
			case isManualDay(x, e.ID) && x.Typed != "":
				e.typed[x.Date] = x.Typed
			case isProjectDay(x, e.ID):
				if e.onProjects[x.Date] == nil {
					e.onProjects[x.Date] = map[string]int{}
				}
				e.onProjects[x.Date][x.ProjectID] += x.Minutes
			}
		}
		e.weeks = map[core.Date]core.LedgerWeek{}
		if e.HasTarget {
			ledger, err := core.Ledger(data, e.ID, e.start, rng.from, rng.to, today)
			if err != nil {
				return nil, err
			}
			for _, w := range ledger {
				e.weeks[w.From] = w
			}
			gaps, err := core.WeekGaps(data, e.ID, e.start, gapsFrom, today, e.Approval)
			if err != nil {
				return nil, err
			}
			for _, g := range gaps {
				_, number := toTime(g.From).ISOWeek()
				s.Gaps = append(s.Gaps, sheetGap{WeekGap: g, Number: number, Employer: e.Name, Missing: len(g.Missing)})
			}
		}
	}
	slices.SortStableFunc(s.Gaps, func(a, b sheetGap) int { return a.From.Compare(b.From) })

	notes := map[core.Date][]string{}
	for _, i := range cat.DayInfos {
		if i.Note != "" && !i.Date.Before(rng.from) && !i.Date.After(end) && slices.ContainsFunc(s.Employers, func(e *sheetEmployer) bool { return e.ID == i.EmployerID }) {
			notes[i.Date] = append(notes[i.Date], i.Note)
		}
	}

	for week := rng.from; !week.After(rng.to); week = week.AddDays(7) {
		w, err := h.sheetWeek(lang, s, data, week, notes)
		if err != nil {
			return nil, err
		}
		s.Weeks = append(s.Weeks, w)
		if week == s.ThisWeek {
			s.focus(w)
		}
		if week == s.ThisWeek {
			s.Stats.Target = w.Target
			s.Stats.Earned = w.Earned
			s.Stats.Balance, s.Stats.HasBalance = w.Balance, w.HasBalance
			for _, d := range w.Days {
				if !d.Future {
					s.Stats.Done += d.Total
				}
			}
		}
	}
	return s, nil
}

// focus finds the field that has the focus when the page opens: the first working day of the week without anything
// booked, in the column of the day's own employer (or the first column, where the detail does not show it).
func (s *sheet) focus(w sheetWeek) {
	for _, d := range w.Days {
		e := d.Default
		day := e.days[d.Date]
		working := day.Target > 0 || (!day.HasTarget && !d.Weekend)
		if !working || d.Holiday != "" || day.Actual > 0 || e.codes[d.Date].text != "" {
			continue
		}
		for i, c := range d.Cells {
			if c.Column.Employer == e && c.Column.Project == nil && !c.Fixed && c.Raw == "" {
				s.FocusDate, s.FocusCol = d.Date, s.Columns[i].Key
				return
			}
		}
		if len(d.Cells) > 0 && !d.Cells[0].Fixed && d.Cells[0].Raw == "" {
			s.FocusDate, s.FocusCol = d.Date, s.Columns[0].Key
			return
		}
	}
}

// sheetWeek builds one week of the sheet.
func (h *handler) sheetWeek(lang string, s *sheet, data core.Data, from core.Date, notes map[core.Date][]string) (sheetWeek, error) {
	w := sheetWeek{From: from, To: from.AddDays(6), Current: from == s.ThisWeek, Future: from.After(s.Today),
		Sums: make([]int, len(s.Columns))}
	_, w.Number = toTime(from).ISOWeek()
	cat := data.Catalog
	states := map[*sheetEmployer]string{}
	for i := range 7 {
		date := from.AddDays(i)
		d := sheetDay{Date: date, Weekday: int(date.Weekday()), Today: date == s.Today, Future: date.After(s.Today),
			Weekend: date.Weekday() == time.Saturday || date.Weekday() == time.Sunday}
		for _, e := range s.Employers {
			day := e.days[date]
			if day.Holiday != "" && d.Holiday == "" {
				d.Holiday = day.Holiday
			}
			if d.Default == nil && day.Target > 0 {
				d.Default = e
			}
			if day.Actual > 0 || e.manual[date] > 0 || e.codes[date].text != "" {
				d.HasTotal = true
			}
			d.Total += day.Actual
			if e.HasTarget {
				d.Target += day.AdjustedTarget
				if !d.Future && day.HasTarget && !date.Before(e.start) {
					d.Diff += day.Balance
					if day.AdjustedTarget > 0 || day.Actual > 0 {
						d.HasDiff = true
					}
				}
			}
			if period, ok := cat.PeriodAt(e.ID, date); ok {
				state := "locked"
				if e.Approval && period.Status == core.SubmissionApproved {
					state = "approved"
					if _, open := cat.OpenCorrection(period.ID); open {
						state = "requested"
					}
				}
				states[e] = state
			}
			if !s.Summed || e == s.Primary {
				continue
			}
			d.Others += day.Actual
		}
		if d.Default == nil {
			d.Default = s.Primary
		}
		for c := range s.Columns {
			col := &s.Columns[c]
			cell := h.sheetCell(lang, cat, col, date)
			d.Cells = append(d.Cells, cell)
			if !cell.Fixed {
				w.Editable = true
			}
			if cell.Approved {
				w.Approved = true
			}
			if col.Project != nil {
				w.Sums[c] += col.Employer.onProjects[date][col.Project.ID]
			} else {
				w.Sums[c] += col.Employer.days[date].Actual
			}
		}
		d.Note = strings.Join(dayNotes(s, date, notes[date]), " · ")
		d.NoteFixed = h.dayFixed(cat, d.Default, date)
		h.dayLine(lang, s, cat, &d)
		if !d.NoteFixed || !d.LineFixed {
			w.Editable = true
		}
		w.Total += d.Total
		w.Target += d.Target
		w.Diff += d.Diff
		w.Days = append(w.Days, d)
	}
	for _, e := range s.Employers {
		if lw, ok := e.weeks[from]; ok && !w.Future {
			w.Balance += lw.Balance
			w.HasBalance = true
		}
		if state, ok := states[e]; ok {
			w.Status = append(w.Status, weekStatus{Employer: e, State: state})
		}
		earnings, err := core.ComputeEarnings(data, e.ID, from, w.To)
		if err != nil {
			return w, err
		}
		w.Earned += earnings.Total[s.Currency]
		for _, d := range e.days {
			if d.Date.Within(from, w.To) && (d.Actual > 0 || d.Target > 0) {
				w.Reports = append(w.Reports, e)
				break
			}
		}
	}
	slices.SortFunc(w.Reports, func(a, b *sheetEmployer) int { return a.Index - b.Index })
	return w, nil
}

// dayNotes are the notes of a day: those of its details, and the notes the former list kept on the day's hours.
func dayNotes(s *sheet, date core.Date, infoNotes []string) []string {
	out := slices.Clone(infoNotes)
	for _, e := range s.Employers {
		if n := e.entryNotes[date]; n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
		if c := e.codes[date]; c.replaceable && c.note != "" && !slices.Contains(out, c.note) {
			out = append(out, c.note)
		}
	}
	return out
}

// dayFixed reports whether the employer's day is handed in or approved, so nothing of it changes in place.
func (h *handler) dayFixed(cat core.Catalog, e *sheetEmployer, date core.Date) bool {
	_, ok := cat.PeriodAt(e.ID, date)
	return ok
}

// sheetCell is the field of a column on a day.
func (h *handler) sheetCell(lang string, cat core.Catalog, col *sheetColumn, date core.Date) sheetCell {
	e := col.Employer
	cell := sheetCell{Column: col}
	period, inPeriod := cat.PeriodAt(e.ID, date)
	changeable := inPeriod && e.Approval && period.Status == core.SubmissionApproved
	if col.Project != nil {
		if m := e.onProjects[date][col.Project.ID]; m > 0 {
			cell.Shown = hoursText(lang, m)
			cell.Raw = cell.Shown
		}
		// Projects of a week handed in or approved stay as they are: a change request carries the day's hours only.
		cell.Fixed = inPeriod
		return cell
	}
	day := e.days[date]
	c := e.codes[date]
	manual, part := e.manual[date], e.parts[date]
	hours := ""
	if manual > 0 {
		hours = h.hoursOfPartIn(lang, manual, part)
	}
	switch {
	case inPeriod && !changeable:
		cell.Fixed = true
		cell.Shown = h.dayTextIn(lang, manual, part, c)
	case c.fixed:
		cell.Fixed = true
		cell.Shown = c.text
	case manual > 0 && c.replaceable && c.half:
		cell.Shown = c.text + " " + hours
		cell.Raw = cell.Shown
		if t := e.typed[date]; t != "" {
			cell.Raw = c.text + " " + t
		}
	case manual > 0:
		cell.Shown, cell.Raw = hours, hours
		if t := e.typed[date]; t != "" {
			cell.Raw = t
		}
	case c.replaceable:
		cell.Shown, cell.Raw = c.text, c.text
	}
	if changeable {
		cell.Approved = true
		if request, open := cat.OpenCorrection(period.ID); open {
			types := cat.AbsenceTypesFor(e.ID)
			for _, p := range request.Proposal {
				if p.Date == date {
					cell.Proposed, cell.HasProposal = h.proposedTextIn(lang, types, p), true
				}
			}
		}
	}
	onProjects := 0
	for id, m := range e.onProjects[date] {
		if id != "" {
			onProjects += m
		}
	}
	switch {
	case cell.Shown != "":
	case onProjects > 0:
		cell.Placeholder = hoursText(lang, onProjects)
	case day.Actual > 0:
		cell.Placeholder = hoursText(lang, day.Actual)
	case c.text != "":
		cell.Placeholder = c.text
	}
	if manual > 0 && onProjects > 0 {
		cell.Open = day.Unbooked
	}
	return cell
}

// dayLine writes the journal's line of a day and its chips: the hours of the day's own employer without its key, every
// other employer's with it, a project's hours with its word behind them, then the note.
func (h *handler) dayLine(lang string, s *sheet, cat core.Catalog, d *sheetDay) {
	var words []string
	fixed := 0
	// The day's own employer first: hours without a key belong to it, and to the employer of the words before them.
	order := append([]*sheetEmployer{d.Default}, slices.DeleteFunc(slices.Clone(s.Employers), func(e *sheetEmployer) bool { return e == d.Default })...)
	for _, e := range order {
		col := sheetColumn{Employer: e}
		cell := h.sheetCell(lang, cat, &col, d.Date)
		key := e.Key
		if e == d.Default {
			key = ""
		}
		if cell.Fixed {
			fixed++
			if cell.Shown != "" {
				d.Chips = append(d.Chips, sheetChip{Employer: e, Text: cell.Shown, Code: true})
			}
		} else if cell.Raw != "" {
			words = append(words, key+cell.Raw)
			d.Chips = append(d.Chips, sheetChip{Employer: e, Text: h.chipText(lang, cell.Shown)})
		}
		for _, p := range e.Projects {
			m := e.onProjects[d.Date][p.ID]
			if m == 0 {
				continue
			}
			if !cell.Fixed {
				words = append(words, key+hoursText(lang, m)+" "+core.ProjectWord(p))
			}
			d.Chips = append(d.Chips, sheetChip{Employer: e, Text: p.Name + " " + hoursText(lang, m) + " " + h.catalog[lang]["sheet.hours.unit"]})
		}
		if cell.Open > 0 {
			d.Chips = append(d.Chips, sheetChip{Employer: e, Open: true, Text: h.catalog.translator(lang)("sheet.open.of", hoursText(lang, cell.Open), e.Name)})
		}
		if cell.HasProposal {
			d.Chips = append(d.Chips, sheetChip{Employer: e, Text: "→ " + cell.Proposed})
		}
	}
	if d.Note != "" {
		words = append(words, d.Note)
	}
	d.Line = strings.Join(words, " ")
	d.LineFixed = fixed == len(s.Employers)
}

// chipText writes a day's value as words for its chip: codes by their names, hours with their unit.
func (h *handler) chipText(lang string, shown string) string {
	var out []string
	for _, word := range strings.Fields(shown) {
		if code, ok := core.ParseDayCode(word); ok {
			name := h.catalog[lang]["absence."+code.Absence]
			if name == "" {
				name = word
			}
			if code.HalfDay {
				name = "½ " + name
			}
			out = append(out, name)
			continue
		}
		if word != "" && word[0] >= '0' && word[0] <= '9' {
			word += " " + h.catalog[lang]["sheet.hours.unit"]
		}
		out = append(out, word)
	}
	return strings.Join(out, " ")
}

// activeProjects are the employer's projects that are not archived, by name.
func activeProjects(cat core.Catalog, employerID string) []core.Project {
	var out []core.Project
	for _, p := range projectsOf(cat, employerID) {
		if !p.Archived {
			out = append(out, p)
		}
	}
	return out
}

// isProjectDay reports whether an entry is hours of a day on a project, as the day page and the sheet book them.
func isProjectDay(e core.Entry, employerID string) bool {
	return e.EmployerID == employerID && e.DeletedAt == nil && e.Kind == core.EntryWork && e.Precision == core.PrecisionDay &&
		e.ProjectID != "" && e.Start == nil
}

// sheetBounds is the range of weeks a request asks for: from=… opens there, before=… loads the weeks before it, and
// without either the sheet opens a few weeks before this week. to is never before the planning weeks after this one.
func (h *handler) sheetBounds(ctx context.Context, cat core.Catalog, q map[string][]string, today core.Date) (sheetRange, core.Date, error) {
	employers := activeEmployers(cat)
	weekStart := time.Monday
	if len(employers) > 0 {
		weekStart = employers[0].WeekStart
	}
	this := today.StartOfWeek(weekStart)
	get := func(name string) string {
		if v := q[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	rng := sheetRange{from: this.AddDays(-7 * weeksBefore), to: this.AddDays(7 * weeksAhead)}
	asked := false
	if before, err := core.ParseDate(get("before")); err == nil {
		rng.to = before.StartOfWeek(weekStart).AddDays(-7)
		rng.from = rng.to.AddDays(-7 * (weeksPerLoad - 1))
	} else if want, err := core.ParseDate(get("from")); err == nil && want.StartOfWeek(weekStart).Before(rng.from) {
		// A week asked for by name opens even before the first records, to book there.
		rng.from, asked = want.StartOfWeek(weekStart), true
	}
	if to, err := core.ParseDate(get("to")); err == nil && to.StartOfWeek(weekStart).After(rng.to) && get("before") == "" {
		rng.to = minDate(to.StartOfWeek(weekStart), this.AddDays(7*52))
	}
	earliest := this
	for _, e := range employers {
		first, err := h.earliestWeek(ctx, cat, e, this)
		if err != nil {
			return rng, earliest, err
		}
		earliest = minDate(earliest, first)
	}
	if rng.from.Before(earliest) && !asked {
		rng.from = earliest
	}
	return rng, minDate(earliest, rng.from), nil
}

// earliestWeek is the first week with anything of the employer: its first schedule, or its first entry.
func (h *handler) earliestWeek(ctx context.Context, cat core.Catalog, emp core.Employer, latest core.Date) (core.Date, error) {
	first := latest
	if start, ok := accountStart(cat, emp.ID, latest); ok && start.Before(first) {
		first = start
	}
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: emp.Owner, EmployerID: emp.ID})
	if err != nil {
		return first, err
	}
	for _, e := range entries {
		if e.DeletedAt == nil && e.Date.Before(first) {
			first = e.Date
		}
	}
	return first.StartOfWeek(emp.WeekStart), nil
}

// sheetWeekView, cellView and noteView hand a week, a field and the note of a day to their templates.
type sheetWeekView struct {
	S *sheet
	W sheetWeek
}

type cellView struct {
	C     sheetCell
	Date  core.Date
	Week  core.Date
	X     string
	Label string
	Focus bool
}

type noteView struct {
	D    sheetDay
	Week core.Date
	X    string
}
