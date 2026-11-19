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

// A week of five days with the keyboard alone: the first empty day has the focus, a number and Enter save it and
// hand the focus to the next day. Ten key presses, no finger and no mouse.
func TestAWeekInTenKeys(t *testing.T) {
	b := setup(t)
	b.signUp("lea", false)
	b.addEmployer("Office", "schedule", "8")
	b.must(`location.pathname === '/' && document.querySelectorAll('[data-week]').length === 2`, "the start page is the list")
	b.reachable("list")
	b.shot("list")
	keys := 0
	days := []string{"2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19", "2026-11-20"}
	for i, day := range days {
		b.must(`document.activeElement?.id === 'v-`+day+`'`, "the focus waits on "+day)
		b.press(strconv.Itoa(8 + i%2))
		b.press("Enter")
		keys += 2
		b.must(`document.querySelector('#v-`+day+`')?.value === '`+strconv.Itoa(8+i%2)+`' && location.search.includes('done=saved')`, day+" is saved")
		b.settle()
	}
	if keys > 10 {
		t.Fatalf("%d key presses for a week", keys)
	}
	b.must(`!document.activeElement || document.activeElement === document.body`, "a full week leaves no field waiting")
	// 8 + 9 + 8 up to today, the clock's Wednesday: +1 against 24 hours.
	b.must(`document.querySelector('[data-week="2026-11-16"] [data-week-sum]').textContent === '25' &&
		document.querySelector('[data-week="2026-11-16"] [data-week-diff]').textContent === '+1'`, "the week sums up to today")
	b.shot("list-week")

	// A code instead of hours, and a field that says early it cannot read a value.
	b.typeInto(`#v-2026-11-20`, "7 Stunden")
	b.must(`!document.querySelector('#v-2026-11-20').checkValidity()`, "the field refuses what is neither hours nor a code")
	b.typeInto(`#v-2026-11-20`, "½v")
	b.must(`document.querySelector('#v-2026-11-20').checkValidity()`, "the field takes a code")
	b.tap(`[data-week="2026-11-16"] footer button[type=submit]`)
	b.must(`document.querySelector('#v-2026-11-20')?.value === '½V' && !!document.querySelector('[data-cell="2026-11-20"] [data-code]')`,
		"the half day of leave shows in the row and in the month")
	b.settle()
	b.reachable("list with codes")
}

// The weeks that lack something are named on top; a tap leads to the week, which is changed like this one. On a wide
// screen this week and last week stand side by side, on a phone one above the other.
func TestListNoticesAndOlderWeeks(t *testing.T) {
	b := setup(t)
	b.signUp("ben", false)
	b.addEmployer("Office", "schedule", "8")
	b.must(`document.querySelectorAll('[data-gap]').length === 3`, "three weeks lack days")
	phone := [2]box{b.rect(`[data-week="2026-11-09"]`), b.rect(`[data-week="2026-11-16"]`)}
	if phone[1].Top <= phone[0].Top || phone[0].Left != phone[1].Left {
		t.Errorf("on a phone the weeks stand one above the other: %+v", phone)
	}
	b.tap(`[data-gap="2026-11-02"] a`)
	b.must(`location.search.includes('w=2026-11-02') && !!document.querySelector('[data-week="2026-11-02"]')`, "the notice leads to its week")
	b.settle()
	b.typeInto(`#v-2026-11-02`, "8")
	b.tap(`[data-week="2026-11-02"] footer button[type=submit]`)
	b.must(`document.querySelector('#v-2026-11-02')?.value === '8' && document.querySelector('[data-gap="2026-11-02"] [data-gap-missing]')?.textContent.startsWith('4 ')`,
		"the older week takes its hours and the notice counts one day less")
	b.settle()

	b.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 1280, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil)
	b.open("/")
	wide := [2]box{b.rect(`[data-week="2026-11-09"]`), b.rect(`[data-week="2026-11-16"]`)}
	if wide[0].Top != wide[1].Top || wide[1].Left <= wide[0].Right {
		t.Errorf("on a wide screen the weeks stand side by side: %+v", wide)
	}
	b.shot("list-wide")
	b.reachable(fmt.Sprintf("list at %v wide", 1280))
}

// The list of every week opens at the newest week, at the bottom; scrolling up loads the weeks before it until the
// first week with records, and what was on the screen stays there.
func TestEveryWeekWhileScrolling(t *testing.T) {
	b := setup(t)
	b.signUp("eva", false)
	b.addEmployer("Office", "schedule", "8")
	// A day in June makes the first week with records.
	b.open("/?w=2026-06-01")
	b.typeInto(`#v-2026-06-02`, "8")
	b.tap(`[data-week="2026-06-01"] footer button[type=submit]`)
	b.must(`document.querySelector('#v-2026-06-02')?.value === '8'`, "June has its day")
	b.settle()
	b.tap(`[data-all-weeks]`)
	b.must(`location.pathname === '/weeks' && document.querySelectorAll('[data-week]').length === 8`, "eight weeks at first")
	b.settle()
	b.must(`scrollY + innerHeight >= document.documentElement.scrollHeight - 2`, "the page opens at the bottom")
	b.must(`[...document.querySelectorAll('[data-week]')].at(-1).dataset.week === '2026-11-16'`, "the newest week is the last")
	for loads := 0; b.eval(`!!document.querySelector('[data-weeks-start]')`) != "true"; loads++ {
		if loads > 6 {
			t.Fatal("scrolling up does not reach the first week")
		}
		count := b.eval(`document.querySelectorAll('[data-week]').length`)
		anchor := b.eval(`document.querySelector('[data-week]').dataset.week`)
		b.eval(`window.scrollTo({top: 0, behavior: 'instant'})`)
		b.must(`document.querySelectorAll('[data-week]').length > `+count, "older weeks load")
		// The week that was first is still where the eye left it, not pushed away by what loaded above it.
		b.must(`Math.abs(document.querySelector('[data-week="`+anchor+`"]').getBoundingClientRect().top) < innerHeight`, "the view stays")
	}
	b.must(`document.querySelector('[data-week]').dataset.week === '2026-06-01' && document.querySelectorAll('[data-week]').length === 25`,
		"every week since June, in order")
	b.reachable("every week")
	b.shot("every-week")
	// A week far up is changed in place and the page returns to it.
	b.typeInto(`#v-2026-06-03`, "6")
	b.tap(`[data-week="2026-06-01"] footer button[type=submit]`)
	b.must(`location.search.includes('from=2026-06-01') && location.hash === '#week-2026-06-01' && document.querySelector('#v-2026-06-03')?.value === '6'`,
		"the change returns to its week")
	b.settle()
}

// On a managed employer a change to an approved week becomes a change request; the approved values count until the
// administrator approves it, and then the week holds the new values, approved again.
func TestChangeRequestOnAPhone(t *testing.T) {
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
	b.must(`document.querySelectorAll('[data-week]').length === 2`, "tom's start page is the list")
	b.typeInto(`#v-2026-11-16`, "9")
	b.tap(`[data-week="2026-11-16"] footer button[type=submit]`)
	b.must(`document.querySelector('#v-2026-11-16')?.value === '9'`, "Monday is booked")
	b.settle()
	b.open("/week")
	b.tap(`[data-submit-week]`)
	b.must(`location.search.includes('done=submitted')`, "the week is handed in")
	b.settle()
	b.open("/")
	b.must(`!!document.querySelector('[data-week="2026-11-16"] [data-fixed]') && !document.querySelector('#v-2026-11-16')`, "a week handed in has no fields")
	b.signOut()

	b.signIn("boss")
	b.open(org)
	b.tap(`[data-review] [data-approve]`)
	b.must(`location.search.includes('done=decided')`, "the week is approved")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`!!document.querySelector('[data-week="2026-11-16"] [data-approved]') && document.querySelector('#v-2026-11-16')?.value === '9'`,
		"the approved week can be changed")
	b.typeInto(`#v-2026-11-16`, "7")
	b.typeInto(`#reason-2026-11-16`, "Left for the dentist")
	b.tap(`[data-week="2026-11-16"] [data-request-change]`)
	b.must(`location.search.includes('done=requested') && !!document.querySelector('[data-week="2026-11-16"] [data-requested]') &&
		document.querySelector('#v-2026-11-16')?.value === '9' && document.querySelector('[data-day="2026-11-16"] [data-proposed]')?.textContent === '→ 7'`,
		"the request is marked and the approved value still counts")
	b.settle()
	b.reachable("change requested")
	b.shot("change-requested")
	b.signOut()

	b.signIn("boss")
	b.open(org)
	b.must(`document.querySelector('[data-review] [data-changes]')?.textContent.includes('Mon 11-16: 7')`, "the administrator reads what is asked")
	b.shot("change-review")
	b.tap(`[data-review] [data-approve]`)
	b.must(`location.search.includes('done=decided') && !!document.querySelector('[data-reviews-none]')`, "the change is approved")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`document.querySelector('#v-2026-11-16')?.value === '7' && !document.querySelector('[data-requested]') && !!document.querySelector('[data-week="2026-11-16"] [data-approved]')`,
		"the week holds the new value, approved again")
	b.open("/access")
	b.must(`!!document.querySelector('[data-access="change"]')`, "the access log shows the change")
	b.settle()
}

// A craftsman on the phone: eight hours in the list, then the day's clients with their order numbers, and the weekly
// report to print.
func TestProjectsAndWeeklyReportOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("max", false)
	b.addEmployer("Workshop", "schedule", "8")
	b.press("8")
	b.press("Enter")
	b.must(`document.querySelector('#v-2026-11-16')?.value === '8'`, "Monday has its hours")
	b.settle()
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
	b.must(`location.pathname === '/' && document.querySelector('[data-day="2026-11-16"] [data-unbooked]')?.textContent === '1.5 open'`,
		"the list shows what is not booked on projects")
	b.settle()
	b.tap(`[data-week="2026-11-16"] [data-week-report]`)
	b.must(`location.pathname === '/report/week' && document.querySelector('[data-report-project]')?.textContent.includes('Miller · A-17 · 6.5')`,
		"the weekly report names the client with its number")
	b.settle()
	b.reachable("weekly report")
	b.shot("week-report")
}
