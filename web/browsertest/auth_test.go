//go:build browser

// SPDX-License-Identifier: MIT

package browsertest

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Ollornog/EightPerDay/web/auth"
)

const password = "correct horse battery staple"

// The first account comes from the one-time link, with a passkey, on a phone; then it signs out and in again with the
// same passkey, without typing a name.
func TestFirstAccountWithAPasskey(t *testing.T) {
	b := setup(t)
	authenticator := b.passkeys()
	b.open("/login")
	b.must(`document.body.textContent.includes('No account yet')`, "the sign-in page says where the first account comes from")
	b.shot("login")
	b.reachable("sign-in")

	token, err := b.auth.FirstSetupLink(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	b.open("/setup?token=" + token)
	b.must(`document.querySelector('#method-passkey').checked`, "the passkey is the default")
	b.must(`document.querySelector('[data-password-fields]').hidden`, "the password fields hide while a passkey is chosen")
	b.reachable("setup")
	b.shot("setup")
	b.typeInto("#setup-name", "mara")
	b.typeInto("#setup-display", "Mara Schmidt")
	b.tap("form[data-setup] button[type=submit]")
	b.must(`location.pathname === '/' && document.body.textContent.includes('Who do you work for?')`, "the passkey created the account")
	b.settle()
	if n := b.credentials(authenticator); n != 1 {
		t.Fatalf("the authenticator holds %d passkeys", n)
	}
	b.shot("home")
	b.reachable("home")

	b.tap(`.tab-bar a[href="/account"]`)
	b.must(`location.pathname === '/account'`, "the tab bar leads to the account")
	b.settle()
	b.must(`document.querySelector('.tab-bar [aria-current=page]')?.getAttribute('href') === '/account' && getComputedStyle(document.querySelector('.tab-bar')).position === 'fixed' && Math.abs(document.querySelector('.tab-bar').getBoundingClientRect().bottom - innerHeight) < 1`,
		"the tab bar sits at the bottom and marks the open page")
	b.reachable("account")
	b.shot("account")
	b.tap(`form[action="/logout"] button`)
	b.must(`location.pathname === '/login'`, "signed out")
	b.settle()
	b.tap("[data-passkey-login-start]")
	b.must(`location.pathname === '/' && document.body.textContent.includes('Who do you work for?')`, "the passkey signs in again")
	b.settle()
}

// Password and authenticator app, the way a person without a passkey sets them up.
func TestPasswordAndAuthenticatorApp(t *testing.T) {
	b := setup(t)
	token, err := b.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: "tom"}, auth.InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	b.open("/setup?token=" + token)
	b.tap("label[for=method-totp]")
	b.must(`!document.querySelector('[data-password-fields]').hidden`, "the password fields show")
	b.typeInto("#setup-password", password)
	b.typeInto("#setup-password2", password)
	b.tap("form[data-setup] button[type=submit]")
	b.must(`!!document.querySelector('img.qr-code') && document.querySelector('img.qr-code').naturalWidth > 0`, "the QR code shows")
	b.settle()
	b.reachable("authenticator app")
	b.shot("setup-totp")
	secret := b.text(".secret-key")
	if !regexp.MustCompile(`^[A-Z2-7]{16,}$`).MatchString(secret) {
		t.Fatalf("secret %q", secret)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	b.typeInto("#code", code)
	b.tap("form button[type=submit]")
	b.must(`location.pathname === '/' && document.body.textContent.includes('Who do you work for?')`, "the code finished the setup")
	b.settle()

	b.tap(`.tab-bar a[href="/account"]`)
	b.must(`location.pathname === '/account'`, "account")
	b.settle()
	b.tap(`form[action="/logout"] button`)
	b.must(`location.pathname === '/login'`, "signed out")
	b.settle()
	b.typeInto("#login-name", "tom")
	b.typeInto("#login-password", password)
	b.tap(`form[action="/login"] button[type=submit]`)
	b.must(`location.pathname === '/login/code'`, "the password asks for the code")
	b.settle()
	b.shot("login-code")
	next, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	b.typeInto("#code", next)
	b.tap(`form[action="/login/code"] button[type=submit]`)
	b.must(`location.pathname === '/'`, "signed in with password and code")
	b.settle()
}

// The about dialog opens with a finger, names the logo's author, and closes again.
func TestAboutDialog(t *testing.T) {
	b := setup(t)
	b.open("/login")
	b.tap(`header [data-open-dialog="about-dialog"]`)
	b.must(`document.querySelector('#about-dialog').open`, "the about dialog opens")
	b.must(`document.querySelector('#about-dialog [data-attribution]').textContent.includes('Alfredo Creates') && document.querySelector('#about-dialog [data-attribution]').textContent.includes('Flaticon')`,
		"the dialog names the logo's author")
	b.reachable("about dialog")
	b.shot("about")
	b.tap(`#about-dialog [data-close-dialog]`)
	b.must(`!document.querySelector('#about-dialog').open`, "the dialog closes")
}

// Removing something asks first, in a dialog of the page and not in the browser's own box, and a cancel keeps it.
func TestRemovingAsksFirst(t *testing.T) {
	b := setup(t)
	authenticator := b.passkeys()
	token, _ := b.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: "ben"}, auth.InviteTTL)
	b.open("/setup?token=" + token)
	b.tap("form[data-setup] button[type=submit]")
	b.must(`location.pathname === '/'`, "set up with a passkey")
	b.settle()
	b.open("/account")
	// A second passkey, then the first one goes.
	b.typeInto("#new-passkey-name", "Laptop")
	b.call("WebAuthn.addVirtualAuthenticator", map[string]any{"options": map[string]any{
		"protocol": "ctap2", "transport": "usb", "hasResidentKey": true, "hasUserVerification": true,
		"isUserVerified": true, "automaticPresenceSimulation": true,
	}}, nil)
	b.call("WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": authenticator}, nil)
	b.tap("[data-passkey-add-start]")
	if !b.waitFor(`location.search === '?done=passkey' && document.querySelectorAll('[data-passkey]').length === 2`) {
		t.Fatalf("the second passkey is not added: %s %s | %s", b.eval(`document.querySelector('[data-passkey-add]').dataset.failure`), b.text("[data-passkey-add] [data-passkey-error]"),
			b.eval(`fetch('/account/passkeys/begin', {method: 'POST', body: '{}'}).then(r => r.json()).then(o => navigator.credentials.create({publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(o.publicKey)})).then(c => 'ok ' + c.id, e => e.name + ': ' + e.message)`))
	}
	b.settle()
	b.tap(`[data-passkey] form[data-confirm] button`)
	b.must(`document.querySelector('#confirm-dialog').open`, "removing asks first")
	b.reachable("question")
	b.shot("confirm")
	b.tap(`#confirm-dialog [data-close-dialog]`)
	b.must(`!document.querySelector('#confirm-dialog').open && document.querySelectorAll('[data-passkey]').length === 2`, "cancel keeps the passkey")
	b.tap(`[data-passkey] form[data-confirm] button`)
	b.must(`document.querySelector('#confirm-dialog').open`, "asks again")
	b.tap(`#confirm-dialog [data-confirm-ok]`)
	b.must(`location.search === '?done=saved' && document.querySelectorAll('[data-passkey]').length === 1`, "the confirmed removal removes")
	b.settle()
	// The last passkey cannot go: there would be no way left to sign in.
	b.tap(`[data-passkey] form[data-confirm] button`)
	b.tap(`#confirm-dialog [data-confirm-ok]`)
	b.must(`document.body.textContent.includes('last way to sign in') && document.querySelectorAll('[data-passkey]').length === 1`, "the last way to sign in stays")
}

// The administration on a phone: an invitation shows its link once.
func TestInvitationOnAPhone(t *testing.T) {
	b := setup(t)
	token, _ := b.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: "boss", Admin: true}, auth.InviteTTL)
	b.open("/setup?token=" + token)
	b.tap("label[for=method-password]")
	b.typeInto("#setup-password", password)
	b.typeInto("#setup-password2", password)
	b.tap("form[data-setup] button[type=submit]")
	b.must(`location.pathname === '/'`, "set up with a password")
	b.settle()
	b.open("/admin")
	b.typeInto("#invite-name", "ben")
	b.tap(`form[action="/admin/invite"] button[type=submit]`)
	b.must(`!!document.querySelector('input[data-select-on-focus]') && document.querySelector('input[data-select-on-focus]').value.includes('/setup?token=')`, "the link shows")
	b.settle()
	b.reachable("administration")
	b.shot("admin")
}
