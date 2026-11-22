package scenariotest

import (
	"testing"
	"time"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"every 20 ms":   awaitPoll == 20*time.Millisecond,
		"once a second": exitPoll == time.Second,
		"128 plus":      signaledBase == 128,
	})
}
