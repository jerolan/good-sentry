package server

import "testing"

func TestProjectEventThrottleDecision(t *testing.T) {
	for _, test := range []struct {
		name       string
		rate       int
		percentile int
		want       bool
	}{
		{name: "disabled", rate: 0, percentile: 0, want: false},
		{name: "one percent selected", rate: 1, percentile: 0, want: true},
		{name: "one percent retained", rate: 1, percentile: 1, want: false},
		{name: "middle selected", rate: 37, percentile: 36, want: true},
		{name: "middle retained", rate: 37, percentile: 37, want: false},
		{name: "ninety-nine selected", rate: 99, percentile: 98, want: true},
		{name: "ninety-nine retained", rate: 99, percentile: 99, want: false},
		{name: "one hundred always rejected", rate: 100, percentile: -1, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := projectEventThrottleDecision(test.rate, test.percentile); got != test.want {
				t.Fatalf("projectEventThrottleDecision(%d, %d)=%t, want %t", test.rate, test.percentile, got, test.want)
			}
		})
	}
}
