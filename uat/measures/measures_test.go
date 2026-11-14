// Package measures_test holds the code of each acceptance test of specs/measures.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
//
// Each script's gauge, count, names each changed .txt file that holds the word low, then their count. Its monitor,
// lows, names the agent fixer.md, so the agent's session has the role fixer.
package measures_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a gauge on its line starts no fix.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a crossed gauge starts a fix, and the agent reads what the gauge printed.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: the gauge holds the fix to its line, over what the fix changed.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: a reading kiln could not take starts no fix.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a fix that deletes what crossed lands.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: a fix that left the gauge as it was runs once.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }
