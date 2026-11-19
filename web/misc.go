// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// about is the page of the about dialog, for a browser without JavaScript. It names the logo's author, as its license
// requires (assets/LICENSE-icon.md).
func (h *handler) about(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "about", view{Title: h.t(r, "about.title"), Narrow: true})
}

// licenses lists the licenses of everything the program ships: its own, the logo's, and those of the libraries and the
// design system inside the binary.
func (h *handler) licenses(w http.ResponseWriter, r *http.Request) {
	text, err := assetFiles.ReadFile("assets/licenses/THIRD-PARTY.txt")
	if err != nil {
		h.failed(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "licenses", view{Title: h.t(r, "licenses.title"), Data: string(text)})
}

func (h *handler) setLanguage(w http.ResponseWriter, r *http.Request) {
	lang := r.PostFormValue("lang")
	if !known(lang) {
		lang = ""
	}
	if a := accountFrom(r.Context()); a != nil && !h.embedded() {
		if err := h.options.Auth.SetLanguage(r.Context(), a.ID, lang); err != nil {
			h.failed(w, r, err)
			return
		}
	}
	if lang == "" {
		h.clearCookie(w, languageCookie)
	} else {
		http.SetCookie(w, &http.Cookie{Name: languageCookie, Value: lang, Path: h.base() + "/", MaxAge: 365 * 24 * 3600,
			Secure: h.secureCookies(r), SameSite: http.SameSiteLaxMode, HttpOnly: true})
	}
	http.Redirect(w, r, safeNext(r.PostFormValue("back")), http.StatusSeeOther)
}

// manifest makes the program installable on a phone's home screen.
func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	lang := pickLanguage(r, "")
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":        "EightPerDay",
		"short_name":  "EightPerDay",
		"description": h.catalog[lang]["app.description"],
		"lang":        lang,
		"start_url":   "/",
		"scope":       "/",
		"display":     "standalone",
		"icons": []map[string]string{
			{"src": "/assets/logo.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
		},
	})
}

// serviceWorker is served from the root, so its scope is the whole program. It keeps the assets of this build for a
// connection that drops on a building site, and never keeps a page: pages carry personal data and change.
func (h *handler) serviceWorker(w http.ResponseWriter, _ *http.Request) {
	script, err := assetFiles.ReadFile("assets/app/sw.js")
	if err != nil {
		http.Error(w, "missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(script), "__VERSION__", h.assetVersion)))
}
