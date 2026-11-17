// SPDX-License-Identifier: MIT

package auth

import (
	"sync"
	"time"
)

// Throttle slows down guessing. Every failed sign-in counts against the account name and against the address it came
// from; after a few failures, each further one doubles the wait, up to an hour. A success clears the name, not the
// address. It lives in memory: the program is a single process, and a restart forgetting the counters is acceptable.
type Throttle struct {
	mu   sync.Mutex
	keys map[string]*bucket
	now  func() time.Time
}

type bucket struct {
	fails int
	last  time.Time
	until time.Time
}

const (
	// freeFailures for a name: a person mistyping is not slowed down at once.
	freeFailuresName = 5
	// freeFailuresAddress is higher: several people may share an address.
	freeFailuresAddress = 20
	firstWait           = 30 * time.Second
	maxWait             = time.Hour
	// forgetAfter clears a key that saw no failure for this long.
	forgetAfter = 24 * time.Hour
	// maxKeys bounds the memory: beyond it, the oldest half is forgotten.
	maxKeys = 100_000
)

func newThrottle(now func() time.Time) *Throttle {
	return &Throttle{keys: map[string]*bucket{}, now: now}
}

// wait returns how long the keys are still blocked; zero means go ahead.
func (t *Throttle) wait(keys ...string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var longest time.Duration
	for _, key := range keys {
		if b := t.keys[key]; b != nil && b.until.After(now) {
			longest = max(longest, b.until.Sub(now))
		}
	}
	return longest
}

// fail counts a failure against each key.
func (t *Throttle) fail(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if len(t.keys) >= maxKeys {
		t.forgetOldest(now)
	}
	for _, key := range keys {
		b := t.keys[key]
		if b == nil || now.Sub(b.last) > forgetAfter {
			b = &bucket{}
			t.keys[key] = b
		}
		b.fails++
		b.last = now
		free := freeFailuresName
		if len(key) > 3 && key[:3] == "ip:" {
			free = freeFailuresAddress
		}
		if over := b.fails - free; over >= 0 {
			wait := firstWait << min(over, 8)
			b.until = now.Add(min(wait, maxWait))
		}
	}
}

// succeed clears a key.
func (t *Throttle) succeed(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.keys, key)
}

func (t *Throttle) forgetOldest(now time.Time) {
	for key, b := range t.keys {
		if now.Sub(b.last) > time.Minute || len(t.keys) > maxKeys/2 {
			delete(t.keys, key)
		}
		if len(t.keys) <= maxKeys/2 {
			return
		}
	}
}

func nameKey(username string) string { return "name:" + username }
func addressKey(ip string) string    { return "ip:" + ip }
