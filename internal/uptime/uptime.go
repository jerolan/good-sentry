// Package uptime defines monitor configuration and the results of bounded probes.
// Network probes have no database access; the store owns scheduling and state.
package uptime

import "time"

type Kind string

const (
	HEAD      Kind = "HEAD"
	GET       Kind = "GET"
	POST      Kind = "POST"
	TCP       Kind = "TCP"
	SSL       Kind = "SSL"
	Heartbeat Kind = "heartbeat"
)

type Status string

const (
	Unknown Status = "unknown"
	Up      Status = "up"
	Down    Status = "down"
)

type Config struct {
	Name                  string `json:"name"`
	Type                  Kind   `json:"type"`
	URL                   string `json:"url"`
	Project               string `json:"project"`
	Environment           string `json:"environment"`
	IntervalSeconds       int    `json:"intervalSeconds"`
	TimeoutSeconds        int    `json:"timeoutSeconds"`
	ExpectedStatus        int    `json:"expectedStatus"`
	ExpectedBody          string `json:"expectedBody"`
	ConfirmationThreshold int    `json:"confirmationThreshold"`
	Enabled               bool   `json:"enabled"`
}

func Defaults() Config {
	return Config{Type: HEAD, IntervalSeconds: 60, TimeoutSeconds: 20, ExpectedStatus: 200, ConfirmationThreshold: 1, Enabled: true}
}

type Monitor struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId,omitempty"`
	Config
	Status              Status     `json:"status"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	LastCheck           *time.Time `json:"lastCheck"`
	LastChange          *time.Time `json:"lastChange"`
	NextCheck           time.Time  `json:"nextCheck"`
	CreatedAt           time.Time  `json:"createdAt"`
	HeartbeatURL        string     `json:"heartbeatURL,omitempty"`
	HeartbeatKey        string     `json:"-"`
}

type Lease struct {
	Monitor
	Token     string
	StartedAt time.Time
	ExpiresAt time.Time
}

// Result contains bounded reason codes rather than errors that may expose URLs.
// Heartbeat checks have no network latency or HTTP response status.
type Result struct {
	Up             bool
	ResponseTimeMS *int64
	StatusCode     *int
	Reason         string
}

type Check struct {
	ID             string    `json:"id"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt"`
	Up             bool      `json:"up"`
	Status         Status    `json:"status"`
	PreviousStatus Status    `json:"previousStatus"`
	IsChange       bool      `json:"isChange"`
	ResponseTimeMS *int64    `json:"responseTimeMs"`
	StatusCode     *int      `json:"statusCode"`
	Reason         string    `json:"reason"`
}
