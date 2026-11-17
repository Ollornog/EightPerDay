// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Ollornog/EightPerDay/web/auth"
)

// ceremonyCookie carries the ID of a passkey step between its two requests. It is HttpOnly: the page never sees it.
const ceremonyCookie = "epd_ceremony"

const ceremonyCookieTTL = 10 * time.Minute

// maxJSONBody bounds what a passkey step may send.
const maxJSONBody = 64 << 10

type loginData struct {
	Next      string
	Username  string
	NoAccount bool
}

func (h *handler) loginPage(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	if accountFrom(r.Context()) != nil {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	h.renderLogin(w, r, http.StatusOK, loginData{Next: next}, "")
}

func (h *handler) renderLogin(w http.ResponseWriter, r *http.Request, status int, data loginData, failure string) {
	count, err := h.options.Auth.AccountCount(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	data.NoAccount = count == 0
	h.render(w, r, status, "login", view{Title: h.t(r, "login.title"), Narrow: true, Data: data, Error: failure})
}

// loginFailure turns an error of the auth service into a status and a sentence.
func (h *handler) loginFailure(r *http.Request, err error) (int, string, bool) {
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		return http.StatusTooManyRequests, h.t(r, "login.throttled", int(throttled.Wait.Round(time.Second).Seconds())), true
	case errors.Is(err, auth.ErrInvalid):
		return http.StatusUnauthorized, h.t(r, "login.invalid"), true
	case errors.Is(err, auth.ErrCeremony):
		return http.StatusBadRequest, h.t(r, "login.expired"), true
	}
	return 0, "", false
}

func (h *handler) loginPassword(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	username := r.PostFormValue("username")
	result, err := h.options.Auth.PasswordLogin(r.Context(), h.clientAddress(r), username, r.PostFormValue("password"))
	if err != nil {
		if status, text, ok := h.loginFailure(r, err); ok {
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "60")
			}
			h.renderLogin(w, r, status, loginData{Next: next, Username: username}, text)
			return
		}
		h.failed(w, r, err)
		return
	}
	if result.SecondFactor != "" {
		h.setCookie(w, secondFactorCookie, result.SecondFactor, ceremonyCookieTTL)
		http.Redirect(w, r, "/login/code?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	target, err := h.signIn(w, r, result.Account, next)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) secondFactorPage(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(secondFactorCookie)
	if err != nil || !h.options.Auth.SecondFactorPending(cookie.Value) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login_code", view{Title: h.t(r, "code.title"), Narrow: true,
		Data: loginData{Next: safeNext(r.URL.Query().Get("next"))}})
}

func (h *handler) secondFactor(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	cookie, err := r.Cookie(secondFactorCookie)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	account, err := h.options.Auth.SecondFactorLogin(r.Context(), h.clientAddress(r), cookie.Value, r.PostFormValue("code"))
	if err != nil {
		if errors.Is(err, auth.ErrCeremony) {
			h.clearCookie(w, secondFactorCookie)
			h.renderLogin(w, r, http.StatusBadRequest, loginData{Next: next}, h.t(r, "login.expired"))
			return
		}
		if status, text, ok := h.loginFailure(r, err); ok {
			if errors.Is(err, auth.ErrInvalid) {
				text = h.t(r, "code.invalid")
			}
			h.render(w, r, status, "login_code", view{Title: h.t(r, "code.title"), Narrow: true, Data: loginData{Next: next}, Error: text})
			return
		}
		h.failed(w, r, err)
		return
	}
	target, err := h.signIn(w, r, account, next)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) loginPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.options.Auth.BeginPasskeyLogin()
	if err != nil {
		h.jsonFailed(w, r, err)
		return
	}
	h.setCookie(w, ceremonyCookie, ceremony, ceremonyCookieTTL)
	writeJSON(w, http.StatusOK, options)
}

func (h *handler) loginPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(ceremonyCookie)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, h.t(r, "login.expired"))
		return
	}
	h.clearCookie(w, ceremonyCookie)
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	account, err := h.options.Auth.FinishPasskeyLogin(r.Context(), h.clientAddress(r), cookie.Value, r)
	if err != nil {
		if errors.Is(err, auth.ErrInvalid) || errors.Is(err, auth.ErrCeremony) {
			h.jsonError(w, http.StatusUnauthorized, h.t(r, "login.passkey.failed"))
			return
		}
		if status, text, ok := h.loginFailure(r, err); ok {
			h.jsonError(w, status, text)
			return
		}
		h.jsonFailed(w, r, err)
		return
	}
	target, err := h.signIn(w, r, account, r.URL.Query().Get("next"))
	if err != nil {
		h.jsonFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"next": target})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if token := h.sessionToken(r); token != "" {
		if err := h.options.Auth.EndSession(r.Context(), token); err != nil {
			h.failed(w, r, err)
			return
		}
	}
	h.clearCookie(w, h.sessionCookieName())
	http.Redirect(w, r, "/login?done=signedout", http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *handler) jsonError(w http.ResponseWriter, status int, text string) {
	writeJSON(w, status, map[string]string{"error": text})
}

func (h *handler) jsonFailed(w http.ResponseWriter, r *http.Request, err error) {
	h.log.Error("request failed", "path", r.URL.Path, "error", err.Error())
	h.jsonError(w, http.StatusInternalServerError, h.t(r, "error.internal.text"))
}
