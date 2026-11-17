// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// The Argon2id parameters, after RFC 9106 section 4 with less memory: 64 MiB, three passes, two lanes. A hash takes
// about a tenth of a second on a small server. The parameters are part of every stored hash, so raising them later
// keeps old hashes valid; they are upgraded at the next sign-in.
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonSaltLen = 16
	argonKeyLen  = 32

	// MinPasswordLength follows NIST SP 800-63B: length instead of rules about character classes.
	MinPasswordLength = 12
	// maxPasswordBytes bounds what is hashed, so a huge form field costs nothing.
	maxPasswordBytes = 1024
)

var (
	// ErrPasswordTooShort is returned for a new password below MinPasswordLength characters.
	ErrPasswordTooShort = errors.New("the password is too short")
	// ErrPasswordTooLong is returned for a new password above 1024 bytes.
	ErrPasswordTooLong = errors.New("the password is too long")
	errMalformedHash   = errors.New("malformed password hash")
)

// dummyHash is checked against when an account does not exist, so the answer takes as long as for one that does.
var dummyHash = func() string {
	h, err := hashWith([]byte("no account has this password"), make([]byte, argonSaltLen))
	if err != nil {
		panic(err)
	}
	return h
}()

func checkNewPassword(password string) error {
	if len(password) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	return nil
}

// hashPassword returns the Argon2id hash of a password in the PHC string format.
func (s *Service) hashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	release, err := s.hashSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return hashWith([]byte(password), salt)
}

func hashWith(password, salt []byte) (string, error) {
	key := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword reports whether the password matches the hash, in constant time, and whether the hash uses older
// parameters than the current ones.
func (s *Service) verifyPassword(ctx context.Context, encoded, password string) (ok, outdated bool, err error) {
	if len(password) > maxPasswordBytes {
		password = password[:maxPasswordBytes]
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false, errMalformedHash
	}
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, false, errMalformedHash
	}
	// A stored hash is trusted data, but bounds keep a broken row from taking the server down.
	if memory == 0 || memory > 1024*1024 || passes == 0 || passes > 20 || threads == 0 {
		return false, false, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false, errMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, false, errMalformedHash
	}
	release, err := s.hashSlot(ctx)
	if err != nil {
		return false, false, err
	}
	defer release()
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	ok = subtle.ConstantTimeCompare(got, want) == 1
	outdated = memory != argonMemory || passes != argonTime || threads != argonThreads || len(want) != argonKeyLen
	return ok, outdated, nil
}

// hashSlot waits for one of the few slots in which a hash may run: each takes 64 MiB, and a burst of sign-ins must not
// take all the memory of the server.
func (s *Service) hashSlot(ctx context.Context) (func(), error) {
	select {
	case s.hashSlots <- struct{}{}:
		return func() { <-s.hashSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
