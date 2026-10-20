package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserSerializationOmitsCredentialInternals(t *testing.T) {
	raw, err := json.Marshal(User{ID: "7", Email: "owner@example.test", Name: "Owner", PasswordHash: []byte("secret-hash"), AuthGeneration: 9})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["id"] != "7" || fields["email"] != "owner@example.test" || fields["name"] != "Owner" {
		t.Fatalf("public user fields changed: %s", raw)
	}
	for _, internal := range []string{"passwordHash", "password_hash", "authGeneration", "auth_generation", "secret-hash"} {
		if strings.Contains(string(raw), internal) {
			t.Fatalf("credential internals leaked in user JSON: %s", raw)
		}
	}
}
