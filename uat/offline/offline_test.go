// Package offline holds the acceptance tests of spec 18, "A monitor waits for a connection", in
// .kiln/specs/offline.md. TestAT<N> runs test N.
package offline

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }
