package accountmail

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretBoxRoundTripAndIntegrity(t *testing.T) {
	box, err := NewSecretBox(strings.Repeat("s", 64))
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	sealed, err := box.Seal(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	got, err := box.Open(hash, sealed)
	if err != nil || got != token {
		t.Fatalf("round trip=%q err=%v", got, err)
	}
	if _, err := box.Open(strings.Repeat("b", 64), sealed); err == nil {
		t.Fatal("token ciphertext was accepted under another token hash")
	}
	for _, mutate := range []func([]byte){func(v []byte) { v[len(v)-1] ^= 1 }, func(v []byte) { v[0] ^= 1 }} {
		copy := bytes.Clone(sealed)
		mutate(copy)
		if _, err := box.Open(hash, copy); err == nil {
			t.Fatal("tampered ciphertext accepted")
		}
	}
	if _, err := box.Open(hash, sealed[:2]); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
	if _, err := NewSecretBox("short"); err == nil {
		t.Fatal("short secret accepted")
	}
}

func TestSecretBoxWizardPurposeIsolation(t *testing.T) {
	box, err := NewSecretBox(strings.Repeat("w", 64))
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("1", 64)
	sealed, err := box.SealWizardToken(hash, token)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := box.OpenWizardToken(hash, sealed); err != nil || opened != token {
		t.Fatalf("wizard round trip=%q err=%v", opened, err)
	}
	if _, err := box.OpenEmailVerification(hash, sealed); err == nil {
		t.Fatal("wizard ciphertext accepted as email verification")
	}
}
