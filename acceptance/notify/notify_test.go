// Package notify_test holds the code of each acceptance test of specs/notify.md. TestAT<N> runs testdata/at<N>.txtar,
// whose comments quote test N's Given, When and Then.
package notify_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: a stopped spec notifies, and says why.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a spec waiting for UAT notifies, and sums up its build.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a spec waiting on its branch notifies, and shows its amendments.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: a stopped landing of accepted amendments points back to accept.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }
