package notify

import (
	"testing"
	"time"
)

func TestRetryDelayPreservesProviderMinimum(t *testing.T) {
	for _, tc := range []struct {
		name    string
		attempt int
		minimum time.Duration
		want    time.Duration
	}{
		{"minimum after first attempt", 1, time.Minute, time.Minute},
		{"minimum exceeds backoff ceiling", 20, 2 * time.Hour, 2 * time.Hour},
		{"short hint preserves backoff", 20, time.Second, 15 * time.Minute},
		{"representable duration stays intact", 20, time.Duration(1<<63 - 1), time.Duration(1<<63 - 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RetryDelay(tc.attempt, tc.minimum); got != tc.want {
				t.Fatalf("next-attempt delay=%s, want %s", got, tc.want)
			}
		})
	}
}
