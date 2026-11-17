// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/internal/pgtest"
	"github.com/Ollornog/EightPerDay/web/auth"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"nonsense"}, {"serve", "extra"}, {"link", "--bogus"}, {"link", "--reset"}, {"link", "a", "b"}} {
		var stderr bytes.Buffer
		if code := run(args, env(nil), &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage") {
			t.Errorf("%v: %d %q", args, code, stderr.String())
		}
	}
	var stdout bytes.Buffer
	if code := run([]string{"version"}, env(nil), &stdout, &bytes.Buffer{}); code != 0 || !strings.HasPrefix(stdout.String(), "eightperday ") {
		t.Errorf("%d %q", code, stdout.String())
	}
}

func TestConfig(t *testing.T) {
	base := map[string]string{"EIGHTPERDAY_DATABASE_URL": "postgres://x/y"}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	c, err := readConfig(env(base))
	if err != nil || c.origin.String() != "http://localhost:8080" || c.addr != defaultAddr || c.gate != nil {
		t.Fatalf("%+v %v", c, err)
	}
	for name, values := range map[string]map[string]string{
		"no database":         {},
		"origin with a path":  with(map[string]string{"EIGHTPERDAY_ORIGIN": "https://example.org/time"}),
		"origin without host": with(map[string]string{"EIGHTPERDAY_ORIGIN": "https://"}),
		"origin ftp":          with(map[string]string{"EIGHTPERDAY_ORIGIN": "ftp://example.org"}),
		"gate without proxy":  with(map[string]string{"EIGHTPERDAY_GATE_USER_HEADER": "Remote-User"}),
		"bad proxy":           with(map[string]string{"EIGHTPERDAY_TRUSTED_PROXIES": "somewhere"}),
		"url twice":           with(map[string]string{"EIGHTPERDAY_DATABASE_URL_FILE": "/x"}),
	} {
		if _, err := readConfig(env(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c, err = readConfig(env(with(map[string]string{
		"EIGHTPERDAY_ORIGIN": "https://example.org/", "EIGHTPERDAY_TRUSTED_PROXIES": "192.0.2.1, 10.0.0.0/8",
		"EIGHTPERDAY_GATE_USER_HEADER": "Remote-User", "EIGHTPERDAY_GATE_ADMIN_GROUP": "admins",
	})))
	if err != nil || c.origin.String() != "https://example.org" || len(c.proxies) != 2 || c.proxies[0].String() != "192.0.2.1/32" ||
		c.gate == nil || c.gate.AdminGroup != "admins" {
		t.Fatalf("%+v %v", c, err)
	}
	file := filepath.Join(t.TempDir(), "url")
	if err := os.WriteFile(file, []byte("postgres://from/file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = readConfig(env(map[string]string{"EIGHTPERDAY_DATABASE_URL_FILE": file}))
	if err != nil || c.databaseURL != "postgres://from/file" {
		t.Fatalf("%q %v", c.databaseURL, err)
	}
}

// The whole program: it starts on a fresh database, says how to make the first account without printing a link,
// answers its health check, and the links for the first administrator and for an invitation come from the console.
func TestServeAndLink(t *testing.T) {
	database := pgtest.URL(t)
	port, health := freePort(t), freePort(t)
	values := map[string]string{
		"EIGHTPERDAY_DATABASE_URL": database,
		"EIGHTPERDAY_ADDR":         "127.0.0.1:" + port,
		"EIGHTPERDAY_HEALTH_ADDR":  "127.0.0.1:" + health,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, env(values), &stdout, &stderr) }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if run([]string{"probe"}, env(values), &bytes.Buffer{}, &bytes.Buffer{}) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not healthy: %s %s", stdout.String(), stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The attack: whoever reads the log must not find a link that makes them the administrator.
	logs := stdout.String() + stderr.String()
	if strings.Contains(logs, "token=") || strings.Contains(logs, "/setup") {
		t.Fatalf("the log holds a one-time link: %q", logs)
	}
	if !strings.Contains(stderr.String(), "eightperday link --admin") {
		t.Fatalf("the log does not say how to make the first account: %q", stderr.String())
	}
	response, err := http.Get("http://127.0.0.1:" + port + "/login")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("%d", response.StatusCode)
	}
	// Without an account the first one has to administer the program.
	var refused bytes.Buffer
	if code := run([]string{"link", "ben"}, env(values), &bytes.Buffer{}, &refused); code != 1 || !strings.Contains(refused.String(), "--admin") {
		t.Fatalf("a first link without --admin: %d %q", code, refused.String())
	}
	var first bytes.Buffer
	if code := run([]string{"link", "--admin"}, env(values), &first, &bytes.Buffer{}); code != 0 || !strings.Contains(first.String(), "/setup?token=") {
		t.Fatalf("%d %q", code, first.String())
	}
	var link bytes.Buffer
	if code := run([]string{"link", "--admin", "ben"}, env(values), &link, &bytes.Buffer{}); code != 0 || !strings.Contains(link.String(), "/setup?token=") {
		t.Fatalf("%d %q", code, link.String())
	}
	// The second first link replaced the first one, and it lasts an hour.
	firstToken := strings.TrimSpace(first.String()[strings.Index(first.String(), "token=")+len("token="):])
	secondToken := strings.TrimSpace(link.String()[strings.Index(link.String(), "token=")+len("token="):])
	pool, accounts, err := open(context.Background(), mustConfig(t, values), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := accounts.LookupSetupLink(context.Background(), firstToken); !errors.Is(err, auth.ErrLinkInvalid) {
		t.Fatalf("the replaced first link still works: %v", err)
	}
	if setup, err := accounts.LookupSetupLink(context.Background(), secondToken); err != nil || !setup.Admin || setup.Username != "ben" ||
		time.Until(setup.Expires) > auth.FirstLinkTTL || auth.FirstLinkTTL > time.Hour {
		t.Fatalf("%+v %v", setup, err)
	}
	var failure bytes.Buffer
	if code := run([]string{"link", "--reset", "nobody"}, env(values), &bytes.Buffer{}, &failure); code != 1 || !strings.Contains(failure.String(), "no account") {
		t.Fatalf("%d %q", code, failure.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func mustConfig(t *testing.T, values map[string]string) config {
	t.Helper()
	c, err := readConfig(env(values))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
