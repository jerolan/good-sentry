package server

import (
	"math/rand/v2"
	"net/http"
	"strconv"
)

const projectThrottleRetryAfterSeconds = 600

func projectEventThrottleDecision(rate, percentile int) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 100 {
		return true
	}
	return percentile >= 0 && percentile < rate
}

func shouldThrottleProjectEvent(rate int) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 100 {
		return true
	}
	return projectEventThrottleDecision(rate, rand.IntN(100))
}

func setProjectThrottleHeaders(w http.ResponseWriter) {
	retryAfter := strconv.Itoa(projectThrottleRetryAfterSeconds)
	w.Header().Set("Retry-After", retryAfter)
	w.Header().Set("X-Sentry-Rate-Limits", retryAfter+":event:project:project_throttled")
}

func rejectProjectThrottle(w http.ResponseWriter) {
	setProjectThrottleHeaders(w)
	failure(w, http.StatusTooManyRequests, "Project event throttle applied")
}
