package tui

import (
	"testing"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{"3 lines a notch": wheelLines == 3})
}
