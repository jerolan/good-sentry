package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// secretSealer encrypts small stored secrets with AES-256-GCM under a key and
// associated data derived from SECRET_KEY and a fixed purpose.
type secretSealer struct {
	aead    cipher.AEAD
	purpose string
	max     int
}

func newSecretSealer(secret, purpose string, max int) (*secretSealer, error) {
	if len(secret) < 32 || purpose == "" || max <= 0 {
		return nil, errors.New("invalid secret sealer configuration")
	}
	key := sha256.Sum256([]byte(purpose + "\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretSealer{aead: aead, purpose: purpose, max: max}, nil
}

func (b *secretSealer) seal(plain []byte) ([]byte, error) {
	if b == nil || len(plain) == 0 || len(plain) > b.max {
		return nil, errors.New("invalid secret")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, []byte(b.purpose)), nil
}

func (b *secretSealer) open(sealed []byte) ([]byte, error) {
	if b == nil || len(sealed) < b.aead.NonceSize()+b.aead.Overhead() || len(sealed) > b.max+b.aead.NonceSize()+b.aead.Overhead() {
		return nil, errors.New("invalid sealed secret")
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte(b.purpose))
	if err != nil || len(plain) == 0 || len(plain) > b.max {
		return nil, errors.New("invalid sealed secret")
	}
	return plain, nil
}
