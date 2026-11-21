package git

import (
	"testing"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the code it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"kiln needs git 2.55 or later": supported("git version 2.55.0") && !supported("git version 2.54.9"),
	})
}
