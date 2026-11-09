// Package stats_test holds the code of each acceptance test of specs/build-stats.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
package stats_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT5: a landed build's summary is in its commit.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }
