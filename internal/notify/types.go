// Package notify owns alert configuration and bounded external delivery.
// PostgreSQL scheduling and transactions belong to the store.
package notify

import "time"

type Kind string

const (
	Frequency Kind = "error_frequency"
	Uptime    Kind = "uptime"
)

type Recipient struct {
	Type        string `json:"type"`
	Destination string `json:"destination"`
}

type Config struct {
	Name            string      `json:"name"`
	Kind            Kind        `json:"kind"`
	Enabled         bool        `json:"enabled"`
	Quantity        int         `json:"quantity"`
	WindowMinutes   int         `json:"windowMinutes"`
	CooldownSeconds int         `json:"cooldownSeconds"`
	Environments    []string    `json:"environments"`
	Recipients      []Recipient `json:"recipients"`
}

func Defaults() Config {
	return Config{Kind: Frequency, Enabled: true, Quantity: 1, WindowMinutes: 5, CooldownSeconds: 900, Environments: []string{}, Recipients: []Recipient{{Type: "team_email"}}}
}

type Rule struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Config
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastTriggered *time.Time `json:"lastTriggered"`
}

type Resource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Project struct {
	Resource
	Slug string `json:"slug"`
}

type Monitor struct {
	Resource
	Status         string `json:"status"`
	PreviousStatus string `json:"previousStatus"`
}

type Payload struct {
	Version       int       `json:"version"`
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	OccurredAt    time.Time `json:"occurredAt"`
	Rule          Resource  `json:"rule"`
	Organization  string    `json:"organization"`
	Project       Project   `json:"project"`
	Issue         *Resource `json:"issue,omitempty"`
	EventID       string    `json:"eventId,omitempty"`
	Monitor       *Monitor  `json:"monitor,omitempty"`
	Count         int       `json:"count,omitempty"`
	WindowMinutes int       `json:"windowMinutes,omitempty"`
	Environment   string    `json:"environment,omitempty"`
	URL           string    `json:"url"`
}

// Public resolves the generated relative route at an external HTTP boundary.
// A changed public domain deliberately changes links in subsequent attempts.
func (p Payload) Public(domain string) Payload {
	p.URL = domain + p.URL
	return p
}

type State string

const (
	Pending    State = "pending"
	Processing State = "processing"
	Sent       State = "sent"
	Failed     State = "failed"
	Canceled   State = "canceled"
)

type Delivery struct {
	ID             string     `json:"id"`
	RuleID         string     `json:"ruleId"`
	RuleName       string     `json:"ruleName"`
	ProjectID      string     `json:"projectId"`
	Recipient      Recipient  `json:"recipient"`
	Payload        Payload    `json:"payload"`
	Status         State      `json:"status"`
	Attempts       int        `json:"attempts"`
	NextAttempt    time.Time  `json:"nextAttempt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	SentAt         *time.Time `json:"sentAt"`
	LastReason     string     `json:"lastReason"`
	LastStatusCode *int       `json:"lastStatusCode"`
}

type Attempt struct {
	Number      int        `json:"number"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	StatusCode  *int       `json:"statusCode"`
	ElapsedMS   *int64     `json:"elapsedMs"`
}

type Lease struct {
	Delivery
	Token     string
	ExpiresAt time.Time
}

type Disposition string

const (
	Delivered Disposition = "delivered"
	Retry     Disposition = "retry"
	Permanent Disposition = "permanent"
)

type Outcome struct {
	Disposition Disposition
	Reason      string
	StatusCode  *int
	ElapsedMS   int64
}
