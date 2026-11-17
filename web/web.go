// SPDX-License-Identifier: MIT

// Package web is the interface of the standalone program: server-rendered pages in the design system C22, a little
// JavaScript of its own for passkeys, dialogs and the stopwatch, and nothing loaded from anywhere else.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/assets"
	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/internal/version"
	"github.com/Ollornog/EightPerDay/web/auth"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed all:assets
var assetFiles embed.FS

const (
	languageCookie = "epd_lang"
	// sessionCookieSecure carries the session on https: the __Host- prefix binds it to this host and path, with Secure.
	sessionCookieSecure = "__Host-epd_session"
	// sessionCookiePlain is for plain http on a developer's machine, where browsers refuse the prefix.
	sessionCookiePlain = "epd_session"
	// secondFactorCookie carries the ID of a sign-in that still owes its code.
	secondFactorCookie = "epd_second"
)

// Gate trusts a sign-in gate in front of the program: a request from one of the trusted proxies with UserHeader set is
// signed in as that account, which is created at its first visit.
type Gate struct {
	// UserHeader names the account, such as Remote-User.
	UserHeader string
	// NameHeader carries the display name, such as Remote-Name. Optional.
	NameHeader string
	// GroupsHeader carries the groups, comma-separated, such as Remote-Groups. Optional.
	GroupsHeader string
	// AdminGroup is the group whose members administer the program. Empty: nobody is made administrator by the gate.
	AdminGroup string
}

// Options configures the handler.
type Options struct {
	Auth *auth.Service
	// Origin is the address people open; changes are only accepted from pages of this origin.
	Origin *url.URL
	// Gate turns on the trusted headers of a sign-in gate. Nil: off.
	Gate *Gate
	// TrustedProxies are the addresses whose X-Forwarded-For and gate headers are believed.
	TrustedProxies []netip.Prefix
	// Store holds the time records (core/pgstore in the program, core/memstore in tests).
	Store core.Store
	// Now replaces the clock in tests.
	Now func() time.Time
}

type handler struct {
	options      Options
	log          *slog.Logger
	pages        map[string]map[string]*template.Template // language, page
	assetVersion string
	catalog      catalog
}

// New returns the complete handler with logging and security headers.
func New(log *slog.Logger, options Options) (http.Handler, error) {
	if options.Auth == nil || options.Origin == nil || options.Store == nil {
		return nil, errors.New("web: Auth, Origin and Store are required")
	}
	if options.Gate != nil && (options.Gate.UserHeader == "" || len(options.TrustedProxies) == 0) {
		return nil, errors.New("web: a gate needs its user header and the addresses of the trusted proxies")
	}
	h := &handler{options: options, log: log}
	var err error
	if h.catalog, err = loadCatalog(); err != nil {
		return nil, err
	}
	if h.assetVersion, err = hashAssets(); err != nil {
		return nil, err
	}
	if err := h.parseTemplates(); err != nil {
		return nil, err
	}
	static, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", staticAssets(h.assetVersion, http.FileServerFS(static))))
	mux.HandleFunc("GET /assets/logo.png", serveBytes("image/png", assets.Logo))
	mux.HandleFunc("GET /favicon.ico", serveBytes("image/png", assets.Logo))
	mux.HandleFunc("GET /manifest.webmanifest", h.manifest)
	mux.HandleFunc("GET /sw.js", h.serviceWorker)
	mux.HandleFunc("GET /about", h.about)
	mux.HandleFunc("GET /licenses", h.licenses)
	mux.HandleFunc("POST /language", h.setLanguage)

	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.loginPassword)
	mux.HandleFunc("GET /login/code", h.secondFactorPage)
	mux.HandleFunc("POST /login/code", h.secondFactor)
	mux.HandleFunc("POST /login/passkey/begin", h.loginPasskeyBegin)
	mux.HandleFunc("POST /login/passkey/finish", h.loginPasskeyFinish)
	mux.HandleFunc("POST /logout", h.logout)

	mux.HandleFunc("GET /setup", h.setupPage)
	mux.HandleFunc("POST /setup", h.setupPassword)
	mux.HandleFunc("POST /setup/code", h.setupCode)
	mux.HandleFunc("POST /setup/passkey/begin", h.setupPasskeyBegin)
	mux.HandleFunc("POST /setup/passkey/finish", h.setupPasskeyFinish)

	signedIn := http.NewServeMux()
	signedIn.HandleFunc("GET /{$}", h.home)
	signedIn.HandleFunc("POST /employers", h.createEmployer)
	signedIn.HandleFunc("GET /employers/{id}", h.employerPage)
	signedIn.HandleFunc("POST /employers/{id}/{action}", h.changeEmployer)
	signedIn.HandleFunc("POST /book/week", h.weekTotal)
	signedIn.HandleFunc("POST /book/days", h.dayTotals)
	signedIn.HandleFunc("POST /book/projects", h.projectTotals)
	signedIn.HandleFunc("POST /book/confirm", h.confirmWeek)
	signedIn.HandleFunc("POST /book/adjust", h.adjust)
	signedIn.HandleFunc("POST /book/span", h.addSpan)
	signedIn.HandleFunc("POST /book/line", h.freeText)
	signedIn.HandleFunc("POST /book/exact", h.materializeWeek)
	signedIn.HandleFunc("POST /book/absence", h.addAbsence)
	signedIn.HandleFunc("POST /absences/{id}/remove", h.removeAbsence)
	signedIn.HandleFunc("POST /entries/{id}/remove", h.removeEntry)
	signedIn.HandleFunc("POST /timer/start", h.startTimer)
	signedIn.HandleFunc("POST /timer/stop", h.stopTimer)
	signedIn.HandleFunc("GET /api/parse", h.parsePreview)
	signedIn.HandleFunc("GET /reports", h.reports)
	signedIn.HandleFunc("GET /export/{format}", h.export)
	signedIn.HandleFunc("GET /orgs", h.orgsPage)
	signedIn.HandleFunc("POST /orgs", h.createOrg)
	signedIn.HandleFunc("GET /orgs/{id}", h.orgPage)
	signedIn.HandleFunc("POST /orgs/{id}/{action}", h.changeOrg)
	signedIn.HandleFunc("GET /account", h.accountPage)
	signedIn.HandleFunc("POST /account/name", h.accountName)
	signedIn.HandleFunc("POST /account/password", h.accountPassword)
	signedIn.HandleFunc("POST /account/password/remove", h.accountPasswordRemove)
	signedIn.HandleFunc("POST /account/totp", h.accountTOTPBegin)
	signedIn.HandleFunc("POST /account/totp/confirm", h.accountTOTPConfirm)
	signedIn.HandleFunc("POST /account/totp/remove", h.accountTOTPRemove)
	signedIn.HandleFunc("POST /account/passkeys/begin", h.accountPasskeyBegin)
	signedIn.HandleFunc("POST /account/passkeys/finish", h.accountPasskeyFinish)
	signedIn.HandleFunc("POST /account/passkeys/{id}/rename", h.accountPasskeyRename)
	signedIn.HandleFunc("POST /account/passkeys/{id}/remove", h.accountPasskeyRemove)
	signedIn.HandleFunc("POST /account/sessions/end", h.accountEndSessions)
	signedIn.HandleFunc("GET /admin", h.adminPage)
	signedIn.HandleFunc("POST /admin/invite", h.adminInvite)
	signedIn.HandleFunc("POST /admin/accounts/{id}/reset", h.adminReset)
	signedIn.HandleFunc("POST /admin/accounts/{id}/{change}", h.adminChange)
	signedIn.HandleFunc("/", h.notFound)
	mux.Handle("/", h.requireAccount(signedIn))
	return accessLog(log, secure(h.sameOriginForChanges(h.identify(mux)))), nil
}

func (h *handler) parseTemplates() error {
	h.pages = map[string]map[string]*template.Template{}
	names, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil {
		return err
	}
	for _, lang := range languages {
		base, err := template.New("").Funcs(template.FuncMap{
			"t":         h.catalog.translator(lang),
			"minutes":   formatMinutes,
			"dataURL":   dataURL,
			"money":     func(cents int64, currency string) string { return formatMoney(lang, cents, currency) },
			"day":       func(d core.Date) string { return formatDate(lang, toTime(d)) },
			"hours":     func(minutes int) string { return formatMinutes(minutes) },
			"signed":    signedMinutes,
			"half":      func(days float64) string { return formatDays(lang, days) },
			"add":       func(a, b int) int { return a + b },
			"entriesOf": func(entries []entryView, back string) entriesData { return entriesData{Entries: entries, Back: back} },
			"projectChoice": func(id string, projects []core.Project) projectChoice {
				return projectChoice{ID: id, Projects: projects}
			},
			"newEmployerForm": func() newEmployer { return newEmployerData(nil) },
			"dateOf":          func(t *time.Time) core.Date { return core.DateOf(*t) },
			"date":            func(t time.Time) string { return formatDate(lang, t) },
		}).ParseFS(templateFiles, "templates/layout.html")
		if err != nil {
			return err
		}
		h.pages[lang] = map[string]*template.Template{}
		for _, name := range names {
			page := strings.TrimSuffix(strings.TrimPrefix(name, "templates/"), ".html")
			if page == "layout" {
				continue
			}
			clone, err := base.Clone()
			if err != nil {
				return err
			}
			if h.pages[lang][page], err = clone.ParseFS(templateFiles, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// view is what every page gets.
type view struct {
	Lang      string
	Title     string
	Version   string
	Program   string
	Account   *auth.Account
	Path      string
	Gate      bool
	Notice    string
	Error     string
	Data      any
	Narrow    bool // a page with one small card, such as the sign-in
	Languages []string
}

type contextKey int

const accountKey contextKey = 1

func accountFrom(ctx context.Context) *auth.Account {
	a, _ := ctx.Value(accountKey).(*auth.Account)
	return a
}

// render writes a page. An error in a template is logged and answered with a plain 500, never half a page.
func (h *handler) render(w http.ResponseWriter, r *http.Request, status int, page string, v view) {
	v.Account = accountFrom(r.Context())
	accountLanguage := ""
	if v.Account != nil {
		accountLanguage = v.Account.Language
	}
	v.Lang = pickLanguage(r, accountLanguage)
	v.Version = h.assetVersion
	v.Program = version.Version
	v.Path = r.URL.Path
	v.Gate = h.options.Gate != nil
	v.Languages = languages
	if v.Notice == "" {
		v.Notice = h.notice(v.Lang, r.URL.Query().Get("done"))
	}
	var out bytes.Buffer
	if err := h.pages[v.Lang][page].ExecuteTemplate(&out, "layout", v); err != nil {
		h.log.Error("template failed", slog.String("page", page), slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(out.Bytes())
}

// notice turns the short key a redirect carries into a sentence; unknown keys say nothing, so a link cannot put text
// on the page.
func (h *handler) notice(lang, key string) string {
	if key == "" || strings.ContainsAny(key, " .") {
		return ""
	}
	text, ok := h.catalog[lang]["done."+key]
	if !ok {
		return ""
	}
	return text
}

func (h *handler) t(r *http.Request, key string, args ...any) string {
	accountLanguage := ""
	if a := accountFrom(r.Context()); a != nil {
		accountLanguage = a.Language
	}
	return h.catalog.translator(pickLanguage(r, accountLanguage))(key, args...)
}

// message shows a page with one sentence, for errors and dead ends.
func (h *handler) message(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	h.render(w, r, status, "message", view{Title: title, Narrow: true, Data: text})
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.message(w, r, http.StatusNotFound, h.t(r, "error.notfound.title"), h.t(r, "error.notfound.text"))
}

func (h *handler) failed(w http.ResponseWriter, r *http.Request, err error) {
	h.log.Error("request failed", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
	h.message(w, r, http.StatusInternalServerError, h.t(r, "error.internal.title"), h.t(r, "error.internal.text"))
}

func hashAssets() (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(assetFiles, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := assetFiles.ReadFile(p)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(hash, "%s %d\n", p, len(content))
		_, _ = hash.Write(content)
		return nil
	})
	_, _ = hash.Write(assets.Logo)
	return hex.EncodeToString(hash.Sum(nil))[:12], err
}

// staticAssets lets the browser keep an asset for a year when its URL names the version of this build (?v=…), and a
// day otherwise.
func staticAssets(version string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if version != "" && r.URL.Query().Get("v") == version {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}

func serveBytes(contentType string, content []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(content)
	}
}

// dataURL lets a PNG made by the program itself into an image source; html/template refuses data URLs otherwise.
func dataURL(value string) template.URL {
	if strings.HasPrefix(value, "data:image/png;base64,") && !strings.ContainsAny(value, "\"'<> ") {
		return template.URL(value)
	}
	return ""
}

// formatMinutes writes a duration in minutes as hours and minutes, such as 7:30 or -0:45.
func formatMinutes(minutes int) string {
	sign := ""
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	return fmt.Sprintf("%s%d:%02d", sign, minutes/60, minutes%60)
}

type entriesData struct {
	Entries []entryView
	Back    string
}

type projectChoice struct {
	ID       string
	Projects []core.Project
}

// signedMinutes writes a balance with its sign, such as +1:30 or -0:45; zero has none.
func signedMinutes(minutes int) string {
	if minutes > 0 {
		return "+" + formatMinutes(minutes)
	}
	return formatMinutes(minutes)
}

// formatDays writes days with at most one decimal, such as 30 or 12,5.
func formatDays(lang string, days float64) string {
	text := strconv.FormatFloat(days, 'f', -1, 64)
	if lang == "de" {
		text = strings.ReplaceAll(text, ".", ",")
	}
	return text
}

func formatDate(lang string, t time.Time) string {
	if lang == "de" {
		return t.Format("02.01.2006")
	}
	return t.Format("2006-01-02")
}
