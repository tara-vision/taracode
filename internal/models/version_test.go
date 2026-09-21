package models

import "testing"

func TestVersionBefore(t *testing.T) {
	cases := []struct {
		have, need string
		want       bool
	}{
		{"0.34.2", "0.32.12", false},
		{"0.32.7", "0.32.12", true},
		{"0.9.0", "0.10.0", true},
		{"v0.34.2", "0.34.2", false},
		{"0.34.2-rc1", "0.34.2", false},
		{"", "0.1.0", false},
		{"garbage", "0.1.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.have+"__"+tc.need, func(t *testing.T) {
			if got := versionBefore(tc.have, tc.need); got != tc.want {
				t.Fatalf("versionBefore(%q, %q) = %v, want %v", tc.have, tc.need, got, tc.want)
			}
		})
	}
}
