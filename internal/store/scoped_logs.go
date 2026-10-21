package store

import (
	"context"
	"errors"

	"goodsentry/internal/otlp"
)

var ErrInvalidLogFilter = errors.New("invalid log filter")

// ScopedLogs returns one lookahead row after applying the complete grant in SQL.
func (s *Store) ScopedLogs(ctx context.Context, access ManagementAccess, filter LogFilter, offset, limit int) ([]Log, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || offset < 0 || offset > 100000000 || limit < 1 || limit > 100 {
		return nil, ErrInvalidLogFilter
	}
	if filter.Project != "" {
		project, err := s.ManagementProject(ctx, access, filter.Project)
		if err != nil {
			return nil, err
		}
		filter.Project = project.ID
	}
	return s.readLogs(ctx, access, filter, int64(offset), limit+1)
}

func (s *Store) ScopedLog(ctx context.Context, access ManagementAccess, logID string, limits otlp.Limits) (LogDetail, error) {
	if referenceID(access.UserID) == 0 || referenceID(access.OrganizationID) == 0 || referenceID(logID) == 0 {
		return LogDetail{}, ErrInvalidLogFilter
	}
	return s.readLog(ctx, access, "", logID, limits)
}
