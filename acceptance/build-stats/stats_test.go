// Package stats_test holds the code of each acceptance test of specs/build-stats.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
package stats_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a build's stats sum all its sessions, by role.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: waiting is shown apart from working.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: failed calls are grouped by cause.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: stats outlive the spec.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a landed build's summary is in its commit.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: a run that wrote no result is not shown as free.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }
