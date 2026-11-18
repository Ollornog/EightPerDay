//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import (
	"context"
	"testing"

	"github.com/Ollornog/EightPerDay/web/auth"
)

// signUp makes an account with a password through an invitation, on the phone.
func (b *browser) signUp(name string, admin bool) {
	b.t.Helper()
	token, err := b.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: name, Admin: admin}, auth.InviteTTL)
	if err != nil {
		b.t.Fatal(err)
	}
	b.open("/setup?token=" + token)
	b.tap("label[for=method-password]")
	b.typeInto("#setup-password", password)
	b.typeInto("#setup-password2", password)
	b.tap("form[data-setup] button[type=submit]")
	b.must(`location.pathname === '/'`, "signed up")
	b.settle()
}

// addEmployer fills the start form with a finger: name, mode, hours per weekday.
func (b *browser) addEmployer(name, mode, hours string) {
	b.t.Helper()
	b.typeInto("#new-name", name)
	b.tap("label[for=new-mode-" + mode + "]")
	if hours != "" {
		b.typeInto("#new-hours", hours)
	}
	b.tap("form[data-employer-form] button[type=submit]")
	b.must(`location.search.includes('done=created')`, "the employer is added")
	b.settle()
}

// The craftsman: the plan counts, he books only that he was sick on one day and left early on another, and confirms
// the week. All on a phone, with a thumb.
func TestPlanOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("tom", false)
	b.reachable("start")
	b.shot("start")
	b.addEmployer("Site work", "plan", "8")
	b.open("/week")
	b.must(`document.body.textContent.includes('as planned')`, "the week shows the plan")
	b.reachable("plan week")
	b.shot("plan")
	// Sick on the first day of the week.
	b.tap(`[data-day] form[action="/book/absence"] input[value=sick] ~ button`)
	b.must(`location.search.includes('done=saved') && document.querySelector('[data-day] .badge')?.textContent.includes('Sick')`, "sick shows on the day")
	b.settle()
	// The clock of the program stands on a Wednesday: Tuesday left early by an hour and a half.
	b.tap(`[data-day="2026-11-17"] [data-deviation] summary`)
	b.typeInto(`[data-deviation][open] input[name=minutes]`, "1:30")
	b.tap(`[data-deviation][open] button[data-earlier]`)
	b.must(`location.search.includes('done=saved') && document.body.textContent.includes('Deviation -1:30')`, "the deviation is booked")
	b.settle()
	b.tap(`[data-confirm-week]`)
	b.must(`!!document.querySelector('[data-week-confirmed]')`, "the week is confirmed")
	b.settle()
	b.reachable("confirmed week")
	b.shot("plan-confirmed")
}

// Exact: the stopwatch starts and stops with a finger, and the free-text line says what it understood while typing.
func TestStopwatchAndLineOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("mara", false)
	b.addEmployer("Agency", "exact", "")
	b.open("/week")
	b.tap(`form[action="/timer/start"] button[type=submit]`)
	b.must(`!!document.querySelector('[data-timer-elapsed]')`, "the stopwatch runs")
	b.settle()
	b.shot("exact")
	b.reachable("exact")
	b.tap(`form[action="/timer/stop"] button`)
	b.must(`location.search.includes('done=stopped') && !document.querySelector('[data-timer-elapsed]')`, "the stopwatch stops")
	b.settle()
	// A collapsible card opens with a tap on its head.
	b.tap(`[data-span-form] summary`)
	b.must(`document.querySelector('[data-span-form]').open && document.querySelector('#span-start').getClientRects().length > 0`, "the card opens")
	b.settle()
	b.shot("exact-span")
	b.reachable("span card")
	b.typeInto("#line", "8-12:30 30m Pause @Website #meeting")
	b.must(`document.querySelector('[data-line-preview]').textContent.includes('8:00–12:30') && document.querySelector('[data-line-preview]').textContent.includes('@Website')`,
		"the line says what it understood")
	b.settle()
	b.tap(`form[action="/book/line"] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelectorAll('[data-entry]').length >= 2`, "the line is booked")
	b.settle()
}

// Weekly total, projects only and the reports, each on the phone.
func TestOtherModesAndReportsOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("ina", true)
	b.addEmployer("Cleaning", "week_sum", "")
	b.open("/week")
	// The browser knows a duration from the field's pattern (also: the pattern compiles in the browser's syntax).
	b.typeInto("#week-hours", "7 Stunden")
	b.must(`!document.querySelector('#week-hours').checkValidity()`, "the field says early that this is no duration")
	b.typeInto("#week-hours", "12,5")
	b.must(`document.querySelector('#week-hours').checkValidity()`, "the field takes a duration")
	b.tap(`form[action="/book/week"] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelector('#week-hours').value === '12:30'`, "the week total is kept")
	b.settle()
	b.shot("week-sum")
	b.reachable("week sum")

	b.tap(`[data-open-dialog="new-employer"]`)
	b.must(`document.querySelector('#new-employer').open`, "the dialog for a new employer opens")
	b.typeInto("#new-employer #new-name", "Design studio")
	b.tap("#new-employer label[for=new-mode-projects_only]")
	b.tap("#new-employer form[data-employer-form] button[type=submit]")
	b.must(`location.search.includes('done=created')`, "the second employer is added")
	b.settle()
	b.open("/week")
	b.typeInto("#new-project", "Logo")
	b.tap(`form[action$="/project"] button[type=submit]`)
	b.must(`document.querySelectorAll('[data-project-row]').length === 1`, "the project is added")
	b.settle()
	b.typeInto(`[data-project-row] input`, "3")
	b.tap(`form[action="/book/projects"] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelector('[data-project-row] input').value === '3:00'`, "the project hours are kept")
	b.settle()
	b.shot("projects")
	b.reachable("projects")

	// The employer switcher: a choice shows the other employer at once.
	b.eval(`(() => { const s = document.querySelector('#pick-employer'); s.value = [...s.options].find(o => o.text === 'Cleaning').value; s.dispatchEvent(new Event('change', {bubbles: true})); })()`)
	b.must(`!!document.querySelector('#week-hours')`, "the switcher shows the other employer")
	b.settle()
	// The weekly total split into days, after the question: Monday to Friday 2:30 each.
	b.tap(`[data-spread]`)
	b.must(`document.querySelector('#confirm-dialog').open`, "splitting asks first")
	b.tap(`#confirm-dialog [data-confirm-ok]`)
	b.must(`location.search.includes('done=spread') && document.querySelector('[data-day="2026-11-16"] input').value === '2:30'`, "the week is split into days")
	b.settle()
	b.reachable("week split into days")
	b.shot("week-split")

	b.tap(`.tab-bar a[href="/reports"]`)
	b.must(`location.pathname === '/reports' && document.querySelectorAll('[data-report]').length === 2`, "the report lists both employers")
	b.settle()
	b.reachable("reports")
	b.shot("reports")

	b.open("/orgs")
	b.typeInto("#org-name", "ACME")
	b.tap(`form[action="/orgs"] button[type=submit]`)
	b.must(`location.pathname.startsWith('/orgs/')`, "the organization is founded")
	b.settle()
	b.reachable("organization")
	b.shot("org")

	b.open("/")
	b.tap(`a[aria-label="Settings of this employer"]`)
	b.must(`location.pathname.startsWith('/employers/')`, "the settings of the employer open")
	b.settle()
	b.reachable("employer settings")
	b.shot("employer")
	// A night surcharge, typed with a thumb.
	b.tap(`details:has(form[action$="/surcharge"]) summary`)
	b.typeInto("#sc-percent", "25")
	b.typeInto("#sc-start", "22:00")
	b.typeInto("#sc-end", "6:00")
	b.tap(`form[action$="/surcharge"] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && document.querySelectorAll('[data-surcharge]').length === 1`, "the surcharge is saved")
	b.settle()
	b.reachable("employer pay")
	b.shot("employer-pay")
}
