package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"goodsentry/internal/store"
)

// recoveryRoute accepts a session or a bearer credential. Missing credentials
// answer Unauthorized in the published shape.
func (s *Server) recoveryRoute(next http.HandlerFunc) http.Handler {
	bearer := s.compatibleAny(nil, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Authorization")) > 0 {
			bearer.ServeHTTP(w, r)
			return
		}
		if _, ok := s.compatibleAccount(w, r); !ok {
			return
		}
		next(w, r)
	})
}

// recoveryCodeHash binds a staged code to this instance secret.
func (s *Server) recoveryCodeHash(code string) string {
	return s.hash("compat recovery code v1:" + code)
}

func (s *Server) generateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	user := current(r).User
	codes := make([]string, 0, 10)
	seen := map[string]bool{}
	for len(codes) < 10 {
		value, err := rand.Int(rand.Reader, big.NewInt(100000000))
		if err != nil {
			report(w, err)
			return
		}
		code := fmt.Sprintf("%08d", value.Int64())
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	hashes := make([]string, len(codes))
	for i, code := range codes {
		hashes[i] = s.recoveryCodeHash(code)
	}
	if err := s.db.StageCompatRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string][]string{"codes": codes})
}

func (s *Server) consumeRecoveryCode(w http.ResponseWriter, r *http.Request) {
	user := current(r).User
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		failure(w, 400, "Cannot parse request body")
		return
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		validationFailure(w, []any{accountBodyIssue("", "missing", "Field required")})
		return
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		failure(w, 400, "Cannot parse request body")
		return
	}
	raw, present := fields["code"]
	if !present {
		validationFailure(w, []any{accountBodyIssue("code", "missing", "Field required")})
		return
	}
	var code any
	if err := json.Unmarshal(raw, &code); err != nil {
		failure(w, 400, "Cannot parse request body")
		return
	}
	text, ok := code.(string)
	if !ok {
		validationFailure(w, []any{accountBodyIssue("code", "string_type", "Input should be a valid string")})
		return
	}
	err = s.db.ConsumeCompatRecoveryCode(r.Context(), user.ID, s.recoveryCodeHash(text))
	switch {
	case err == nil:
		writeJSON(w, 204, nil)
	case errors.Is(err, store.ErrNoRecoveryCodes):
		failure(w, 400, "No recovery codes set, use GET first")
	case errors.Is(err, store.ErrInvalidRecoveryCode):
		failure(w, 400, "Invalid code")
	default:
		report(w, err)
	}
}
