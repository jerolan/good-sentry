package accountmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const (
	tokenBytes = 32
	maxSealed  = 256
)

type SecretBox struct{ aead cipher.AEAD }

func NewSecretBox(secret string) (*SecretBox, error) {
	if len(secret) < 32 {
		return nil, errors.New("account mail secret must contain at least 32 characters")
	}
	key := sha256.Sum256([]byte("GoodSentry account mail v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validTokenHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func validToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *SecretBox) Seal(tokenHash, token string) ([]byte, error) {
	if s == nil || !validTokenHash(tokenHash) || !validToken(token) {
		return nil, errors.New("invalid reset token")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(token), []byte("GoodSentry password reset v1:"+tokenHash)), nil
}

func (s *SecretBox) Open(tokenHash string, sealed []byte) (string, error) {
	if s == nil || !validTokenHash(tokenHash) || len(sealed) < s.aead.NonceSize()+s.aead.Overhead() || len(sealed) > maxSealed {
		return "", errors.New("invalid reset token")
	}
	nonce, ciphertext := sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ciphertext, []byte("GoodSentry password reset v1:"+tokenHash))
	if err != nil || !validToken(string(plain)) {
		return "", errors.New("invalid reset token")
	}
	return string(plain), nil
}

func (s *SecretBox) SealEmailVerification(tokenHash, token string) ([]byte, error) {
	return s.sealWithPurpose("GoodSentry email verification v1:", tokenHash, token)
}

func (s *SecretBox) OpenEmailVerification(tokenHash string, sealed []byte) (string, error) {
	return s.openWithPurpose("GoodSentry email verification v1:", tokenHash, sealed)
}

func (s *SecretBox) sealWithPurpose(purpose, tokenHash, token string) ([]byte, error) {
	if s == nil || !validTokenHash(tokenHash) || !validToken(token) {
		return nil, errors.New("invalid account token")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(token), []byte(purpose+tokenHash)), nil
}

func (s *SecretBox) openWithPurpose(purpose, tokenHash string, sealed []byte) (string, error) {
	if s == nil || !validTokenHash(tokenHash) || len(sealed) < s.aead.NonceSize()+s.aead.Overhead() || len(sealed) > maxSealed {
		return "", errors.New("invalid account token")
	}
	nonce, ciphertext := sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ciphertext, []byte(purpose+tokenHash))
	if err != nil || !validToken(string(plain)) {
		return "", errors.New("invalid account token")
	}
	return string(plain), nil
}
