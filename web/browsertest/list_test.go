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
