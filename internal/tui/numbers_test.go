package tui

import (
	"testing"
	"time"
)

// TestSpan holds that span picks its unit from the rounded time.
// A time just under a minute or an hour shows in the next unit.
func TestSpan(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{59*time.Second + 600*time.Millisecond, "1m"},
		{59*time.Minute + 45*time.Second, "1h00m"},
	} {
		if got := span(c.d); got != c.want {
			t.Errorf("span(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
