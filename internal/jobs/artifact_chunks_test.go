package jobs

import (
	"testing"
	"time"
)

func TestArtifactCleanupIntervalClampsDerivedSweep(t *testing.T) {
	for _, tc := range []struct {
		ttl  time.Duration
		want time.Duration
	}{
		{24 * time.Hour, time.Hour},
		{168 * time.Hour, time.Hour},
		{time.Hour, 150 * time.Second},
		{24 * time.Minute, time.Minute},
		{0, time.Minute},
	} {
		if got := artifactCleanupInterval(tc.ttl); got != tc.want {
			t.Fatalf("ttl %s interval=%s want=%s", tc.ttl, got, tc.want)
		}
	}
}
