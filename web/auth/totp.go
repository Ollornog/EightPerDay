// SPDX-License-Identifier: MIT

package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod = 30

var totpOptions = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// TOTPEnrollment is what a person needs to add the account to an authenticator app.
type TOTPEnrollment struct {
	// Ceremony is the ID under which the server keeps the secret until a code confirms it.
	Ceremony string
	// Secret is the base32 secret, to type in by hand.
	Secret string
	// URL is the otpauth address the QR code carries.
	URL string
	// QR is the QR code as a PNG data URL.
	QR string
}

func newTOTPKey(username string) (*otp.Key, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "EightPerDay", AccountName: username,
		Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, "", err
	}
	image, err := key.Image(240, 240)
	if err != nil {
		return nil, "", err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, image); err != nil {
		return nil, "", err
	}
	return key, "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// totpStep returns the time step a code belongs to, allowing one step of clock drift either way, or 0 when the code
// is wrong.
func totpStep(secret, code string, now time.Time) int64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0
	}
	current := now.Unix() / totpPeriod
	for _, step := range []int64{current - 1, current, current + 1} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0), totpOptions)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step
		}
	}
	return 0
}

// useTOTP checks a code for an account and spends its time step, so the same code cannot sign in twice.
func (s *Service) useTOTP(ctx context.Context, q querier, accountID, code string) (bool, error) {
	var secret string
	if err := q.QueryRow(ctx, "SELECT coalesce(totp_secret, '') FROM epd_account WHERE id = $1", accountID).Scan(&secret); err != nil {
		return false, err
	}
	if secret == "" {
		return false, nil
	}
	step := totpStep(secret, code, s.cfg.Now())
	if step == 0 {
		return false, nil
	}
	tag, err := q.Exec(ctx, "UPDATE epd_account SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2", accountID, step)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
