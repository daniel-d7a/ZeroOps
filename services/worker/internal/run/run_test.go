// Unit tests for the run role's pure decision logic (no broker/Docker).
package run

import "testing"

// TestActionSkip pins the run/action dispatch rule (plan §8.6 + the
// redelivery rescue in actionSkip): queued runs, terminal states always
// skip, a running action skips as a live sibling's claim — unless the
// message is redelivered, meaning the previous holder died mid-job and the
// row would otherwise wedge forever behind the API's open-action 409.
func TestActionSkip(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		redelivered bool
		wantSkip    bool
	}{
		{"queued fresh", "queued", false, false},
		{"queued after retry bounce", "queued", true, false},
		{"running live claim", "running", false, true},
		{"running orphaned claim rescued", "running", true, false},
		{"done", "done", false, true},
		{"done redelivered", "done", true, true},
		{"failed", "failed", true, true},
		{"unknown status", "weird", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionSkip(tc.status, tc.redelivered); got != tc.wantSkip {
				t.Errorf("actionSkip(%q, %v) = %v, want %v",
					tc.status, tc.redelivered, got, tc.wantSkip)
			}
		})
	}
}
