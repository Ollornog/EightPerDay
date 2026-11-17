// SPDX-License-Identifier: MIT

// Command eightperday is the standalone time tracking program: one binary, PostgreSQL, a web interface.
//
// Configuration comes from the environment, defaults in brackets:
//
//	EIGHTPERDAY_DATABASE_URL       PostgreSQL connection string (required); or a file with it in
//	EIGHTPERDAY_DATABASE_URL_FILE  …_FILE, for a container secret
//	EIGHTPERDAY_ORIGIN             the address people open, such as https://example.org ("http://localhost:<port>")
//	EIGHTPERDAY_ADDR               address of the web interface ("127.0.0.1:8080")
//	EIGHTPERDAY_HEALTH_ADDR        address of the health endpoint ("127.0.0.1:8081")
//	EIGHTPERDAY_TRUSTED_PROXIES    comma-separated networks whose X-Forwarded-For and gate headers are believed
//	EIGHTPERDAY_GATE_USER_HEADER   header of a sign-in gate that names the account, such as Remote-User (off)
//	EIGHTPERDAY_GATE_NAME_HEADER   header with the display name, such as Remote-Name
//	EIGHTPERDAY_GATE_GROUPS_HEADER header with the groups, such as Remote-Groups
//	EIGHTPERDAY_GATE_ADMIN_GROUP   the group whose members administer the program
//
// Commands:
//
//	eightperday serve                       run the program; without an account it says how to make the first one
//	eightperday link [--admin] [<name>]     print a one-time link that invites someone (optionally with a fixed name);
//	                                        without an account: the link for the first administrator, valid one hour
//	eightperday link --reset <name>         print a one-time link that replaces the sign-in of an account
//	eightperday probe                       ask the health endpoint (for the container's health check)
//	eightperday version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ollornog/EightPerDay/core/pgstore"
	"github.com/Ollornog/EightPerDay/internal/version"
	"github.com/Ollornog/EightPerDay/web"
	"github.com/Ollornog/EightPerDay/web/auth"
)

const (
	usage             = "usage: eightperday serve | eightperday link [--admin] [<name>] | eightperday link --reset <name> | eightperday probe | eightperday version"
	defaultAddr       = "127.0.0.1:8080"
	defaultHealthAddr = "127.0.0.1:8081"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run executes a command and returns the exit code of the process.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch {
	case args[0] == "version" && len(args) == 1:
		_, _ = fmt.Fprintln(stdout, "eightperday", version.Version)
	case args[0] == "probe" && len(args) == 1:
		err = web.Probe(context.Background(), envOr(getenv, "EIGHTPERDAY_HEALTH_ADDR", defaultHealthAddr))
	case args[0] == "serve" && len(args) == 1:
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer stop()
		err = serve(ctx, getenv, stdout, stderr)
	case args[0] == "link":
		var request linkRequest
		if request, err = parseLink(args[1:]); err != nil {
			_, _ = fmt.Fprintln(stderr, usage)
			return 2
		}
		err = link(context.Background(), getenv, request, stdout)
	default:
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "eightperday:", err)
		return 1
	}
	return 0
}

func envOr(getenv func(string) string, name, fallback string) string {
	if value := strings.TrimSpace(getenv(name)); value != "" {
		return value
	}
	return fallback
}

// config is what serve and link read from the environment.
type config struct {
	databaseURL string
	origin      *url.URL
	addr        string
	healthAddr  string
	proxies     []netip.Prefix
	gate        *web.Gate
}

func readConfig(getenv func(string) string) (config, error) {
	c := config{
		addr:       envOr(getenv, "EIGHTPERDAY_ADDR", defaultAddr),
		healthAddr: envOr(getenv, "EIGHTPERDAY_HEALTH_ADDR", defaultHealthAddr),
	}
	c.databaseURL = strings.TrimSpace(getenv("EIGHTPERDAY_DATABASE_URL"))
	if file := strings.TrimSpace(getenv("EIGHTPERDAY_DATABASE_URL_FILE")); file != "" {
		if c.databaseURL != "" {
			return c, errors.New("set EIGHTPERDAY_DATABASE_URL or EIGHTPERDAY_DATABASE_URL_FILE, not both")
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return c, fmt.Errorf("EIGHTPERDAY_DATABASE_URL_FILE: %w", err)
		}
		c.databaseURL = strings.TrimSpace(string(raw))
	}
	if c.databaseURL == "" {
		return c, errors.New("EIGHTPERDAY_DATABASE_URL is missing: the program keeps its data in PostgreSQL")
	}
	_, port, err := net.SplitHostPort(c.addr)
	if err != nil {
		return c, fmt.Errorf("EIGHTPERDAY_ADDR: %w", err)
	}
	origin := envOr(getenv, "EIGHTPERDAY_ORIGIN", "http://localhost:"+port)
	if c.origin, err = url.Parse(origin); err != nil || (c.origin.Scheme != "https" && c.origin.Scheme != "http") ||
		c.origin.Host == "" || c.origin.Path != "" && c.origin.Path != "/" || c.origin.RawQuery != "" || c.origin.User != nil {
		return c, fmt.Errorf("EIGHTPERDAY_ORIGIN %q: give scheme and host only, such as https://example.org", origin)
	}
	c.origin.Path = ""
	for _, part := range strings.Split(getenv("EIGHTPERDAY_TRUSTED_PROXIES"), ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			addr, addrErr := netip.ParseAddr(part)
			if addrErr != nil {
				return c, fmt.Errorf("EIGHTPERDAY_TRUSTED_PROXIES: %q is neither a network nor an address", part)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.proxies = append(c.proxies, prefix.Masked())
	}
	if header := strings.TrimSpace(getenv("EIGHTPERDAY_GATE_USER_HEADER")); header != "" {
		if len(c.proxies) == 0 {
			return c, errors.New("EIGHTPERDAY_GATE_USER_HEADER needs EIGHTPERDAY_TRUSTED_PROXIES: anyone could send the header otherwise")
		}
		c.gate = &web.Gate{
			UserHeader:   header,
			NameHeader:   strings.TrimSpace(getenv("EIGHTPERDAY_GATE_NAME_HEADER")),
			GroupsHeader: strings.TrimSpace(getenv("EIGHTPERDAY_GATE_GROUPS_HEADER")),
			AdminGroup:   strings.TrimSpace(getenv("EIGHTPERDAY_GATE_ADMIN_GROUP")),
		}
	}
	return c, nil
}

// tenant is the one tenant of the standalone program: all its rows carry it, row-level security holds them to it.
const tenant = "eightperday"

// storeOptions are part of the schema; they never change once the migrations ran.
var storeOptions = pgstore.Options{Prefix: "epd_", TenantSetting: "eightperday.tenant"}

// openStore runs the migrations of the core library and opens its store on a pool whose connections carry the tenant.
func openStore(ctx context.Context, c config) (*pgxpool.Pool, *pgstore.Store, error) {
	pool, err := pgstore.Connect(ctx, c.databaseURL, storeOptions, tenant)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}
	if err := pgstore.Migrate(ctx, pool, storeOptions); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("core migrations: %w", err)
	}
	store, err := pgstore.New(pool, storeOptions)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, store, nil
}

func open(ctx context.Context, c config, log *slog.Logger) (*pgxpool.Pool, *auth.Service, error) {
	pool, err := pgxpool.New(ctx, c.databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}
	connect, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err = pool.Ping(connect); err == nil {
			break
		}
		select {
		case <-connect.Done():
			pool.Close()
			return nil, nil, fmt.Errorf("database does not answer: %w", err)
		case <-time.After(time.Second):
		}
	}
	accounts, err := auth.Open(ctx, pool, auth.Config{Origin: c.origin}, log)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, accounts, nil
}

func serve(ctx context.Context, getenv func(string) string, stdout, stderr io.Writer) error {
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	c, err := readConfig(getenv)
	if err != nil {
		return err
	}
	pool, accounts, err := open(ctx, c, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	storePool, store, err := openStore(ctx, c)
	if err != nil {
		return err
	}
	defer storePool.Close()
	if n, err := accounts.AccountCount(ctx); err != nil {
		return err
	} else if n == 0 {
		// The console is the way the first account comes in: there is no sign-up. The link itself never goes to the
		// log, where everyone who reads the log could take it.
		_, _ = fmt.Fprint(stderr, firstAccountHint)
	}
	handler, err := web.New(log, web.Options{Auth: accounts, Store: store, Origin: c.origin, Gate: c.gate, TrustedProxies: c.proxies})
	if err != nil {
		return err
	}
	go cleanUp(ctx, accounts, log)
	ready := func() bool {
		ping, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return pool.Ping(ping) == nil && storePool.Ping(ping) == nil
	}
	log.Info("eightperday", slog.String("version", version.Version), slog.String("origin", c.origin.String()))
	return web.Serve(ctx, log, web.NewServer(c.addr, handler, log), web.NewServer(c.healthAddr, web.Health(ready), log))
}

func cleanUp(ctx context.Context, accounts *auth.Service, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := accounts.CleanUp(ctx); err != nil && ctx.Err() == nil {
				log.Warn("clean-up failed", slog.String("error", err.Error()))
			}
		}
	}
}

// firstAccountHint is what serve says while there is no account: how to make the link, never the link.
const firstAccountHint = "\nNo account yet. Make the one-time link for the first administrator at the server's console:\n\n" +
	"  eightperday link --admin\n\n(in a container: docker compose exec eightperday /eightperday link --admin)." +
	" It lasts one hour, and a new one replaces it.\n"

func setupURL(origin *url.URL, token string) string {
	return origin.Scheme + "://" + origin.Host + "/setup?token=" + token
}

type linkRequest struct {
	admin bool
	reset bool
	name  string
}

func parseLink(args []string) (linkRequest, error) {
	var request linkRequest
	for _, arg := range args {
		switch {
		case arg == "--admin":
			request.admin = true
		case arg == "--reset":
			request.reset = true
		case strings.HasPrefix(arg, "-") || request.name != "":
			return request, errors.New("bad argument")
		default:
			request.name = arg
		}
	}
	if request.reset && (request.name == "" || request.admin) {
		return request, errors.New("--reset needs a name and nothing else")
	}
	return request, nil
}

// link prints a one-time link, for a person at the server's console.
func link(ctx context.Context, getenv func(string) string, request linkRequest, stdout io.Writer) error {
	c, err := readConfig(getenv)
	if err != nil {
		return err
	}
	pool, accounts, err := open(ctx, c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	defer pool.Close()
	if n, err := accounts.AccountCount(ctx); err != nil {
		return err
	} else if n == 0 && !request.reset {
		// The first account administers the program; its link is short-lived and replaces any before it.
		if !request.admin {
			return errors.New("there is no account yet: the first one administers the program, so make its link with --admin")
		}
		token, err := accounts.FirstSetupLink(ctx, request.name)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, setupURL(c.origin, token))
		return err
	}
	setup := auth.SetupLink{Username: request.name, Admin: request.admin}
	if request.reset {
		account, err := findAccount(ctx, accounts, request.name)
		if err != nil {
			return err
		}
		setup = auth.SetupLink{AccountID: account.ID}
	}
	token, err := accounts.NewSetupLink(ctx, setup, auth.InviteTTL)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, setupURL(c.origin, token))
	return err
}

func findAccount(ctx context.Context, accounts *auth.Service, name string) (*auth.Account, error) {
	name, err := auth.NormalizeName(name)
	if err != nil {
		return nil, err
	}
	all, err := accounts.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if a.Username == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no account named %q", name)
}
