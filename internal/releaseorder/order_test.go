package releaseorder_test

import (
	"strings"
	"testing"

	"goodsentry/internal/releaseorder"
)

func TestCompareNativeReleaseOrder(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0", "0.0.0.0", 0},
		{"1", "1.0.0", 0},
		{"1.2", "1.2.0.0", 0},
		{"1.2.3.4", "1.2.3", 1},
		{"2", "10", -1},
		{"1.9.99", "1.10", -1},
		{"1.2.3.9", "1.2.3.10", -1},
		{"18446744073709551615", "18446744073709551616", -1},
		{strings.Repeat("9", 90), "1" + strings.Repeat("0", 90), -1},
		{"1.99999999999999999999999999999999", "2", -1},
		{"1-99999999999999999999999999999999", "1-100000000000000000000000000000000", -1},
		{"1-99999999999999999999999999999999", "1-a", -1},
		{"1-alpha", "1-alpha.0", -1},
		{"1-A", "1-a", -1},
		{"1-0", "1-0A", -1},
		{"1--", "1-0A", -1},
		{"1+001", "1+999", 0},
		{"1-alpha+001", "1-alpha+build.0001-x", 0},
		{"app@1.2.3", "app@1.3", -1},
		{"组织/ 🐛@1.2-rc+001", "组织/ 🐛@1.2.0.0-rc+other", 0},
		{"scope@app@1", "scope@app@2", -1},
	} {
		t.Run(tc.a+" versus "+tc.b, func(t *testing.T) {
			got, comparable := releaseorder.Compare(tc.a, tc.b)
			if !comparable || got != tc.want {
				t.Fatalf("Compare(%q,%q)=(%d,%v), want (%d,true)", tc.a, tc.b, got, comparable, tc.want)
			}
			if reverse, comparable := releaseorder.Compare(tc.b, tc.a); !comparable || reverse != -tc.want {
				t.Fatalf("reverse Compare(%q,%q)=(%d,%v)", tc.b, tc.a, reverse, comparable)
			}
		})
	}
	// This sequence is the SemVer 2.0 precedence example, independently specified.
	sequence := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	for index := 1; index < len(sequence); index++ {
		if got, comparable := releaseorder.Compare(sequence[index-1], sequence[index]); !comparable || got != -1 {
			t.Fatalf("SemVer precedence %q < %q: (%d,%v)", sequence[index-1], sequence[index], got, comparable)
		}
	}
}

func TestCompareIncomparableReleasePairs(t *testing.T) {
	invalid := []string{
		"", "v1", "V1.2.3", "1.2.3.4.5", ".1", "1.", "1..2", "1.2..4",
		"00", "01.2", "1.02", "1.2.03", "1.2.3.00", "+1", "-1", "1e3", "1.5e2",
		" 1", "1 ", "１.2.3", "1-", "1-01", "1-alpha..1", "1-.alpha", "1-alpha.",
		"1-α", "1-alpha_1", "1+", "1+a..b", "1+.build", "1+build.", "1+a_b", "1+build+other", "1+🐛",
		"@1", "pkg@", "pkg@v1", "pkg@1.2.3.4.5", "pkg@1-01", "pkg@1+bad_build", "1\x00", "1\xff",
	}
	for _, bad := range invalid {
		t.Run("invalid "+bad, func(t *testing.T) {
			for _, pair := range [][2]string{{bad, "2"}, {"2", bad}, {bad, bad}} {
				if got, comparable := releaseorder.Compare(pair[0], pair[1]); comparable || got != 0 {
					t.Fatalf("Compare(%q,%q)=(%d,%v), want (0,false)", pair[0], pair[1], got, comparable)
				}
			}
		})
	}
	for _, pair := range [][2]string{{"a@1", "b@2"}, {"App@1", "app@1"}, {"1", "app@1"}, {"scope@app@1", "app@1"}, {"组织@1", "組織@1"}} {
		for _, p := range [][2]string{pair, {pair[1], pair[0]}} {
			if got, comparable := releaseorder.Compare(p[0], p[1]); comparable || got != 0 {
				t.Fatalf("different package Compare(%q,%q)=(%d,%v)", p[0], p[1], got, comparable)
			}
		}
	}
}

func TestProjectUsesSemanticNewestSample(t *testing.T) {
	for _, tc := range []struct {
		name   string
		recent []string
		want   bool
	}{
		{"empty", nil, false},
		{"one semantic", []string{"1"}, true},
		{"one opaque", []string{"commit-sha"}, false},
		{"two newest wins", []string{"app@1+0001", "opaque"}, true},
		{"two older does not win", []string{"opaque", "1"}, false},
		{"three third qualifies", []string{"opaque", "v1", "1-rc"}, true},
		{"four fourth excluded", []string{"opaque", "v1", "bad", "1"}, false},
		{"nine only one needed", []string{"bad", "1", "bad", "bad", "bad", "bad", "bad", "bad", "bad"}, true},
		{"ten only two rejected", []string{"1", "bad", "bad", "2", "bad", "bad", "bad", "bad", "bad", "bad"}, false},
		{"ten three but none recent", []string{"bad", "bad", "bad", "1", "2", "3", "bad", "bad", "bad", "bad"}, false},
		{"ten three including recent", []string{"bad", "a@1", "bad", "b@2", "bad", "bad", "bad", "bad", "bad", "c@3"}, true},
		{"eleventh cannot qualify sample", []string{"1", "bad", "bad", "2", "bad", "bad", "bad", "bad", "bad", "bad", "3"}, false},
		{"ignore tail after ten", []string{"1", "bad", "bad", "2", "3", "bad", "bad", "bad", "bad", "bad", "bad"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseorder.ProjectUsesSemantic(tc.recent); got != tc.want {
				t.Fatalf("ProjectUsesSemantic(%q)=%v want=%v", tc.recent, got, tc.want)
			}
		})
	}
}
