package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goodsentry/internal/store"
)

const wizardTransferTTL = 10 * time.Minute

func (s *Server) registerWizardRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/0/wizard/{$}", s.compatibleWizardCreate)
	mux.HandleFunc("GET /api/0/wizard/{hash}/{$}", s.compatibleWizardRead)
	mux.HandleFunc("DELETE /api/0/wizard/{hash}/{$}", s.compatibleWizardDelete)
	mux.Handle("POST /api/0/wizard-set-token/{$}", s.compatibleAny([]string{"org:read", "org:write"}, s.compatibleWizardSetToken))
	mux.HandleFunc("GET /account/settings/wizard/{hash}/{$}", s.wizardPage)
}

func wizardDigest(secret string, value []byte) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte("GoodSentry wizard hash v1:"))
	_, _ = h.Write(value)
	return h.Sum(nil)
}

func makeWizardHash(secret string, now time.Time) (string, error) {
	raw := make([]byte, 16)
	binary.BigEndian.PutUint64(raw[:8], uint64(now.Unix()))
	if _, err := rand.Read(raw[8:]); err != nil {
		return "", err
	}
	digest := wizardDigest(secret, raw)
	return hex.EncodeToString(append(raw, digest[:16]...)), nil
}

func validWizardHash(secret, value string, now time.Time) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return false
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(decoded[:8])), 0)
	age := now.Sub(issued)
	if age < -time.Minute || age > wizardTransferTTL {
		return false
	}
	expected := wizardDigest(secret, decoded[:16])[:16]
	return subtle.ConstantTimeCompare(expected, decoded[16:]) == 1
}

func (s *Server) compatibleWizardCreate(w http.ResponseWriter, r *http.Request) {
	if _, valid := discoveryNoQuery(w, r); !valid {
		return
	}
	hash, err := makeWizardHash(s.cfg.Secret, time.Now())
	if err != nil {
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	writeJSON(w, 200, map[string]string{"hash": hash})
}

type wizardOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type wizardDSN struct {
	Public string `json:"public"`
	Secret string `json:"secret"`
}

type wizardKey struct {
	DSN wizardDSN `json:"dsn"`
}

type wizardProject struct {
	ID           string             `json:"id"`
	Slug         string             `json:"slug"`
	Organization wizardOrganization `json:"organization"`
	Keys         []wizardKey        `json:"keys"`
}

type wizardPayload struct {
	Projects []wizardProject `json:"projects"`
	APIKeys  struct {
		Token string `json:"token"`
	} `json:"apiKeys"`
}

func wizardHashQuery(w http.ResponseWriter, r *http.Request) bool {
	q, valid := credentialQuery(w, r)
	return valid && discoveryQueryKeys(w, q, "auth")
}

func (s *Server) compatibleWizardRead(w http.ResponseWriter, r *http.Request) {
	if !wizardHashQuery(w, r) {
		return
	}
	hash := r.PathValue("hash")
	if !validWizardHash(s.cfg.Secret, hash, time.Now()) {
		failure(w, 404, "Wizard transfer not found")
		return
	}
	record, err := s.db.WizardTransfer(r.Context(), s.hash(hash))
	if err != nil {
		report(w, err)
		return
	}
	if s.account == nil {
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	token, err := s.account.OpenWizardToken(hash, record.TokenCiphertext)
	if err != nil {
		slog.Error("Wizard token decryption failed", "error", err)
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	projects, err := s.db.WizardProjects(r.Context(), record.UserID, record.OrganizationID)
	if err != nil {
		report(w, err)
		return
	}
	payload := wizardPayload{Projects: []wizardProject{}}
	payload.APIKeys.Token = "gs_" + token
	indexes := map[string]int{}
	for _, project := range projects {
		index, exists := indexes[project.ID]
		if !exists {
			index = len(payload.Projects)
			indexes[project.ID] = index
			payload.Projects = append(payload.Projects, wizardProject{
				ID: project.ID, Slug: project.Slug,
				Organization: wizardOrganization{ID: project.OrganizationID, Name: project.OrganizationName, Slug: project.OrganizationSlug},
				Keys:         []wizardKey{},
			})
		}
		payload.Projects[index].Keys = append(payload.Projects[index].Keys, wizardKey{DSN: wizardDSN{
			Public: s.dsn(store.Project{ID: project.ID, PublicKey: project.PublicKey}),
		}})
	}
	writeJSON(w, 200, payload)
}

func (s *Server) compatibleWizardDelete(w http.ResponseWriter, r *http.Request) {
	if !wizardHashQuery(w, r) {
		return
	}
	hash := r.PathValue("hash")
	if !validWizardHash(s.cfg.Secret, hash, time.Now()) {
		failure(w, 404, "Wizard transfer not found")
		return
	}
	if err := s.db.DeleteWizardTransfer(r.Context(), s.hash(hash)); err != nil {
		report(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

func (s *Server) compatibleWizardSetToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Hash         string `json:"hash"`
		Organization string `json:"organization"`
	}
	if !bodyJSON(w, r, &input) {
		return
	}
	input.Hash = strings.TrimSpace(input.Hash)
	input.Organization = strings.TrimSpace(input.Organization)
	if !validWizardHash(s.cfg.Secret, input.Hash, time.Now()) {
		failure(w, 400, "Invalid or expired wizard hash")
		return
	}
	if s.account == nil {
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	userID := current(r).User.ID
	organization := input.Organization
	if grant, ok := r.Context().Value(apiGrantKey{}).(store.APITokenGrant); ok && grant.OrganizationID != "" {
		bound, err := s.db.ManagementOrganization(r.Context(), userID, grant.OrganizationID)
		if err != nil {
			report(w, err)
			return
		}
		if organization != "" && organization != bound.ID && organization != bound.Slug {
			failure(w, 404, "Not found")
			return
		}
		organization = bound.Slug
	} else if organization == "" {
		organizations, err := s.db.Organizations(r.Context(), userID)
		if err != nil {
			report(w, err)
			return
		}
		for _, candidate := range organizations {
			if candidate.Role == "owner" || candidate.Role == "admin" {
				organization = candidate.Slug
				break
			}
		}
	}
	if organization == "" {
		failure(w, 400, "Choose an organization you can manage")
		return
	}
	secret, err := token()
	if err != nil {
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	ciphertext, err := s.account.SealWizardToken(input.Hash, secret)
	if err != nil {
		failure(w, 503, "Wizard setup is unavailable")
		return
	}
	if err = s.db.CreateWizardTransfer(r.Context(), userID, current(r).User.AuthGeneration, organization,
		s.hash(input.Hash), s.hash("gs_"+secret), "gs_"+secret[:8], ciphertext, wizardTransferTTL); err != nil {
		if errors.Is(err, store.ErrConflict) {
			failure(w, 409, "This wizard hash already has a transfer")
		} else {
			report(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]string{"hash": input.Hash})
}

func (s *Server) wizardPage(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !validWizardHash(s.cfg.Secret, hash, time.Now()) {
		failure(w, 400, "Invalid or expired wizard link")
		return
	}
	query := url.Values{"view": {"wizard"}, "wizard": {hash}}
	if organization := r.URL.Query().Get("org_slug"); organization != "" {
		query.Set("org", organization)
	}
	http.Redirect(w, r, s.cfg.Prefix+"/?"+query.Encode(), http.StatusSeeOther)
}
