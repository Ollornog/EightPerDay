// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Ollornog/EightPerDay/web/auth"
)

type setupData struct {
	Token       string
	Link        auth.SetupLink
	AccountName string // for a reset: the name of the account
	Username    string
	DisplayName string
	Method      string
	Enrollment  *auth.TOTPEnrollment
	MinLength   int
}

func (h *handler) setupPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	data, ok := h.setupLink(w, r, token)
	if !ok {
		return
	}
	data.Method = "passkey"
	h.render(w, r, http.StatusOK, "setup", view{Title: h.t(r, "setup.title"), Narrow: true, Data: data})
}

// setupLink looks the link up and answers a page when it is not valid.
func (h *handler) setupLink(w http.ResponseWriter, r *http.Request, token string) (setupData, bool) {
	link, err := h.options.Auth.LookupSetupLink(r.Context(), token)
	if errors.Is(err, auth.ErrLinkInvalid) {
		h.message(w, r, http.StatusNotFound, h.t(r, "setup.invalid.title"), h.t(r, "setup.invalid.text"))
		return setupData{}, false
	}
	if err != nil {
		h.failed(w, r, err)
		return setupData{}, false
	}
	data := setupData{Token: token, Link: link, Username: link.Username, MinLength: auth.MinPasswordLength}
	if link.Reset() {
		account, err := h.options.Auth.Account(r.Context(), link.AccountID)
		if err != nil {
			h.failed(w, r, err)
			return setupData{}, false
		}
		data.AccountName = account.Username
	}
	return data, true
}

// setupError turns an error of a setup step into a sentence, or "" when it is not the person's to fix.
func (h *handler) setupError(r *http.Request, err error) string {
	switch {
	case errors.Is(err, auth.ErrNameTaken):
		return h.t(r, "setup.error.taken")
	case errors.Is(err, auth.ErrBadName):
		return h.t(r, "setup.error.name")
	case errors.Is(err, auth.ErrPasswordTooShort):
		return h.t(r, "password.error.short", auth.MinPasswordLength)
	case errors.Is(err, auth.ErrPasswordTooLong):
		return h.t(r, "password.error.long")
	case errors.Is(err, auth.ErrInvalid):
		return h.t(r, "code.invalid")
	case errors.Is(err, auth.ErrCeremony):
		return h.t(r, "login.expired")
	}
	return ""
}

func (h *handler) setupPassword(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	data, ok := h.setupLink(w, r, token)
	if !ok {
		return
	}
	choice := auth.SetupChoice{Username: r.PostFormValue("username"), DisplayName: r.PostFormValue("display_name")}
	data.Username, data.DisplayName, data.Method = choice.Username, choice.DisplayName, r.PostFormValue("method")
	if data.Link.Username != "" {
		data.Username = data.Link.Username
	}
	again := func(status int, text string) {
		h.render(w, r, status, "setup", view{Title: h.t(r, "setup.title"), Narrow: true, Data: data, Error: text})
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password2") {
		again(http.StatusBadRequest, h.t(r, "password.error.mismatch"))
		return
	}
	switch data.Method {
	case "password":
		account, err := h.options.Auth.SetupWithPassword(r.Context(), token, choice, password)
		if err != nil {
			if text := h.setupError(r, err); text != "" {
				again(http.StatusBadRequest, text)
				return
			}
			h.failed(w, r, err)
			return
		}
		h.setupDone(w, r, account)
	case "password_totp":
		enrollment, err := h.options.Auth.BeginSetupWithTOTP(r.Context(), token, choice, password)
		if err != nil {
			if text := h.setupError(r, err); text != "" {
				again(http.StatusBadRequest, text)
				return
			}
			h.failed(w, r, err)
			return
		}
		data.Enrollment = enrollment
		h.render(w, r, http.StatusOK, "setup_code", view{Title: h.t(r, "setup.totp.title"), Narrow: true, Data: data})
	default:
		// A passkey is set up by the page's script; without it, the person chooses a password.
		again(http.StatusBadRequest, h.t(r, "setup.error.script"))
	}
}

func (h *handler) setupDone(w http.ResponseWriter, r *http.Request, account *auth.Account) {
	target, err := h.signIn(w, r, account, "/?done=welcome")
	if err != nil {
		h.failed(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) setupCode(w http.ResponseWriter, r *http.Request) {
	ceremony := r.PostFormValue("ceremony")
	account, err := h.options.Auth.FinishSetupWithTOTP(r.Context(), ceremony, r.PostFormValue("code"))
	if err != nil {
		// After a wrong code the same QR code shows again, until a few wrong codes end the attempt.
		if enrollment := h.options.Auth.PendingSetupTOTP(ceremony); errors.Is(err, auth.ErrInvalid) && enrollment != nil {
			h.render(w, r, http.StatusBadRequest, "setup_code", view{Title: h.t(r, "setup.totp.title"), Narrow: true,
				Data: setupData{Enrollment: enrollment}, Error: h.t(r, "code.invalid")})
			return
		}
		if text := h.setupError(r, err); text != "" {
			h.message(w, r, http.StatusBadRequest, h.t(r, "setup.invalid.title"), text)
			return
		}
		h.failed(w, r, err)
		return
	}
	h.setupDone(w, r, account)
}

type setupPasskeyRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func (h *handler) setupPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	var request setupPasskeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&request); err != nil {
		h.jsonError(w, http.StatusBadRequest, h.t(r, "error.request"))
		return
	}
	options, ceremony, err := h.options.Auth.BeginSetupWithPasskey(r.Context(), request.Token,
		auth.SetupChoice{Username: request.Username, DisplayName: request.DisplayName})
	if err != nil {
		if errors.Is(err, auth.ErrLinkInvalid) {
			h.jsonError(w, http.StatusNotFound, h.t(r, "setup.invalid.text"))
			return
		}
		if text := h.setupError(r, err); text != "" {
			h.jsonError(w, http.StatusBadRequest, text)
			return
		}
		h.jsonFailed(w, r, err)
		return
	}
	h.setCookie(w, ceremonyCookie, ceremony, ceremonyCookieTTL)
	writeJSON(w, http.StatusOK, options)
}

func (h *handler) setupPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(ceremonyCookie)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, h.t(r, "login.expired"))
		return
	}
	h.clearCookie(w, ceremonyCookie)
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	account, err := h.options.Auth.FinishSetupWithPasskey(r.Context(), cookie.Value, r.URL.Query().Get("name"), r)
	if err != nil {
		if errors.Is(err, auth.ErrLinkInvalid) {
			h.jsonError(w, http.StatusNotFound, h.t(r, "setup.invalid.text"))
			return
		}
		if text := h.setupError(r, err); text != "" {
			h.jsonError(w, http.StatusBadRequest, text)
			return
		}
		h.jsonFailed(w, r, err)
		return
	}
	target, err := h.signIn(w, r, account, "/?done=welcome")
	if err != nil {
		h.jsonFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"next": target})
}
