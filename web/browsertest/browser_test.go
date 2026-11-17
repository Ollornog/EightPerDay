//go:build browser

// SPDX-License-Identifier: MIT

// Package browsertest runs the real program in a real headless Chrome, on the screen of a phone, with fingers instead
// of a mouse and a virtual passkey authenticator. A missing Chrome fails the test: a skipped browser test is not a green
// one. It talks to Chrome over a pipe, without a websocket and without a third-party library.
//
//	go test -tags browser ./web/browsertest/
//
// BROWSER_SHOTS=<folder> also saves a picture of every main view.
package browsertest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ollornog/EightPerDay/core/memstore"
	"github.com/Ollornog/EightPerDay/internal/pgtest"
	"github.com/Ollornog/EightPerDay/web"
	"github.com/Ollornog/EightPerDay/web/auth"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func findChrome(t *testing.T) string {
	t.Helper()
	for _, name := range []string{os.Getenv("CHROME"), "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if name == "" {
			continue
		}
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
	}
	t.Fatal("no Chrome or Chromium found (set CHROME=...): the browser test is required")
	return ""
}

type message struct {
	ID        int                       `json:"id,omitempty"`
	Method    string                    `json:"method,omitempty"`
	Params    json.RawMessage           `json:"params,omitempty"`
	Result    json.RawMessage           `json:"result,omitempty"`
	Error     *struct{ Message string } `json:"error,omitempty"`
	SessionID string                    `json:"sessionId,omitempty"`
}

type browser struct {
	t       *testing.T
	out     io.Writer
	mu      sync.Mutex
	next    int
	waiting map[int]chan message
	events  chan message
	session string
	exited  chan struct{}
	exitErr error
	stderr  *tail
	base    string
	auth    *auth.Service
}

// tail keeps the last bytes written to it, so a silent Chrome can say why.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > 4096 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}

func (w *tail) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// setup starts the program on a throwaway database and a Chrome with the screen of a phone, 390 by 844.
func setup(t *testing.T) *browser {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	server := httptest.NewUnstartedServer(nil)
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	origin, _ := url.Parse("http://localhost:" + port)
	log := slog.New(slog.DiscardHandler)
	accounts, err := auth.Open(context.Background(), pool, auth.Config{Origin: origin}, log)
	if err != nil {
		t.Fatal(err)
	}
	b0store := memstore.New()
	// The program's clock stands on Wednesday, 18 November 2026, so a week looks the same on every day the test runs.
	handler, err := web.New(log, web.Options{Auth: accounts, Origin: origin, Store: b0store,
		Now: func() time.Time { return time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)

	b := start(t)
	b.base, b.auth = origin.String(), accounts
	var target struct{ TargetID string }
	b.call("Target.createTarget", map[string]any{"url": "about:blank"}, &target)
	var session struct{ SessionID string }
	b.call("Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &session)
	b.session = session.SessionID
	for _, domain := range []string{"Page.enable", "Network.enable", "Log.enable", "Runtime.enable"} {
		b.call(domain, map[string]any{}, nil)
	}
	b.call("Network.setCacheDisabled", map[string]any{"cacheDisabled": true}, nil)
	// The tests read English; the German texts are checked by the handler tests.
	b.call("Network.setExtraHTTPHeaders", map[string]any{"headers": map[string]string{"Accept-Language": "en"}}, nil)
	b.phone(390, 844)
	return b
}

func start(t *testing.T) *browser {
	t.Helper()
	toChrome, weWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fromChrome, chromeWrites, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// Not t.TempDir: helper processes keep writing after the main process ends, which would fail its cleanup.
	profile, err := os.MkdirTemp("", "eightperday-browsertest-")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(findChrome(t), "--headless=new", "--remote-debugging-pipe", "--no-first-run",
		"--no-default-browser-check", "--disable-gpu", "--disable-extensions", "--disable-sync",
		"--disable-background-networking", "--disable-component-update", "--no-sandbox",
		"--user-data-dir="+profile, "--window-size=390,844", "about:blank")
	cmd.ExtraFiles = []*os.File{toChrome, chromeWrites} // Chrome reads fd 3 and writes fd 4
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr := &tail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	_ = toChrome.Close()
	_ = chromeWrites.Close()
	b := &browser{t: t, out: weWrite, waiting: map[int]chan message{}, events: make(chan message, 4096), exited: exited, stderr: stderr}
	go func() {
		b.exitErr = cmd.Wait()
		close(exited)
	}()
	go func() {
		reader := bufio.NewReader(fromChrome)
		for {
			raw, err := reader.ReadBytes(0)
			if err != nil {
				close(b.events)
				return
			}
			var m message
			if json.Unmarshal(bytes.TrimSuffix(raw, []byte{0}), &m) != nil {
				continue
			}
			if m.ID != 0 {
				b.mu.Lock()
				answer := b.waiting[m.ID]
				delete(b.waiting, m.ID)
				b.mu.Unlock()
				if answer != nil {
					answer <- m
				}
				continue
			}
			select {
			case b.events <- m:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		_ = weWrite.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the whole group: renderer, GPU, crash handler
		<-exited
		for range 20 {
			if os.RemoveAll(profile) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return b
}

func (b *browser) call(method string, params, result any) {
	b.t.Helper()
	if err := b.try(method, params, result); err != nil {
		b.t.Fatalf("%s: %v", method, err)
	}
}

// navigating tells the error of a call that met a page being replaced, which is worth another try.
func navigating(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "navigated or closed") || strings.Contains(err.Error(), "Cannot find context"))
}

// try sends a call and returns the error Chrome answers with, instead of failing the test.
func (b *browser) try(method string, params, result any) error {
	b.t.Helper()
	b.mu.Lock()
	b.next++
	id := b.next
	answer := make(chan message, 1)
	b.waiting[id] = answer
	b.mu.Unlock()
	raw, _ := json.Marshal(params)
	m := message{ID: id, Method: method, Params: raw, SessionID: b.session}
	if method == "Target.createTarget" || method == "Target.attachToTarget" {
		m.SessionID = ""
	}
	encoded, _ := json.Marshal(m)
	if _, err := b.out.Write(append(encoded, 0)); err != nil {
		return fmt.Errorf("%v; %s", err, b.why())
	}
	select {
	case r := <-answer:
		if r.Error != nil {
			return errors.New(r.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(r.Result, result)
		}
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("no answer; %s", b.why())
	}
}

func (b *browser) why() string {
	state := "Chrome still runs"
	select {
	case <-b.exited:
		state = fmt.Sprintf("Chrome has ended (%v)", b.exitErr)
	default:
	}
	return state + "; the end of its stderr:\n" + b.stderr.String()
}

// findings is what a page did besides showing itself: requests, failures, CSP reports and script errors.
type findings struct {
	requests, failed, csp, errors []string
}

// drain reads the events of the page until the network has been quiet for a while after the last load.
func (b *browser) drain(needLoad bool) findings {
	b.t.Helper()
	var f findings
	loaded := !needLoad
	quiet := time.NewTimer(700 * time.Millisecond)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case m, open := <-b.events:
			if !open {
				b.t.Fatal("Chrome has ended")
			}
			if m.SessionID != b.session {
				continue
			}
			switch m.Method {
			case "Page.loadEventFired":
				loaded = true
				quiet.Reset(500 * time.Millisecond)
			case "Network.requestWillBeSent":
				var p struct{ Request struct{ URL string } }
				_ = json.Unmarshal(m.Params, &p)
				f.requests = append(f.requests, p.Request.URL)
				quiet.Reset(500 * time.Millisecond)
			case "Network.loadingFailed":
				var p struct{ ErrorText, BlockedReason, Type string }
				_ = json.Unmarshal(m.Params, &p)
				f.failed = append(f.failed, fmt.Sprintf("%s %s %s", p.Type, p.ErrorText, p.BlockedReason))
			case "Network.responseReceived":
				var p struct {
					Response struct {
						URL    string
						Status int
					}
				}
				_ = json.Unmarshal(m.Params, &p)
				if p.Response.Status >= 400 {
					f.failed = append(f.failed, fmt.Sprintf("%d %s", p.Response.Status, p.Response.URL))
				}
			case "Log.entryAdded":
				var p struct {
					Entry struct{ Source, Level, Text string }
				}
				_ = json.Unmarshal(m.Params, &p)
				if p.Entry.Source == "security" || strings.Contains(p.Entry.Text, "Content Security Policy") {
					f.csp = append(f.csp, p.Entry.Text)
				} else if p.Entry.Level == "error" {
					f.errors = append(f.errors, p.Entry.Source+": "+p.Entry.Text)
				}
			case "Runtime.exceptionThrown":
				f.errors = append(f.errors, "exception: "+string(m.Params))
			}
		case <-quiet.C:
			if loaded {
				return f
			}
		case <-deadline:
			b.t.Fatal("the page does not settle")
		}
	}
}

// open loads an address of the program and fails on anything the page should not do: a request to another origin, a
// failed request, a CSP report or a script error.
func (b *browser) open(path string) {
	b.t.Helper()
	b.call("Page.navigate", map[string]any{"url": b.base + path}, nil)
	b.clean(b.drain(true))
}

func (b *browser) clean(f findings) {
	b.t.Helper()
	for _, request := range f.requests {
		if !strings.HasPrefix(request, b.base+"/") && !strings.HasPrefix(request, "data:") {
			b.t.Errorf("a request to somewhere else: %s", request)
		}
	}
	for _, list := range [][]string{f.failed, f.csp, f.errors} {
		for _, entry := range list {
			b.t.Errorf("%s", entry)
		}
	}
}

// settle waits for what a tap set off: a navigation or a request in the background.
func (b *browser) settle() {
	b.t.Helper()
	b.clean(b.drain(false))
}

func (b *browser) eval(expression string) string {
	b.t.Helper()
	var result struct {
		Result struct {
			Value any
		}
		ExceptionDetails *struct {
			Text      string
			Exception struct{ Description string }
		}
	}
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}
	err := b.try("Runtime.evaluate", params, &result)
	for tries := 0; navigating(err) && tries < 50; tries++ {
		time.Sleep(50 * time.Millisecond)
		err = b.try("Runtime.evaluate", params, &result)
	}
	if err != nil {
		b.t.Fatalf("Runtime.evaluate: %v", err)
	}
	if result.ExceptionDetails != nil {
		b.t.Fatalf("%s: %s %s", expression, result.ExceptionDetails.Text, result.ExceptionDetails.Exception.Description)
	}
	return fmt.Sprint(result.Result.Value)
}

// waitFor polls an expression until it is true.
func (b *browser) waitFor(expression string) bool {
	b.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		// A navigation may be under way: a page without a body yet is not an answer.
		if b.eval(`(() => { try { return (`+expression+`) === true } catch { return false } })()`) == "true" {
			return true
		}
	}
	return false
}

func (b *browser) must(expression, what string) {
	b.t.Helper()
	if !b.waitFor(expression) {
		b.t.Fatalf("%s: %s (at %s: %.400s)", what, expression, b.eval("location.href"), b.eval("document.body?.innerText"))
	}
}

// phone makes the page a phone of this size: a narrow viewport, touch instead of a mouse, and no hover.
func (b *browser) phone(width, height int) {
	b.t.Helper()
	b.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": true}, nil)
	b.call("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true, "maxTouchPoints": 5}, nil)
	b.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "hover", "value": "none"}, {"name": "pointer", "value": "coarse"}}}, nil)
}

type box struct{ X, Y, Left, Right, Top, Bottom, Width, Height float64 }

// rect scrolls an element into view, as a thumb would, and returns where it is on the screen.
func (b *browser) rect(selector string) box {
	b.t.Helper()
	b.must(`!!document.querySelector(`+strconv.Quote(selector)+`)`, "no element "+selector)
	raw := b.eval(`JSON.stringify((() => { const e = document.querySelector(` + strconv.Quote(selector) + `); e.scrollIntoView({block: 'center', behavior: 'instant'}); const r = e.getBoundingClientRect();
		return {X: r.left + r.width / 2, Y: r.top + r.height / 2, Left: r.left, Right: r.right, Top: r.top, Bottom: r.bottom, Width: r.width, Height: r.height}; })())`)
	var r box
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		b.t.Fatalf("%s: %s", selector, raw)
	}
	return r
}

// tap touches the middle of an element with a finger. The element has to be there, visible, big enough for a finger
// and the topmost thing at that point: a covered control cannot be tapped.
func (b *browser) tap(selector string) {
	b.t.Helper()
	// A thumb waits until what it aims at has come to rest, such as a section that opens with an animation.
	var r box
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		r = b.rect(selector)
		if r.Width < 1 || r.Height < 1 {
			b.t.Fatalf("%s is not visible", selector)
		}
		top := b.eval(fmt.Sprintf(`(() => { const e = document.elementFromPoint(%f, %f), t = document.querySelector(%s); return !!e && (e === t || t.contains(e)); })()`, r.X, r.Y, strconv.Quote(selector)))
		if top == "true" {
			break
		}
		if time.Now().After(deadline) {
			b.t.Fatalf("%s is covered by something else: %s", selector,
				b.eval(fmt.Sprintf(`document.elementFromPoint(%f, %f)?.outerHTML.slice(0, 160)`, r.X, r.Y)))
		}
	}
	b.call("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []map[string]any{{"x": r.X, "y": r.Y}}}, nil)
	time.Sleep(60 * time.Millisecond)
	b.call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []map[string]any{}}, nil)
}

// typeInto taps a field and types text into it, as the keyboard of a phone does.
func (b *browser) typeInto(selector, text string) {
	b.t.Helper()
	b.tap(selector)
	b.eval(`document.querySelector(` + strconv.Quote(selector) + `).select?.()`)
	b.call("Input.insertText", map[string]any{"text": text}, nil)
}

func (b *browser) text(selector string) string {
	b.t.Helper()
	return b.eval(`document.querySelector(` + strconv.Quote(selector) + `)?.textContent.trim().replace(/\s+/g, ' ') ?? ''`)
}

// shot saves a picture of the page when BROWSER_SHOTS names a folder.
func (b *browser) shot(name string) {
	b.t.Helper()
	folder := os.Getenv("BROWSER_SHOTS")
	if folder == "" {
		return
	}
	var result struct{ Data string }
	b.call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true}, &result)
	picture, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, name+".png"), picture, 0o644); err != nil {
		b.t.Fatal(err)
	}
}

// passkeys gives the browser a platform authenticator that keeps passkeys and verifies the person at once, like a phone
// with a fingerprint already on the sensor.
func (b *browser) passkeys() string {
	b.t.Helper()
	b.call("WebAuthn.enable", map[string]any{}, nil)
	var result struct{ AuthenticatorID string }
	b.call("WebAuthn.addVirtualAuthenticator", map[string]any{"options": map[string]any{
		"protocol": "ctap2", "transport": "internal", "hasResidentKey": true, "hasUserVerification": true,
		"isUserVerified": true, "automaticPresenceSimulation": true,
	}}, &result)
	return result.AuthenticatorID
}

// credentials counts the passkeys the authenticator holds.
func (b *browser) credentials(authenticator string) int {
	b.t.Helper()
	var result struct{ Credentials []json.RawMessage }
	b.call("WebAuthn.getCredentials", map[string]any{"authenticatorId": authenticator}, &result)
	return len(result.Credentials)
}

// unreachable lists what a hand cannot reach on this screen: the page scrolling sideways, a control wider than the
// screen, text cut off by an edge.
const unreachable = `(() => {
	const W = innerWidth, bad = [];
	if (document.documentElement.scrollWidth > W) bad.push('the page scrolls sideways: ' + document.documentElement.scrollWidth + ' > ' + W);
	for (const e of document.querySelectorAll('a, button, input, select, textarea, label')) {
		if (!e.getClientRects().length || e.closest('dialog:not([open])')) continue;
		const r = e.getBoundingClientRect();
		if (r.left < -1 || r.right > W + 1) bad.push('outside the screen: ' + e.outerHTML.slice(0, 80));
	}
	// Scrolled to the end, the last part of the page stays above the tab bar.
	const bar = document.querySelector('.tab-bar'), main = document.querySelector('main');
	if (bar && bar.getClientRects().length && main && main.lastElementChild) {
		const y = scrollY;
		scrollTo({top: document.documentElement.scrollHeight, behavior: 'instant'});
		if (main.lastElementChild.getBoundingClientRect().bottom > bar.getBoundingClientRect().top + 1) bad.push('the end of the page hides behind the tab bar');
		scrollTo({top: y, behavior: 'instant'});
	}
	return JSON.stringify(bad);
})()`

func (b *browser) reachable(where string) {
	b.t.Helper()
	if got := b.eval(unreachable); got != "[]" {
		b.t.Errorf("%s: %s", where, got)
	}
}
