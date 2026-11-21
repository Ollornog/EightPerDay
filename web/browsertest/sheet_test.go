//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import (
	"fmt"
	"strconv"
	"testing"
)

// press sends one key from a keyboard to the focused element: a character, or Enter.
func (b *browser) press(key string) {
	b.t.Helper()
	down := map[string]any{"type": "keyDown", "key": key, "text": key}
	if key == "Enter" {
		down = map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "text": "\r"}
	}
	b.call("Input.dispatchKeyEvent", down, nil)
	b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": key}, nil)
}

var keyCodes = map[string]int{"Tab": 9, "Escape": 27, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40}

// key sends a key that types nothing (Tab, Escape, the arrows), with Shift when shift is set.
func (b *browser) key(name string, shift bool) {
	b.t.Helper()
	modifiers := 0
	if shift {
		modifiers = 8
	}
	event := map[string]any{"type": "rawKeyDown", "key": name, "code": name, "windowsVirtualKeyCode": keyCodes[name], "modifiers": modifiers}
	b.call("Input.dispatchKeyEvent", event, nil)
	event["type"] = "keyUp"
	b.call("Input.dispatchKeyEvent", event, nil)
}

// wide gives the browser the screen of a desk, where the sheet opens as a table.
func (b *browser) wide() {
	b.t.Helper()
	b.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 1280, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil)
}

// saved waits until every field the page sent has its answer.
func (b *browser) saved() {
	b.t.Helper()
	b.must(`!!document.querySelector('[data-sheet]') && !document.querySelector('[data-sheet][data-saving]')`, "the fields are saved")
	b.settle()
}

// field is the selector of the field of a day: the employer's own (e-…) in the table and the grid, the line in the
// journal.
func field(date, col string) string {
	if col == "line" || col == "note" {
		return `#c-` + date + `-` + col
	}
	return `[data-cell][data-date="` + date + `"][data-col^="` + col + `"]`
}

// typeWeek types the hours of five days into the field that has the focus and the ones the keys lead to, and
// counts the keys.
func (b *browser) typeWeek(days []string, values []string, col, next string) int {
	b.t.Helper()
	keys := 0
	for i, day := range days {
		b.must(`document.activeElement?.matches(`+strconv.Quote(field(day, col))+`)`, "the focus waits on "+day)
		for _, c := range values[i] {
			b.press(string(c))
			keys++
		}
		if next == "Enter" {
			b.press("Enter")
		} else {
			b.key(next, false)
		}
		keys++
	}
	return keys
}

var workdays = []string{"2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19", "2026-11-20"}

// The table on a desk: a week of five days with the keyboard alone, Enter leads to the next day and saves. Tab and
// the arrows lead to the next column, Esc takes back what was typed, a value the server cannot read stays marked at
// its field, and the week's row sticks under the head of the table.
func TestTableWeekInTenKeys(t *testing.T) {
	b := setup(t)
	b.wide()
	b.signUp("lea", false)
	b.addEmployer("Office", "schedule", "8")
	b.must(`document.querySelector('[data-sheet]')?.dataset.view === 'table' && !!document.querySelector('[data-sheet-table]')`, "the start page is the table")
	b.reachable("table")
	b.shot("table")
	// A field is the cell (C22's td[data-field] > .input): no frame of its own; today's row is tinted by aria-current.
	b.must(`(() => { const f = getComputedStyle(document.querySelector('input[data-cell][data-col^="e-"]'));
		return f.borderTopWidth === '0px' && f.borderRadius === '0px'; })()`, "a field has no frame of its own")
	b.must(`getComputedStyle(document.querySelector('#today > td')).backgroundColor !== getComputedStyle(document.querySelector('[data-line]:not(#today) > td')).backgroundColor`,
		"today's row stands out")
	// The sticky first cell is tinted like the rest of the row, and opaque (what scrolls under it must not shine through).
	b.must(`(() => { const c = [...document.querySelectorAll('#today > *')].map(e => getComputedStyle(e).backgroundColor);
		return new Set(c).size === 1 && c[0] !== 'rgba(0, 0, 0, 0)' && !c[0].includes('/'); })()`, "today's row is tinted in its first cell too")
	keys := b.typeWeek(workdays, []string{"8", "9", "8", "9", "8"}, "e-", "Enter")
	if keys > 10 {
		t.Fatalf("%d key presses for a week", keys)
	}
	b.must(`document.activeElement?.matches('[data-date="2026-11-21"]')`, "Enter after Friday leads to Saturday")
	b.saved()
	// 8 + 9 + 8 up to today, the clock's Wednesday: +1 against 24 hours; the week holds 42.
	b.must(`document.querySelector('[data-slot="wk-2026-11-16-sum"]')?.textContent.trim() === '42' &&
		document.querySelector('[data-slot="wk-2026-11-16-diff"]')?.textContent.trim() === '+1' &&
		document.querySelector('[data-stat-week]')?.textContent.trim() === '25 / 40 h'`, "the week sums up to today")
	b.shot("table-week")

	// Shift+Enter and ↑ go back, Tab and → at the edge to the next column, ← back.
	b.press("Enter")
	b.must(`document.activeElement?.matches('[data-date="2026-11-22"]')`, "Enter leads to Sunday")
	b.press("Enter")
	b.must(`document.activeElement?.matches('[data-date="2026-11-23"]')`, "Enter leads over the week's row to the next week")
	b.key("ArrowUp", false)
	b.key("ArrowUp", false)
	b.key("ArrowUp", false)
	b.must(`document.activeElement?.matches('[data-cell][data-date="2026-11-20"]')`, "↑ goes back to Friday")
	b.key("Tab", false)
	b.must(`document.activeElement?.id === 'c-2026-11-20-note'`, "Tab leads to the note")
	b.key("ArrowLeft", false)
	b.must(`document.activeElement?.matches('[data-cell][data-date="2026-11-20"][data-col^="e-"]')`, "← leads back to the hours")
	// A span with its break: the field shows the hours, and gives back what was typed.
	for _, c := range "7-12:30 30m" {
		b.press(string(c))
	}
	b.key("Escape", false)
	b.must(`document.activeElement?.value === '8'`, "Esc takes back what was typed")
	b.eval(`document.activeElement.select()`)
	b.call("Input.insertText", map[string]any{"text": "7-12:30 30m"}, nil)
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('[data-cell][data-date="2026-11-20"]').value === '5' &&
		document.querySelector('[data-cell][data-date="2026-11-20"]').dataset.raw === '7-12:30 30m'`, "a span shows its hours")
	b.key("ArrowUp", true)
	b.must(`document.activeElement?.matches('[data-cell][data-date="2026-11-20"]') && document.activeElement.value === '7-12:30 30m'`,
		"in the field the span comes back as typed")
	// A value the server cannot read stays, marked at its field.
	b.eval(`document.activeElement.select()`)
	b.call("Input.insertText", map[string]any{"text": "7 hours"}, nil)
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('[data-cell][data-date="2026-11-20"]').getAttribute('aria-invalid') === 'true' &&
		!document.querySelector('[data-sheet-error]').hidden && document.querySelector('[data-sheet-error]').textContent.includes('Not understood')`,
		"the field shows that it was not understood")
	b.shot("table-error")

	// The week's row sticks right below the head of the table while its days scroll under it.
	b.eval(`(() => { const s = document.querySelector('[data-sheet-scroller]'); const row = document.querySelector('#week-2026-11-09 [data-line]');
		s.scrollTop += row.getBoundingClientRect().top - s.getBoundingClientRect().top + 60; })()`)
	if !b.waitFor(`(() => { const head = document.querySelector('[data-sheet-table] thead th').getBoundingClientRect();
		const week = document.querySelector('#week-2026-11-09 [data-week-row] th').getBoundingClientRect();
		return Math.abs(week.top - head.bottom) < 2; })()`) {
		t.Fatalf("the week's row does not stick under the head: %s", b.eval(`JSON.stringify([document.querySelector('[data-sheet-table] thead').getBoundingClientRect(),
			document.querySelector('#week-2026-11-09 [data-week-row] th').getBoundingClientRect(), document.querySelector('#week-2026-11-09 [data-week-row] th').style.top,
			document.querySelector('[data-sheet-scroller]').getBoundingClientRect(), document.querySelector('[data-sheet-scroller]').scrollTop])`))
	}
	b.must(`(() => { const s = document.querySelector('[data-sheet-scroller]'); s.scrollLeft = 400;
		const day = document.querySelector('#week-2026-11-09 [data-line] th').getBoundingClientRect();
		return Math.abs(day.left - s.getBoundingClientRect().left) < 2; })()`, "the date column sticks on the left")
	b.shot("table-sticky")
	// Today leads back.
	b.tap(`[data-go-today]`)
	b.must(`(() => { const s = document.querySelector('[data-sheet-scroller]').getBoundingClientRect(); const r = document.querySelector('#today').getBoundingClientRect();
		return r.top > s.top && r.bottom < s.bottom; })()`, "the button leads to today")
}

// The journal on a phone: the screen is narrow, so the sheet opens as the journal without being asked. A week in ten
// keys, a second employer by its key, a project by a word, and the line without the focus as chips.
func TestJournalOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("ben", false)
	b.addEmployer("Office", "schedule", "8")
	b.must(`document.querySelector('[data-sheet]')?.dataset.view === 'journal' && location.search.includes('view=journal')`,
		"a narrow screen opens the journal")
	b.reachable("journal")
	b.shot("journal")
	keys := b.typeWeek(workdays, []string{"8", "9", "8", "9", "8"}, "line", "Enter")
	if keys > 10 {
		t.Fatalf("%d key presses for a week", keys)
	}
	b.saved()
	b.must(`document.querySelector('[data-slot="d-2026-11-17-sum"]')?.textContent.trim() === '9' &&
		document.querySelector('[data-slot="ch-2026-11-17"] [data-chip]')?.textContent.includes('9 h')`, "the day shows its hours as a chip")
	// A second employer with a project, booked in the same line by its key and a word.
	b.tap(`[data-open-dialog="new-employer"]`)
	b.typeInto("#new-employer #new-name", "Photo studio")
	b.tap("#new-employer label[for=new-mode-projects_only]")
	b.tap("#new-employer form[data-employer-form] button[type=submit]")
	b.must(`location.search.includes('done=created')`, "the second employer is added")
	b.settle()
	b.open("/week")
	b.eval(`document.querySelector('#pick-employer').value = [...document.querySelectorAll('#pick-employer option')].find(o => o.textContent === 'Photo studio').value`)
	b.eval(`document.querySelector('#pick-employer').form.requestSubmit()`)
	b.must(`!!document.querySelector('#new-project')`, "the studio's week opens")
	b.settle()
	b.typeInto(`#new-project`, "Studio")
	b.tap(`form[action$="/project"] button[type=submit]`)
	b.must(`location.search.includes('done=')`, "the project is made")
	b.settle()
	b.open("/")
	b.must(`!!document.querySelector('[data-hint]')?.textContent.includes('p3 studio')`, "the hint names the key and the project's word")
	b.tap(field("2026-11-18", "line"))
	b.eval(`document.activeElement.select()`)
	b.call("Input.insertText", map[string]any{"text": "8 p3 studio long day"}, nil)
	b.press("Enter")
	b.saved()
	b.must(`(() => { const chips = document.querySelector('[data-slot="ch-2026-11-18"]').textContent;
		return chips.includes('Studio 3 h') && chips.includes('long day') && document.querySelector('[data-slot="d-2026-11-18-sum"]').textContent.trim() === '11'; })()`,
		"the line books both employers, the project and the note")
	b.must(`document.querySelector('#c-2026-11-18-line').value === '8 p3 studio long day'`, "the line is written back the same way")
	// Without the focus the line shows as chips (C22's .chip-input), with the focus as the field; the marks differ.
	b.eval(`document.activeElement.blur()`)
	b.must(`getComputedStyle(document.querySelector('#c-2026-11-18-line')).opacity === '0'`, "without the focus the field is hidden behind its chips")
	b.eval(`document.querySelector('#c-2026-11-18-line').focus()`)
	b.must(`getComputedStyle(document.querySelector('#c-2026-11-18-line')).opacity === '1'`, "with the focus the field shows")
	b.eval(`document.activeElement.blur()`)
	b.must(`(() => { const marks = [...document.querySelectorAll('[data-slot="ch-2026-11-18"] [data-chip][data-mark]')].map(c => getComputedStyle(c, '::before').backgroundColor);
		return marks.length === 2 && marks[0] !== marks[1]; })()`, "each employer has its own mark")
	b.reachable("journal with two employers")
	b.shot("journal-two")
}

// The grid of a week: chosen in the menu, kept for the person; Tab runs over the days, Enter to the next row.
func TestGridWeekInTenKeys(t *testing.T) {
	b := setup(t)
	b.wide()
	b.signUp("eva", false)
	b.addEmployer("Office", "schedule", "8")
	// The menu is C22's popover: Escape closes it and gives the focus back to its button, so does a click outside.
	b.tap(`[data-view-menu] summary`)
	b.must(`document.querySelector('[data-view-menu]').open && getComputedStyle(document.querySelector('[data-sheet-prefs]')).visibility === 'visible'`, "the menu opens")
	b.eval(`document.querySelector('[data-choose-view="grid"]').focus()`)
	b.key("Escape", false)
	b.must(`!document.querySelector('[data-view-menu]').open && document.activeElement === document.querySelector('[data-view-menu] summary')`,
		"Escape closes the menu and the focus goes back")
	b.tap(`[data-view-menu] summary`)
	b.must(`document.querySelector('[data-view-menu]').open`, "the menu opens again")
	b.tap(`[data-hint]`)
	b.must(`!document.querySelector('[data-view-menu]').open`, "a click outside closes the menu")
	b.tap(`[data-view-menu] summary`)
	b.tap(`[data-choose-view="grid"]`)
	b.must(`document.querySelector('[data-sheet]')?.dataset.view === 'grid' && !!document.querySelector('[data-sheet-grid]')`, "the grid is chosen")
	b.settle()
	// Today is a column of the grid: C22's th[aria-current="date"], no utility class of the app's own.
	b.must(`(() => { const h = document.querySelector('[data-sheet-grid] thead th[aria-current="date"]');
		return !!h && !h.classList.contains('bg-accent') && getComputedStyle(h).boxShadow !== 'none'; })()`, "today's head of the grid is C22's column of today")
	b.shot("grid")
	b.tap(field("2026-11-16", "e-"))
	keys := b.typeWeek(workdays, []string{"8", "9", "8", "9", "8"}, "e-", "Tab")
	if keys > 10 {
		t.Fatalf("%d key presses for a week", keys)
	}
	b.saved()
	b.must(`document.querySelector('[data-slot="wk-2026-11-16-sum"]')?.textContent.trim() === '42'`, "the week sums")
	b.tap(field("2026-11-17", "e-"))
	b.press("Enter")
	b.must(`document.activeElement?.id === 'c-2026-11-17-note'`, "Enter leads to the next row of the same day")
	b.reachable("grid")
	b.shot("grid-week")
	// Kept: the page opens as the grid again, and the menu says so.
	b.open("/")
	b.must(`document.querySelector('[data-sheet]')?.dataset.view === 'grid' &&
		document.querySelector('[data-choose-view="grid"]').getAttribute('aria-pressed') === 'true'`, "the grid is kept")
	// The detail is kept as well.
	b.tap(`[data-view-menu] summary`)
	b.tap(`[data-choose-detail="projects"]`)
	b.must(`document.querySelector('[data-choose-detail="projects"]')?.getAttribute('aria-pressed') === 'true'`, "the detail is chosen")
	b.settle()
	b.open("/")
	b.must(`document.querySelector('[data-choose-detail="projects"]')?.getAttribute('aria-pressed') === 'true' &&
		document.querySelector('[data-sheet]')?.dataset.view === 'grid'`, "the detail is kept")
}

// Older weeks load while scrolling up, back to the first week with records, and what was on the screen stays there.
func TestOlderWeeksWhileScrolling(t *testing.T) {
	b := setup(t)
	b.wide()
	b.signUp("ida", false)
	b.addEmployer("Office", "schedule", "8")
	b.open("/?from=2026-06-01#week-2026-06-01")
	b.tap(field("2026-06-02", "e-"))
	b.press("8")
	b.press("Enter")
	b.saved()
	b.open("/")
	b.must(`document.querySelectorAll('[data-week]').length === 6`, "six weeks at first")
	for loads := 0; b.eval(`!!document.querySelector('[data-weeks-start]')`) != "true"; loads++ {
		if loads > 6 {
			t.Fatal("scrolling up does not reach the first week")
		}
		count := b.eval(`document.querySelectorAll('[data-week]').length`)
		anchor := b.eval(`document.querySelector('[data-week]').dataset.week`)
		b.eval(`document.querySelector('[data-sheet-scroller]').scrollTop = 0`)
		b.must(`document.querySelectorAll('[data-week]').length > `+count, "older weeks load")
		b.must(`(() => { const s = document.querySelector('[data-sheet-scroller]').getBoundingClientRect();
			return Math.abs(document.querySelector('[data-week="`+anchor+`"]').getBoundingClientRect().top - s.top) < s.height; })()`, "the view stays")
	}
	b.must(`document.querySelector('[data-week]').dataset.week === '2026-06-01' && document.querySelectorAll('[data-week]').length === 27`,
		"every week since June, in order")
	b.must(`document.querySelector('[data-cell][data-date="2026-06-02"]').value === '8'`, "June has its day")
	b.shot("older-weeks")
	// A week far up is changed in place.
	b.tap(field("2026-06-03", "e-"))
	b.press("6")
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('[data-slot="wk-2026-06-01-sum"]').textContent.trim() === '14'`, "the week far up sums anew")
}

// On a managed employer a change to an approved week becomes a change request in every view, for hours and notes
// alike: the field asks for the reason, the approved value counts until the administrator approves, and then the week
// holds the new values.
func TestChangeRequestInEveryView(t *testing.T) {
	b := setup(t)
	b.signUp("tom", false)
	b.signOut()
	b.signUp("boss", true)
	b.open("/orgs")
	b.typeInto("#org-name", "ACME")
	b.tap(`form[action="/orgs"] button[type=submit]`)
	b.must(`location.pathname.startsWith('/orgs/')`, "the organization is founded")
	b.settle()
	org := b.eval(`location.pathname`)
	b.tap(`details:has(form[action$="/member"]) summary`)
	b.tap(`form[action$="/member"] button[type=submit]`)
	b.must(`document.querySelectorAll('[data-member]').length === 2`, "the member joins")
	b.settle()
	b.tap(`label[for=feature-approval]`)
	b.typeInto(`#feature-agreement`, "Works agreement 2026, v1")
	b.tap(`label[for=feature-confirm]`)
	b.tap(`form[data-features] button[type=submit]`)
	b.must(`!!document.querySelector('[data-reviews-none]')`, "approval is on")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`!!document.querySelector('[data-sheet]')`, "tom's start page is the sheet")
	b.tap(field("2026-11-16", "line"))
	b.eval(`document.activeElement.select()`)
	b.call("Input.insertText", map[string]any{"text": "9"}, nil)
	b.press("Enter")
	b.saved()
	b.open("/week")
	b.tap(`[data-submit-week]`)
	b.must(`location.search.includes('done=submitted')`, "the week is handed in")
	b.settle()
	b.open("/")
	b.must(`!!document.querySelector('[data-slot="c-2026-11-16-line"][data-fixed]') && !!document.querySelector('[data-state="locked"]')`,
		"a week handed in has no fields")
	b.signOut()

	b.signIn("boss")
	b.open(org)
	b.tap(`[data-review] [data-approve]`)
	b.must(`location.search.includes('done=decided')`, "the week is approved")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`!!document.querySelector('[data-week="2026-11-16"] [data-state="approved"]') && document.querySelector('#c-2026-11-16-line')?.value === '9'`,
		"the approved week can be changed")
	// Journal, table and grid: each change asks for the reason and goes as a request.
	for i, step := range []struct{ view, date, col, value, shows string }{
		{"journal", "2026-11-16", "line", "7", "→ 7"},
		{"table", "2026-11-17", "e-", "6", "→ 6"},
		{"grid", "2026-11-18", "e-", "5", "→ 5"},
		{"table", "2026-11-19", "note", "Doctor at nine", "→ Doctor at nine"},
	} {
		b.open("/?view=" + step.view)
		b.tap(field(step.date, step.col))
		b.eval(`document.activeElement.select()`)
		b.call("Input.insertText", map[string]any{"text": step.value}, nil)
		b.press("Enter")
		if !b.waitFor(`document.querySelector('#reason-dialog')?.open === true`) {
			t.Fatalf("%s: the change does not ask for its reason: %s", step.view, b.eval(`JSON.stringify({active: document.activeElement?.id,
				value: document.querySelector(`+strconv.Quote(field(step.date, step.col))+`)?.value, error: document.querySelector('[data-sheet-error]').textContent,
				saving: document.querySelector('[data-sheet]').hasAttribute('data-saving')})`))
		}
		if i == 0 {
			b.shot("change-reason")
		}
		b.typeInto(`#reason-text`, fmt.Sprintf("Left early (%s)", step.view))
		b.tap(`[data-reason-ok]`)
		b.saved()
		b.must(`!!document.querySelector('[data-week="2026-11-16"] [data-state="requested"]') && document.body.textContent.includes('`+step.shows+`')`,
			step.view+": the request is marked and the approved value still counts")
	}
	// The day's page: the details of a day of the approved week go the same way, with the reason in the form.
	b.open("/day?d=2026-11-20")
	b.must(`!!document.querySelector('[data-day-request]') && !!document.querySelector('#di-reason')`,
		"the day's page says that the week is approved")
	b.tap(`[data-day-info] summary`)
	b.typeInto(`#di-place`, "Valley")
	b.typeInto(`#di-reason`, "Forgot the place")
	b.tap(`[data-day-info-form] button[type=submit]`)
	b.must(`location.search.includes('done=requested') && document.querySelector('#di-place')?.value === '' &&
		(document.querySelector('[data-day-pending]')?.textContent || '').includes('place Valley')`,
		"the details are asked for and the approved ones still count")
	b.settle()
	b.reachable("day of an approved week")
	b.shot("day-change-requested")
	// The note of the managed employer's day says that the organization sees it.
	b.open("/?view=table")
	b.must(`document.querySelector('#m-2026-11-19-note [data-note-org]')?.textContent === 'visible to ACME' &&
		document.querySelector('#c-2026-11-19-note')?.getAttribute('aria-describedby') === 'm-2026-11-19-note'`, "the note field names the organization")
	b.reachable("change requested")
	b.shot("change-requested")
	b.signOut()

	b.signIn("boss")
	b.open(org)
	b.must(`(() => { const t = document.querySelector('[data-review] [data-changes]')?.textContent || '';
		return t.includes('Mon 11-16: 7') && t.includes('Tue 11-17: 6') && t.includes('Wed 11-18: 5') &&
			t.includes('Thu 11-19: – („Doctor at nine“)') && t.includes('Fri 11-20: – · place Valley'); })()`, "the administrator reads what is asked")
	b.shot("change-review")
	b.tap(`[data-review] [data-approve]`)
	b.must(`location.search.includes('done=decided') && !!document.querySelector('[data-reviews-none]')`, "the change is approved")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`document.querySelector('#c-2026-11-16-line')?.value === '7' && !document.querySelector('[data-state="requested"]') &&
		document.querySelector('#c-2026-11-19-line')?.value === 'Doctor at nine' &&
		!!document.querySelector('[data-week="2026-11-16"] [data-state="approved"]')`, "the week holds the new values, approved again")
	b.open("/?view=journal")
	b.must(`document.querySelector('[data-day="2026-11-19"] [data-note-org]')?.textContent === 'visible to ACME'`,
		"the journal names the organization next to the note")
	b.open("/day?d=2026-11-20")
	b.must(`document.querySelector('#di-place')?.value === 'Valley' && !document.querySelector('[data-day-pending]')`,
		"the approved details are booked")
	b.open("/access")
	b.must(`!!document.querySelector('[data-access="change"]')`, "the access log shows the change")
	b.settle()
}

// A craftsman on the phone: eight hours in the journal, then the day's clients with their order numbers, and the
// weekly report to print.
func TestProjectsAndWeeklyReportOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("max", false)
	b.addEmployer("Workshop", "schedule", "8")
	b.press("8")
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('#c-2026-11-16-line')?.value === '8'`, "Monday has its hours")
	b.tap(`[data-day="2026-11-16"] [data-day-link]`)
	b.must(`location.pathname === '/day'`, "the day opens")
	b.settle()
	b.typeInto(`#pd-project`, "Miller")
	b.typeInto(`#pd-number`, "A-17")
	b.typeInto(`#pd-hours`, "6,5")
	b.tap(`[data-project-day] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelectorAll('[data-booking]').length === 1 &&
		document.querySelector('[data-day-unbooked]').textContent === '1.5'`, "the client is booked and the rest is open")
	b.settle()
	b.reachable("day")
	b.shot("day")
	b.tap(`a[aria-label="Back"]`)
	b.must(`location.pathname === '/' && document.querySelector('[data-slot="ch-2026-11-16"] [data-chip-open]')?.textContent.startsWith('1.5 h')`,
		"the journal shows what is not booked on projects")
	b.settle()
	b.tap(`[data-week="2026-11-16"] [data-week-report]`)
	b.must(`location.pathname === '/report/week' && document.querySelector('[data-report-project]')?.textContent.includes('Miller · A-17 · 6.5')`,
		"the weekly report names the client with its number")
	b.settle()
	b.reachable("weekly report")
	b.shot("week-report")
}

// Two jobs of a season on the phone: hours in the journal, a tandem flight with its photos booked on the day, and both
// in one week with what was earned and how it was taken.
func TestTwoEmployersAndIncomeOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("sam", false)
	b.addEmployer("Ski school", "schedule", "")
	b.typeInto(field("2026-11-16", "line"), "6")
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('#c-2026-11-16-line')?.value === '6'`, "the ski school has its day")
	b.tap(`[data-open-dialog="new-employer"]`)
	b.typeInto("#new-employer #new-name", "Tandem flights")
	b.tap("#new-employer label[for=new-mode-schedule]")
	b.tap("#new-employer form[data-employer-form] button[type=submit]")
	b.must(`location.search.includes('done=created')`, "the second employer is added")
	b.settle()
	b.open("/?view=table")
	tandem := b.eval(`[...document.querySelectorAll('th[data-col^="e-"]')].find(th => th.textContent.includes('Tandem')).dataset.col.slice(2)`)
	b.open("/day?d=2026-11-17&e=" + tandem)
	b.must(`location.pathname === '/day' && document.body.textContent.includes('Tandem flights')`, "Tuesday of the flights opens")
	b.settle()
	b.typeInto(`#in-amount`, "110")
	b.typeInto(`#in-client`, "A guest")
	b.eval(`document.querySelector('#in-paid').value = 'card'`)
	b.tap(`[data-income-form] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelectorAll('[data-income]').length === 1`, "the flight is booked")
	b.settle()
	b.eval(`document.querySelector('#in-kind').value = 'photos'; document.querySelector('#in-paid').value = 'cash'`)
	b.typeInto(`#in-amount`, "30")
	b.tap(`[data-income-form] button[type=submit]`)
	b.must(`document.querySelectorAll('[data-income]').length === 2`, "the photos are booked")
	b.settle()
	b.reachable("day with income")
	b.shot("day-income")
	b.open("/")
	b.tap(`[data-week="2026-11-16"] [data-all-employers]`)
	b.must(`location.pathname === '/all' && document.querySelectorAll('[data-column]').length === 2 &&
		document.querySelector('[data-card]').textContent === '110.00 EUR' && document.querySelector('[data-cash]').textContent === '30.00 EUR'`,
		"both employers in one week, with what was taken")
	b.settle()
	b.reachable("all employers")
	b.shot("all-employers")
}

// A ski instructor's week with the keyboard alone: whole days as hours, a morning or an afternoon with a letter behind
// the hours, and half a day of leave with an afternoon of work. Each day is its value and Enter, a half day one key
// more; the lines show what was typed, and the week of every employer pays the halves.
func TestHalfDaysWithTheKeyboard(t *testing.T) {
	b := setup(t)
	b.signUp("ina", false)
	b.addEmployer("Ski school", "schedule", "6")
	b.tap(`a[aria-label="Settings of this employer"]`)
	b.must(`location.pathname.startsWith('/employers/')`, "the settings open")
	b.settle()
	b.tap(`details:has(form[action$="/pay"]) summary`)
	b.eval(`document.querySelector('#pay-basis').value = 'per_assignment'; document.querySelector('#pay-unit').value = 'day'`)
	b.typeInto(`#pay-amount`, "120")
	b.typeInto(`#pay-half`, "70")
	b.eval(`document.querySelector("#pay-from").value = "2026-01-01"`)
	b.tap(`form[action$="/pay"] button[type=submit]`)
	b.must(`!!document.querySelector('[data-half-amount]')`, "the rule keeps its amount for half a day")
	b.settle()
	b.reachable("pay with half a day")
	b.open("/")
	keys := 0
	for _, day := range []struct{ date, typed, shows string }{
		{"2026-11-16", "6", "6"}, {"2026-11-17", "3v", "3 AM"}, {"2026-11-18", "3p", "3 PM"}, {"2026-11-19", "6", "6"},
		{"2026-11-20", "3a", "3 AM"},
	} {
		b.must(`document.activeElement?.id === 'c-`+day.date+`-line'`, "the focus waits on "+day.date)
		for _, key := range day.typed {
			b.press(string(key))
		}
		b.press("Enter")
		keys += len(day.typed) + 1
	}
	b.saved()
	for _, day := range []struct{ date, shows string }{{"2026-11-16", "6"}, {"2026-11-17", "3 AM"}, {"2026-11-18", "3 PM"},
		{"2026-11-19", "6"}, {"2026-11-20", "3 AM"}} {
		b.must(`document.querySelector('#c-`+day.date+`-line')?.value === '`+day.shows+`'`, day.date+" is saved")
	}
	// Five days and Enter, and one letter for each of the three half days.
	if keys > 13 {
		t.Fatalf("%d key presses for a week with three half days", keys)
	}
	b.shot("journal-half-days")
	b.typeInto(`#c-2026-11-20-line`, "v 3p")
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('#c-2026-11-20-line').getAttribute('aria-invalid') === 'true'`, "a whole day of leave takes no hours")
	b.typeInto(`#c-2026-11-20-line`, "½v 3p")
	b.press("Enter")
	b.saved()
	b.must(`document.querySelector('#c-2026-11-20-line')?.value === '½V 3 PM' &&
		document.querySelector('#c-2026-11-20-line').getAttribute('aria-invalid') !== 'true'`, "half a day of leave and an afternoon of work")
	b.reachable("journal with half days")
	b.open("/all?w=2026-11-16")
	// 120 + 120 + 3 × 70 = 450 in seven halves of a day.
	b.must(`document.querySelector('[data-week-earned]').textContent === '450.00 EUR' &&
		document.querySelector('[data-average]').textContent === '128.57 EUR'`, "the week pays the halves and averages them")
	b.reachable("all employers with half days")
	b.shot("all-half-days")
}

// Standby on the phone: a rate for active standby per call-out, the standby of a day with its call-outs, and the pay
// in the week.
func TestStandbyOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("ole", false)
	b.addEmployer("Taxi", "schedule", "")
	b.tap(`a[aria-label="Settings of this employer"]`)
	b.must(`location.pathname.startsWith('/employers/')`, "the settings open")
	b.settle()
	b.must(`!!document.querySelector('#emp-standby') && document.querySelector('#emp-standby').value === 'active'`,
		"active standby counts as working time by default")
	b.tap(`details:has(form[action$="/pay"]) summary`)
	b.eval(`document.querySelector('#pay-basis').value = 'standby_active'; document.querySelector('#pay-unit').value = 'assignment'`)
	b.typeInto(`#pay-amount`, "40")
	b.eval(`document.querySelector("#pay-from").value = "2026-01-01"`)
	b.tap(`form[action$="/pay"] button[type=submit]`)
	b.must(`[...document.querySelectorAll('[data-payrule]')].some(r => r.textContent.includes('Standby, active'))`, "the standby rule is kept")
	b.settle()
	b.open("/day?d=2026-11-17")
	b.tap(`[data-day-info] summary`)
	b.typeInto(`#di-sp`, "8")
	b.typeInto(`#di-sa`, "2")
	b.typeInto(`#di-co`, "2")
	b.tap(`[data-day-info] form button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelector('[data-standby-work]')?.textContent.startsWith('2 hours')`,
		"the active standby counts as working time")
	b.settle()
	b.reachable("day with standby")
	b.shot("day-standby")
	b.open("/all?w=2026-11-16")
	b.must(`document.querySelector('[data-week-earned]').textContent === '80.00 EUR' && !!document.querySelector('[data-standby]')`,
		"two call-outs are paid in the week")
	b.reachable("all employers with standby")
}
