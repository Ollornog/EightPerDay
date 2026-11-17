// SPDX-License-Identifier: MIT

// Package auth signs people in to the standalone program: local accounts with a passkey (the default), a password with
// a code from an authenticator app, or a password alone. There is no sign-up: the first administrator comes from a
// short-lived one-time link made at the server's console (eightperday link --admin), everyone else from an invitation. The core library knows
// nothing of this (ADR-1); the accounts live in tables of their own.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	// ErrInvalid is the one answer to a failed sign-in, whatever was wrong: name, password, code or account state.
	ErrInvalid = errors.New("sign-in failed")
	// ErrLinkInvalid is returned for a one-time link that is unknown, used or expired.
	ErrLinkInvalid = errors.New("the link is not valid")
	// ErrNameTaken is returned when an account with that name exists.
	ErrNameTaken = errors.New("the name is taken")
	// ErrBadName is returned for a name that does not fit the rules.
	ErrBadName = errors.New("the name is not valid")
	// ErrLastMethod is returned when a change would leave an account without a way to sign in.
	ErrLastMethod = errors.New("the account would have no way to sign in")
	// ErrLastAdmin is returned when a change would leave the program without an active administrator.
	ErrLastAdmin = errors.New("the program would have no administrator")
	// ErrNotFound is returned for an account or passkey that does not exist.
	ErrNotFound = errors.New("not found")
	// ErrCeremony is returned for a passkey ceremony that is unknown, expired or failed.
	ErrCeremony = errors.New("the passkey step failed")
)

// ThrottledError says how long to wait before the next try.
type ThrottledError struct{ Wait time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("too many failed sign-ins, try again in %s", e.Wait.Round(time.Second))
}

// Config configures the service.
type Config struct {
	// Origin is the address people open, such as https://example.org: passkeys are bound to its host name, and
	// cookies are marked secure when it uses https.
	Origin *url.URL
	// SessionTTL is how long a session lasts without use. Zero means 30 days.
	SessionTTL time.Duration
	// Now replaces the clock in tests.
	Now func() time.Time
}

// Service holds the accounts.
type Service struct {
	db         *pgxpool.Pool
	cfg        Config
	log        *slog.Logger
	webauthn   *webauthn.WebAuthn
	hashSlots  chan struct{}
	throttle   *Throttle
	ceremonies *ceremonies
}

// Account is a person who can sign in.
type Account struct {
	ID          string
	Username    string
	DisplayName string
	Admin       bool
	Disabled    bool
	HasPassword bool
	HasTOTP     bool
	Language    string
	Created     time.Time
	Passkeys    []Passkey

	webauthnID  []byte
	credentials []webauthn.Credential
}

// Passkey is a registered passkey of an account.
type Passkey struct {
	ID       string // base64url of the credential ID
	Name     string
	Created  time.Time
	LastUsed *time.Time
}

// WebAuthnID implements webauthn.User.
func (a *Account) WebAuthnID() []byte { return a.webauthnID }

// WebAuthnName implements webauthn.User.
func (a *Account) WebAuthnName() string { return a.Username }

// WebAuthnDisplayName implements webauthn.User.
func (a *Account) WebAuthnDisplayName() string { return a.DisplayName }

// WebAuthnCredentials implements webauthn.User.
func (a *Account) WebAuthnCredentials() []webauthn.Credential { return a.credentials }

// Open runs the migrations and returns the service.
func Open(ctx context.Context, db *pgxpool.Pool, cfg Config, log *slog.Logger) (*Service, error) {
	if cfg.Origin == nil || cfg.Origin.Host == "" || (cfg.Origin.Scheme != "https" && cfg.Origin.Scheme != "http") {
		return nil, errors.New("auth: the origin needs a scheme (http or https) and a host")
	}
	if _, err := netip.ParseAddr(strings.Trim(cfg.Origin.Hostname(), "[]")); err == nil {
		return nil, errors.New("auth: passkeys need a host name, not an IP address, in the origin (localhost works on one machine)")
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.Origin.Hostname(),
		RPDisplayName: "EightPerDay",
		RPOrigins:     []string{cfg.Origin.Scheme + "://" + cfg.Origin.Host},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	s := &Service{
		db:         db,
		cfg:        cfg,
		log:        log,
		webauthn:   wa,
		hashSlots:  make(chan struct{}, max(2, runtime.NumCPU()/2)),
		throttle:   newThrottle(cfg.Now),
		ceremonies: newCeremonies(cfg.Now),
	}
	if err := s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("auth: migrations: %w", err)
	}
	return s, nil
}

// SecureCookies reports whether the origin uses https, so cookies must be marked secure.
func (s *Service) SecureCookies() bool { return s.cfg.Origin.Scheme == "https" }

// SessionTTL is how long a session lasts without use.
func (s *Service) SessionTTL() time.Duration { return s.cfg.SessionTTL }

func (s *Service) migrate(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Two processes starting at once must not both migrate.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('eightperday auth migrations'))"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS epd_auth_migration (name text PRIMARY KEY, applied timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(files)
	for _, file := range files {
		var done bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM epd_auth_migration WHERE name = $1)", file).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO epd_auth_migration (name) VALUES ($1)", file); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NormalizeName returns the account name in its stored form: trimmed and in lower case.
func NormalizeName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !namePattern.MatchString(name) {
		return "", ErrBadName
	}
	return name, nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

const accountColumns = "id, username, display_name, admin, disabled, webauthn_id, password_hash IS NOT NULL, totp_secret IS NOT NULL, language, created"

func scanAccount(row pgx.Row) (*Account, error) {
	a := &Account{}
	err := row.Scan(&a.ID, &a.Username, &a.DisplayName, &a.Admin, &a.Disabled, &a.webauthnID, &a.HasPassword, &a.HasTOTP, &a.Language, &a.Created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// loadPasskeys fills the passkeys of an account.
func loadPasskeys(ctx context.Context, q querier, a *Account) error {
	rows, err := q.Query(ctx, "SELECT id, name, credential, created, last_used FROM epd_passkey WHERE account_id = $1 ORDER BY created", a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	a.Passkeys, a.credentials = nil, nil
	for rows.Next() {
		var id []byte
		var p Passkey
		var credential webauthn.Credential
		if err := rows.Scan(&id, &p.Name, &credential, &p.Created, &p.LastUsed); err != nil {
			return err
		}
		p.ID = base64.RawURLEncoding.EncodeToString(id)
		a.Passkeys = append(a.Passkeys, p)
		a.credentials = append(a.credentials, credential)
	}
	return rows.Err()
}

// Account returns an account with its passkeys.
func (s *Service) Account(ctx context.Context, id string) (*Account, error) {
	a, err := scanAccount(s.db.QueryRow(ctx, "SELECT "+accountColumns+" FROM epd_account WHERE id = $1", id))
	if err != nil {
		return nil, err
	}
	return a, loadPasskeys(ctx, s.db, a)
}

func (s *Service) accountByName(ctx context.Context, q querier, username string) (*Account, error) {
	a, err := scanAccount(q.QueryRow(ctx, "SELECT "+accountColumns+" FROM epd_account WHERE username = $1", username))
	if err != nil {
		return nil, err
	}
	return a, loadPasskeys(ctx, q, a)
}

// Accounts lists all accounts, by name.
func (s *Service) Accounts(ctx context.Context) ([]*Account, error) {
	rows, err := s.db.Query(ctx, "SELECT "+accountColumns+" FROM epd_account ORDER BY username")
	if err != nil {
		return nil, err
	}
	accounts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Account, error) { return scanAccount(row) })
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if err := loadPasskeys(ctx, s.db, a); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

// AccountCount returns the number of accounts.
func (s *Service) AccountCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, "SELECT count(*) FROM epd_account").Scan(&n)
	return n, err
}

// insertAccount creates an account inside a transaction.
func insertAccount(ctx context.Context, q querier, username, displayName string, admin bool) (*Account, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	handle := make([]byte, 64)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}
	if displayName = strings.TrimSpace(displayName); displayName == "" {
		displayName = username
	}
	if len(displayName) > 200 {
		displayName = displayName[:200]
	}
	_, err = q.Exec(ctx, "INSERT INTO epd_account (id, username, display_name, admin, webauthn_id) VALUES ($1, $2, $3, $4, $5)",
		id, username, displayName, admin, handle)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrNameTaken
	}
	if err != nil {
		return nil, err
	}
	return scanAccount(q.QueryRow(ctx, "SELECT "+accountColumns+" FROM epd_account WHERE id = $1", id))
}

// SetLanguage keeps the language an account chose, "de", "en" or empty for the browser's.
func (s *Service) SetLanguage(ctx context.Context, accountID, language string) error {
	_, err := s.db.Exec(ctx, "UPDATE epd_account SET language = $2 WHERE id = $1", accountID, language)
	return err
}

// SetDisplayName changes the name shown for an account.
func (s *Service) SetDisplayName(ctx context.Context, accountID, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 200 {
		return ErrBadName
	}
	_, err := s.db.Exec(ctx, "UPDATE epd_account SET display_name = $2 WHERE id = $1", accountID, displayName)
	return err
}

// SetDisabled turns an account off or on. A disabled account cannot sign in, and its sessions end. The last active
// administrator cannot be disabled.
func (s *Service) SetDisabled(ctx context.Context, accountID string, disabled bool) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('eightperday admins'))"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE epd_account SET disabled = $2 WHERE id = $1", accountID, disabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := checkAdminLeft(ctx, tx); err != nil {
			return err
		}
		if disabled {
			_, err = tx.Exec(ctx, "DELETE FROM epd_session WHERE account_id = $1", accountID)
		}
		return err
	})
}

// SetAdmin grants or takes the administration. The last active administrator keeps it.
func (s *Service) SetAdmin(ctx context.Context, accountID string, admin bool) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('eightperday admins'))"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE epd_account SET admin = $2 WHERE id = $1", accountID, admin)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return checkAdminLeft(ctx, tx)
	})
}

func checkAdminLeft(ctx context.Context, q querier) error {
	var admins int
	if err := q.QueryRow(ctx, "SELECT count(*) FROM epd_account WHERE admin AND NOT disabled").Scan(&admins); err != nil {
		return err
	}
	if admins == 0 {
		return ErrLastAdmin
	}
	return nil
}

// GateAccount returns the account a trusted sign-in gate in front of the program names, and creates it at its first
// visit: the gate decides who may enter. Whether it is an administrator follows the gate on every visit.
func (s *Service) GateAccount(ctx context.Context, username, displayName string, admin bool) (*Account, error) {
	username, err := NormalizeName(username)
	if err != nil {
		return nil, err
	}
	var account *Account
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		account, err = s.accountByName(ctx, tx, username)
		if errors.Is(err, ErrNotFound) {
			account, err = insertAccount(ctx, tx, username, displayName, admin)
			return err
		}
		if err != nil {
			return err
		}
		if account.Admin != admin {
			if _, err := tx.Exec(ctx, "UPDATE epd_account SET admin = $2 WHERE id = $1", account.ID, admin); err != nil {
				return err
			}
			account.Admin = admin
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if account.Disabled {
		return nil, ErrInvalid
	}
	return account, nil
}

// passkeyCredentialID decodes the ID of a passkey as the pages name it.
func passkeyCredentialID(id string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) == 0 {
		return nil, ErrNotFound
	}
	return raw, nil
}

// protocolError tells a failed passkey step apart from a broken database in the log, and returns ErrCeremony.
func (s *Service) protocolError(step string, err error) error {
	var perr *protocol.Error
	if errors.As(err, &perr) {
		s.log.Info("passkey step failed", slog.String("step", step), slog.String("type", perr.Type), slog.String("detail", perr.DevInfo))
	} else {
		s.log.Info("passkey step failed", slog.String("step", step), slog.String("error", err.Error()))
	}
	return ErrCeremony
}
