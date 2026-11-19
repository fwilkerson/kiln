// Package resolve_test holds the acceptance tests of .kiln/specs/resolve.md, spec 14: an accepted fix that main
// moved past still lands. TestAT<N> runs test N.
package resolve_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }
