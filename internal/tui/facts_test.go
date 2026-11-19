package tui

import (
	"testing"
	"time"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"3 lines a notch":        wheelLines == 3,
		"idle for over a minute": idleWarned == time.Minute,
		"the last 10 builds that ended, beside the 10 before them": window == 10,
	})
}
