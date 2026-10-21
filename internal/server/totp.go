package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Two-factor authentication follows RFC6238 with the widely deployed
// HMAC-SHA1/six-digit/30-second parameters, so any TOTP client can be used.
const (
	totpPeriodSeconds = 30
	totpDigits        = 6
	totpSecretBytes   = 20
	totpWindow        = 1
	recoveryCodeCount = 10
	recoveryCodeBytes = 5
	maxSealedTOTP     = 128
	totpIssuer        = "GoodSentry"
)

var errInvalidTOTPSecret = errors.New("invalid two-factor secret")

// totpBox seals the per-account TOTP secret at rest with AES-256-GCM under a key
// derived from SECRET_KEY.
type totpBox struct{ aead cipher.AEAD }

func newTOTPBox(secret string) (*totpBox, error) {
	if len(secret) < 32 {
		return nil, errors.New("two-factor secret must contain at least 32 characters")
	}
	key := sha256.Sum256([]byte("GoodSentry two-factor v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &totpBox{aead: aead}, nil
}

func (b *totpBox) seal(plain []byte) ([]byte, error) {
	if b == nil || len(plain) == 0 || len(plain) > 64 {
		return nil, errInvalidTOTPSecret
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, []byte("GoodSentry two-factor v1")), nil
}

func (b *totpBox) open(sealed []byte) ([]byte, error) {
	if b == nil || len(sealed) < b.aead.NonceSize()+b.aead.Overhead() || len(sealed) > maxSealedTOTP {
		return nil, errInvalidTOTPSecret
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte("GoodSentry two-factor v1"))
	if err != nil || len(plain) == 0 || len(plain) > 64 {
		return nil, errInvalidTOTPSecret
	}
	return plain, nil
}

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns the raw key material and its base32 presentation.
func newTOTPSecret() ([]byte, string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	return raw, totpEncoding.EncodeToString(raw), nil
}

func decodeTOTPSecret(value string) ([]byte, error) {
	raw, err := totpEncoding.DecodeString(value)
	if err != nil || len(raw) != totpSecretBytes {
		return nil, errInvalidTOTPSecret
	}
	return raw, nil
}

func totpStep(at time.Time) int64 { return at.Unix() / totpPeriodSeconds }

func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

func validTOTPCode(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for index := 0; index < len(code); index++ {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return true
}

// matchTOTPStep returns the newest accepted step whose code matches. The caller
// refuses any step at or below the recorded one, which makes an observed code
// single-use.
func matchTOTPStep(secret []byte, code string, at time.Time) (int64, bool) {
	if !validTOTPCode(code) {
		return 0, false
	}
	current := totpStep(at)
	for delta := int64(totpWindow); delta >= -totpWindow; delta-- {
		step := current + delta
		if step < 0 {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// newRecoveryCodes returns single-use values that are shown once and stored only
// as hashes.
func newRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		codes = append(codes, hex.EncodeToString(raw))
	}
	return codes, nil
}

func validRecoveryCode(code string) bool {
	if len(code) != recoveryCodeBytes*2 {
		return false
	}
	for index := 0; index < len(code); index++ {
		if c := code[index]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func totpURL(account, secret string) string {
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", totpIssuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", totpDigits))
	params.Set("period", fmt.Sprintf("%d", totpPeriodSeconds))
	return "otpauth://totp/" + url.PathEscape(totpIssuer+":"+account) + "?" + params.Encode()
}
