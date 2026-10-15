package store

import (
	"encoding/json"
	"time"
)

type TraceDetail struct {
	TraceID            string             `json:"traceId"`
	ProjectID          string             `json:"projectId"`
	NodeCount          int64              `json:"nodeCount"`
	ProjectCount       int64              `json:"projectCount"`
	ErrorCount         int64              `json:"errorCount"`
	LogCount           int64              `json:"logCount"`
	LogsEnabled        bool               `json:"logsEnabled"`
	StartTimestamp     *time.Time         `json:"startTimestamp"`
	EndTimestamp       *time.Time         `json:"endTimestamp"`
	Duration           *float64           `json:"duration"`
	Relationships      TraceRelationships `json:"relationships"`
	InvalidSpanIDCount int64              `json:"invalidSpanIdCount"`
	UntimedCount       int64              `json:"untimedCount"`
	Data               []TraceNode        `json:"data"`
	Page               int                `json:"page"`
	PageSize           int                `json:"pageSize"`
	HasMore            bool               `json:"hasMore"`
}

type TraceRelationships struct {
	None      int64 `json:"none"`
	Resolved  int64 `json:"resolved"`
	Missing   int64 `json:"missing"`
	Ambiguous int64 `json:"ambiguous"`
	Invalid   int64 `json:"invalid"`
	Cycle     int64 `json:"cycle"`
}

type TraceNode struct {
	ID               string     `json:"id"`
	Source           string     `json:"source"`
	RecordID         string     `json:"recordId"`
	Ordinal          int        `json:"ordinal"`
	Kind             string     `json:"kind"`
	ProjectID        string     `json:"projectId"`
	ProjectName      string     `json:"projectName"`
	Name             string     `json:"name"`
	Op               string     `json:"op"`
	Status           string     `json:"status"`
	Environment      string     `json:"environment"`
	Release          string     `json:"release"`
	SpanID           string     `json:"spanId"`
	ParentSpanID     string     `json:"parentSpanId"`
	SpanIDStatus     string     `json:"spanIdStatus"`
	ParentStatus     string     `json:"parentStatus"`
	ParentID         string     `json:"parentId"`
	ParentPage       int        `json:"parentPage"`
	Depth            int        `json:"depth"`
	ChildCount       int        `json:"childCount"`
	StartTimestamp   *time.Time `json:"startTimestamp"`
	EndTimestamp     *time.Time `json:"endTimestamp"`
	Duration         *float64   `json:"duration"`
	Offset           *float64   `json:"offset"`
	SummaryTruncated bool       `json:"summaryTruncated"`
}

type TraceNodeData struct {
	ID        string          `json:"id"`
	Source    string          `json:"source"`
	RecordID  string          `json:"recordId"`
	Ordinal   int             `json:"ordinal"`
	ProjectID string          `json:"projectId"`
	Data      json.RawMessage `json:"data"`
}

type TraceErrors struct {
	Data     []TraceError `json:"data"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	HasMore  bool         `json:"hasMore"`
}

type TraceError struct {
	ID               string    `json:"id"`
	EventID          string    `json:"eventId"`
	IssueID          string    `json:"issueId"`
	ProjectID        string    `json:"projectId"`
	ProjectName      string    `json:"projectName"`
	Title            string    `json:"title"`
	Level            string    `json:"level"`
	Timestamp        time.Time `json:"timestamp"`
	SpanID           string    `json:"spanId"`
	SummaryTruncated bool      `json:"summaryTruncated"`
}
