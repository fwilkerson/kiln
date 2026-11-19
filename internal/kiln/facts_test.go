package kiln

import (
	"testing"
	"time"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{
		"pauses it for 5 minutes":              unknownReset == 5*time.Minute,
		"last 16 KiB of each command's output": outputKept == 16<<10,
		"its interval, less 30 minutes, ago":   slack == 30*time.Minute,
		"up to 3 tries":                        auditTries == 3,
		"of at most 72":                        titleMax == 72,
	})
}
