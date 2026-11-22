// Package tend_test holds the acceptance tests of .kiln/specs/tend.md, spec 16. kiln tend opens the developer's
// own session on what stopped, then kiln goes on.
//
//	go test ./uat/tend -run TestAT7
//
// TestAT<N> runs testdata/at<N>.txtar, the script of the spec's acceptance test N.
package tend_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

func TestAT1(t *testing.T)  { scenariotest.Run(t, "testdata/at1.txtar") }
func TestAT2(t *testing.T)  { scenariotest.Run(t, "testdata/at2.txtar") }
func TestAT3(t *testing.T)  { scenariotest.Run(t, "testdata/at3.txtar") }
func TestAT4(t *testing.T)  { scenariotest.Run(t, "testdata/at4.txtar") }
func TestAT5(t *testing.T)  { scenariotest.Run(t, "testdata/at5.txtar") }
func TestAT6(t *testing.T)  { scenariotest.Run(t, "testdata/at6.txtar") }
func TestAT7(t *testing.T)  { scenariotest.Run(t, "testdata/at7.txtar") }
func TestAT8(t *testing.T)  { scenariotest.Run(t, "testdata/at8.txtar") }
func TestAT9(t *testing.T)  { scenariotest.Run(t, "testdata/at9.txtar") }
func TestAT10(t *testing.T) { scenariotest.Run(t, "testdata/at10.txtar") }
func TestAT11(t *testing.T) { scenariotest.Run(t, "testdata/at11.txtar") }
func TestAT12(t *testing.T) { scenariotest.Run(t, "testdata/at12.txtar") }
