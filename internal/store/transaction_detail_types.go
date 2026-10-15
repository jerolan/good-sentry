package store

import (
	"encoding/json"
	"time"
)

type TransactionReference struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

type TransactionDetail struct {
	Sample         TransactionReference    `json:"sample"`
	ProjectName    string                  `json:"projectName"`
	EventID        string                  `json:"eventId"`
	TraceID        string                  `json:"traceId"`
	SpanID         string                  `json:"spanId"`
	ParentSpanID   string                  `json:"parentSpanId"`
	Name           string                  `json:"name"`
	Op             string                  `json:"op"`
	Status         string                  `json:"status"`
	Environment    string                  `json:"environment"`
	Release        string                  `json:"release"`
	StartTimestamp *time.Time              `json:"startTimestamp"`
	EndTimestamp   time.Time               `json:"endTimestamp"`
	Duration       float64                 `json:"duration"`
	Data           json.RawMessage         `json:"data"`
	SpanScope      string                  `json:"spanScope"`
	Spans          []TransactionDetailSpan `json:"spans"`
	SpanCount      int64                   `json:"spanCount"`
	Page           int                     `json:"page"`
	PageSize       int                     `json:"pageSize"`
	HasMore        bool                    `json:"hasMore"`
}

type TransactionDetailSpan struct {
	ID             string          `json:"id"`
	SpanID         string          `json:"spanId"`
	ParentSpanID   string          `json:"parentSpanId"`
	Name           string          `json:"name"`
	Op             string          `json:"op"`
	Status         string          `json:"status"`
	StartTimestamp *time.Time      `json:"startTimestamp"`
	EndTimestamp   *time.Time      `json:"endTimestamp"`
	Duration       *float64        `json:"duration"`
	Data           json.RawMessage `json:"data"`
}
