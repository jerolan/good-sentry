package server

import (
	"strings"
	"testing"
	"time"
)

// RFC6238 vectors: the published eight-digit SHA-1 codes truncated to the six
// digits GoodSentry issues.
func TestTOTPCodeMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := totpCode(secret, totpStep(time.Unix(tc.unix, 0))); got != tc.want {
			t.Fatalf("code at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestMatchTOTPStepAcceptsOneStepEitherSide(t *testing.T) {
	secret := []byte("12345678901234567890")
	at := time.Unix(1111111111, 0)
	current := totpStep(at)
	for _, delta := range []int64{-1, 0, 1} {
		code := totpCode(secret, current+delta)
		step, ok := matchTOTPStep(secret, code, at)
		if !ok || step != current+delta {
			t.Fatalf("delta %d matched=%t step=%d", delta, ok, step)
		}
	}
	for _, delta := range []int64{-2, 2} {
		if _, ok := matchTOTPStep(secret, totpCode(secret, current+delta), at); ok {
			t.Fatalf("delta %d outside the window was accepted", delta)
		}
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", "28708a"} {
		if _, ok := matchTOTPStep(secret, code, at); ok {
			t.Fatalf("code %q was accepted", code)
		}
	}
}

func TestTOTPBoxSealsAndRejectsTampering(t *testing.T) {
	box, err := newTOTPBox(strings.Repeat("s", 40))
	if err != nil {
		t.Fatal(err)
	}
	raw, secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != totpSecretBytes || len(secret) != 32 {
		t.Fatalf("secret raw=%d encoded=%d", len(raw), len(secret))
	}
	decoded, err := decodeTOTPSecret(secret)
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("decode err=%v", err)
	}
	sealed, err := box.seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == string(raw) {
		t.Fatal("sealed secret contains the plaintext")
	}
	opened, err := box.open(sealed)
	if err != nil || string(opened) != string(raw) {
		t.Fatalf("open err=%v", err)
	}
	sealed[len(sealed)-1] ^= 0x01
	if _, err = box.open(sealed); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
	if _, err = box.open([]byte("short")); err == nil {
		t.Fatal("short ciphertext was accepted")
	}
	other, err := newTOTPBox(strings.Repeat("t", 40))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err = box.seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.open(sealed); err == nil {
		t.Fatal("another secret opened the ciphertext")
	}
	if _, err = newTOTPBox("too-short"); err == nil {
		t.Fatal("a short deployment secret was accepted")
	}
	var absent *totpBox
	if _, err = absent.seal(raw); err == nil {
		t.Fatal("a nil box sealed a secret")
	}
}

func TestRecoveryCodesAreDistinctSingleUseValues(t *testing.T) {
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("codes=%d", len(codes))
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if !validRecoveryCode(code) || len(code) != 10 {
			t.Fatalf("code %q is not a valid value", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q", code)
		}
		seen[code] = true
	}
	for _, code := range []string{"", "short", "0123456789a", "012345678Z", "ABCDEF1234"} {
		if validRecoveryCode(code) {
			t.Fatalf("code %q was accepted", code)
		}
	}
}

func TestTOTPURLCarriesEnrollmentParameters(t *testing.T) {
	url := totpURL("owner@example.test", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{
		"otpauth://totp/", "GoodSentry:owner@example.test", "secret=JBSWY3DPEHPK3PXP",
		"issuer=GoodSentry", "algorithm=SHA1", "digits=6", "period=30",
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("url %q is missing %q", url, want)
		}
	}
}

func TestChallengeTokenSpelling(t *testing.T) {
	if !validChallengeToken(strings.Repeat("0", 64)) {
		t.Fatal("a 64-digit token was rejected")
	}
	for _, value := range []string{strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("A", 64), strings.Repeat("g", 64), ""} {
		if validChallengeToken(value) {
			t.Fatalf("token %q was accepted", value)
		}
	}
}
