//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import "testing"

// Leave planned on a phone: days are tapped in the year's months and booked in one step; the account shows them as
// planned, and the days carry the code of leave.
func TestLeavePlanningOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("ida", false)
	b.addEmployer("Office", "schedule", "8")
	b.tap(`.tab-bar a[href="/leave"]`)
	b.must(`location.pathname === '/leave' && document.querySelectorAll('[data-month]').length === 12`, "the leave page shows the year")
	b.settle()
	b.reachable("leave")
	b.shot("leave")
	for _, day := range []string{"2026-12-21", "2026-12-22", "2026-12-23"} {
		b.tap(`[data-pick="` + day + `"]`)
	}
	b.must(`document.querySelectorAll('[data-leave-plan] input[name=d]:checked').length === 3`, "three days are marked")
	b.tap(`[data-plan-leave]`)
	b.must(`location.search.includes('done=planned') && document.querySelector('[data-planned]')?.textContent === '3' &&
		document.querySelector('[data-cell="2026-12-22"] [data-code]')?.textContent === 'V'`, "the leave is planned and shows in the month")
	b.settle()
	b.must(`document.querySelectorAll('[data-leave]').length === 1`, "one span of leave")
	b.reachable("leave planned")
	b.shot("leave-planned")
}
