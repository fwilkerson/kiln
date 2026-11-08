// Package kiln_test holds the code of each acceptance test of specs/kiln.md. TestAT<N> runs testdata/at<N>.txtar,
// whose comments quote test N's Given, When and Then.
package kiln_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: an approved spec lands.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a build can't change what the developer approved.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: main moves only to work gated on it.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: main stays green.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: an amended spec is delivered whole, and waits.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: accepting a spec retires it.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: revising a spec builds it again.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT8: a usage limit only pauses.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }

// TestAT9: a build that can't pass its gate stops.
func TestAT9(t *testing.T) { scenariotest.Run(t, "testdata/at9.txtar") }

// TestAT10: a test that fails on main too doesn't block a build.
func TestAT10(t *testing.T) { scenariotest.Run(t, "testdata/at10.txtar") }
