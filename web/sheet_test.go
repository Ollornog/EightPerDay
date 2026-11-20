// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/web/auth"
)

func attr(body, name string) string {
	m := regexp.MustCompile(name + `>([^<]*)<`).FindStringSubmatch(body)
	if m == nil {
		return "(none)"
	}
	return m[1]
}

func (s *site) absences(owner string) []core.Absence {
	s.t.Helper()
	all, err := s.store.Absences(context.Background(), core.AbsenceFilter{Owner: owner})
	if err != nil {
		s.t.Fatal(err)
	}
	var live []core.Absence
	for _, a := range all {
		if a.DeletedAt == nil {
			live = append(live, a)
		}
	}
	return live
}

// cell saves one field of the sheet as the page's script does, and returns the answer.
func (s *site) cell(date core.Date, col, value, reason string) (int, cellAnswer) {
	s.t.Helper()
	a := s.post("/sheet/cell", url.Values{"date": {date.String()}, "col": {col}, "value": {value}, "reason": {reason},
		"view": {"table"}, "to": {"2026-11-30"}})
	var out cellAnswer
	if err := json.Unmarshal([]byte(a.body), &out); err != nil {
		s.t.Fatalf("%s %s %q: %d %.300s", date, col, value, a.status, a.body)
	}
	return a.status, out
}

func (s *site) mustCell(date core.Date, col, value string) cellAnswer {
	s.t.Helper()
	status, a := s.cell(date, col, value, "")
	if status != http.StatusOK || a.Error != "" || a.HTML == "" {
		s.t.Fatalf("%s %s %q: %d %+v", date, col, value, status, a)
	}
	return a
}

// sheetOf builds the view model of a person's sheet directly, the way the start page does.
func sheetOf(t *testing.T, s *site, owner string, q url.Values) *sheet {
	t.Helper()
	h := &handler{options: Options{Store: s.store, Now: func() time.Time { return testNow }}, log: slog.New(slog.DiscardHandler)}
	var err error
	if h.catalog, err = loadCatalog(); err != nil {
		t.Fatal(err)
	}
	if err := h.parseTemplates(); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), accountKey, &auth.Account{ID: owner, Language: "en"})
	cat, err := s.store.Catalog(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/?"+q.Encode(), nil)
	out, err := h.sheetFor(r, cat, q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func weekOfSheet(t *testing.T, s *sheet, from core.Date) sheetWeek {
	t.Helper()
	for _, w := range s.Weeks {
		if w.From == from {
			return w
		}
	}
	t.Fatalf("no week %s", from)
	return sheetWeek{}
}

func columnKeys(s *sheet) []string {
	var out []string
	for _, c := range s.Columns {
		out = append(out, c.Key)
	}
	return out
}

// fieldValue is the value of a field of the sheet page.
func fieldValue(t *testing.T, body string, date core.Date, col string) string {
	t.Helper()
	m := regexp.MustCompile(`id="c-` + date.String() + `-` + regexp.QuoteMeta(col) + `"[^>]*?value="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no field %s %s", date, col)
	}
	return m[1]
}

// The start page is the sheet: it opens at this week, three weeks before it and two to plan after it, the first empty
// working day has the focus, unfinished weeks are named, and the tools of a mode stay on the week page.
func TestSheetIsTheStartPage(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	body := s.get("/").body
	var weeks []string
	for _, m := range regexp.MustCompile(`<tbody id="week-([0-9-]+)"`).FindAllStringSubmatch(body, -1) {
		weeks = append(weeks, m[1])
	}
	// The schedule starts on 1 November: the sheet begins with the first week of the account.
	if strings.Join(weeks, " ") != "2026-10-26 2026-11-02 2026-11-09 2026-11-16 2026-11-23 2026-11-30" {
		t.Fatalf("weeks %v", weeks)
	}
	if !strings.Contains(body, `<tbody id="week-2026-11-16" data-week="2026-11-16" data-current>`) || !strings.Contains(body, `id="today"`) {
		t.Error("this week or today is not marked")
	}
	if focus := regexp.MustCompile(`id="c-([0-9-]+)-e-` + emp.ID + `"[^>]*autofocus`).FindStringSubmatch(body); focus == nil || focus[1] != "2026-11-16" {
		t.Errorf("the focus is on %v, not on Monday", focus)
	}
	if !strings.Contains(body, `data-weeks-start`) || strings.Contains(body, "data-load-older") {
		t.Error("the first week of the account is not the start")
	}
	gaps := regexp.MustCompile(`data-gap="([0-9-]+)"`).FindAllStringSubmatch(body, -1)
	if len(gaps) != 3 || gaps[0][1] != "2026-11-02" || !strings.Contains(body, `href="/?from=2026-11-02#week-2026-11-02"`) {
		t.Errorf("gaps %v", gaps)
	}
	for _, tool := range []string{`action="/book/line"`, "data-approval", `action="/timer/start"`, "data-month-grid", "data-year-grid"} {
		if strings.Contains(body, tool) {
			t.Errorf("the start page shows %s", tool)
		}
	}
	if !strings.Contains(s.get("/week?e="+emp.ID).body, `action="/book/line"`) {
		t.Error("the week page lost its tools")
	}
	// The hint names the input of the view, the head the numbers of this week.
	if !strings.Contains(body, `data-hint`) || !strings.Contains(body, `<kbd class="kbd">8-13:30 30m</kbd>`) ||
		!strings.Contains(body, `data-stat-week>0 / 40 h<`) {
		t.Errorf("hint or numbers missing: %s", regexp.MustCompile(`data-stat-week>[^<]*<`).FindString(body))
	}
}

// The detail: automatic picks hours for one employer, its projects as columns once it has some, and a column per
// employer for several; a detail chosen by hand holds even when the records say more, and sums what it hides.
func TestSheetDetailLevels(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	office := s.employer(me, "Office", core.ModeSchedule, "8")
	if sh := sheetOf(t, s, me.ID, nil); sh.Level != detailHours || strings.Join(columnKeys(sh), " ") != "e-"+office.ID {
		t.Fatalf("one employer: %s %v", sh.Level, columnKeys(sh))
	}
	s.mustPost("/employers/"+office.ID+"/project", url.Values{"name": {"Roof"}, "back": {"/"}})
	sh := sheetOf(t, s, me.ID, nil)
	if sh.Level != detailProjects || len(sh.Columns) != 2 || sh.Columns[1].Project == nil || sh.Columns[1].Project.Name != "Roof" {
		t.Fatalf("one employer with a project: %s %v", sh.Level, columnKeys(sh))
	}
	studio := s.employer(me, "Photo studio", core.ModeProjectsOnly, "")
	sh = sheetOf(t, s, me.ID, nil)
	if sh.Level != detailEmployers || strings.Join(columnKeys(sh), " ") != "e-"+office.ID+" e-"+studio.ID || !sh.Columns[0].Expander {
		t.Fatalf("two employers: %s %v", sh.Level, columnKeys(sh))
	}
	// The projects of an employer open next to it, and stay open for this person.
	s.mustPost("/sheet/prefs", url.Values{"expand": {office.ID}, "back": {"/"}})
	if sh = sheetOf(t, s, me.ID, nil); len(sh.Columns) != 3 || sh.Columns[1].Project == nil {
		t.Fatalf("opened: %v", columnKeys(sh))
	}
	// Hours only, chosen by hand: one column, the other employer's hours summed into the day.
	s.mustPost("/sheet/prefs", url.Values{"detail": {"hours"}, "back": {"/"}})
	s.mustCell(monday, "e-"+studio.ID, "3")
	sh = sheetOf(t, s, me.ID, nil)
	week := weekOfSheet(t, sh, monday)
	if sh.Detail != detailHours || len(sh.Columns) != 1 || !sh.Summed || week.Days[0].Others != 180 || week.Days[0].Total != 180 {
		t.Fatalf("hours only: %s %v summed %v others %d total %d", sh.Detail, columnKeys(sh), sh.Summed, week.Days[0].Others, week.Days[0].Total)
	}
	if body := s.get("/").body; !strings.Contains(body, `data-others>+3 other<`) {
		t.Error("the hours of the hidden employer do not show")
	}
}

// Fields of the table: hours with a comma, a clock, a span with its break (given back as typed), codes for leave and a
// half day of sickness, a note; the week's sums, differences and balance follow; saving the same again books nothing.
func TestSheetFieldsTakeHoursAndCodes(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	col := "e-" + emp.ID
	s.mustCell(monday, col, "8,5")
	s.mustCell(monday, "note", "inventory")
	s.mustCell(tuesday, col, "u")
	s.mustCell(wednesday, col, "½K")
	answer := s.mustCell(wednesday.AddDays(1), col, "8-16:30 1h")
	entries := s.entries(me.ID)
	if len(entries) != 2 || entries[0].Minutes != 510 || entries[1].Minutes != 450 || entries[1].Typed != "8-16:30 1h" {
		t.Fatalf("entries %+v", entries)
	}
	absences := s.absences(me.ID)
	if len(absences) != 2 || absences[0].Type != core.AbsenceVacation || absences[1].Type != core.AbsenceSick || !absences[1].HalfDay {
		t.Fatalf("absences %+v", absences)
	}
	// The answer holds the week changed and those after it, and the numbers on top.
	if !strings.Contains(answer.HTML, `data-slot="wk-2026-11-16-sum"`) || strings.Contains(answer.HTML, `data-slot="wk-2026-11-09-sum"`) ||
		!strings.Contains(answer.HTML, `data-slot="stats"`) {
		t.Errorf("answer %.300s", answer.HTML)
	}
	body := s.get("/").body
	for date, want := range map[core.Date]string{monday: "8.5", tuesday: "V", wednesday: "½S", wednesday.AddDays(1): "7.5"} {
		if got := fieldValue(t, body, date, col); got != want {
			t.Errorf("%s shows %q, want %q", date, got, want)
		}
	}
	if !strings.Contains(body, `data-raw="8-16:30 1h"`) || fieldValue(t, body, monday, "note") != "inventory" {
		t.Error("the span is not given back as typed, or the note is lost")
	}
	sh := sheetOf(t, s, me.ID, nil)
	week := weekOfSheet(t, sh, monday)
	// Up to today, Wednesday: +0.5 on Monday, leave takes the target, half a day sick and nothing worked is -4.
	if week.Total != 510+450 || week.Diff != 30-240 || !week.HasBalance || week.Balance != -80*60+30-240 {
		t.Errorf("week total %d, diff %d, balance %d", week.Total, week.Diff, week.Balance)
	}
	if sh.Stats.Done != 510 || sh.Stats.Target != 40*60-480-240 || sh.Stats.Balance != week.Balance {
		t.Errorf("stats %+v", sh.Stats)
	}
	// The same value again writes nothing.
	history, _ := s.store.Revisions(context.Background(), me.ID, "")
	s.mustCell(monday, col, "8.5")
	s.mustCell(tuesday, col, "V")
	if after, _ := s.store.Revisions(context.Background(), me.ID, ""); len(after) != len(history) {
		t.Errorf("saving the same again wrote %d revisions", len(after)-len(history))
	}
	// Leave becomes hours, hours are cleared: the absence and the entry go through the history.
	s.mustCell(tuesday, col, "6")
	s.mustCell(monday, col, "")
	if absences := s.absences(me.ID); len(absences) != 1 || absences[0].Type != core.AbsenceSick {
		t.Errorf("the leave was not replaced: %+v", absences)
	}
	if entries := s.entries(me.ID); len(entries) != 2 || entries[0].Date != tuesday || entries[0].Minutes != 360 {
		t.Errorf("entries after the change %+v", entries)
	}
	// An older week is changed the same way, on a private employer simply.
	s.mustCell(core.MustDate("2026-11-02"), col, "8")
	if body := s.get("/").body; fieldValue(t, body, core.MustDate("2026-11-02"), col) != "8" {
		t.Error("the older week did not take its hours")
	}
}

func TestSheetRefusesWhatItCannotRead(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	for _, value := range []string{"x", "-2", "25", "U½U", "U 4", "4x", "½U ½K", "25v", "8-8"} {
		if status, a := s.cell(monday, "e-"+emp.ID, value, ""); a.Error == "" {
			t.Errorf("%q: %d %+v", value, status, a)
		}
	}
	if status, a := s.cell(monday, "e-nobody", "8", ""); a.Error == "" {
		t.Errorf("an unknown employer: %d %+v", status, a)
	}
	if len(s.entries(me.ID)) != 0 || len(s.absences(me.ID)) != 0 {
		t.Error("a refused field booked something")
	}
}

// Projects: hours typed for the day hold the hours on its projects, and what they leave is open; without typed hours
// the projects make the day and show as the placeholder of the employer's field.
func TestSheetOpenHoursAndProjects(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Studio", core.ModeSchedule, "8")
	s.mustPost("/employers/"+emp.ID+"/project", url.Values{"name": {"Wedding Haas"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	project := cat.Projects[0]
	s.mustCell(monday, "p-"+project.ID, "3")
	sh := sheetOf(t, s, me.ID, nil)
	day := weekOfSheet(t, sh, monday).Days[0]
	if day.Cells[0].Placeholder != "3" || day.Cells[0].Shown != "" || day.Cells[1].Shown != "3" || day.Total != 180 {
		t.Fatalf("projects make the day: %+v / %+v, total %d", day.Cells[0], day.Cells[1], day.Total)
	}
	s.mustCell(monday, "e-"+emp.ID, "5")
	day = weekOfSheet(t, sheetOf(t, s, me.ID, nil), monday).Days[0]
	if day.Cells[0].Open != 120 || day.Total != 300 {
		t.Fatalf("typed hours hold the project: open %d, total %d", day.Cells[0].Open, day.Total)
	}
	if body := s.get("/").body; !strings.Contains(body, `<span data-open>2 open</span>`) {
		t.Error("the open hours do not show")
	}
	week := weekOfSheet(t, sheetOf(t, s, me.ID, nil), monday)
	if week.Sums[0] != 300 || week.Sums[1] != 180 {
		t.Errorf("week sums per column %v", week.Sums)
	}
	if status, a := s.cell(monday, "p-"+project.ID, "U", ""); a.Error == "" {
		t.Errorf("a code on a project: %d", status)
	}
	s.mustCell(monday, "p-"+project.ID, "")
	if day := weekOfSheet(t, sheetOf(t, s, me.ID, nil), monday).Days[0]; day.Cells[0].Open != 0 || day.Cells[1].Shown != "" {
		t.Error("the project's hours were not cleared")
	}
}

// A public holiday and half days: the holiday names itself and takes the target, half a day of leave next to hours
// covers half the target, and the half of a day is written behind the hours.
func TestSheetHolidayAndHalfDays(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	col := "e-" + emp.ID
	s.mustCell(monday, col, "½U 4n")
	s.mustCell(tuesday, col, "3v")
	sh := sheetOf(t, s, me.ID, url.Values{"from": {"2026-12-21"}, "to": {"2026-12-28"}})
	christmas := weekOfSheet(t, sh, core.MustDate("2026-12-21")).Days[4]
	if christmas.Date != core.MustDate("2026-12-25") || christmas.Holiday == "" || christmas.Target != 0 {
		t.Errorf("Christmas: %+v", christmas)
	}
	week := weekOfSheet(t, sh, monday)
	if c := week.Days[0].Cells[0]; c.Shown != "½V 4 PM" || week.Days[0].Total != 240 || week.Days[0].Target != 240 || week.Days[0].Diff != 0 {
		t.Errorf("half a day of leave with hours: %+v total %d diff %d", c, week.Days[0].Total, week.Days[0].Diff)
	}
	if c := week.Days[1].Cells[0]; c.Shown != "3 AM" || week.Days[1].Diff != -300 {
		t.Errorf("a morning: %+v diff %d", c, week.Days[1].Diff)
	}
	if entries := s.entries(me.ID); len(entries) != 2 || entries[0].Part != core.Afternoon || entries[1].Part != core.Morning {
		t.Errorf("entries %+v", entries)
	}
}

// The journal's line of a day books on every employer: plain hours on the day's own employer, a key in front for
// another, a word behind the hours for a project, the rest a note. The line is written back the same way, and what it
// no longer says goes.
func TestJournalLine(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	practice := s.employer(me, "Practice", core.ModeSchedule, "5")
	photo := s.employer(me, "Photo studio", core.ModeProjectsOnly, "")
	s.mustPost("/employers/"+photo.ID+"/project", url.Values{"name": {"Studio"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	studio := cat.Projects[0]
	s.mustCell(monday, "line", "5 p3 studio p1 edit pictures")
	entries := s.entries(me.ID)
	if len(entries) != 3 {
		t.Fatalf("entries %+v", entries)
	}
	byEmployer := map[string]int{}
	for _, e := range entries {
		byEmployer[e.EmployerID+"/"+e.ProjectID] += e.Minutes
	}
	if byEmployer[practice.ID+"/"] != 300 || byEmployer[photo.ID+"/"+studio.ID] != 180 || byEmployer[photo.ID+"/"] != 60 {
		t.Errorf("booked %v", byEmployer)
	}
	sh := sheetOf(t, s, me.ID, url.Values{"view": {"journal"}})
	day := weekOfSheet(t, sh, monday).Days[0]
	if sh.View != viewJournal || day.Line != "5 p1 p3 studio edit pictures" || day.Note != "edit pictures" {
		t.Errorf("view %s, line %q, note %q", sh.View, day.Line, day.Note)
	}
	if len(day.Chips) != 3 || !strings.Contains(day.Chips[2].Text, "Studio 3") {
		t.Errorf("chips %+v", day.Chips)
	}
	// What the line no longer says goes; a code for the day's own employer.
	s.mustCell(monday, "line", "U")
	if entries := s.entries(me.ID); len(entries) != 0 {
		t.Errorf("entries left %+v", entries)
	}
	if a := s.absences(me.ID); len(a) != 1 || a[0].EmployerID != practice.ID || a[0].Type != core.AbsenceVacation {
		t.Errorf("absences %+v", a)
	}
	if day := weekOfSheet(t, sheetOf(t, s, me.ID, nil), monday).Days[0]; day.Note != "" {
		t.Errorf("the note stayed: %q", day.Note)
	}
	if status, a := s.cell(monday, "line", "5 @nothing", ""); a.Error == "" {
		t.Errorf("an unknown project: %d %+v", status, a)
	}
	// The page of the journal: chips without the focus, the line with it.
	body := s.get("/?view=journal").body
	if !strings.Contains(body, `data-sheet-journal`) || !strings.Contains(body, `id="c-2026-11-16-line"`) || !strings.Contains(body, "data-chips") {
		t.Error("the journal page lacks its lines")
	}
}

// A handed-in week shows its values without fields and refuses changes.
func TestSheetHandedInWeek(t *testing.T) {
	_, worker, _, _, managed := approvalSite(t)
	worker.mustCell(monday, "e-"+managed.ID, "9")
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	body := worker.get("/").body
	if strings.Contains(body, `name="c:2026-11-16:e-`+managed.ID+`"`) || !strings.Contains(body, `data-fixed>9<`) ||
		!strings.Contains(body, `data-state="locked"`) {
		t.Error("the handed-in week still has fields")
	}
	for _, col := range []string{"e-" + managed.ID, "line", "note"} {
		if _, a := worker.cell(monday, col, "7", ""); a.Error == "" {
			t.Errorf("%s: a handed-in day changed: %+v", col, a)
		}
	}
}

// A change to an approved week on a managed employer becomes a change request in every view: the approved values
// count until the organization decides; approved, they are booked and the week is approved again.
func TestSheetChangeRequest(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	col := "e-" + managed.ID
	worker.mustCell(monday, col, "9")
	worker.mustCell(monday, "note", "dentist")
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {submissions(t, s, member.ID)[0].ID}, "decision": {"approve"}, "back": {"/"}})
	body := worker.get("/").body
	if !strings.Contains(body, `data-state="approved"`) || fieldValue(t, body, monday, col) != "9" {
		t.Fatal("the approved week cannot be changed")
	}
	// The value it holds, sent again (a line whose other employers changed), asks for nothing.
	if _, a := worker.cell(monday, col, "9", ""); a.Reason || a.Error != "" || a.Done != "" {
		t.Fatalf("the same value asks for a request: %+v", a)
	}
	// The table's field: without a reason the answer asks for one.
	if status, a := worker.cell(monday, col, "7", ""); status != http.StatusOK || !a.Reason {
		t.Fatalf("a change without a reason: %d %+v", status, a)
	}
	if status, a := worker.cell(monday, col, "7", "left for the dentist"); status != http.StatusOK || a.Done != "requested" {
		t.Fatalf("the request: %d %+v", status, a)
	}
	// The journal's line takes further days into the same request.
	if status, a := worker.cell(tuesday, "line", "U", "and a day of leave"); status != http.StatusOK || a.Done != "requested" {
		t.Fatalf("the line: %d %+v", status, a)
	}
	// The form without JavaScript (the grid's and the table's) does the same.
	form := url.Values{"back": {"/"}, "c:2026-11-18:" + col: {"6"}, "w:2026-11-18:" + col: {""}}
	if a := worker.post("/sheet/week", form); a.status != http.StatusBadRequest || !strings.Contains(a.body, "says why") {
		t.Fatalf("the week form without a reason: %d", a.status)
	}
	form.Set("reason", "and Wednesday")
	if a := worker.mustPost("/sheet/week", form); !strings.Contains(a.header.Get("Location"), "done=requested") {
		t.Errorf("redirect %s", a.header.Get("Location"))
	}
	if e := worker.entries(member.ID); len(e) != 1 || e[0].Minutes != 540 {
		t.Fatalf("the approved values changed before the decision: %+v", e)
	}
	var request core.Submission
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection {
			if request.ID != "" {
				t.Fatal("a second change request instead of one")
			}
			request = x
		}
	}
	if len(request.Proposal) != 3 || request.Proposal[0].Minutes != 420 || request.Proposal[0].Note != "dentist" ||
		request.Proposal[1].Absence != core.AbsenceVacation || request.Proposal[2].Minutes != 360 {
		t.Fatalf("change request %+v", request)
	}
	sh := sheetOf(t, worker, member.ID, nil)
	week := weekOfSheet(t, sh, monday)
	if c := week.Days[0].Cells[0]; c.Shown != "9" || !c.HasProposal || c.Proposed != "7" || week.Status[0].State != "requested" {
		t.Errorf("the request is not marked next to the approved value: %+v %+v", c, week.Status)
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {request.ID}, "decision": {"approve"}, "back": {"/"}})
	if e := worker.entries(member.ID); len(e) != 2 || e[0].Minutes != 420 || e[1].Minutes != 360 {
		t.Fatalf("the approved request was not booked: %+v", e)
	}
	if day := weekOfSheet(t, sheetOf(t, worker, member.ID, nil), monday).Days[0]; day.Note != "dentist" {
		t.Errorf("the note of the day is %q", day.Note)
	}
}

// Older weeks load in parts of eight until the first week with records; the address from=… opens there without
// JavaScript.
func TestSheetLoadsOlderWeeks(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	s.mustCell(core.MustDate("2026-06-02"), "e-"+emp.ID, "8")
	weeksOf := func(body string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`<tbody id="week-([0-9-]+)"`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	body := s.get("/").body
	seen := weeksOf(body)
	if len(seen) != 6 || seen[0] != "2026-10-26" {
		t.Fatalf("first weeks %v", seen)
	}
	part := regexp.MustCompile(`data-load-older data-part="([^"]+)"`)
	for loads := 0; ; loads++ {
		next := part.FindStringSubmatch(body)
		if next == nil {
			break
		}
		if loads > 5 {
			t.Fatal("the weeks before do not end")
		}
		body = s.get(strings.ReplaceAll(next[1], "&amp;", "&")).body
		if !strings.HasPrefix(body, "<div data-sheet-part>") || strings.Contains(body, "<html") {
			t.Fatalf("a part is a whole page: %.200s", body)
		}
		seen = append(weeksOf(body), seen...)
	}
	if !strings.Contains(body, "data-weeks-start") || seen[0] != "2026-06-01" || len(seen) != 27 {
		t.Errorf("the weeks end at %v after %d weeks", seen[0], len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if core.MustDate(seen[i]) != core.MustDate(seen[i-1]).AddDays(7) {
			t.Fatalf("weeks out of order at %d: %v", i, seen)
		}
	}
	if w := weeksOf(s.get("/?from=2026-08-10").body); len(w) != 17 || w[0] != "2026-08-10" {
		t.Errorf("from an older week: %v", w)
	}
}

// The view and the detail are the person's: kept in the store, for nobody else, and a phone without a choice opens
// the journal.
func TestSheetPreferencesArePerPerson(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	s.employer(me, "Office", core.ModeSchedule, "8")
	if body := s.get("/").body; !strings.Contains(body, `data-view="table" data-auto-view`) {
		t.Error("without a choice the table is the view")
	}
	if a := s.do(s.client, http.MethodGet, "/", nil, map[string]string{"Sec-CH-UA-Mobile": "?1"}); !strings.Contains(a.body, `data-view="journal" data-auto-view`) {
		t.Error("a phone without a choice does not open the journal")
	}
	s.mustPost("/sheet/prefs", url.Values{"view": {"grid"}, "back": {"/"}})
	s.mustPost("/sheet/prefs", url.Values{"detail": {"employers"}, "back": {"/"}})
	s.mustPost("/sheet/prefs", url.Values{"view": {"nonsense"}, "detail": {"nonsense"}, "back": {"/"}})
	body := s.get("/").body
	if !strings.Contains(body, `data-view="grid"`) || strings.Contains(body, "data-auto-view") || !strings.Contains(body, `data-sheet-grid`) ||
		!strings.Contains(body, `data-choose-detail="employers"`) || !regexp.MustCompile(`aria-pressed="true" data-choose-detail="employers"`).MatchString(body) {
		t.Error("the chosen view and detail are not kept")
	}
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if p := cat.PreferencesOf(me.ID); p.View != "grid" || p.Detail != "employers" || len(cat.Preferences) != 1 {
		t.Errorf("preferences %+v", cat.Preferences)
	}
	// Another person keeps their own.
	other := newSiteSharing(t, s)
	you := other.me("ben", false)
	other.employer(you, "Shop", core.ModeSchedule, "8")
	if body := other.get("/").body; !strings.Contains(body, `data-view="table" data-auto-view`) {
		t.Error("the preferences of one person reach another")
	}
}

// The form of a week without JavaScript writes only the fields that changed.
func TestSheetWeekFormWithoutScript(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	col := "e-" + emp.ID
	body := s.get("/").body
	if !strings.Contains(body, `<form method="post" action="/sheet/week" id="wf-2026-11-16"`) ||
		!strings.Contains(body, `name="c:2026-11-16:`+col+`" form="wf-2026-11-16"`) {
		t.Fatal("a week is no form")
	}
	s.mustPost("/sheet/week", url.Values{"back": {"/?from=2026-11-16#week-2026-11-16"},
		"c:2026-11-16:" + col: {"8"}, "w:2026-11-16:" + col: {""},
		"c:2026-11-17:" + col: {"7"}, "w:2026-11-17:" + col: {""},
		"c:2026-11-17:note": {"a note"}, "w:2026-11-17:note": {""}})
	history, _ := s.store.Revisions(context.Background(), me.ID, "")
	// The same week again, and a stale form that only changes one day: the other stays as it is.
	s.mustCell(core.MustDate("2026-11-18"), col, "6")
	a := s.mustPost("/sheet/week", url.Values{"back": {"/?from=2026-11-16#week-2026-11-16"},
		"c:2026-11-16:" + col: {"8"}, "w:2026-11-16:" + col: {"8"},
		"c:2026-11-18:" + col: {""}, "w:2026-11-18:" + col: {""}})
	if loc := a.header.Get("Location"); !strings.Contains(loc, "from=2026-11-16") || !strings.Contains(loc, "done=saved") || !strings.HasSuffix(loc, "#week-2026-11-16") {
		t.Errorf("redirect %s", a.header.Get("Location"))
	}
	if after, _ := s.store.Revisions(context.Background(), me.ID, ""); len(after) != len(history)+1 {
		t.Errorf("%d revisions for one change", len(after)-len(history))
	}
	body = s.get("/").body
	if fieldValue(t, body, monday, col) != "8" || fieldValue(t, body, tuesday, col) != "7" || fieldValue(t, body, wednesday, col) != "6" ||
		fieldValue(t, body, tuesday, "note") != "a note" {
		t.Error("the week form did not book what it said")
	}
}

// A correction of the time account with its reason is on the week page now: the balance follows; a member of an
// organization cannot correct a managed account.
func TestCorrectionOfTheTimeAccount(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	back := "/week?e=" + emp.ID
	if !strings.Contains(s.get(back).body, "data-corrections") {
		t.Fatal("the week page offers no correction")
	}
	if a := s.post("/book/correction", url.Values{"employer": {emp.ID}, "date": {"2026-11-17"}, "minutes": {"2"}, "back": {back}}); a.status != http.StatusBadRequest {
		t.Errorf("a correction without a reason: %d", a.status)
	}
	s.mustPost("/book/correction", url.Values{"employer": {emp.ID}, "date": {"2026-11-17"}, "minutes": {"2"}, "direction": {"less"},
		"reason": {"paid out"}, "back": {back}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.Adjustments) != 1 || cat.Adjustments[0].Minutes != -120 || cat.Adjustments[0].Reason != "paid out" {
		t.Fatalf("adjustments %+v", cat.Adjustments)
	}
	// Weeks 45 and 46 are empty (-80), this week up to today -24, and the payout -2.
	if sh := sheetOf(t, s, me.ID, nil); sh.Stats.Balance != -106*60 {
		t.Errorf("the balance is %d", sh.Stats.Balance)
	}
	s.mustPost("/corrections/"+cat.Adjustments[0].ID+"/remove", url.Values{"back": {back}})
	if cat, _ := s.store.Catalog(context.Background(), me.ID); len(cat.Adjustments) != 0 {
		t.Error("the correction was not removed")
	}
	_, worker, _, _, managed := approvalSite(t)
	if a := worker.post("/book/correction", url.Values{"employer": {managed.ID}, "date": {"2026-11-17"}, "minutes": {"2"},
		"reason": {"own payout"}, "back": {"/"}}); a.status != http.StatusBadRequest || !strings.Contains(a.body, "You may not change this") {
		t.Errorf("a member corrected the managed account: %d", a.status)
	}
	if strings.Contains(worker.get("/week?e="+managed.ID).body, "data-corrections") {
		t.Error("a member is offered the correction")
	}
}

// Notes and projects of an approved week go the way of the hours: a change request with a reason, in the table's
// field, in the journal's line and in the form without JavaScript; one request holds them all, the approved values
// count until the organization decides, and the decision books them and lands in the access log.
func TestSheetChangeRequestForNotesAndProjects(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	studio := core.Project{ID: core.NewID(), Owner: member.ID, EmployerID: managed.ID, Name: "Studio"}
	if err := s.store.Put(context.Background(), studio); err != nil {
		t.Fatal(err)
	}
	col, pcol := "e-"+managed.ID, "p-"+studio.ID
	worker.mustCell(monday, col, "9")
	worker.mustCell(monday, "note", "dentist")
	worker.mustCell(monday, pcol, "2")
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {submissions(t, s, member.ID)[0].ID}, "decision": {"approve"}, "back": {"/"}})
	body := worker.get("/").body
	if strings.Contains(body, `data-slot="c-2026-11-16-note" data-fixed`) || fieldValue(t, body, monday, pcol) != "2" {
		t.Fatal("the note and the project of the approved week are not fields")
	}
	// Without a reason each asks for one; with it each becomes a request, never a change in place.
	for _, c := range [][2]string{{"note", "dentist, then home"}, {pcol, "3"}} {
		if status, a := worker.cell(monday, c[0], c[1], ""); status != http.StatusOK || !a.Reason {
			t.Fatalf("%s without a reason: %d %+v", c[0], status, a)
		}
		if status, a := worker.cell(monday, c[0], c[1], "the afternoon"); status != http.StatusOK || a.Done != "requested" {
			t.Fatalf("%s: %d %+v", c[0], status, a)
		}
	}
	// The journal's line: hours as a span, a project and a note of Tuesday in one save.
	if status, a := worker.cell(tuesday, "line", "8-16:30 30m 1 studio late shift", "forgot Tuesday"); status != http.StatusOK ||
		a.Done != "requested" {
		t.Fatalf("the line: %d %+v", status, a)
	}
	// The form without JavaScript: a note alone.
	form := url.Values{"back": {"/"}, "c:2026-11-18:note": {"training"}, "w:2026-11-18:note": {""}, "reason": {"and Wednesday"}}
	if a := worker.mustPost("/sheet/week", form); !strings.Contains(a.header.Get("Location"), "done=requested") {
		t.Errorf("redirect %s", a.header.Get("Location"))
	}
	// The details of the day go the same way, and keep the note asked for.
	worker.mustPost("/book/dayinfo", url.Values{"employer": {managed.ID}, "date": {monday.String()}, "place": {"Berlin"}, "back": {"/"},
		"reason": {"the place"}})
	if e := worker.entries(member.ID); len(e) != 2 || e[0].Minutes+e[1].Minutes != 540+120 {
		t.Fatalf("the approved values changed before the decision: %+v", e)
	}
	if day := weekOfSheet(t, sheetOf(t, worker, member.ID, nil), monday).Days[0]; day.Note != "dentist" ||
		!day.NoteHasProposal || day.NoteProposed != "dentist, then home" {
		t.Fatalf("the approved note changed or the request is not marked: %+v", day)
	}
	var request core.Submission
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection {
			if request.ID != "" {
				t.Fatal("a second change request instead of one")
			}
			request = x
		}
	}
	if p := request.Proposal; len(p) != 3 ||
		p[0].Minutes != 540 || p[0].Note != "dentist, then home" || p[0].Details == nil || p[0].Details.Place != "Berlin" || len(p[0].Projects) != 1 || p[0].Projects[0].Minutes != 180 ||
		p[1].Minutes != 480 || p[1].Typed != "8-16:30 30m" || p[1].Note != "late shift" || len(p[1].Projects) != 1 ||
		p[1].Projects[0].Minutes != 60 || p[2].Minutes != 0 || p[2].Note != "training" || len(p[2].Projects) != 0 {
		t.Fatalf("change request %+v", p)
	}
	week := weekOfSheet(t, sheetOf(t, worker, member.ID, nil), monday)
	if cells := week.Days[0].Cells; len(cells) != 2 || cells[0].HasProposal || !cells[1].HasProposal || cells[1].Proposed != "3" {
		t.Errorf("Monday's hours did not change, its project did: %+v", cells)
	}
	var chips []string
	for _, c := range week.Days[0].Chips {
		chips = append(chips, c.Text)
	}
	if got := strings.Join(chips, " | "); !strings.Contains(got, "→ Studio 3 h") || !strings.Contains(got, "→ dentist, then home") {
		t.Errorf("the journal's chips do not show the request: %s", got)
	}
	if review := s.get("/orgs/" + org).body; !strings.Contains(review, "Studio 3 h") || !strings.Contains(review, "dentist, then home") {
		t.Error("the reviewer does not see the projects and the note asked for")
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {request.ID}, "decision": {"approve"}, "back": {"/"}})
	booked := map[string]int{}
	for _, e := range worker.entries(member.ID) {
		booked[e.Date.String()+" "+e.ProjectID+" "+e.Typed] += e.Minutes
	}
	if booked["2026-11-16  "] != 540 || booked["2026-11-16 "+studio.ID+" "] != 180 || booked["2026-11-17  8-16:30 30m"] != 480 ||
		booked["2026-11-17 "+studio.ID+" "] != 60 || len(booked) != 4 {
		t.Fatalf("the approved request was not booked: %v", booked)
	}
	days := weekOfSheet(t, sheetOf(t, worker, member.ID, nil), monday).Days
	if days[0].Note != "dentist, then home" || days[1].Note != "late shift" || days[2].Note != "training" {
		t.Errorf("notes %q %q %q", days[0].Note, days[1].Note, days[2].Note)
	}
	if cat, _ := s.store.Catalog(context.Background(), member.ID); len(cat.DayInfos) != 3 {
		t.Errorf("details %+v", cat.DayInfos)
	} else if i, _ := cat.DayInfoOf(managed.ID, monday); i.Place != "Berlin" || i.Note != "dentist, then home" {
		t.Errorf("Monday's details %+v", i)
	}
	if log := worker.get("/access").body; !strings.Contains(log, "Changed") {
		t.Error("the access log lacks the change")
	}
	// Approved again, the week is closed again: a note without a reason asks for one.
	if _, a := worker.cell(monday, "note", "again", ""); !a.Reason {
		t.Errorf("the week approved again took a change in place: %+v", a)
	}
}

// A project of a week handed in (not yet approved) stays as it is, in every view.
func TestSheetHandedInWeekKeepsProjects(t *testing.T) {
	s, worker, _, member, managed := approvalSite(t)
	studio := core.Project{ID: core.NewID(), Owner: member.ID, EmployerID: managed.ID, Name: "Studio"}
	if err := s.store.Put(context.Background(), studio); err != nil {
		t.Fatal(err)
	}
	worker.mustCell(monday, "p-"+studio.ID, "2")
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	for _, c := range [][2]string{{"p-" + studio.ID, "3"}, {"line", "3 studio"}} {
		if _, a := worker.cell(monday, c[0], c[1], "why"); a.Error == "" || a.Done != "" {
			t.Errorf("%s: a handed-in project changed: %+v", c[0], a)
		}
	}
}

// Spans typed into the sheet count for the rest between two working days in the person's overview, across employers.
func TestRestHintFromTypedSpans(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("tom", false)
	late := s.employer(me, "Bar", core.ModeExact, "")
	early := s.employer(me, "Bakery", core.ModeExact, "")
	s.mustCell(monday, "e-"+late.ID, "14-22")
	s.mustCell(tuesday, "e-"+early.ID, "6-14 30m")
	report := s.get("/reports?month=2026-11").body
	if !strings.Contains(report, "2026-11-17: only 8:00 h of rest") {
		t.Errorf("no hint on the rest: %.2000s", report[strings.Index(report, "<main"):])
	}
}

// The note of a day of a managed employer says at its field that the organization sees it, in every view and in the
// form without JavaScript; a private employer's note says nothing of the kind.
func TestSheetNoteVisibleToTheOrganization(t *testing.T) {
	_, worker, _, _, _ := approvalSite(t)
	worker.mustCell(monday, "note", "dentist")
	marker := `data-note-org>visible to ACME<`
	for _, view := range []string{"table", "grid", "journal"} {
		body := worker.get("/?view=" + view).body
		if !strings.Contains(body, marker) {
			t.Errorf("%s: the note does not say the organization sees it", view)
		}
		if view != "journal" && !strings.Contains(body, `name="c:2026-11-16:note" form="wf-2026-11-16" value="dentist" data-raw="dentist" maxlength="2000" autocomplete="off"`) {
			t.Errorf("%s: the note is not a field of the week's form", view)
		}
	}
	if body := worker.get("/?view=table").body; !strings.Contains(body, `aria-describedby="m-2026-11-16-note"`) ||
		!regexp.MustCompile(`id="m-2026-11-16-note" data-slot="m-2026-11-16-note"><span data-note-org>visible to ACME</span>`).MatchString(body) {
		t.Error("the field is not described by the line that names the organization")
	}
	worker.post("/language", url.Values{"lang": {"de"}, "back": {"/"}})
	if body := worker.get("/").body; !strings.Contains(body, `data-note-org>sichtbar für ACME<`) {
		t.Error("the German page does not say it")
	}

	// A person with a private employer alone sees no such line.
	other, _ := trackSite(t)
	me := other.me("ann", false)
	other.employer(me, "Own shop", core.ModeExact, "")
	other.mustCell(monday, "note", "inventory")
	for _, view := range []string{"table", "grid", "journal"} {
		if strings.Contains(other.get("/?view="+view).body, "data-note-org") {
			t.Errorf("%s: a private note says an organization sees it", view)
		}
	}
}
