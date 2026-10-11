package queue

import (
	"testing"

	amqp091 "github.com/rabbitmq/amqp091-go"
)

func withAttempt(v any) amqp091.Delivery {
	d := amqp091.Delivery{Headers: amqp091.Table{}}
	if v != nil {
		d.Headers["attempt"] = v
	}
	return d
}

func TestReadAttemptAcceptsAllHeaderWidths(t *testing.T) {
	cases := []struct {
		header any
		want   int64
	}{
		{nil, 0}, // header absent
		{int64(2), 2},
		{int32(2), 2},
		{int16(2), 2},
		{uint8(2), 2},
		{"2", 0}, // foreign type is not a retry count — treat as first delivery
		{true, 0},
	}
	for _, c := range cases {
		if got := ReadAttempt(withAttempt(c.header)); got != c.want {
			t.Errorf("ReadAttempt(%#v) = %d, want %d", c.header, got, c.want)
		}
	}
}

// plan §6 / §11: 3 deliveries (attempt 0, 1, 2) then dead.
func TestRetryDecisionThreeStrikes(t *testing.T) {
	cases := []struct {
		attempt   any
		gaveUp    bool
		next      int64
	}{
		{nil, false, 1},    // first delivery → retry #1
		{int64(0), false, 1},
		{int64(1), false, 2}, // one retry done → retry #2
		{int64(2), true, 3},  // two retries done → dead
		{int64(9), true, 10}, // already past the limit stays dead
	}
	for _, c := range cases {
		gaveUp, next := retryDecision(withAttempt(c.attempt))
		if gaveUp != c.gaveUp || next != c.next {
			t.Errorf("retryDecision(attempt=%v) = (%v, %d), want (%v, %d)",
				c.attempt, gaveUp, next, c.gaveUp, c.next)
		}
	}
}
