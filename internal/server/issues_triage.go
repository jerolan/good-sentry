package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"goodsentry/internal/store"
)

// Parse only the native triage object's exact keys, retaining null/omission
// and rejecting invalid UTF-8 before JSON can replace its bytes.
func issueObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, errors.New("Invalid JSON request")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("Expected a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("Invalid JSON field")
		}
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return nil, errors.New("Unknown field: " + key)
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("Repeated field: " + key)
		}
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err = dec.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

func issueBody(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]json.RawMessage, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		failure(w, 400, "Invalid JSON request")
		return nil, false
	}
	fields, err := issueObject(raw, allowed...)
	if err != nil {
		failure(w, 400, err.Error())
		return nil, false
	}
	return fields, true
}

func (s *Server) triage(w http.ResponseWriter, r *http.Request) {
	fields, ok := issueBody(w, r, "status", "assignedTo", "ignore", "resolution")
	if !ok {
		return
	}
	if len(fields) == 0 {
		failure(w, 400, "Provide status, assignedTo, ignore or resolution")
		return
	}
	var change store.IssueUpdate
	if raw, present := fields["status"]; present {
		if err := json.Unmarshal(raw, &change.Status); err != nil || change.Status != "unresolved" && change.Status != "resolved" && change.Status != "ignored" {
			failure(w, 400, "Status must be unresolved, resolved or ignored")
			return
		}
	}
	if raw, present := fields["ignore"]; present {
		if change.Status != "ignored" {
			failure(w, 400, "Ignore requires explicit ignored status")
			return
		}
		change.Ignore = &store.IssueIgnoreChange{}
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			condition, err := issueObject(raw, "minutes", "count", "userCount", "window", "userWindow")
			if err != nil || len(condition) == 0 {
				failure(w, 400, "Ignore must contain minutes, count, userCount, window or userWindow")
				return
			}
			change.Ignore.Condition = &store.IssueIgnoreCondition{}
			for name, value := range condition {
				number, err := strconv.ParseInt(string(bytes.TrimSpace(value)), 10, 32)
				if err != nil || number <= 0 {
					failure(w, 400, "Ignore conditions must be positive 32-bit JSON integers")
					return
				}
				n := int32(number)
				switch name {
				case "minutes":
					change.Ignore.Condition.Minutes = &n
				case "count":
					change.Ignore.Condition.Count = &n
				case "userCount":
					change.Ignore.Condition.UserCount = &n
				case "window":
					if n > 10080 {
						failure(w, 400, "Ignore window must be between 1 and 10080 minutes")
						return
					}
					change.Ignore.Condition.Window = &n
				case "userWindow":
					if n > 10080 {
						failure(w, 400, "Ignore user window must be between 1 and 10080 minutes")
						return
					}
					change.Ignore.Condition.UserWindow = &n
				}
			}
			if change.Ignore.Condition.Window != nil && change.Ignore.Condition.Count == nil {
				failure(w, 400, "Ignore window requires a positive count")
				return
			}
			if change.Ignore.Condition.UserWindow != nil && change.Ignore.Condition.UserCount == nil {
				failure(w, 400, "Ignore user window requires a positive userCount")
				return
			}
		}
	}
	if raw, present := fields["resolution"]; present {
		var err error
		change.Resolution, err = issueResolutionChange(raw, change.Status)
		if err != nil {
			failure(w, 400, err.Error())
			return
		}
	}
	if raw, present := fields["assignedTo"]; present {
		change.Assignment = &store.IssueAssignmentChange{}
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			target, err := issueObject(raw, "type", "id")
			if err != nil || len(target) != 2 {
				failure(w, 400, "Assignment must contain type and id")
				return
			}
			var ref store.IssueActorRef
			if json.Unmarshal(target["type"], &ref.Type) != nil || ref.Type != "user" && ref.Type != "team" || json.Unmarshal(target["id"], &ref.ID) != nil || !issueTargetID(ref.ID) {
				failure(w, 400, "Assignment requires user/team type and a canonical positive ID string")
				return
			}
			change.Assignment.Target = &ref
		}
	}
	item, err := s.db.UpdateIssue(r.Context(), current(r).User.ID, r.PathValue("issue"), change)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, item)
}

func issueTargetID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func issueReport(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidIssueChange) {
		failure(w, 400, err.Error())
		return
	}
	report(w, err)
}

func issueHistoryQuery(w http.ResponseWriter, r *http.Request, search bool) (string, int, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "Invalid history query encoding")
		return "", 0, false
	}
	page := 1
	for key, items := range values {
		if len(items) != 1 || key != "page" && !(search && key == "query") || !utf8.ValidString(items[0]) || strings.ContainsRune(items[0], 0) {
			failure(w, 400, "Invalid history query parameter")
			return "", 0, false
		}
		if key == "page" {
			page, err = strconv.Atoi(items[0])
			if err != nil || page < 1 || page > 1000000 || strconv.Itoa(page) != items[0] {
				failure(w, 400, "Page must be between 1 and 1000000")
				return "", 0, false
			}
		}
	}
	if len(values.Get("query")) > 200 {
		failure(w, 400, "Assignment search is limited to 200 bytes")
		return "", 0, false
	}
	return values.Get("query"), page, true
}

func (s *Server) issueAssignees(w http.ResponseWriter, r *http.Request) {
	query, page, ok := issueHistoryQuery(w, r, true)
	if !ok {
		return
	}
	items, total, err := s.db.IssueAssignees(r.Context(), current(r).User.ID, r.PathValue("issue"), query, page)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "page": page, "pageSize": 25})
}

func (s *Server) issueActivity(w http.ResponseWriter, r *http.Request) {
	_, page, ok := issueHistoryQuery(w, r, false)
	if !ok {
		return
	}
	items, total, err := s.db.IssueActivities(r.Context(), current(r).User.ID, r.PathValue("issue"), page)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "page": page, "pageSize": 25})
}

func (s *Server) issueComments(w http.ResponseWriter, r *http.Request) {
	_, page, ok := issueHistoryQuery(w, r, false)
	if !ok {
		return
	}
	items, total, err := s.db.IssueComments(r.Context(), current(r).User.ID, r.PathValue("issue"), page)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items, "total": total, "page": page, "pageSize": 25})
}

func commentBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	fields, ok := issueBody(w, r, "text")
	if !ok {
		return "", false
	}
	var text string
	if len(fields) != 1 || json.Unmarshal(fields["text"], &text) != nil {
		failure(w, 400, "Provide comment text")
		return "", false
	}
	return text, true
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	text, ok := commentBody(w, r)
	if !ok {
		return
	}
	comment, err := s.db.AddComment(r.Context(), current(r).User.ID, r.PathValue("issue"), text)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 201, comment)
}

func (s *Server) editComment(w http.ResponseWriter, r *http.Request) {
	text, ok := commentBody(w, r)
	if !ok {
		return
	}
	comment, err := s.db.EditComment(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("comment"), text)
	if err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 200, comment)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteComment(r.Context(), current(r).User.ID, r.PathValue("issue"), r.PathValue("comment")); err != nil {
		issueReport(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
