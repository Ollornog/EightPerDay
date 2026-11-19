// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/memstore"
	"github.com/Ollornog/EightPerDay/internal/pgtest"
	"github.com/Ollornog/EightPerDay/web/auth"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// site is the program on a test server with a browser-like client: it keeps cookies and sends the Origin of the program
// with every change, as a page of the program would.
type site struct {
	t      *testing.T
	server *httptest.Server
	auth   *auth.Service
	store  core.Store
	client *http.Client
	origin string
}

type siteOption func(*Options)

func newSite(t *testing.T, options ...siteOption) *site {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	server := httptest.NewUnstartedServer(nil)
	// Passkeys need a host name, so the program is opened as localhost, as a person would on their own machine.
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	origin, _ := url.Parse("http://localhost:" + port)
	log := slog.New(slog.NewTextHandler(testLog{t}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	accounts, err := auth.Open(context.Background(), pool, auth.Config{Origin: origin}, log)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Auth: accounts, Origin: origin, Store: memstore.New(), Now: func() time.Time { return testNow }}
	for _, option := range options {
		option(&o)
	}
	handler, err := New(log, o)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)
	s := &site{t: t, server: server, auth: accounts, store: o.Store, origin: origin.String()}
	s.client = s.newClient()
	return s
}

// testLog shows what the program logs as a warning or worse next to the test that caused it.
type testLog struct{ t *testing.T }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func (s *site) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type answer struct {
	status int
	header http.Header
	body   string
}

func (s *site) do(client *http.Client, method, path string, form url.Values, headers map[string]string) answer {
	s.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, s.origin+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != http.MethodGet {
		request.Header.Set("Origin", s.origin)
	}
	for name, value := range headers {
		if value == "" {
			request.Header.Del(name)
		} else {
			request.Header.Set(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, _ := io.ReadAll(response.Body)
	// html/template writes some characters as references (+ as &#43;); the tests read the text as a person sees it.
	return answer{status: response.StatusCode, header: response.Header, body: html.UnescapeString(string(raw))}
}

func (s *site) get(path string) answer { return s.do(s.client, http.MethodGet, path, nil, nil) }

func (s *site) post(path string, form url.Values) answer {
	return s.do(s.client, http.MethodPost, path, form, nil)
}

const password = "correct horse battery staple"

// testNow is a Wednesday morning in Berlin: the week runs from Monday 2026-11-16 to Sunday 2026-11-22.
var testNow = time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)

// account makes an account through an invitation and signs the site's client in with it.
func (s *site) account(name string, admin bool) *auth.Account {
	s.t.Helper()
	token, err := s.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: name, Admin: admin}, auth.InviteTTL)
	if err != nil {
		s.t.Fatal(err)
	}
	a := s.post("/setup", url.Values{"token": {token}, "method": {"password"}, "password": {password}, "password2": {password}, "display_name": {"Mara " + name}})
	if a.status != http.StatusSeeOther || a.header.Get("Location") != "/?done=welcome" {
		s.t.Fatalf("setup: %d %s %.300s", a.status, a.header.Get("Location"), a.body)
	}
	all, _ := s.auth.Accounts(context.Background())
	for _, account := range all {
		if account.Username == name {
			return account
		}
	}
	s.t.Fatal("no account")
	return nil
}

func TestSignInIsNeeded(t *testing.T) {
	s := newSite(t)
	for _, path := range []string{"/", "/account", "/admin"} {
		a := s.get(path)
		if a.status != http.StatusSeeOther || a.header.Get("Location") != "/login?next="+url.QueryEscape(path) {
			t.Errorf("%s: %d %s", path, a.status, a.header.Get("Location"))
		}
	}
	if a := s.post("/account/name", url.Values{"display_name": {"x"}}); a.status != http.StatusUnauthorized {
		t.Errorf("a change without an account: %d", a.status)
	}
	for _, path := range []string{"/login", "/about", "/licenses", "/legal", "/legal/works-agreement.md", "/manifest.webmanifest", "/sw.js", "/favicon.ico", "/assets/app/app.js"} {
		if a := s.get(path); a.status != http.StatusOK {
			t.Errorf("%s needs no account but answered %d", path, a.status)
		}
	}
}

func TestFirstAccountThroughTheOneTimeLink(t *testing.T) {
	s := newSite(t)
	if !strings.Contains(s.get("/login").body, "No account yet") {
		t.Fatal("the sign-in page does not say where the first account comes from")
	}
	token, err := s.auth.FirstSetupLink(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	page := s.get("/setup?token=" + token)
	if page.status != http.StatusOK || !strings.Contains(page.body, "first account") {
		t.Fatalf("%d %.500s", page.status, page.body)
	}
	if !strings.Contains(page.body, `value="passkey" checked`) {
		t.Error("the passkey is not the default")
	}
	a := s.post("/setup", url.Values{"token": {token}, "username": {"Mara"}, "display_name": {"Mara S."}, "method": {"password"},
		"password": {password}, "password2": {password}})
	if a.status != http.StatusSeeOther {
		t.Fatalf("%d %.500s", a.status, a.body)
	}
	home := s.get("/?done=welcome")
	if home.status != http.StatusOK || !strings.Contains(home.body, "Who do you work for?") || !strings.Contains(home.body, "Your account is ready") {
		t.Fatalf("%d %.800s", home.status, home.body)
	}
	if again := s.get("/setup?token=" + token); again.status != http.StatusNotFound {
		t.Fatalf("the link works twice: %d", again.status)
	}
	if strings.Contains(s.get("/login").body, "No account yet") {
		t.Fatal("the hint stays after the first account")
	}
}

func TestSetupRefusesWhatIsWrong(t *testing.T) {
	s := newSite(t)
	token, _ := s.auth.NewSetupLink(context.Background(), auth.SetupLink{}, auth.InviteTTL)
	for _, c := range []struct {
		form url.Values
		says string
	}{
		{url.Values{"username": {"mara"}, "method": {"password"}, "password": {password}, "password2": {password + "x"}}, "not the same"},
		{url.Values{"username": {"mara"}, "method": {"password"}, "password": {"short"}, "password2": {"short"}}, "at least 12"},
		{url.Values{"username": {"no spaces"}, "method": {"password"}, "password": {password}, "password2": {password}}, "not allowed"},
		{url.Values{"username": {"mara"}, "method": {"passkey"}}, "needs JavaScript"},
	} {
		c.form.Set("token", token)
		a := s.post("/setup", c.form)
		if a.status != http.StatusBadRequest || !strings.Contains(a.body, c.says) {
			t.Errorf("%v: %d, want %q in %.300s", c.form, a.status, c.says, a.body)
		}
	}
	if a := s.post("/setup", url.Values{"token": {"made-up"}, "method": {"password"}}); a.status != http.StatusNotFound {
		t.Errorf("an unknown link: %d", a.status)
	}
}

func TestPasswordSignInAndOut(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	fresh := s.newClient()
	a := s.do(fresh, http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {"wrong password!"}, "next": {"/account"}}, nil)
	if a.status != http.StatusUnauthorized || !strings.Contains(a.body, "Name or password is wrong") {
		t.Fatalf("%d", a.status)
	}
	a = s.do(fresh, http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {password}, "next": {"/account"}}, nil)
	if a.status != http.StatusSeeOther || a.header.Get("Location") != "/account" {
		t.Fatalf("%d %s", a.status, a.header.Get("Location"))
	}
	if page := s.do(fresh, http.MethodGet, "/account", nil, nil); page.status != http.StatusOK {
		t.Fatalf("signed in, the account page answers %d", page.status)
	}
	a = s.do(fresh, http.MethodPost, "/logout", url.Values{}, nil)
	if a.status != http.StatusSeeOther {
		t.Fatal(a.status)
	}
	if page := s.do(fresh, http.MethodGet, "/account", nil, nil); page.status != http.StatusSeeOther {
		t.Fatalf("signed out, the account page answers %d", page.status)
	}
}

func TestSignInOnlySendsToOwnPages(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	for _, next := range []string{"//evil.example/x", "https://evil.example/", "/\\evil.example", "javascript:alert(1)", "/ok\r\nSet-Cookie: x=y"} {
		a := s.do(s.newClient(), http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {password}, "next": {next}}, nil)
		if a.status != http.StatusSeeOther || a.header.Get("Location") != "/" {
			t.Errorf("next %q sent to %q", next, a.header.Get("Location"))
		}
	}
	for next, want := range map[string]string{"/account": "/account", "/a?b=c": "/a?b=c", "": "/", "a": "/"} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q", next, got)
		}
	}
}

func TestSecondFactorOverHTTP(t *testing.T) {
	s := newSite(t)
	token, _ := s.auth.NewSetupLink(context.Background(), auth.SetupLink{Username: "tom"}, auth.InviteTTL)
	a := s.post("/setup", url.Values{"token": {token}, "method": {"password_totp"}, "password": {password}, "password2": {password}})
	if a.status != http.StatusOK || !strings.Contains(a.body, `src="data:image/png;base64,`) {
		t.Fatalf("%d %.500s", a.status, a.body)
	}
	secret := regexp.MustCompile(`secret-key[^>]*>([A-Z2-7]+)<`).FindStringSubmatch(a.body)
	ceremony := regexp.MustCompile(`name="ceremony" value="([^"]+)"`).FindStringSubmatch(a.body)
	if secret == nil || ceremony == nil {
		t.Fatalf("no secret or ceremony: %.1500s", a.body)
	}
	wrong := s.post("/setup/code", url.Values{"ceremony": {ceremony[1]}, "code": {"000000"}})
	if wrong.status != http.StatusBadRequest || !strings.Contains(wrong.body, secret[1]) {
		t.Fatalf("after a wrong code the QR code is gone: %d", wrong.status)
	}
	code, _ := totp.GenerateCode(secret[1], time.Now())
	if a := s.post("/setup/code", url.Values{"ceremony": {ceremony[1]}, "code": {code}}); a.status != http.StatusSeeOther {
		t.Fatalf("%d %.300s", a.status, a.body)
	}
	fresh := s.newClient()
	a = s.do(fresh, http.MethodPost, "/login", url.Values{"username": {"tom"}, "password": {password}, "next": {"/account"}}, nil)
	if a.status != http.StatusSeeOther || !strings.HasPrefix(a.header.Get("Location"), "/login/code") {
		t.Fatalf("a password alone: %d %s", a.status, a.header.Get("Location"))
	}
	if page := s.do(fresh, http.MethodGet, "/account", nil, nil); page.status != http.StatusSeeOther {
		t.Fatal("half signed in is signed in")
	}
	if page := s.do(fresh, http.MethodGet, a.header.Get("Location"), nil, nil); page.status != http.StatusOK || !strings.Contains(page.body, "one-time-code") {
		t.Fatalf("%d", page.status)
	}
	// The code of the setup was spent; the next step's code signs in.
	next, _ := totp.GenerateCode(secret[1], time.Now().Add(30*time.Second))
	a = s.do(fresh, http.MethodPost, "/login/code", url.Values{"code": {next}, "next": {"/account"}}, nil)
	if a.status != http.StatusSeeOther || a.header.Get("Location") != "/account" {
		t.Fatalf("%d %s %.300s", a.status, a.header.Get("Location"), a.body)
	}
	if page := s.do(fresh, http.MethodGet, "/account", nil, nil); page.status != http.StatusOK {
		t.Fatal(page.status)
	}
	// Without a pending sign-in the code page sends back.
	if page := s.do(s.newClient(), http.MethodGet, "/login/code", nil, nil); page.status != http.StatusSeeOther {
		t.Fatal(page.status)
	}
}

func TestThrottledSignInSaysSo(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	var a answer
	for range 6 {
		a = s.do(s.newClient(), http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {"guessing it"}}, nil)
	}
	if a.status != http.StatusTooManyRequests || a.header.Get("Retry-After") == "" || !strings.Contains(a.body, "Too many failed attempts") {
		t.Fatalf("%d %.300s", a.status, a.body)
	}
}

func TestChangesOnlyFromOwnPages(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	form := url.Values{"display_name": {"Changed"}}
	for _, headers := range []map[string]string{
		{"Origin": ""},
		{"Origin": "https://evil.example"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "none", "Origin": ""},
	} {
		if a := s.do(s.client, http.MethodPost, "/account/name", form, headers); a.status != http.StatusForbidden {
			t.Errorf("%v: %d", headers, a.status)
		}
	}
	// Signing in from another site is refused too: it would sign the victim in to the attacker's account.
	if a := s.do(s.newClient(), http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {password}}, map[string]string{"Origin": "https://evil.example"}); a.status != http.StatusForbidden {
		t.Errorf("login from another site: %d", a.status)
	}
	if a := s.do(s.client, http.MethodPost, "/account/name", form, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": ""}); a.status != http.StatusSeeOther {
		t.Errorf("same origin: %d", a.status)
	}
}

func TestSessionCookie(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	a := s.do(s.newClient(), http.MethodPost, "/login", url.Values{"username": {"mara"}, "password": {password}}, nil)
	cookie := a.header.Get("Set-Cookie")
	for _, want := range []string{"epd_session=", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("%q lacks %s", cookie, want)
		}
	}
	// On https the cookie is Secure and bound to the host by its prefix.
	h := &handler{options: Options{Auth: secureAuth(t)}}
	recorder := httptest.NewRecorder()
	h.setCookie(recorder, h.sessionCookieName(), "x", time.Hour)
	if got := recorder.Header().Get("Set-Cookie"); !strings.HasPrefix(got, "__Host-epd_session=") || !strings.Contains(got, "Secure") {
		t.Errorf("on https: %q", got)
	}
}

func secureAuth(t *testing.T) *auth.Service {
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	origin, _ := url.Parse("https://example.org")
	a, err := auth.Open(context.Background(), pool, auth.Config{Origin: origin}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdministration(t *testing.T) {
	s := newSite(t)
	s.account("boss", true)
	invite := s.post("/admin/invite", url.Values{"username": {"ben"}})
	link := regexp.MustCompile(`value="(http://[^"]+/setup\?token=[^"]+)"`).FindStringSubmatch(invite.body)
	if invite.status != http.StatusOK || link == nil {
		t.Fatalf("%d %.800s", invite.status, invite.body)
	}
	token := strings.SplitN(link[1], "token=", 2)[1]
	if l, err := s.auth.LookupSetupLink(context.Background(), token); err != nil || l.Username != "ben" || l.Admin {
		t.Fatalf("%+v %v", l, err)
	}
	if a := s.post("/admin/invite", url.Values{"username": {"boss"}}); a.status != http.StatusBadRequest {
		t.Errorf("an invitation for a taken name: %d", a.status)
	}
	member := newSite(t)
	member.account("ben", false)
	if a := member.get("/admin"); a.status != http.StatusForbidden {
		t.Errorf("a member sees the administration: %d", a.status)
	}
	if a := member.post("/admin/invite", url.Values{}); a.status != http.StatusForbidden {
		t.Errorf("a member invites: %d", a.status)
	}
}

func TestAdminCannotLockEveryoneOut(t *testing.T) {
	s := newSite(t)
	boss := s.account("boss", true)
	if a := s.post("/admin/accounts/"+boss.ID+"/member", url.Values{}); a.status != http.StatusBadRequest || !strings.Contains(a.body, "at least one active administrator") {
		t.Fatalf("%d", a.status)
	}
}

func TestGateHeadersOnlyFromTrustedProxies(t *testing.T) {
	gate := func(o *Options) {
		o.Gate = &Gate{UserHeader: "Remote-User", GroupsHeader: "Remote-Groups", AdminGroup: "time-admins"}
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	}
	s := newSite(t, gate)
	a := s.do(s.newClient(), http.MethodGet, "/account", nil, map[string]string{"Remote-User": "Gina", "Remote-Groups": "staff, time-admins"})
	if a.status != http.StatusOK || !strings.Contains(a.body, "You sign in as gina") {
		t.Fatalf("%d %.300s", a.status, a.body)
	}
	if a := s.do(s.newClient(), http.MethodGet, "/admin", nil, map[string]string{"Remote-User": "gina", "Remote-Groups": "time-admins"}); a.status != http.StatusOK {
		t.Errorf("the admin group of the gate does not count: %d", a.status)
	}
	if a := s.do(s.newClient(), http.MethodGet, "/admin", nil, map[string]string{"Remote-User": "gina", "Remote-Groups": "time-admins-not"}); a.status != http.StatusForbidden {
		t.Errorf("a look-alike group counts: %d", a.status)
	}
	// The same header from an address that is not a trusted proxy counts for nothing.
	untrusted := newSite(t, func(o *Options) {
		gate(o)
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	})
	if a := untrusted.do(untrusted.newClient(), http.MethodGet, "/", nil, map[string]string{"Remote-User": "gina"}); a.status != http.StatusSeeOther {
		t.Fatalf("the gate header was believed from anywhere: %d", a.status)
	}
	if _, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Options{Auth: s.auth, Store: memstore.New(), Origin: &url.URL{Scheme: "http", Host: "x"}, Gate: &Gate{UserHeader: "Remote-User"}}); err == nil {
		t.Fatal("a gate without trusted proxies was accepted")
	}
}

func TestClientAddress(t *testing.T) {
	h := &handler{options: Options{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("::1/128")}}}
	for _, c := range []struct{ remote, forwarded, want string }{
		{"198.51.100.5:1234", "", "198.51.100.5"},
		{"198.51.100.5:1234", "192.0.2.1", "198.51.100.5"}, // not from a proxy: the header is ignored
		{"203.0.113.3:1234", "192.0.2.1", "192.0.2.1"},
		{"203.0.113.3:1234", "192.0.2.77, 192.0.2.1, 203.0.113.9", "192.0.2.1"}, // the client's own entry on the left does not count
		{"203.0.113.3:1234", "", "203.0.113.3"},
		{"203.0.113.3:1234", "garbage", "203.0.113.3"},
		{"[::1]:1234", "2001:db8::1", "2001:db8::1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := h.clientAddress(r); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.remote, c.forwarded, got, c.want)
		}
	}
}

func TestSecurityHeadersOnEveryAnswer(t *testing.T) {
	s := newSite(t)
	for _, path := range []string{"/login", "/", "/assets/app/app.js", "/nothing-here", "/setup?token=x"} {
		a := s.get(path)
		if a.header.Get("Content-Security-Policy") != CSP || a.header.Get("X-Frame-Options") != "DENY" || a.header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %v", path, a.header)
		}
	}
	if strings.Contains(CSP, "unsafe") {
		t.Error("the CSP allows something unsafe")
	}
}

// pages renders every page there is, in both languages, for the checks on markup.
func pages(t *testing.T) map[string]string {
	t.Helper()
	s, _ := trackSite(t)
	boss := s.me("boss", true)
	out := map[string]string{}
	token, _ := s.auth.NewSetupLink(context.Background(), auth.SetupLink{}, auth.InviteTTL)
	out["start"] = s.get("/").body
	var employers []core.Employer
	for _, mode := range core.Modes {
		employers = append(employers, s.employer(boss, "Employer "+string(mode), mode, "8"))
	}
	s.mustPost("/employers/"+employers[3].ID+"/project", url.Values{"name": {"Roof"}, "back": {"/"}})
	s.mustPost("/timer/start", url.Values{"employer": {employers[3].ID}, "back": {"/"}})
	s.mustPost("/book/line", url.Values{"employer": {employers[1].ID}, "line": {"Mo 8-12 @Roof #x note"}, "back": {"/"}})
	s.mustPost("/book/line", url.Values{"employer": {employers[2].ID}, "line": {"Di krank"}, "back": {"/"}})
	s.mustPost("/employers/"+employers[0].ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"20"}, "from": {"2026-01-01"}, "back": {"/"}})
	s.mustPost("/book/week", url.Values{"employer": {employers[0].ID}, "week": {monday.String()}, "hours": {"20"}, "back": {"/"}})
	s.mustPost("/orgs", url.Values{"name": {"ACME"}, "back": {"/orgs"}})
	memberships, _ := s.store.Memberships(context.Background(), "", boss.ID)
	org := memberships[0].Organization
	s.mustPost("/orgs/"+org+"/template", url.Values{"name": {"Full"}, "mode": {"plan"}, "hours-1": {"8"}, "back": {"/"}})
	s.mustPost("/orgs/"+org+"/features", url.Values{"supervisor_view": {"on"}, "confirm": {"on"}, "agreement": {"BV"}, "back": {"/"}})
	for _, lang := range languages {
		s.post("/language", url.Values{"lang": {lang}, "back": {"/"}})
		paths := []string{"/account", "/admin", "/about", "/licenses", "/legal", "/nothing", "/setup?token=" + token, "/setup?token=bad",
			"/reports", "/reports?month=2026-10", "/orgs", "/orgs/" + org}
		for _, e := range employers {
			paths = append(paths, "/?e="+e.ID, "/?e="+e.ID+"&w=2026-01-05", "/week?e="+e.ID, "/weeks?e="+e.ID, "/employers/"+e.ID)
		}
		for _, path := range paths {
			out[lang+" "+path] = s.get(path).body
		}
		out[lang+" totp"] = s.post("/account/totp", url.Values{}).body
		out[lang+" invite"] = s.post("/admin/invite", url.Values{}).body
		out[lang+" error"] = s.post("/book/line", url.Values{"employer": {employers[1].ID}, "line": {"@a @b"}}).body
		anonymous := s.newClient()
		s.do(anonymous, http.MethodPost, "/language", url.Values{"lang": {lang}}, nil)
		out[lang+" /login"] = s.do(anonymous, http.MethodGet, "/login", nil, nil).body
		out[lang+" wrong"] = s.do(anonymous, http.MethodPost, "/login", url.Values{"username": {"x"}, "password": {"y"}}, nil).body
	}
	return out
}

func TestPagesAreComplete(t *testing.T) {
	for name, body := range pages(t) {
		if missing := regexp.MustCompile(`!\w+(\.\w+)+`).FindString(body); missing != "" {
			t.Errorf("%s: text %s is missing from the catalog", name, missing)
		}
		if !strings.Contains(body, "</html>") {
			t.Errorf("%s: not a whole page", name)
		}
		if strings.HasPrefix(name, "de ") && !strings.Contains(body, `lang="de"`) {
			t.Errorf("%s: not German", name)
		}
	}
}

func TestPagesHaveNoInlineCode(t *testing.T) {
	inline := regexp.MustCompile(`(?i)\sstyle=|\son[a-z]+=|<style|<script(?:\s+defer)?>`)
	for name, body := range pages(t) {
		if found := inline.FindString(body); found != "" {
			t.Errorf("%s: %q would need a weaker CSP", name, found)
		}
	}
	script, err := assetFiles.ReadFile("assets/app/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if found := regexp.MustCompile(`\sstyle="|\son[a-z]+="|confirm\(|innerHTML`).Find(script); found != nil {
		t.Errorf("app.js uses %q", found)
	}
}

func TestNothingFromThirdParties(t *testing.T) {
	foreign := regexp.MustCompile(`(?i)(?:src|<link[^>]+href|action)="(?:https?:)?//`)
	for name, body := range pages(t) {
		if found := foreign.FindString(body); found != "" {
			t.Errorf("%s: %q", name, found)
		}
	}
	css, err := assetFiles.ReadFile("assets/c22/css/c22-vega.css")
	if err != nil {
		t.Fatal(err)
	}
	if found := regexp.MustCompile(`url\(["']?(?:https?:)?//[^)]*`).Find(css); found != nil {
		t.Errorf("the stylesheet loads %s", found)
	}
}

func TestAboutNamesTheLogoAuthor(t *testing.T) {
	for name, body := range pages(t) {
		if !strings.Contains(body, `href="https://www.flaticon.com/authors/alfredo-creates">Alfredo Creates</a>`) ||
			!strings.Contains(body, `href="https://www.flaticon.com/">Flaticon</a>`) {
			t.Errorf("%s: the about dialog does not name the logo's author", name)
		}
	}
}

// Every class comes from C22: the app has no stylesheet of its own.
func TestEveryClassExistsInTheStylesheet(t *testing.T) {
	known := map[string]bool{}
	css, err := assetFiles.ReadFile("assets/c22/css/c22-vega.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`\.((?:\\.|[\w-])+)`).FindAllStringSubmatch(string(css), -1) {
		known[strings.ReplaceAll(match[1], `\`, "")] = true
	}
	if own, _ := fs.Glob(assetFiles, "assets/app/*.css"); len(own) > 0 {
		t.Errorf("%v: styles belong in C22", own)
	}
	if len(known) < 500 {
		t.Fatalf("only %d classes found, the check itself is broken", len(known))
	}
	for _, control := range []string{"made-up-class", "py-13"} {
		if known[control] {
			t.Fatalf("control class %q found, the check itself is broken", control)
		}
	}
	used := map[string]string{}
	attribute := regexp.MustCompile(`class="([^"{]*)"`)
	files, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		content, _ := templateFiles.ReadFile(file)
		if regexp.MustCompile(`class="[^"]*\{\{`).Match(content) {
			t.Errorf("%s: a class made in the template cannot be checked", file)
		}
		for _, match := range attribute.FindAllStringSubmatch(string(content), -1) {
			for _, class := range strings.Fields(match[1]) {
				used[class] = file
			}
		}
	}
	if len(used) < 80 {
		t.Fatalf("only %d classes in use, the check itself is broken", len(used))
	}
	for class, where := range used {
		if !known[class] {
			t.Errorf("%s: class %q is not in the stylesheet", where, class)
		}
	}
}

func TestCatalogsAgree(t *testing.T) {
	c, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for key := range c["en"] {
		if _, ok := c["de"][key]; !ok {
			t.Errorf("de lacks %s", key)
		}
	}
	for key := range c["de"] {
		if _, ok := c["en"][key]; !ok {
			t.Errorf("en lacks %s", key)
		}
	}
	verb := regexp.MustCompile(`%[a-z]`)
	for key, text := range c["en"] {
		if strings.Join(verb.FindAllString(text, -1), "") != strings.Join(verb.FindAllString(c["de"][key], -1), "") {
			t.Errorf("%s: the placeholders differ", key)
		}
	}
}

func TestLanguage(t *testing.T) {
	for _, c := range []struct{ account, cookie, accept, want string }{
		{"", "", "", "en"},
		{"", "", "de-AT,de;q=0.9,en;q=0.5", "de"},
		{"", "", "fr-FR, de;q=0.5", "de"},
		{"", "en", "de", "en"},
		{"de", "en", "en", "de"},
		{"xx", "yy", "zz", "en"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: languageCookie, Value: c.cookie})
		}
		r.Header.Set("Accept-Language", c.accept)
		if got := pickLanguage(r, c.account); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}

func TestNoticesComeOnlyFromTheCatalog(t *testing.T) {
	s := newSite(t)
	s.account("mara", false)
	if body := s.get("/?done=saved").body; !strings.Contains(body, "Saved.") {
		t.Error("a known notice is not shown")
	}
	if body := s.get("/?done=" + url.QueryEscape("<b>hacked</b>")).body; strings.Contains(body, "hacked") {
		t.Error("a link puts text on the page")
	}
}

func TestAssetsArrive(t *testing.T) {
	s := newSite(t)
	for path, contentType := range map[string]string{
		"/assets/c22/css/c22-vega.css":              "text/css",
		"/assets/c22/fonts/inter.woff2":             "font/woff2",
		"/assets/app/app.js":                        "javascript",
		"/assets/app/theme.js":                      "javascript",
		"/assets/c22/js/basecoat.all.min.js":        "javascript",
		"/assets/logo.png":                          "image/png",
		"/favicon.ico":                              "image/png",
		"/assets/c22/licenses/Inter-OFL.txt":        "text/plain",
		"/assets/app/licenses/Lucide-LICENSE.txt":   "text/plain",
		"/assets/licenses/THIRD-PARTY.txt":          "text/plain",
		"/manifest.webmanifest":                     "application/manifest+json",
		"/sw.js":                                    "javascript",
		"/assets/c22/licenses/Basecoat-LICENSE":     "text/plain",
		"/assets/c22/licenses/Tailwind-LICENSE":     "text/plain",
		"/assets/c22/licenses/C22-LICENSE":          "text/plain",
		"/assets/licenses/THIRD-PARTY.txt?v=x":      "text/plain",
		"/assets/app/licenses/Lucide-LICENSE.txt?x": "text/plain",
	} {
		a := s.get(path)
		if a.status != http.StatusOK || !strings.Contains(a.header.Get("Content-Type"), contentType) {
			t.Errorf("%s: %d %s", path, a.status, a.header.Get("Content-Type"))
		}
	}
	worker := s.get("/sw.js").body
	if strings.Contains(worker, "__VERSION__") || !strings.Contains(worker, `"epd-`) {
		t.Error("the service worker does not name the build")
	}
}

func TestLicensesPageListsTheModules(t *testing.T) {
	s := newSite(t)
	body := s.get("/licenses").body
	for _, module := range []string{"github.com/go-webauthn/webauthn", "github.com/pquerna/otp", "github.com/jackc/pgx/v5", "golang.org/x/crypto", "The Go standard library"} {
		if !strings.Contains(body, module) {
			t.Errorf("/licenses does not name %s", module)
		}
	}
}

func TestHealth(t *testing.T) {
	ready := false
	server := httptest.NewServer(Health(func() bool { return ready }))
	defer server.Close()
	for want, isReady := range map[int]bool{http.StatusServiceUnavailable: false, http.StatusOK: true} {
		ready = isReady
		response, err := http.Get(server.URL + healthPath)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("ready %v: %d", isReady, response.StatusCode)
		}
	}
	// The loop above ran over a map, in no fixed order: the probe needs a ready server of its own.
	ready = true
	if err := Probe(context.Background(), strings.TrimPrefix(server.URL, "http://")); err != nil {
		t.Errorf("probe: %v", err)
	}
	ready = false
	if err := Probe(context.Background(), strings.TrimPrefix(server.URL, "http://")); err == nil {
		t.Error("probe of a server that is not ready passed")
	}
}

func TestAccessLogLeavesOutTheQuery(t *testing.T) {
	var out strings.Builder
	handler := accessLog(slog.New(slog.NewTextHandler(&out, nil)), http.NotFoundHandler())
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/setup?token=secret-token", nil))
	if strings.Contains(out.String(), "secret-token") || !strings.Contains(out.String(), "/setup") {
		t.Errorf("%s", out.String())
	}
}

func TestFormatMinutes(t *testing.T) {
	for minutes, want := range map[int]string{0: "0:00", 45: "0:45", 510: "8:30", -45: "-0:45", -90: "-1:30", 6000: "100:00"} {
		if got := formatMinutes(minutes); got != want {
			t.Errorf("%d: %s", minutes, got)
		}
	}
}
