package server

import (
	"net/http"
	"time"

	"goodsentry/internal/store"
)

type compatibleIssueCommit struct {
	ID          string    `json:"id"`
	Message     string    `json:"message"`
	DateCreated time.Time `json:"dateCreated"`
	AuthorName  string    `json:"authorName"`
	AuthorEmail string    `json:"authorEmail"`
}

func projectCompatibleIssueCommit(commit store.ReleaseCommit) compatibleIssueCommit {
	return compatibleIssueCommit{
		ID: commit.ID, Message: commit.Message, DateCreated: commit.DateCreated.UTC(),
		AuthorName: commit.AuthorName, AuthorEmail: commit.AuthorEmail,
	}
}

func (s *Server) compatibleIssueCommits(w http.ResponseWriter, r *http.Request) {
	access, ok := s.compatibleEventAccess(w, r)
	if !ok {
		return
	}
	if !issueTargetID(r.PathValue("issue")) {
		failure(w, 400, "Issue IDs must be canonical positive decimal int64 strings")
		return
	}
	query, ok := credentialQuery(w, r)
	if !ok {
		return
	}
	if len(query) != 0 {
		failure(w, 400, "Issue commit reads do not accept query parameters")
		return
	}
	commits, err := s.db.CompatibleIssueCommits(r.Context(), access, r.PathValue("issue"))
	if err != nil {
		reportCompatibleIssue(w, err)
		return
	}
	result := make([]compatibleIssueCommit, 0, len(commits))
	for _, commit := range commits {
		result = append(result, projectCompatibleIssueCommit(commit))
	}
	writeJSON(w, 200, result)
}
