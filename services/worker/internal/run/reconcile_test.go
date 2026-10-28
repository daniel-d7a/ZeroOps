// Unit test for the plan §8.7 startup-reconcile decision (no Docker/Postgres).
package run

import "testing"

func TestShouldStart(t *testing.T) {
	cases := []struct {
		name         string
		deploymentID string
		openAction   bool
		running      bool
		want         bool
	}{
		{"never deployed", "", false, false, false},
		{"open action blocks", "dep-1", true, false, false},
		{"container already running", "dep-1", false, true, false},
		{"container stopped", "dep-1", false, false, true},
		{"container gone", "dep-1", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldStart(tc.deploymentID, tc.openAction, tc.running); got != tc.want {
				t.Errorf("shouldStart(%q, %v, %v) = %v, want %v",
					tc.deploymentID, tc.openAction, tc.running, got, tc.want)
			}
		})
	}
}
