//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import "testing"

// signOut signs out through the account page.
func (b *browser) signOut() {
	b.t.Helper()
	b.open("/account")
	b.tap(`form[action="/logout"] button`)
	b.must(`location.pathname === '/login'`, "signed out")
	b.settle()
}

// signIn signs in with name and password.
func (b *browser) signIn(name string) {
	b.t.Helper()
	b.typeInto("#login-name", name)
	b.typeInto("#login-password", password)
	b.tap(`form[action="/login"] button[type=submit]`)
	b.must(`location.pathname === '/'`, "signed in as "+name)
	b.settle()
}

// Approval on phones: the member hands in a week with a tap, the administrator rejects it with a reason, and the member
// sees the decision and who looked at the data.
func TestApprovalOnAPhone(t *testing.T) {
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
	b.must(`!!document.querySelector('[data-reviews-none]')`, "approval is on, nothing waits yet")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`!!document.querySelector('[data-approval]')`, "the member's week offers approval")
	b.tap(`[data-submit-week]`)
	b.must(`location.search.includes('done=submitted') && !!document.querySelector('[data-week-locked]')`, "the week is handed in")
	b.settle()
	b.reachable("week handed in")
	b.shot("approval-member")
	b.signOut()

	b.signIn("boss")
	b.open(org)
	b.must(`document.querySelectorAll('[data-review]').length === 1`, "the administrator sees the week")
	b.reachable("approvals")
	b.shot("approval-admin")
	b.tap(`[data-reject] summary`)
	b.typeInto(`[data-reject] textarea`, "Tuesday is missing")
	b.tap(`[data-reject] button[type=submit]`)
	b.must(`location.search.includes('done=decided') && !!document.querySelector('[data-reviews-none]')`, "the week is rejected")
	b.settle()
	b.signOut()

	b.signIn("tom")
	b.must(`document.querySelector('[data-submission="rejected"] [data-reason]')?.textContent.includes('Tuesday is missing') && !!document.querySelector('[data-submit-week]')`,
		"the member reads why, and the week is open again")
	b.settle()
	b.open("/reports")
	b.tap(`a[href="/access"]`)
	b.must(`location.pathname === '/access' && !!document.querySelector('[data-access="decide"]') && !!document.querySelector('[data-access="read"]')`,
		"the access log shows the look and the decision")
	b.settle()
	b.reachable("access log")
	b.shot("access")
}
