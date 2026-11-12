// Package gauges_test holds the code of each acceptance test of specs/gauges.md. TestAT<N> runs testdata/at<N>.txtar,
// whose comments quote test N's Given, When and Then. TestAT7 drives kiln tui in a pseudo-terminal, which no
// sandboxed session can open, so it runs in kiln's gate.
package gauges_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a landing gets a reading.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a reading past the line shows it crossed, and the build still lands.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a gauge measures the landing's own work.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: a reading kiln could not take is unknown.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a landing by kiln accept is read.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: each gauge reads every landing in turn.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: kiln tui shows each gauge's readings over the landings.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }
