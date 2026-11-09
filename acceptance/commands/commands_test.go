// Package commands_test holds the code of each acceptance test of specs/commands.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
package commands_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: amendments wait for an answer.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: rejecting amendments drops the build.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a landed spec builds again only once its spec changed.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: an accepted landing that stops can land again.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: completion offers only what a command takes.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: the reinstall builds main.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }
