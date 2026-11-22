package claude

import (
	"testing"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"the last 64 KiB of its standard error": stderrKept == 64<<10,
		"at most 200 bytes of a call's":         lineKept == 200,
	})
}
