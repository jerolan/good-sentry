package notify

import "time"

// RetryDelay keeps receiver minimums even when they exceed the backoff ceiling.
func RetryDelay(attempt int, minimum time.Duration) time.Duration {
	delay := 15 * time.Second
	for n := 1; n < attempt && delay < 15*time.Minute; n++ {
		delay = min(delay*2, 15*time.Minute)
	}
	return max(delay, minimum)
}
