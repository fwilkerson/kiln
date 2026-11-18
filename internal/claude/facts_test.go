package claude

import (
	"testing"
	"time"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"the last 64 KiB of its standard error": stderrKept == 64<<10,
		"waits at most 10 s":                    outputGrace == 10*time.Second,
		"at most 200 bytes of a call's":         lineKept == 200,
	})
}
