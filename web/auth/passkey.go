// SPDX-License-Identifier: MIT

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

var randRead = rand.Read

func cleanPasskeyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

func insertPasskey(ctx context.Context, q querier, accountID, name string, credential *webauthn.Credential) error {
	_, err := q.Exec(ctx, "INSERT INTO epd_passkey (id, account_id, name, credential) VALUES ($1, $2, $3, $4)",
		credential.ID, accountID, cleanPasskeyName(name), credential)
	return err
}

// BeginPasskeyLogin returns the options for navigator.credentials.get. No name is asked for: the passkey says whose it
// is, so nothing tells a stranger which accounts exist.
func (s *Service) BeginPasskeyLogin() (*protocol.CredentialAssertion, string, error) {
	options, session, err := s.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	id, err := s.ceremonies.put(&ceremony{kind: kindLoginPasskey, session: session})
	if err != nil {
		return nil, "", err
	}
	return options, id, nil
}

// FinishPasskeyLogin verifies the answer of the authenticator and returns the account it belongs to.
func (s *Service) FinishPasskeyLogin(ctx context.Context, address, ceremonyID string, r *http.Request) (*Account, error) {
	if wait := s.throttle.wait(addressKey(address)); wait > 0 {
		return nil, &ThrottledError{Wait: wait}
	}
	pending := s.ceremonies.take(ceremonyID, kindLoginPasskey)
	if pending == nil {
		return nil, ErrCeremony
	}
	var found *Account
	lookup := func(rawID, userHandle []byte) (webauthn.User, error) {
		var accountID string
		err := s.db.QueryRow(ctx, "SELECT account_id FROM epd_passkey WHERE id = $1", rawID).Scan(&accountID)
		if err != nil {
			return nil, ErrInvalid
		}
		a, err := s.Account(ctx, accountID)
		if err != nil {
			return nil, ErrInvalid
		}
		// The handle the authenticator sends has to be the account's own.
		if !bytes.Equal(a.webauthnID, userHandle) {
			return nil, ErrInvalid
		}
		found = a
		return a, nil
	}
	credential, err := s.webauthn.FinishDiscoverableLogin(lookup, *pending.session, r)
	if err != nil || found == nil {
		s.throttle.fail(addressKey(address))
		if err == nil {
			err = ErrInvalid
		}
		_ = s.protocolError("login", err)
		return nil, ErrInvalid
	}
	if found.Disabled {
		s.throttle.fail(addressKey(address))
		return nil, ErrInvalid
	}
	// A counter that went backwards hints at a cloned authenticator; go-webauthn marks it, the sign-in is refused.
	if credential.Authenticator.CloneWarning {
		s.log.Warn("passkey counter went backwards, sign-in refused", "account", found.ID)
		return nil, ErrInvalid
	}
	if _, err := s.db.Exec(ctx, "UPDATE epd_passkey SET credential = $2, last_used = now() WHERE id = $1", credential.ID, credential); err != nil {
		return nil, err
	}
	return found, nil
}

// BeginAddPasskey returns the options to register one more passkey for a signed-in account. Its existing passkeys are
// excluded, so the same device is not registered twice.
func (s *Service) BeginAddPasskey(ctx context.Context, accountID string) (*protocol.CredentialCreation, string, error) {
	a, err := s.Account(ctx, accountID)
	if err != nil {
		return nil, "", err
	}
	options, session, err := s.webauthn.BeginRegistration(a, append(registrationOptions,
		webauthn.WithExclusions(webauthn.Credentials(a.credentials).CredentialDescriptors()))...)
	if err != nil {
		return nil, "", err
	}
	id, err := s.ceremonies.put(&ceremony{kind: kindAddPasskey, session: session, accountID: accountID})
	if err != nil {
		return nil, "", err
	}
	return options, id, nil
}

// FinishAddPasskey verifies and stores the new passkey.
func (s *Service) FinishAddPasskey(ctx context.Context, accountID, ceremonyID, name string, r *http.Request) error {
	pending := s.ceremonies.take(ceremonyID, kindAddPasskey)
	if pending == nil || pending.accountID != accountID {
		return ErrCeremony
	}
	a, err := s.Account(ctx, accountID)
	if err != nil {
		return err
	}
	credential, err := s.webauthn.FinishRegistration(a, *pending.session, r)
	if err != nil {
		return s.protocolError("add", err)
	}
	return insertPasskey(ctx, s.db, accountID, name, credential)
}

// RenamePasskey changes the name of a passkey of the account.
func (s *Service) RenamePasskey(ctx context.Context, accountID, passkeyID, name string) error {
	id, err := passkeyCredentialID(passkeyID)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, "UPDATE epd_passkey SET name = $3 WHERE id = $1 AND account_id = $2", id, accountID, cleanPasskeyName(name))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RemovePasskey removes a passkey of the account, unless it is the last way to sign in.
func (s *Service) RemovePasskey(ctx context.Context, accountID, passkeyID string) error {
	id, err := passkeyCredentialID(passkeyID)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockAccount(ctx, tx, accountID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM epd_passkey WHERE id = $1 AND account_id = $2", id, accountID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return checkMethodLeft(ctx, tx, accountID)
	})
}

// lockAccount serializes changes to the sign-in of one account, so two removals cannot each leave the other's method.
func lockAccount(ctx context.Context, q querier, accountID string) error {
	var id string
	err := q.QueryRow(ctx, "SELECT id FROM epd_account WHERE id = $1 FOR UPDATE", accountID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func checkMethodLeft(ctx context.Context, q querier, accountID string) error {
	var ok bool
	err := q.QueryRow(ctx, `SELECT password_hash IS NOT NULL OR EXISTS (SELECT 1 FROM epd_passkey WHERE account_id = $1)
		FROM epd_account WHERE id = $1`, accountID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLastMethod
	}
	return nil
}
