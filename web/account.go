// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"net/http"

	"github.com/Ollornog/EightPerDay/web/auth"
)

type accountData struct {
	Enrollment *auth.TOTPEnrollment
	MinLength  int
}

func (h *handler) accountPage(w http.ResponseWriter, r *http.Request) {
	h.renderAccount(w, r, http.StatusOK, accountData{}, "")
}

func (h *handler) renderAccount(w http.ResponseWriter, r *http.Request, status int, data accountData, failure string) {
	data.MinLength = auth.MinPasswordLength
	h.render(w, r, status, "account", view{Title: h.t(r, "account.title"), Data: data, Error: failure})
}

// accountError answers a refused change on the account page, or reports false when the error is not the person's to
// fix.
func (h *handler) accountError(w http.ResponseWriter, r *http.Request, err error) bool {
	var text string
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		text = h.t(r, "login.throttled", int(throttled.Wait.Seconds()))
	case errors.Is(err, auth.ErrInvalid):
		text = h.t(r, "account.error.invalid")
	case errors.Is(err, auth.ErrLastMethod):
		text = h.t(r, "account.error.lastmethod")
	case errors.Is(err, auth.ErrPasswordTooShort):
		text = h.t(r, "password.error.short", auth.MinPasswordLength)
	case errors.Is(err, auth.ErrPasswordTooLong):
		text = h.t(r, "password.error.long")
	case errors.Is(err, auth.ErrBadName):
		text = h.t(r, "account.error.name")
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, auth.ErrCeremony):
		text = h.t(r, "login.expired")
	default:
		return false
	}
	h.renderAccount(w, r, http.StatusBadRequest, accountData{}, text)
	return true
}

func (h *handler) accountDone(w http.ResponseWriter, r *http.Request, err error, done string) {
	if err != nil {
		if !h.accountError(w, r, err) {
			h.failed(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/account?done="+done, http.StatusSeeOther)
}

func (h *handler) accountName(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	h.accountDone(w, r, h.options.Auth.SetDisplayName(r.Context(), a.ID, r.PostFormValue("display_name")), "saved")
}

func (h *handler) accountPassword(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	if r.PostFormValue("password") != r.PostFormValue("password2") {
		h.renderAccount(w, r, http.StatusBadRequest, accountData{}, h.t(r, "password.error.mismatch"))
		return
	}
	err := h.options.Auth.SetPassword(r.Context(), h.clientAddress(r), a.ID, r.PostFormValue("current"), r.PostFormValue("password"))
	h.accountDone(w, r, err, "password")
}

func (h *handler) accountPasswordRemove(w http.ResponseWriter, r *http.Request) {
	h.accountDone(w, r, h.options.Auth.RemovePassword(r.Context(), accountFrom(r.Context()).ID), "saved")
}

func (h *handler) accountTOTPBegin(w http.ResponseWriter, r *http.Request) {
	enrollment, err := h.options.Auth.BeginEnableTOTP(r.Context(), accountFrom(r.Context()).ID)
	if err != nil {
		h.accountDone(w, r, err, "")
		return
	}
	h.renderAccount(w, r, http.StatusOK, accountData{Enrollment: enrollment}, "")
}

func (h *handler) accountTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	a := accountFrom(r.Context())
	ceremony := r.PostFormValue("ceremony")
	err := h.options.Auth.FinishEnableTOTP(r.Context(), a.ID, ceremony, r.PostFormValue("code"))
	if errors.Is(err, auth.ErrInvalid) {
		if enrollment := h.options.Auth.PendingEnableTOTP(a.ID, ceremony); enrollment != nil {
			h.renderAccount(w, r, http.StatusBadRequest, accountData{Enrollment: enrollment}, h.t(r, "code.invalid"))
			return
		}
	}
	h.accountDone(w, r, err, "totp")
}

func (h *handler) accountTOTPRemove(w http.ResponseWriter, r *http.Request) {
	err := h.options.Auth.DisableTOTP(r.Context(), h.clientAddress(r), accountFrom(r.Context()).ID, r.PostFormValue("code"))
	h.accountDone(w, r, err, "saved")
}

func (h *handler) accountPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.options.Auth.BeginAddPasskey(r.Context(), accountFrom(r.Context()).ID)
	if err != nil {
		h.jsonFailed(w, r, err)
		return
	}
	h.setCookie(w, ceremonyCookie, ceremony, ceremonyCookieTTL)
	writeJSON(w, http.StatusOK, options)
}

func (h *handler) accountPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(ceremonyCookie)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, h.t(r, "login.expired"))
		return
	}
	h.clearCookie(w, ceremonyCookie)
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	err = h.options.Auth.FinishAddPasskey(r.Context(), accountFrom(r.Context()).ID, cookie.Value, r.URL.Query().Get("name"), r)
	if err != nil {
		if errors.Is(err, auth.ErrCeremony) {
			h.jsonError(w, http.StatusBadRequest, h.t(r, "login.passkey.failed"))
			return
		}
		h.jsonFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"next": "/account?done=passkey"})
}

func (h *handler) accountPasskeyRename(w http.ResponseWriter, r *http.Request) {
	err := h.options.Auth.RenamePasskey(r.Context(), accountFrom(r.Context()).ID, r.PathValue("id"), r.PostFormValue("name"))
	h.accountDone(w, r, err, "saved")
}

func (h *handler) accountPasskeyRemove(w http.ResponseWriter, r *http.Request) {
	err := h.options.Auth.RemovePasskey(r.Context(), accountFrom(r.Context()).ID, r.PathValue("id"))
	h.accountDone(w, r, err, "saved")
}

func (h *handler) accountEndSessions(w http.ResponseWriter, r *http.Request) {
	_, err := h.options.Auth.EndOtherSessions(r.Context(), accountFrom(r.Context()).ID, h.sessionToken(r))
	h.accountDone(w, r, err, "sessions")
}

type adminData struct {
	Accounts []*auth.Account
	// Link is a new one-time link, shown once.
	Link     string
	LinkFor  string
	LinkDays int
}

func (h *handler) adminOnly(w http.ResponseWriter, r *http.Request) bool {
	if !accountFrom(r.Context()).Admin {
		h.message(w, r, http.StatusForbidden, h.t(r, "error.forbidden.title"), h.t(r, "error.forbidden.text"))
		return false
	}
	return true
}

func (h *handler) adminPage(w http.ResponseWriter, r *http.Request) {
	if h.adminOnly(w, r) {
		h.renderAdmin(w, r, http.StatusOK, adminData{}, "")
	}
}

func (h *handler) renderAdmin(w http.ResponseWriter, r *http.Request, status int, data adminData, failure string) {
	accounts, err := h.options.Auth.Accounts(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	data.Accounts = accounts
	data.LinkDays = int(auth.InviteTTL.Hours() / 24)
	h.render(w, r, status, "admin", view{Title: h.t(r, "admin.title"), Data: data, Error: failure})
}

// setupURL is the address of a one-time link.
func (h *handler) setupURL(token string) string {
	return h.options.Origin.Scheme + "://" + h.options.Origin.Host + "/setup?token=" + token
}

func (h *handler) adminInvite(w http.ResponseWriter, r *http.Request) {
	if !h.adminOnly(w, r) {
		return
	}
	link := auth.SetupLink{Username: r.PostFormValue("username"), Admin: r.PostFormValue("admin") == "on"}
	token, err := h.options.Auth.NewSetupLink(r.Context(), link, auth.InviteTTL)
	switch {
	case errors.Is(err, auth.ErrNameTaken):
		h.renderAdmin(w, r, http.StatusBadRequest, adminData{}, h.t(r, "setup.error.taken"))
	case errors.Is(err, auth.ErrBadName):
		h.renderAdmin(w, r, http.StatusBadRequest, adminData{}, h.t(r, "setup.error.name"))
	case err != nil:
		h.failed(w, r, err)
	default:
		h.renderAdmin(w, r, http.StatusOK, adminData{Link: h.setupURL(token), LinkFor: link.Username}, "")
	}
}

func (h *handler) adminReset(w http.ResponseWriter, r *http.Request) {
	if !h.adminOnly(w, r) {
		return
	}
	account, err := h.options.Auth.Account(r.Context(), r.PathValue("id"))
	if errors.Is(err, auth.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if err != nil {
		h.failed(w, r, err)
		return
	}
	token, err := h.options.Auth.NewSetupLink(r.Context(), auth.SetupLink{AccountID: account.ID}, auth.InviteTTL)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	h.renderAdmin(w, r, http.StatusOK, adminData{Link: h.setupURL(token), LinkFor: account.Username}, "")
}

func (h *handler) adminChange(w http.ResponseWriter, r *http.Request) {
	if !h.adminOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	var err error
	switch r.PathValue("change") {
	case "disable":
		err = h.options.Auth.SetDisabled(r.Context(), id, true)
	case "enable":
		err = h.options.Auth.SetDisabled(r.Context(), id, false)
	case "admin":
		err = h.options.Auth.SetAdmin(r.Context(), id, true)
	case "member":
		err = h.options.Auth.SetAdmin(r.Context(), id, false)
	default:
		h.notFound(w, r)
		return
	}
	switch {
	case errors.Is(err, auth.ErrLastAdmin):
		h.renderAdmin(w, r, http.StatusBadRequest, adminData{}, h.t(r, "admin.error.lastadmin"))
	case errors.Is(err, auth.ErrNotFound):
		h.notFound(w, r)
	case err != nil:
		h.failed(w, r, err)
	default:
		http.Redirect(w, r, "/admin?done=saved", http.StatusSeeOther)
	}
}
