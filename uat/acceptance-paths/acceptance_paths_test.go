// Package acceptancepaths_test holds the code of each acceptance test of spec 13, acceptance-paths. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
package acceptancepaths_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: the verify session writes under each path, and the build session under none.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a build queued under one path builds with that path.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a revised spec's build starts without its old tests in every path.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: kiln refuses a spec named for a harness folder.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: kiln refuses a build agent that names any path.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: kiln refuses a path without {spec}.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: a monitor's fix writes under no path.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT8: each session is told each path.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }

// TestAT9: kiln refuses a list whose paths could meet.
func TestAT9(t *testing.T) { scenariotest.Run(t, "testdata/at9.txtar") }
