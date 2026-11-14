// Package monitors_test holds the code of each acceptance test of specs/monitors.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
//
// The spec names no role for a monitor's agent session. So a script gives its steps under each name it could carry:
// build, monitor and the agent file's name. A script reads what ran from the markers its checks,
// commands and sessions leave under _given/, which git leaves out.
package monitors_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: kiln shows what a check found.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a monitor runs when it is due, or when named.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a command's fix lands.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: the agent fixes what the command left failing.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a check that stays out of the gate.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: a fix that can't pass stops, and waits for something to change.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: an agent asks for the developer once.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT8: a session that fails.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }

// TestAT9: a fix that waits for accept.
func TestAT9(t *testing.T) { scenariotest.Run(t, "testdata/at9.txtar") }

// TestAT10: a fix writes the tests only when its monitor says it may.
func TestAT10(t *testing.T) { scenariotest.Run(t, "testdata/at10.txtar") }

// TestAT11: a monitor waits for a running build.
func TestAT11(t *testing.T) { scenariotest.Run(t, "testdata/at11.txtar") }
