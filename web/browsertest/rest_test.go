//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import "testing"

// settled waits until no animation runs any more, such as a details section that C22 lets grow.
func (b *browser) settled() {
	b.t.Helper()
	b.must(`!document.getAnimations().some(a => a.playState === 'running')`, "the page comes to rest")
}

// On a phone: the regions carry names, an administrator who records time sees their own leave request marked and
// another administrator decides it, the week view reminds of open leave, and the legal templates open and download.
func TestOwnRequestReminderAndTemplatesOnAPhone(t *testing.T) {
	b := setup(t)
	b.signUp("ann", false)
	b.signOut()
	b.signUp("boss", true)
	b.open("/orgs")
	b.must(`document.querySelector('#org-region option[value="DE-BY-AUGSBURG"]')?.textContent === 'Bavaria: city of Augsburg (with the Peace Festival)'`,
		"the regions carry names")
	b.typeInto("#org-name", "ACME")
	b.tap(`form[action="/orgs"] button[type=submit]`)
	b.must(`location.pathname.startsWith('/orgs/')`, "the organization is founded")
	b.settle()
	org := b.eval(`location.pathname`)

	// A template with leave, ann joins as an administrator who records time, approval goes on.
	b.tap(`details:has(form[action$="/template"]) summary`)
	b.typeInto("#tpl-name", "Full time")
	b.eval(`document.querySelector('#tpl-mode').value = 'plan'`)
	for _, day := range []string{"1", "2", "3", "4", "5"} {
		b.typeInto("#tpl-hours-"+day, "8")
	}
	b.typeInto("#tpl-leave", "30")
	b.tap(`form[action$="/template"] button[type=submit]`)
	b.must(`document.querySelectorAll('[data-template]').length === 1`, "the template is saved")
	b.settle()
	b.tap(`details:has(form[action$="/member"]) summary`)
	b.eval(`document.querySelector('#member-role').value = 'admin'`)
	b.eval(`document.querySelector('#member-template').value = 'Full time'`)
	b.tap(`form[action$="/member"] button[type=submit]`)
	b.must(`document.querySelectorAll('[data-member]').length === 2`, "ann joins")
	b.settle()
	b.tap(`label[for=feature-approval]`)
	b.typeInto(`#feature-agreement`, "Works agreement 2026, v1")
	b.tap(`label[for=feature-confirm]`)
	b.tap(`form[data-features] button[type=submit]`)
	b.must(`!!document.querySelector('[data-reviews-none]')`, "approval is on")
	b.settle()

	// The templates: the page says first that they are no legal advice, a template opens and can be downloaded.
	b.tap(`[data-legal-link]`)
	b.must(`location.pathname === '/legal' && !!document.querySelector('[data-legal-disclaimer]')`, "the legal page says what it is")
	b.settle()
	b.tap(`[data-legal-file="works-agreement.md"] summary`)
	b.must(`document.querySelector('[data-legal-file="works-agreement.md"]').open`, "the template opens")
	b.settled()
	b.must(`document.querySelector('[data-legal-file="works-agreement.md"] pre').getBoundingClientRect().height > 100`, "its text shows")
	b.must(`!!document.querySelector('[data-legal-file="works-agreement.md"] a[download][href="/legal/works-agreement.md"]')`, "it can be downloaded")
	// must does not wait for a promise; eval does.
	if got := b.eval(`fetch('/legal/works-agreement.md').then(r => r.ok && r.headers.get('content-disposition').includes('attachment') ? r.text() : '').then(t => t.includes('No legal advice'))`); got != "true" {
		t.Errorf("the download is not the template: %s", got)
	}
	b.reachable("legal templates")
	b.shot("legal")
	b.signOut()

	// Ann reads the reminder of her open leave and asks for a day off.
	b.signIn("ann")
	b.must(`!!document.querySelector('[data-leave-reminder]') && document.querySelector('[data-leave-reminder]').textContent.includes('Leave left in 2026')`,
		"the week view reminds of open leave")
	b.reachable("leave reminder")
	b.shot("leave-reminder")
	b.tap(`[data-absence-form] summary`)
	b.settled()
	// The form, not the quick leave button of a day in the plan above it: the form comes with today, Wednesday.
	b.must(`document.querySelector('#absence-from').value === '2026-11-18'`, "the form comes with today")
	b.tap(`[data-absence-form] form[action="/book/absence"] button[type=submit]`)
	b.must(`[...document.querySelectorAll('[data-absence]')].some(a => a.textContent.includes('2026-11-18'))`, "the leave is requested for today")
	b.settle()
	b.open(org)
	b.must(`document.querySelectorAll('[data-review]').length === 1 && !!document.querySelector('[data-review] [data-review-own]') && !document.querySelector('[data-review] [data-approve]')`,
		"her own request is marked, without buttons")
	b.must(`document.querySelector('[data-review]').textContent.includes('2026-11-18')`, "the request names the day asked for")
	b.reachable("own request")
	b.shot("review-own")
	b.signOut()

	// Another administrator decides it.
	b.signIn("boss")
	b.open(org)
	b.must(`!!document.querySelector('[data-review] [data-approve]') && !document.querySelector('[data-review-own]')`, "boss can decide it")
	b.tap(`[data-review] [data-approve]`)
	b.must(`location.search.includes('done=decided') && !!document.querySelector('[data-reviews-none]')`, "the leave is approved")
	b.settle()

	// The end of the year is near and ann has leave open without a notice on its lapse: boss records it, for today.
	b.must(`document.querySelectorAll('[data-owed]').length === 1`, "the organization owes ann the notice")
	b.tap(`[data-owed] [data-notice-add] summary`)
	b.settled()
	b.must(`document.querySelector('[data-owed] input[name=notified]').value === '2026-11-18'`, "the notice comes with today")
	b.typeInto(`[data-owed] textarea[name=text]`, "Letter: please take your leave by 31 December.")
	b.reachable("owed notice")
	b.shot("notice-owed")
	b.tap(`[data-owed] form[data-notice-form] button[type=submit]`)
	b.must(`location.search.includes('done=saved') && !!document.querySelector('[data-owed-none]')`, "the notice is no longer owed")
	b.settle()
	b.signOut()

	// Ann reads on her week view that the notice was given.
	b.signIn("ann")
	b.must(`!!document.querySelector('[data-reminder-notice]') && document.querySelector('[data-reminder-notice]').textContent.includes('2026-11-18')`,
		"ann reads the notice")
}
