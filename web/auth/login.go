// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// PasswordResult is the outcome of a correct password: either the account is signed in, or it still owes a code from
// its authenticator app, under SecondFactor.
type PasswordResult struct {
	Account      *Account
	SecondFactor string
}

// PasswordLogin checks a name and a password. Every failure looks the same and takes as long, whether the name exists
// or not, and counts against the name and the address.
func (s *Service) PasswordLogin(ctx context.Context, address, username, password string) (PasswordResult, error) {
	name, nameErr := NormalizeName(username)
	if nameErr != nil {
		name = "?" // still throttled and still hashed, so a bad name costs as much as a wrong password
	}
	keys := []string{nameKey(name), addressKey(address)}
	if wait := s.throttle.wait(keys...); wait > 0 {
		return PasswordResult{}, &ThrottledError{Wait: wait}
	}
	var hash *string
	var account *Account
	if nameErr == nil {
		a, err := s.accountByName(ctx, s.db, name)
		switch {
		case err == nil:
			account = a
			if err := s.db.QueryRow(ctx, "SELECT password_hash FROM epd_account WHERE id = $1", a.ID).Scan(&hash); err != nil {
				return PasswordResult{}, err
			}
		case !errors.Is(err, ErrNotFound):
			return PasswordResult{}, err
		}
	}
	encoded := dummyHash
	if hash != nil {
		encoded = *hash
	}
	ok, outdated, err := s.verifyPassword(ctx, encoded, password)
	if err != nil {
		return PasswordResult{}, err
	}
	if !ok || hash == nil || account == nil || account.Disabled {
		s.throttle.fail(keys...)
		return PasswordResult{}, ErrInvalid
	}
	if outdated {
		if fresh, err := s.hashPassword(ctx, password); err == nil {
			_, _ = s.db.Exec(ctx, "UPDATE epd_account SET password_hash = $2 WHERE id = $1", account.ID, fresh)
		}
	}
	if account.HasTOTP {
		id, err := s.ceremonies.put(&ceremony{kind: kindSecondFactor, accountID: account.ID, username: name})
		if err != nil {
			return PasswordResult{}, err
		}
		return PasswordResult{SecondFactor: id}, nil
	}
	s.throttle.succeed(nameKey(name))
	return PasswordResult{Account: account}, nil
}

// SecondFactorLogin checks the code from the authenticator app after a correct password. A few wrong codes end the
// attempt, and every wrong one counts against the name and the address like a wrong password.
func (s *Service) SecondFactorLogin(ctx context.Context, address, ceremonyID, code string) (*Account, error) {
	pending := s.ceremonies.peek(ceremonyID, kindSecondFactor)
	if pending == nil {
		return nil, ErrCeremony
	}
	keys := []string{nameKey(pending.username), addressKey(address)}
	if wait := s.throttle.wait(keys...); wait > 0 {
		return nil, &ThrottledError{Wait: wait}
	}
	ok, err := s.useTOTP(ctx, s.db, pending.accountID, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.ceremonies.failAttempt(ceremonyID)
		s.throttle.fail(keys...)
		return nil, ErrInvalid
	}
	s.ceremonies.drop(ceremonyID)
	a, err := s.Account(ctx, pending.accountID)
	if err != nil {
		return nil, err
	}
	if a.Disabled {
		return nil, ErrInvalid
	}
	s.throttle.succeed(nameKey(pending.username))
	return a, nil
}

// SecondFactorPending reports whether a ceremony for the code still waits.
func (s *Service) SecondFactorPending(ceremonyID string) bool {
	return s.ceremonies.peek(ceremonyID, kindSecondFactor) != nil
}

// NewSession starts a session and returns its token.
func (s *Service) NewSession(ctx context.Context, accountID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := s.cfg.Now()
	_, err = s.db.Exec(ctx, "INSERT INTO epd_session (token_hash, account_id, created, last_seen, expires) VALUES ($1, $2, $3, $3, $4)",
		tokenHash(token), accountID, now, now.Add(s.cfg.SessionTTL))
	return token, err
}

// maxSessionAge ends a session even when it is used every day.
const maxSessionAge = 365 * 24 * time.Hour

// SessionAccount returns the account of a session. Use extends it, at most once a minute to spare the database.
func (s *Service) SessionAccount(ctx context.Context, token string) (*Account, error) {
	if token == "" || len(token) > 100 {
		return nil, ErrInvalid
	}
	now := s.cfg.Now()
	var accountID string
	var created, lastSeen time.Time
	err := s.db.QueryRow(ctx, `SELECT s.account_id, s.created, s.last_seen FROM epd_session s JOIN epd_account a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires > $2 AND NOT a.disabled`, tokenHash(token), now).Scan(&accountID, &created, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	if now.Sub(lastSeen) > time.Minute {
		expires := min(now.Add(s.cfg.SessionTTL).UnixNano(), created.Add(maxSessionAge).UnixNano())
		if _, err := s.db.Exec(ctx, "UPDATE epd_session SET last_seen = $2, expires = $3 WHERE token_hash = $1",
			tokenHash(token), now, time.Unix(0, expires)); err != nil {
			return nil, err
		}
	}
	return s.Account(ctx, accountID)
}

// EndSession signs a session out.
func (s *Service) EndSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM epd_session WHERE token_hash = $1", tokenHash(token))
	return err
}

// EndOtherSessions signs out every session of the account but the given one.
func (s *Service) EndOtherSessions(ctx context.Context, accountID, keepToken string) (int64, error) {
	tag, err := s.db.Exec(ctx, "DELETE FROM epd_session WHERE account_id = $1 AND token_hash <> $2", accountID, tokenHash(keepToken))
	return tag.RowsAffected(), err
}

// CleanUp removes expired sessions and links.
func (s *Service) CleanUp(ctx context.Context) error {
	now := s.cfg.Now()
	if _, err := s.db.Exec(ctx, "DELETE FROM epd_session WHERE expires < $1", now); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, "DELETE FROM epd_setup_link WHERE expires < $1", now)
	return err
}

// SetPassword sets or changes the password of a signed-in account. Changing one needs the current password; setting
// the first one does not, the account is signed in with a passkey.
func (s *Service) SetPassword(ctx context.Context, address, accountID, current, password string) error {
	if err := checkNewPassword(password); err != nil {
		return err
	}
	var old *string
	if err := s.db.QueryRow(ctx, "SELECT password_hash FROM epd_account WHERE id = $1", accountID).Scan(&old); err != nil {
		return err
	}
	if old != nil {
		keys := []string{nameKey("id:" + accountID), addressKey(address)}
		if wait := s.throttle.wait(keys...); wait > 0 {
			return &ThrottledError{Wait: wait}
		}
		ok, _, err := s.verifyPassword(ctx, *old, current)
		if err != nil {
			return err
		}
		if !ok {
			s.throttle.fail(keys...)
			return ErrInvalid
		}
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, "UPDATE epd_account SET password_hash = $2 WHERE id = $1", accountID, hash)
	return err
}

// RemovePassword removes the password, and with it the authenticator app, when the account has a passkey.
func (s *Service) RemovePassword(ctx context.Context, accountID string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockAccount(ctx, tx, accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE epd_account SET password_hash = NULL, totp_secret = NULL WHERE id = $1", accountID); err != nil {
			return err
		}
		return checkMethodLeft(ctx, tx, accountID)
	})
}

// BeginEnableTOTP returns a new secret for an account that has a password.
func (s *Service) BeginEnableTOTP(ctx context.Context, accountID string) (*TOTPEnrollment, error) {
	a, err := s.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !a.HasPassword {
		return nil, ErrLastMethod
	}
	key, qr, err := newTOTPKey(a.Username)
	if err != nil {
		return nil, err
	}
	enrollment := TOTPEnrollment{Secret: key.Secret(), URL: key.URL(), QR: qr}
	id, err := s.ceremonies.put(&ceremony{kind: kindEnableTOTP, accountID: accountID, secret: key.Secret(), totp: &TOTPEnrollment{Secret: enrollment.Secret, URL: enrollment.URL, QR: enrollment.QR}})
	if err != nil {
		return nil, err
	}
	enrollment.Ceremony = id
	return &enrollment, nil
}

// PendingEnableTOTP returns the enrollment of the account that still waits for its code, or nil.
func (s *Service) PendingEnableTOTP(accountID, ceremonyID string) *TOTPEnrollment {
	if pending := s.ceremonies.peek(ceremonyID, kindEnableTOTP); pending != nil && pending.accountID == accountID {
		copied := *pending.totp
		copied.Ceremony = ceremonyID
		return &copied
	}
	return nil
}

// FinishEnableTOTP stores the secret once a code from it is right.
func (s *Service) FinishEnableTOTP(ctx context.Context, accountID, ceremonyID, code string) error {
	pending := s.ceremonies.peek(ceremonyID, kindEnableTOTP)
	if pending == nil || pending.accountID != accountID {
		return ErrCeremony
	}
	step := totpStep(pending.secret, code, s.cfg.Now())
	if step == 0 {
		s.ceremonies.failAttempt(ceremonyID)
		return ErrInvalid
	}
	s.ceremonies.drop(ceremonyID)
	_, err := s.db.Exec(ctx, "UPDATE epd_account SET totp_secret = $2, totp_last_step = $3 WHERE id = $1", accountID, pending.secret, step)
	return err
}

// DisableTOTP removes the authenticator app; a current code proves the person still has it.
func (s *Service) DisableTOTP(ctx context.Context, address, accountID, code string) error {
	keys := []string{nameKey("id:" + accountID), addressKey(address)}
	if wait := s.throttle.wait(keys...); wait > 0 {
		return &ThrottledError{Wait: wait}
	}
	ok, err := s.useTOTP(ctx, s.db, accountID, code)
	if err != nil {
		return err
	}
	if !ok {
		s.throttle.fail(keys...)
		return ErrInvalid
	}
	_, err = s.db.Exec(ctx, "UPDATE epd_account SET totp_secret = NULL WHERE id = $1", accountID)
	return err
}
