// Package languages_test holds the code of each acceptance test of spec 8, languages. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then. In tests 1 to 4 and 8, the gate command
// copies JUnit XML that pytest wrote to $KILN_JUNIT. It copies one file at the build's commit, and another at main.
package languages_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a pytest test that fails on main too does not block a build.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a pytest test that fails only on the build's commit fails the gate.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a test that fails in setup on main too does not block a build.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: a collection error fails the gate even when main has it.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a failure that names no test points at KILN_JUNIT.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: a build that changes a Python file and the README gets an audit of the directories it changed.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: kiln init --preset python writes Python's defaults.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT8: a run cut short is not excused.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }
