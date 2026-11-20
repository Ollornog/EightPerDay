// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/memstore"
	"github.com/Ollornog/EightPerDay/web/auth"
)

// directory is the accounts of an embedding application, for the tests.
type directory map[string]*auth.Account

func (d directory) Account(_ context.Context, id string) (*auth.Account, error) {
	if a, ok := d[id]; ok {
		return a, nil
	}
	return nil, auth.ErrInvalid
}

func (d directory) Accounts(context.Context) ([]*auth.Account, error) {
	var out []*auth.Account
	for _, a := range d {
		out = append(out, a)
	}
	return out, nil
}

// host is an application that embeds the interface at /time and signs people in by a header of its own.
type host struct {
	t      *testing.T
	server *httptest.Server
	store  *memstore.Store
	people directory
}

const userHeader = "X-Host-User"

func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{t: t, store: memstore.New(), people: directory{
		"u-anna": {ID: "u-anna", Username: "anna", DisplayName: "Anna"},
		"u-ben":  {ID: "u-ben", Username: "ben", DisplayName: "Ben", Language: "de"},
	}}
	handler, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Options{Store: h.store,
		Now: func() time.Time { return testNow },
		Embedding: &Embedding{Base: "/time", Directory: h.people, ThemeKey: "ud.theme",
			Account: func(r *http.Request) *auth.Account { return h.people[r.Header.Get(userHeader)] }}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/time/", handler)
	mux.Handle("/time", handler)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("the host")) })
	h.server = httptest.NewServer(mux)
	t.Cleanup(h.server.Close)
	return h
}

func (h *host) do(method, path, user string, form url.Values, headers map[string]string) answer {
	h.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if user != "" {
		request.Header.Set(userHeader, user)
	}
	if method != http.MethodGet {
		request.Header.Set("Origin", h.server.URL)
	}
	for k, v := range headers {
		if v == "" {
			request.Header.Del(k)
		} else {
			request.Header.Set(k, v)
		}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, _ := io.ReadAll(response.Body)
	return answer{status: response.StatusCode, header: response.Header, body: string(raw)}
}

func TestEmbeddingNeeds(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	who := func(*http.Request) *auth.Account { return nil }
	for name, o := range map[string]Options{
		"no store":     {Embedding: &Embedding{Account: who, Directory: directory{}}},
		"no account":   {Store: memstore.New(), Embedding: &Embedding{Directory: directory{}}},
		"no directory": {Store: memstore.New(), Embedding: &Embedding{Account: who}},
		"base slash":   {Store: memstore.New(), Embedding: &Embedding{Base: "/time/", Account: who, Directory: directory{}}},
		"base relative": {Store: memstore.New(), Embedding: &Embedding{Base: "time", Account: who,
			Directory: directory{}}},
		"base quote": {Store: memstore.New(), Embedding: &Embedding{Base: `/t"x`, Account: who, Directory: directory{}}},
		"gate": {Store: memstore.New(), Gate: &Gate{UserHeader: "Remote-User"},
			Embedding: &Embedding{Account: who, Directory: directory{}}},
		"standalone without auth": {Store: memstore.New(), Origin: &url.URL{Scheme: "http", Host: "x"}},
	} {
		if _, err := New(log, o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A store without stopwatches needs them passed.
	if _, err := New(log, Options{Store: plainStore{memstore.New()}, Embedding: &Embedding{Account: who,
		Directory: directory{}}}); err == nil {
		t.Error("a store without stopwatches and no Timers accepted")
	}
}

// plainStore is memstore without core.Stopwatches: its RunningTimer of another shape hides the one of memstore.
type plainStore struct{ *memstore.Store }

func (p plainStore) RunningTimer() {}

func TestEmbeddedPages(t *testing.T) {
	h := newHost(t)
	page := h.do(http.MethodGet, "/time/", "u-anna", nil, nil)
	if page.status != http.StatusOK {
		t.Fatalf("start page: %d %s", page.status, page.body)
	}
	// Every link, form, script and style of the page lies below the mount path.
	for _, m := range regexp.MustCompile(`(?:href|action|src|data-part)="(/[^"]*)"`).FindAllStringSubmatch(page.body, -1) {
		if !strings.HasPrefix(m[1], "/time/") {
			t.Errorf("a link out of the mount path: %s", m[0])
		}
	}
	for _, want := range []string{`data-base="/time"`, `data-theme-key="ud.theme"`, `href="/time/orgs"`} {
		if !strings.Contains(page.body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	for _, unwanted := range []string{`/account"`, "manifest.webmanifest", "data-theme-toggle", `/login`} {
		if strings.Contains(page.body, unwanted) {
			t.Errorf("the embedded page shows %s", unwanted)
		}
	}
	if csp := page.header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'self'") ||
		page.header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("framing: %q %q", csp, page.header.Get("X-Frame-Options"))
	}
	if css := h.do(http.MethodGet, "/time/assets/c22/css/c22-vega.css", "", nil, nil); css.status != http.StatusOK {
		t.Errorf("assets below the mount path: %d", css.status)
	}
	// The mount path alone leads into it; what is not below it is not ours.
	if a := h.do(http.MethodGet, "/time?x=1", "u-anna", nil, nil); a.status != http.StatusMovedPermanently ||
		a.header.Get("Location") != "/time/?x=1" {
		t.Errorf("mount path without slash: %d %s", a.status, a.header.Get("Location"))
	}
	// The language comes from the application's account.
	if de := h.do(http.MethodGet, "/time/", "u-ben", nil, nil); !strings.Contains(de.body, `lang="de"`) {
		t.Error("the account's language is not used")
	}
}

func TestEmbeddedSignInIsTheApplications(t *testing.T) {
	h := newHost(t)
	for _, path := range []string{"/time/", "/time/week", "/time/orgs", "/time/api/parse?line=2h"} {
		if a := h.do(http.MethodGet, path, "", nil, nil); a.status != http.StatusUnauthorized {
			t.Errorf("%s without an account: %d", path, a.status)
		}
		if a := h.do(http.MethodGet, path, "u-nobody", nil, nil); a.status != http.StatusUnauthorized {
			t.Errorf("%s with an unknown account: %d", path, a.status)
		}
	}
	// The program's own sign-in, accounts and administration are gone, and no organization is founded here.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/time/login"}, {http.MethodPost, "/time/login"}, {http.MethodGet, "/time/setup"},
		{http.MethodPost, "/time/logout"}, {http.MethodGet, "/time/account"}, {http.MethodGet, "/time/admin"},
		{http.MethodPost, "/time/admin/invite"}, {http.MethodPost, "/time/orgs"}, {http.MethodGet, "/time/sw.js"},
		{http.MethodGet, "/time/manifest.webmanifest"},
	} {
		if a := h.do(c.method, c.path, "u-anna", url.Values{"name": {"Org"}}, nil); a.status != http.StatusNotFound {
			t.Errorf("%s %s: %d", c.method, c.path, a.status)
		}
	}
	if orgs, _ := h.store.Memberships(context.Background(), "", "u-anna"); len(orgs) != 0 {
		t.Errorf("an organization was founded: %+v", orgs)
	}
}

func TestEmbeddedBooking(t *testing.T) {
	h := newHost(t)
	ctx := context.Background()
	// A change from another site is refused; from the application's own host it goes through, and the redirect
	// stays below the mount path.
	form := url.Values{"name": {"Own work"}, "kind": {"private"}, "mode": {"week_sum"}}
	if a := h.do(http.MethodPost, "/time/employers", "u-anna", form, map[string]string{"Origin": "https://evil.example"}); a.status != http.StatusForbidden {
		t.Fatalf("change from another site: %d", a.status)
	}
	a := h.do(http.MethodPost, "/time/employers", "u-anna", form, nil)
	if a.status != http.StatusSeeOther || !strings.HasPrefix(a.header.Get("Location"), "/time/") {
		t.Fatalf("create employer: %d %s %s", a.status, a.header.Get("Location"), a.body)
	}
	cat, err := h.store.Catalog(ctx, "u-anna")
	if err != nil || len(cat.Employers) != 1 {
		t.Fatalf("the person of the account owns the employer: %+v %v", cat.Employers, err)
	}
	emp := cat.Employers[0]
	if a := h.do(http.MethodPost, "/time/book/week", "u-anna", url.Values{"employer": {emp.ID}, "week": {"2026-11-16"},
		"hours": {"38,5"}, "back": {"/week"}}, nil); a.status != http.StatusSeeOther ||
		!strings.HasPrefix(a.header.Get("Location"), "/time/week") {
		t.Fatalf("book week: %d %s %s", a.status, a.header.Get("Location"), a.body)
	}
	entries, _ := h.store.Entries(ctx, core.EntryFilter{Owner: "u-anna"})
	if len(entries) != 1 || entries[0].Minutes != 38*60+30 {
		t.Fatalf("booked: %+v", entries)
	}
	// The details and incomes of a closed day stay as they are in the embedded interface too (the core refuses them);
	// the day after is open.
	if err := h.store.Put(ctx, core.PeriodClose{ID: "close-1", Owner: "u-anna", EmployerID: emp.ID, From: core.MustDate("2026-11-16"),
		To: core.MustDate("2026-11-22"), ClosedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"2026-11-18", "2026-11-23"} {
		for path, form := range map[string]url.Values{"/time/book/dayinfo": {"place": {"Berlin"}},
			"/time/book/income": {"kind": {"tip"}, "amount": {"5"}}} {
			form.Set("employer", emp.ID)
			form.Set("date", day)
			form.Set("back", "/day")
			a := h.do(http.MethodPost, path, "u-anna", form, nil)
			if closed := day == "2026-11-18"; (closed && a.status != http.StatusBadRequest) || (!closed && a.status != http.StatusSeeOther) {
				t.Errorf("%s on %s: %d", path, day, a.status)
			}
		}
	}
	if cat, _ = h.store.Catalog(ctx, "u-anna"); len(cat.DayInfos) != 1 || cat.DayInfos[0].Date.String() != "2026-11-23" ||
		len(cat.Incomes) != 1 || cat.Incomes[0].Date.String() != "2026-11-23" {
		t.Errorf("details %+v, incomes %+v", cat.DayInfos, cat.Incomes)
	}
	// Another account of the application sees nothing of it.
	if other := h.do(http.MethodGet, "/time/", "u-ben", nil, nil); strings.Contains(other.body, "Own work") {
		t.Error("another account sees the employer")
	}
	// The stopwatch lives in the store, inside the application's tenant.
	exact := url.Values{"name": {"Exact"}, "kind": {"private"}, "mode": {"exact"}}
	h.do(http.MethodPost, "/time/employers", "u-anna", exact, nil)
	cat, _ = h.store.Catalog(ctx, "u-anna")
	var exactID string
	for _, e := range cat.Employers {
		if e.Name == "Exact" {
			exactID = e.ID
		}
	}
	if a := h.do(http.MethodPost, "/time/timer/start", "u-anna", url.Values{"employer": {exactID}, "back": {"/week"}}, nil); a.status != http.StatusSeeOther {
		t.Fatalf("start stopwatch: %d %s", a.status, a.body)
	}
	if timer, err := h.store.RunningTimer(ctx, "u-anna"); err != nil || timer == nil || timer.EmployerID != exactID {
		t.Fatalf("stopwatch in the store: %+v %v", timer, err)
	}
	if err := h.store.StartTimer(ctx, "u-anna", core.Timer{EmployerID: exactID, Started: testNow}); !errors.Is(err, core.ErrTimerRunning) {
		t.Fatalf("second stopwatch: %v", err)
	}
}

func TestMountedRedirects(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/go":
			http.Redirect(w, r, "/week?done=x", http.StatusSeeOther)
		case "/away":
			http.Redirect(w, r, "https://example.org/", http.StatusSeeOther)
		case "/twice":
			w.Header().Set("Location", "//example.org/")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = w.Write([]byte(r.URL.Path))
		}
	})
	h := mounted("/time", next)
	for path, want := range map[string]string{"/time/go": "/time/week?done=x", "/time/away": "https://example.org/",
		"/time/twice": "//example.org/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/time/x/y", nil))
	if rec.Body.String() != "/x/y" {
		t.Errorf("the path below the mount: %q", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/timex", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("a path that only starts like the mount: %d", rec.Code)
	}
	if mounted("", next) == nil {
		t.Error("no base: the handler itself")
	}
}

// The sheet inside another application: its links, its loads and its fields stay below the mount path, and a field
// saves through the application's sign-in.
func TestEmbeddedSheet(t *testing.T) {
	h := newHost(t)
	ctx := context.Background()
	h.do(http.MethodPost, "/time/employers", "u-anna", url.Values{"name": {"Office"}, "kind": {"employer"}, "mode": {"schedule"},
		"weekday_hours": {"8"}}, nil)
	cat, _ := h.store.Catalog(ctx, "u-anna")
	emp := cat.Employers[0]
	page := h.do(http.MethodGet, "/time/", "u-anna", nil, nil).body
	for _, want := range []string{`data-sheet `, `action="/time/sheet/prefs"`, `action="/time/sheet/week"`, `data-base="/time"`,
		`href="/time/day?e=` + emp.ID} {
		if !strings.Contains(page, want) {
			t.Errorf("the embedded sheet lacks %s", want)
		}
	}
	a := h.do(http.MethodPost, "/time/sheet/cell", "u-anna", url.Values{"date": {"2026-11-16"}, "col": {"e-" + emp.ID}, "value": {"7,5"},
		"view": {"journal"}, "to": {"2026-11-30"}}, nil)
	if a.status != http.StatusOK || !strings.Contains(a.body, `"html"`) || !strings.Contains(a.body, `href=\"/time/`) {
		t.Fatalf("a field of the embedded sheet: %d %.300s", a.status, a.body)
	}
	if entries, _ := h.store.Entries(ctx, core.EntryFilter{Owner: "u-anna"}); len(entries) != 1 || entries[0].Minutes != 450 {
		t.Errorf("entries %+v", entries)
	}
	if a := h.do(http.MethodPost, "/time/sheet/prefs", "u-anna", url.Values{"view": {"grid"}, "back": {"/"}}, nil); a.status != http.StatusSeeOther ||
		!strings.HasPrefix(a.header.Get("Location"), "/time/") {
		t.Errorf("preferences: %d %s", a.status, a.header.Get("Location"))
	}
	if cat, _ := h.store.Catalog(ctx, "u-anna"); cat.PreferencesOf("u-anna").View != "grid" {
		t.Error("the embedded preferences are not kept in the application's store")
	}
}
