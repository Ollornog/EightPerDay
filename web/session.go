// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/web/auth"
)

// identify finds out who is asking: the trusted gate first, then the session cookie. It only puts the account into the
// request; requireAccount decides what needs one.
func (h *handler) identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		if account, handled := h.fromGate(w, r); handled {
			if account != nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accountKey, account)))
			}
			return
		}
		if token := h.sessionToken(r); token != "" {
			account, err := h.options.Auth.SessionAccount(r.Context(), token)
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), accountKey, account))
			case errors.Is(err, auth.ErrInvalid):
				h.clearCookie(w, h.sessionCookieName())
			default:
				h.failed(w, r, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// fromGate returns the account a trusted gate names. handled is true when the gate spoke: then either the account is
// there, or the answer was already written.
func (h *handler) fromGate(w http.ResponseWriter, r *http.Request) (*auth.Account, bool) {
	gate := h.options.Gate
	if gate == nil || !h.fromTrustedProxy(r) {
		return nil, false
	}
	user := strings.TrimSpace(r.Header.Get(gate.UserHeader))
	if user == "" {
		return nil, false
	}
	admin := false
	if gate.AdminGroup != "" && gate.GroupsHeader != "" {
		for _, group := range strings.Split(r.Header.Get(gate.GroupsHeader), ",") {
			if strings.TrimSpace(group) == gate.AdminGroup {
				admin = true
			}
		}
	}
	name := ""
	if gate.NameHeader != "" {
		name = r.Header.Get(gate.NameHeader)
	}
	account, err := h.options.Auth.GateAccount(r.Context(), user, name, admin)
	if err != nil {
		if errors.Is(err, auth.ErrBadName) || errors.Is(err, auth.ErrInvalid) {
			h.message(w, r, http.StatusForbidden, h.t(r, "error.gate.title"), h.t(r, "error.gate.text"))
		} else {
			h.failed(w, r, err)
		}
		return nil, true
	}
	return account, true
}

func (h *handler) fromTrustedProxy(r *http.Request) bool {
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	return h.trusted(addr.Addr())
}

func (h *handler) trusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range h.options.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// clientAddress is the address a request came from, for the throttle. Behind trusted proxies it is the last address in
// X-Forwarded-For that is not one of them; anything a client wrote further left is not believed.
func (h *handler) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !h.trusted(addr) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return host
		}
		if !h.trusted(hop) {
			return hop.Unmap().String()
		}
	}
	return host
}

// requireAccount lets only signed-in requests through. A page asks to sign in and comes back afterwards; anything else
// gets a plain 401.
func (h *handler) requireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accountFrom(r.Context()) != nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		http.Error(w, "sign in first", http.StatusUnauthorized)
	})
}

// sameOriginForChanges refuses every change that does not come from a page of this program. Sign-in uses a cookie, so
// a page on another site could otherwise send changes in someone's name (and sign them in to an account of the
// attacker's).
func (h *handler) sameOriginForChanges(next http.Handler) http.Handler {
	origin := h.options.Origin.Scheme + "://" + h.options.Origin.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			next.ServeHTTP(w, r)
			return
		}
		site := r.Header.Get("Sec-Fetch-Site")
		sent := r.Header.Get("Origin")
		if site == "same-origin" || (site == "" && strings.EqualFold(sent, origin)) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "changes are only accepted from this program's own pages", http.StatusForbidden)
	})
}

func (h *handler) sessionCookieName() string {
	if h.options.Auth.SecureCookies() {
		return sessionCookieSecure
	}
	return sessionCookiePlain
}

func (h *handler) sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(h.sessionCookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *handler) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.options.Auth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *handler) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: h.options.Auth.SecureCookies(), SameSite: http.SameSiteLaxMode})
}

// signIn starts a session and sends the person on: to the page they came for when it is one of ours, home otherwise.
func (h *handler) signIn(w http.ResponseWriter, r *http.Request, account *auth.Account, next string) (string, error) {
	token, err := h.options.Auth.NewSession(r.Context(), account.ID)
	if err != nil {
		return "", err
	}
	h.setCookie(w, h.sessionCookieName(), token, h.options.Auth.SessionTTL())
	h.clearCookie(w, secondFactorCookie)
	return safeNext(next), nil
}

// safeNext keeps a redirect on this site: a path that starts with one slash, nothing else.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") ||
		strings.ContainsAny(next, "\r\n\t") {
		return "/"
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "/"
	}
	return next
}
