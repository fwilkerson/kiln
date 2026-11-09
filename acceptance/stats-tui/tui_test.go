// Package tui_test holds the code of each acceptance test of specs/stats-tui.md. TestAT<N> runs testdata/at<N>.txtar,
// whose comments quote test N's Given, When and Then. Each drives kiln tui in a pseudo-terminal, which no sandboxed
// session can open, so they run in kiln's gate.
package tui_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: the running session, live.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a past build, run by run.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: builds are grouped by spec.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: the spec as its build saw it.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: the trend on the home screen.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: finding a spec by name.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: the mouse.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT8: color marks what failed.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }

// TestAT9: no agent opens it by accident.
func TestAT9(t *testing.T) { scenariotest.Run(t, "testdata/at9.txtar") }

// TestAT10: an audit that changed the tree shows it.
func TestAT10(t *testing.T) { scenariotest.Run(t, "testdata/at10.txtar") }
