// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

const (
	// FirstLinkTTL is how long the link for the first administrator lasts. Every start without an account prints a
	// new one.
	FirstLinkTTL = 24 * time.Hour
	// InviteTTL is how long an invitation or a reset link lasts.
	InviteTTL = 7 * 24 * time.Hour
)

// SetupLink is a one-time link to create an account or to replace the sign-in of one.
type SetupLink struct {
	// Username is the fixed name of the new account, or empty when the person chooses it.
	Username string
	Admin    bool
	// AccountID is set for a reset: the account whose sign-in is replaced.
	AccountID string
	Expires   time.Time
}

// Reset reports whether the link replaces the sign-in of an existing account.
func (l SetupLink) Reset() bool { return l.AccountID != "" }

// NewSetupLink stores a one-time link and returns its token. Only the hash of the token is stored.
func (s *Service) NewSetupLink(ctx context.Context, link SetupLink, ttl time.Duration) (string, error) {
	if link.Username != "" {
		name, err := NormalizeName(link.Username)
		if err != nil {
			return "", err
		}
		link.Username = name
		var taken bool
		if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM epd_account WHERE username = $1)", name).Scan(&taken); err != nil {
			return "", err
		}
		if taken && link.AccountID == "" {
			return "", ErrNameTaken
		}
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	var account *string
	if link.AccountID != "" {
		account = &link.AccountID
	}
	_, err = s.db.Exec(ctx, "INSERT INTO epd_setup_link (token_hash, account_id, username, admin, expires) VALUES ($1, $2, $3, $4, $5)",
		tokenHash(token), account, link.Username, link.Admin, s.cfg.Now().Add(ttl))
	return token, err
}

// FirstSetupLink returns a new link for the first administrator while there is no account, and "" once there is one.
func (s *Service) FirstSetupLink(ctx context.Context) (string, error) {
	n, err := s.AccountCount(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	if _, err := s.db.Exec(ctx, "DELETE FROM epd_setup_link WHERE expires < $1", s.cfg.Now()); err != nil {
		return "", err
	}
	return s.NewSetupLink(ctx, SetupLink{Admin: true}, FirstLinkTTL)
}

// LookupSetupLink returns a valid link.
func (s *Service) LookupSetupLink(ctx context.Context, token string) (SetupLink, error) {
	return lookupLink(ctx, s.db, token, s.cfg.Now(), false)
}

func lookupLink(ctx context.Context, q querier, token string, now time.Time, consume bool) (SetupLink, error) {
	var link SetupLink
	var account *string
	sql := "SELECT account_id, username, admin, expires FROM epd_setup_link WHERE token_hash = $1"
	if consume {
		sql = "DELETE FROM epd_setup_link WHERE token_hash = $1 RETURNING account_id, username, admin, expires"
	}
	err := q.QueryRow(ctx, sql, tokenHash(token)).Scan(&account, &link.Username, &link.Admin, &link.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupLink{}, ErrLinkInvalid
	}
	if err != nil {
		return SetupLink{}, err
	}
	if account != nil {
		link.AccountID = *account
	}
	if !link.Expires.After(now) {
		return SetupLink{}, ErrLinkInvalid
	}
	return link, nil
}

// SetupChoice is what a person fills in on the page of a one-time link.
type SetupChoice struct {
	Username    string
	DisplayName string
}

// resolveName decides the name of the account a link creates: the link's own, or the chosen one.
func (s *Service) resolveName(link SetupLink, choice SetupChoice) (string, error) {
	if link.Username != "" {
		return link.Username, nil
	}
	return NormalizeName(choice.Username)
}

// finishSetup consumes the link and creates the account, or for a reset clears the old sign-in of the account and ends
// its sessions. apply then stores the new sign-in in the same transaction.
func (s *Service) finishSetup(ctx context.Context, token string, choice SetupChoice, apply func(tx pgx.Tx, a *Account) error) (*Account, error) {
	var account *Account
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		link, err := lookupLink(ctx, tx, token, s.cfg.Now(), true)
		if err != nil {
			return err
		}
		if link.Reset() {
			account, err = scanAccount(tx.QueryRow(ctx, "SELECT "+accountColumns+" FROM epd_account WHERE id = $1", link.AccountID))
			if err != nil {
				return err
			}
			for _, sql := range []string{
				"UPDATE epd_account SET password_hash = NULL, totp_secret = NULL, totp_last_step = 0 WHERE id = $1",
				"DELETE FROM epd_passkey WHERE account_id = $1",
				"DELETE FROM epd_session WHERE account_id = $1",
			} {
				if _, err := tx.Exec(ctx, sql, account.ID); err != nil {
					return err
				}
			}
		} else {
			name, err := s.resolveName(link, choice)
			if err != nil {
				return err
			}
			if account, err = insertAccount(ctx, tx, name, choice.DisplayName, link.Admin); err != nil {
				return err
			}
		}
		if err := apply(tx, account); err != nil {
			return err
		}
		return loadPasskeys(ctx, tx, account)
	})
	if err != nil {
		return nil, err
	}
	return s.Account(ctx, account.ID)
}

// SetupWithPassword redeems a link with a password and no second factor.
func (s *Service) SetupWithPassword(ctx context.Context, token string, choice SetupChoice, password string) (*Account, error) {
	if err := checkNewPassword(password); err != nil {
		return nil, err
	}
	if _, err := s.LookupSetupLink(ctx, token); err != nil {
		return nil, err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return nil, err
	}
	return s.finishSetup(ctx, token, choice, func(tx pgx.Tx, a *Account) error {
		_, err := tx.Exec(ctx, "UPDATE epd_account SET password_hash = $2 WHERE id = $1", a.ID, hash)
		return err
	})
}

// BeginSetupWithTOTP checks the link and the password and returns a new TOTP secret. The account is created only
// when FinishSetupWithTOTP gets a code from it.
func (s *Service) BeginSetupWithTOTP(ctx context.Context, token string, choice SetupChoice, password string) (*TOTPEnrollment, error) {
	if err := checkNewPassword(password); err != nil {
		return nil, err
	}
	link, err := s.LookupSetupLink(ctx, token)
	if err != nil {
		return nil, err
	}
	name, err := s.accountNameFor(ctx, link, choice)
	if err != nil {
		return nil, err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return nil, err
	}
	key, qr, err := newTOTPKey(name)
	if err != nil {
		return nil, err
	}
	enrollment := TOTPEnrollment{Secret: key.Secret(), URL: key.URL(), QR: qr}
	id, err := s.ceremonies.put(&ceremony{kind: kindSetupTOTP, setupLink: token, username: choice.Username, display: choice.DisplayName,
		secret: key.Secret(), password: hash, totp: &TOTPEnrollment{Secret: enrollment.Secret, URL: enrollment.URL, QR: enrollment.QR}})
	if err != nil {
		return nil, err
	}
	enrollment.Ceremony = id
	return &enrollment, nil
}

// accountNameFor returns the name an account made from the link will have, checking that it is free.
func (s *Service) accountNameFor(ctx context.Context, link SetupLink, choice SetupChoice) (string, error) {
	if link.Reset() {
		a, err := s.Account(ctx, link.AccountID)
		if err != nil {
			return "", err
		}
		return a.Username, nil
	}
	name, err := s.resolveName(link, choice)
	if err != nil {
		return "", err
	}
	var taken bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM epd_account WHERE username = $1)", name).Scan(&taken); err != nil {
		return "", err
	}
	if taken {
		return "", ErrNameTaken
	}
	return name, nil
}

// PendingSetupTOTP returns the enrollment of a setup that still waits for its code, so the page can show the QR code
// again after a wrong code; nil when there is none.
func (s *Service) PendingSetupTOTP(ceremonyID string) *TOTPEnrollment {
	if pending := s.ceremonies.peek(ceremonyID, kindSetupTOTP); pending != nil {
		copied := *pending.totp
		copied.Ceremony = ceremonyID
		return &copied
	}
	return nil
}

// FinishSetupWithTOTP creates the account once the code from the new secret is right. A wrong code may be repeated a
// few times.
func (s *Service) FinishSetupWithTOTP(ctx context.Context, ceremonyID, code string) (*Account, error) {
	pending := s.ceremonies.peek(ceremonyID, kindSetupTOTP)
	if pending == nil {
		return nil, ErrCeremony
	}
	step := totpStep(pending.secret, code, s.cfg.Now())
	if step == 0 {
		s.ceremonies.failAttempt(ceremonyID)
		return nil, ErrInvalid
	}
	s.ceremonies.drop(ceremonyID)
	return s.finishSetup(ctx, pending.setupLink, SetupChoice{Username: pending.username, DisplayName: pending.display}, func(tx pgx.Tx, a *Account) error {
		_, err := tx.Exec(ctx, "UPDATE epd_account SET password_hash = $2, totp_secret = $3, totp_last_step = $4 WHERE id = $1",
			a.ID, pending.password, pending.secret, step)
		return err
	})
}

// registrationOptions asks for a passkey that the device keeps and that verifies the person (a PIN or biometrics).
var registrationOptions = []webauthn.RegistrationOption{
	webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: protocol.ResidentKeyRequired(),
		UserVerification:   protocol.VerificationRequired,
	}),
}

// BeginSetupWithPasskey checks the link and returns the options for navigator.credentials.create. The user handle is
// made here and kept with the ceremony, so the account exists only when the passkey does.
func (s *Service) BeginSetupWithPasskey(ctx context.Context, token string, choice SetupChoice) (*protocol.CredentialCreation, string, error) {
	link, err := s.LookupSetupLink(ctx, token)
	if err != nil {
		return nil, "", err
	}
	name, err := s.accountNameFor(ctx, link, choice)
	if err != nil {
		return nil, "", err
	}
	user := &Account{Username: name, DisplayName: choice.DisplayName, webauthnID: make([]byte, 64)}
	if link.Reset() {
		existing, err := s.Account(ctx, link.AccountID)
		if err != nil {
			return nil, "", err
		}
		user = existing
		user.credentials = nil
	} else if _, err := randRead(user.webauthnID); err != nil {
		return nil, "", err
	}
	if user.DisplayName == "" {
		user.DisplayName = name
	}
	options, session, err := s.webauthn.BeginRegistration(user, registrationOptions...)
	if err != nil {
		return nil, "", err
	}
	id, err := s.ceremonies.put(&ceremony{kind: kindSetupPasskey, session: session, setupLink: token, username: choice.Username, display: choice.DisplayName})
	if err != nil {
		return nil, "", err
	}
	return options, id, nil
}

// FinishSetupWithPasskey verifies the new passkey and creates the account with it.
func (s *Service) FinishSetupWithPasskey(ctx context.Context, ceremonyID, passkeyName string, r *http.Request) (*Account, error) {
	pending := s.ceremonies.take(ceremonyID, kindSetupPasskey)
	if pending == nil {
		return nil, ErrCeremony
	}
	// The handle the browser registered is the one kept in the session data.
	user := &Account{Username: pending.username, DisplayName: pending.display, webauthnID: pending.session.UserID}
	credential, err := s.webauthn.FinishRegistration(user, *pending.session, r)
	if err != nil {
		return nil, s.protocolError("setup", err)
	}
	return s.finishSetup(ctx, pending.setupLink, SetupChoice{Username: pending.username, DisplayName: pending.display}, func(tx pgx.Tx, a *Account) error {
		if _, err := tx.Exec(ctx, "UPDATE epd_account SET webauthn_id = $2 WHERE id = $1", a.ID, pending.session.UserID); err != nil {
			return err
		}
		return insertPasskey(ctx, tx, a.ID, passkeyName, credential)
	})
}
