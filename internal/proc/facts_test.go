package proc

import (
	"testing"
	"time"

	"github.com/fwilkerson/practices/repotest"
)

// TestFacts holds each number the package doc states to the constant it comes from.
func TestFacts(t *testing.T) {
	repotest.Facts(t, map[string]bool{"lsof, which it gives 5 s": lsofLimit == 5*time.Second,
		"gives a group 10 s": outputGrace == 10*time.Second,
	})
}
