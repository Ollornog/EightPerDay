// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	"github.com/Ollornog/EightPerDay/internal/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newService(t *testing.T) (*Service, *clock) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	c := &clock{now: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	origin, _ := url.Parse("https://example.org")
	s, err := Open(context.Background(), pool, Config{Origin: origin, Now: c.Now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

const goodPassword = "correct horse battery staple"

// account makes an account with a password through an invitation, the way a person would.
func account(t *testing.T, s *Service, name string, admin bool) *Account {
	t.Helper()
	ctx := context.Background()
	token, err := s.NewSetupLink(ctx, SetupLink{Username: name, Admin: admin}, InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.SetupWithPassword(ctx, token, SetupChoice{DisplayName: strings.ToUpper(name)}, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestMigrationsRunTwice(t *testing.T) {
	s, _ := newService(t)
	if err := s.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFirstLinkOnlyWithoutAccounts(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	token, err := s.FirstSetupLink(ctx)
	if err != nil || token == "" {
		t.Fatalf("%q %v", token, err)
	}
	link, err := s.LookupSetupLink(ctx, token)
	if err != nil || !link.Admin || link.Username != "" {
		t.Fatalf("%+v %v", link, err)
	}
	a, err := s.SetupWithPassword(ctx, token, SetupChoice{Username: " Mara ", DisplayName: "Mara S."}, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if a.Username != "mara" || !a.Admin || a.DisplayName != "Mara S." || !a.HasPassword || a.HasTOTP {
		t.Fatalf("%+v", a)
	}
	if again, err := s.FirstSetupLink(ctx); err != nil || again != "" {
		t.Fatalf("a second first link: %q %v", again, err)
	}
	if _, err := s.SetupWithPassword(ctx, token, SetupChoice{Username: "eve"}, goodPassword); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("the link worked twice: %v", err)
	}
}

func TestLinksExpire(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	token, err := s.NewSetupLink(ctx, SetupLink{Username: "late"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c.Add(time.Hour + time.Second)
	if _, err := s.SetupWithPassword(ctx, token, SetupChoice{}, goodPassword); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("an expired link worked: %v", err)
	}
	if _, err := s.LookupSetupLink(ctx, "made-up"); !errors.Is(err, ErrLinkInvalid) {
		t.Fatal(err)
	}
}

func TestInvitationKeepsItsName(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	account(t, s, "taken", false)
	if _, err := s.NewSetupLink(ctx, SetupLink{Username: "Taken"}, InviteTTL); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("an invitation for a taken name: %v", err)
	}
	token, err := s.NewSetupLink(ctx, SetupLink{Username: "ben"}, InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.SetupWithPassword(ctx, token, SetupChoice{Username: "someone-else"}, goodPassword)
	if err != nil || a.Username != "ben" || a.Admin {
		t.Fatalf("%+v %v", a, err)
	}
	for _, bad := range []string{"", "-x", "a b", "ä", strings.Repeat("a", 65), "../x"} {
		if _, err := NormalizeName(bad); !errors.Is(err, ErrBadName) {
			t.Errorf("%q passed as a name", bad)
		}
	}
}

func TestPasswordRules(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	token, _ := s.NewSetupLink(ctx, SetupLink{Username: "x"}, InviteTTL)
	if _, err := s.SetupWithPassword(ctx, token, SetupChoice{}, "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatal(err)
	}
	if _, err := s.SetupWithPassword(ctx, token, SetupChoice{}, strings.Repeat("x", 1025)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatal(err)
	}
	// Twelve characters, counted as characters and not as bytes.
	if _, err := s.SetupWithPassword(ctx, token, SetupChoice{}, "ääääääääääää"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordIsHashedWithArgon2id(t *testing.T) {
	s, _ := newService(t)
	a := account(t, s, "hash", false)
	var stored string
	if err := s.db.QueryRow(context.Background(), "SELECT password_hash FROM epd_account WHERE id = $1", a.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "$argon2id$v=19$m=65536,t=3,p=2$") || strings.Contains(stored, goodPassword) {
		t.Fatalf("stored %q", stored)
	}
}

func TestPasswordLogin(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	result, err := s.PasswordLogin(ctx, "192.0.2.1", "MARA", goodPassword)
	if err != nil || result.Account == nil || result.Account.ID != a.ID || result.SecondFactor != "" {
		t.Fatalf("%+v %v", result, err)
	}
	for _, try := range [][2]string{{"mara", "wrong password!"}, {"nobody", goodPassword}, {"", goodPassword}, {"mara", ""}} {
		if _, err := s.PasswordLogin(ctx, "192.0.2.2", try[0], try[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", try, err)
		}
	}
}

func TestDisabledAccountCannotSignIn(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	account(t, s, "boss", true)
	a := account(t, s, "gone", false)
	token, err := s.NewSession(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PasswordLogin(ctx, "192.0.2.1", "gone", goodPassword); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a disabled account signed in: %v", err)
	}
	if _, err := s.SessionAccount(ctx, token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the session of a disabled account lives on: %v", err)
	}
}

func TestThrottleSlowsGuessing(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	account(t, s, "mara", false)
	for i := range freeFailuresName {
		if _, err := s.PasswordLogin(ctx, "192.0.2.1", "mara", "guess number "+string(rune('a'+i))); !errors.Is(err, ErrInvalid) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	// Now even the right password waits: the guesser cannot tell.
	var throttled *ThrottledError
	if _, err := s.PasswordLogin(ctx, "198.51.100.7", "mara", goodPassword); !errors.As(err, &throttled) || throttled.Wait != firstWait {
		t.Fatalf("not throttled: %v", err)
	}
	c.Add(firstWait + time.Second)
	if result, err := s.PasswordLogin(ctx, "198.51.100.7", "mara", goodPassword); err != nil || result.Account == nil {
		t.Fatalf("still throttled after the wait: %v", err)
	}
	// The success cleared the name.
	if _, err := s.PasswordLogin(ctx, "198.51.100.7", "mara", "one more wrong one"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
}

func TestThrottleCountsUnknownNamesAndAddresses(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	for i := range freeFailuresAddress {
		_, _ = s.PasswordLogin(ctx, "203.0.113.9", "name"+string(rune('a'+i)), "wrong password")
	}
	var throttled *ThrottledError
	if _, err := s.PasswordLogin(ctx, "203.0.113.9", "fresh", "wrong password"); !errors.As(err, &throttled) {
		t.Fatalf("an address that guessed twenty names is not slowed down: %v", err)
	}
	if _, err := s.PasswordLogin(ctx, "203.0.113.10", "fresh", "wrong password"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("another address is slowed down too: %v", err)
	}
}

func TestThrottleGrowsAndStops(t *testing.T) {
	c := &clock{now: time.Unix(0, 0)}
	th := newThrottle(c.Now)
	for range freeFailuresName - 1 {
		th.fail("name:x")
	}
	if th.wait("name:x") != 0 {
		t.Fatal("throttled before the free failures are used")
	}
	th.fail("name:x")
	if got := th.wait("name:x"); got != firstWait {
		t.Fatalf("first wait %s", got)
	}
	th.fail("name:x")
	if got := th.wait("name:x"); got != 2*firstWait {
		t.Fatalf("second wait %s", got)
	}
	for range 20 {
		th.fail("name:x")
	}
	if got := th.wait("name:x"); got != maxWait {
		t.Fatalf("the wait grows beyond an hour: %s", got)
	}
}

func TestSecondFactor(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	token, _ := s.NewSetupLink(ctx, SetupLink{Username: "tom"}, InviteTTL)
	enrollment, err := s.BeginSetupWithTOTP(ctx, token, SetupChoice{}, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enrollment.QR, "data:image/png;base64,") || !strings.HasPrefix(enrollment.URL, "otpauth://totp/EightPerDay:tom") {
		t.Fatalf("%+v", enrollment)
	}
	// No account before the code is confirmed.
	if n, _ := s.AccountCount(ctx); n != 0 {
		t.Fatal("the account exists before its code was confirmed")
	}
	if _, err := s.FinishSetupWithTOTP(ctx, enrollment.Ceremony, "000000"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(enrollment.Secret, c.Now())
	a, err := s.FinishSetupWithTOTP(ctx, enrollment.Ceremony, code)
	if err != nil || !a.HasTOTP || !a.HasPassword {
		t.Fatalf("%+v %v", a, err)
	}
	// The setup code was spent; a code of the next step signs in.
	result, err := s.PasswordLogin(ctx, "192.0.2.1", "tom", goodPassword)
	if err != nil || result.Account != nil || result.SecondFactor == "" {
		t.Fatalf("a password alone signed in an account with a second factor: %+v %v", result, err)
	}
	if _, err := s.SecondFactorLogin(ctx, "192.0.2.1", result.SecondFactor, code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the code of the setup worked again: %v", err)
	}
	c.Add(30 * time.Second)
	code, _ = totp.GenerateCode(enrollment.Secret, c.Now())
	signedIn, err := s.SecondFactorLogin(ctx, "192.0.2.1", result.SecondFactor, code)
	if err != nil || signedIn.ID != a.ID {
		t.Fatalf("%v", err)
	}
	// The same code once more, in a new attempt: refused (replay).
	result, _ = s.PasswordLogin(ctx, "192.0.2.1", "tom", goodPassword)
	if _, err := s.SecondFactorLogin(ctx, "192.0.2.1", result.SecondFactor, code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a code worked twice: %v", err)
	}
	// The ceremony of a finished login is gone.
	if _, err := s.SecondFactorLogin(ctx, "192.0.2.1", "made-up", code); !errors.Is(err, ErrCeremony) {
		t.Fatal(err)
	}
}

func TestSecondFactorEndsAfterFewWrongCodes(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	a := account(t, s, "tom", false)
	enrollment, err := s.BeginEnableTOTP(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(enrollment.Secret, c.Now())
	if err := s.FinishEnableTOTP(ctx, a.ID, enrollment.Ceremony, code); err != nil {
		t.Fatal(err)
	}
	result, _ := s.PasswordLogin(ctx, "192.0.2.1", "tom", goodPassword)
	for range maxSecondAttempts {
		_, _ = s.SecondFactorLogin(ctx, "192.0.2.1", result.SecondFactor, "123456")
	}
	c.Add(30 * time.Second)
	code, _ = totp.GenerateCode(enrollment.Secret, c.Now())
	if _, err := s.SecondFactorLogin(ctx, "192.0.2.1", result.SecondFactor, code); err == nil {
		t.Fatal("the attempt survived five wrong codes")
	}
}

func TestDisableTOTPNeedsACode(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	a := account(t, s, "tom", false)
	enrollment, _ := s.BeginEnableTOTP(ctx, a.ID)
	code, _ := totp.GenerateCode(enrollment.Secret, c.Now())
	if err := s.FinishEnableTOTP(ctx, a.ID, enrollment.Ceremony, code); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableTOTP(ctx, "192.0.2.1", a.ID, "000000"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	c.Add(30 * time.Second)
	code, _ = totp.GenerateCode(enrollment.Secret, c.Now())
	if err := s.DisableTOTP(ctx, "192.0.2.1", a.ID, code); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := s.Account(ctx, a.ID); fresh.HasTOTP {
		t.Fatal("still has TOTP")
	}
}

func TestSessions(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	token, err := s.NewSession(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM epd_session WHERE token_hash = $1::bytea", []byte(token)).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("the token is stored in clear: %d %v", stored, err)
	}
	if got, err := s.SessionAccount(ctx, token); err != nil || got.ID != a.ID {
		t.Fatal(err)
	}
	// Use extends it.
	c.Add(29 * 24 * time.Hour)
	if _, err := s.SessionAccount(ctx, token); err != nil {
		t.Fatal(err)
	}
	c.Add(29 * 24 * time.Hour)
	if _, err := s.SessionAccount(ctx, token); err != nil {
		t.Fatalf("a used session expired: %v", err)
	}
	// Unused, it ends.
	c.Add(31 * 24 * time.Hour)
	if _, err := s.SessionAccount(ctx, token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unused session lives on: %v", err)
	}
	other, _ := s.NewSession(ctx, a.ID)
	keep, _ := s.NewSession(ctx, a.ID)
	// The expired session and the other one go.
	if n, err := s.EndOtherSessions(ctx, a.ID, keep); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := s.SessionAccount(ctx, other); !errors.Is(err, ErrInvalid) {
		t.Fatal("the other session lives on")
	}
	if err := s.EndSession(ctx, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionAccount(ctx, keep); !errors.Is(err, ErrInvalid) {
		t.Fatal("signed out, still signed in")
	}
	for _, bad := range []string{"", strings.Repeat("a", 101), "made-up"} {
		if _, err := s.SessionAccount(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestSessionsHaveAnEnd(t *testing.T) {
	s, c := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	token, _ := s.NewSession(ctx, a.ID)
	for range 14 {
		c.Add(28 * 24 * time.Hour)
		_, _ = s.SessionAccount(ctx, token)
	}
	if _, err := s.SessionAccount(ctx, token); !errors.Is(err, ErrInvalid) {
		t.Fatal("a session used every month lives for more than a year")
	}
}

func TestChangePasswordNeedsTheOldOne(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	if err := s.SetPassword(ctx, "192.0.2.1", a.ID, "not the old one", "a brand new password"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.SetPassword(ctx, "192.0.2.1", a.ID, goodPassword, "a brand new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PasswordLogin(ctx, "192.0.2.1", "mara", goodPassword); !errors.Is(err, ErrInvalid) {
		t.Fatal("the old password still works")
	}
	if r, err := s.PasswordLogin(ctx, "192.0.2.1", "mara", "a brand new password"); err != nil || r.Account == nil {
		t.Fatal(err)
	}
}

func TestLastMethodStays(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	if err := s.RemovePassword(ctx, a.ID); !errors.Is(err, ErrLastMethod) {
		t.Fatalf("the only password was removed: %v", err)
	}
	if fresh, _ := s.Account(ctx, a.ID); !fresh.HasPassword {
		t.Fatal("the password is gone after the refusal")
	}
}

func TestLastAdminStays(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	boss := account(t, s, "boss", true)
	other := account(t, s, "other", false)
	if err := s.SetDisabled(ctx, boss.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("the last administrator was disabled: %v", err)
	}
	if err := s.SetAdmin(ctx, boss.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("the last administrator lost the role: %v", err)
	}
	if err := s.SetAdmin(ctx, other.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdmin(ctx, boss.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestResetReplacesTheSignIn(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a := account(t, s, "mara", false)
	session, _ := s.NewSession(ctx, a.ID)
	token, err := s.NewSetupLink(ctx, SetupLink{AccountID: a.ID}, InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := s.LookupSetupLink(ctx, token)
	if !link.Reset() {
		t.Fatal("not a reset")
	}
	fresh, err := s.SetupWithPassword(ctx, token, SetupChoice{Username: "eve"}, "the new password here")
	if err != nil || fresh.ID != a.ID || fresh.Username != "mara" {
		t.Fatalf("%+v %v", fresh, err)
	}
	if _, err := s.SessionAccount(ctx, session); !errors.Is(err, ErrInvalid) {
		t.Fatal("the sessions from before the reset live on")
	}
	if _, err := s.PasswordLogin(ctx, "192.0.2.1", "mara", goodPassword); !errors.Is(err, ErrInvalid) {
		t.Fatal("the old password still works")
	}
}

func TestGateAccount(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a, err := s.GateAccount(ctx, "Mara", "Mara S.", true)
	if err != nil || a.Username != "mara" || !a.Admin || a.HasPassword || len(a.Passkeys) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
	again, err := s.GateAccount(ctx, "mara", "", false)
	if err != nil || again.ID != a.ID || again.Admin {
		t.Fatalf("the gate's word on the role does not count: %+v %v", again, err)
	}
	if _, err := s.GateAccount(ctx, "not a name", "", false); !errors.Is(err, ErrBadName) {
		t.Fatal(err)
	}
}

func TestCeremoniesExpireAndAreSingleUse(t *testing.T) {
	c := &clock{now: time.Unix(0, 0)}
	store := newCeremonies(c.Now)
	id, _ := store.put(&ceremony{kind: kindLoginPasskey})
	if store.take(id, kindAddPasskey) != nil {
		t.Fatal("taken as another kind")
	}
	if store.take(id, kindLoginPasskey) == nil {
		t.Fatal("not found")
	}
	if store.take(id, kindLoginPasskey) != nil {
		t.Fatal("taken twice")
	}
	id, _ = store.put(&ceremony{kind: kindLoginPasskey})
	c.now = c.now.Add(ceremonyTTL + time.Second)
	if store.take(id, kindLoginPasskey) != nil {
		t.Fatal("an expired ceremony")
	}
	for range maxCeremonies + 10 {
		_, _ = store.put(&ceremony{kind: kindLoginPasskey})
	}
	if len(store.m) > maxCeremonies {
		t.Fatalf("%d ceremonies kept", len(store.m))
	}
}

func TestTOTPStepAllowsOneStepOfDrift(t *testing.T) {
	key, _, err := newTOTPKey("x")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	for offset, want := range map[time.Duration]bool{-60 * time.Second: false, -30 * time.Second: true, 0: true, 30 * time.Second: true, 60 * time.Second: false} {
		code, _ := totp.GenerateCode(key.Secret(), now.Add(offset))
		if got := totpStep(key.Secret(), code, now) != 0; got != want {
			t.Errorf("offset %s: accepted %v", offset, got)
		}
	}
	if totpStep(key.Secret(), "12345", now) != 0 || totpStep(key.Secret(), "", now) != 0 {
		t.Error("a short code passed")
	}
}
