// SPDX-License-Identifier: MIT

package auth

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ceremonyKind keeps a step of one flow from being finished by another.
type ceremonyKind int

const (
	kindSetupPasskey ceremonyKind = iota + 1
	kindSetupTOTP
	kindLoginPasskey
	kindAddPasskey
	kindEnableTOTP
	kindSecondFactor
)

// ceremony is what the server keeps between two steps of a flow: the challenge of a passkey, a TOTP secret that is not
// confirmed yet, or an account that gave its password and still owes the code. It never reaches the browser; the
// browser holds only its random ID.
type ceremony struct {
	kind      ceremonyKind
	session   *webauthn.SessionData
	setupLink string // the token of the one-time link
	username  string
	display   string
	accountID string
	secret    string // a TOTP secret
	totp      *TOTPEnrollment
	password  string // the hash of the chosen password, for a setup that still needs its TOTP code
	attempts  int
	expires   time.Time
}

const (
	ceremonyTTL       = 10 * time.Minute
	maxCeremonies     = 10_000
	maxSecondAttempts = 5
)

type ceremonies struct {
	mu  sync.Mutex
	m   map[string]*ceremony
	now func() time.Time
}

func newCeremonies(now func() time.Time) *ceremonies {
	return &ceremonies{m: map[string]*ceremony{}, now: now}
}

// put keeps a ceremony and returns its ID. When too many are open, the expired ones go first, then the oldest.
func (c *ceremonies) put(item *ceremony) (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	item.expires = now.Add(ceremonyTTL)
	if len(c.m) >= maxCeremonies {
		var oldestID string
		var oldest time.Time
		for key, value := range c.m {
			if value.expires.Before(now) {
				delete(c.m, key)
				continue
			}
			if oldestID == "" || value.expires.Before(oldest) {
				oldestID, oldest = key, value.expires
			}
		}
		if len(c.m) >= maxCeremonies {
			delete(c.m, oldestID)
		}
	}
	c.m[id] = item
	return id, nil
}

// take removes a ceremony of the given kind and returns it, or nil when there is none or it expired.
func (c *ceremonies) take(id string, kind ceremonyKind) *ceremony {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.m[id]
	if item == nil || item.kind != kind {
		return nil
	}
	delete(c.m, id)
	if item.expires.Before(c.now()) {
		return nil
	}
	return item
}

// peek returns a ceremony of the given kind without removing it, for a step that may be repeated.
func (c *ceremonies) peek(id string, kind ceremonyKind) *ceremony {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.m[id]
	if item == nil || item.kind != kind || item.expires.Before(c.now()) {
		return nil
	}
	return item
}

// failAttempt counts a wrong code against a ceremony and drops it after too many.
func (c *ceremonies) failAttempt(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item := c.m[id]; item != nil {
		item.attempts++
		if item.attempts >= maxSecondAttempts {
			delete(c.m, id)
		}
	}
}

func (c *ceremonies) drop(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, id)
}
