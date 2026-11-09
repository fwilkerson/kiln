// Package tui_test holds the code of each acceptance test of specs/tui-reading.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then. Each drives kiln tui in a
// pseudo-terminal, which no sandboxed session can open, so they run in kiln's gate.
package tui_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a run's calls, newest first.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a spec's builds, newest first.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a run's whole answer.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: keys a MacBook has.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: every key, on ?.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: one mark for what kiln does not know.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: a spec's text, legible.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }
