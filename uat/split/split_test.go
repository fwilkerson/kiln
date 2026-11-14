// Package split_test holds the code of each acceptance test of .kiln/specs/split.md. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then.
package split_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT8: a landed build of today's workflow leaves no audit worktree.
func TestAT8(t *testing.T) { scenariotest.Run(t, "testdata/at8.txtar") }

// TestAT1: a split build lands the code and its tests.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: neither session sees the other's work.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: a failing stack goes to the verify session first.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: a bug the verify session reports goes to the build session.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: the audit reads the stack once its gate passes.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: each session writes only its side.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: a landed build leaves no checkout.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }

// TestAT9: a stopped build keeps what the developer resumes.
func TestAT9(t *testing.T) { scenariotest.Run(t, "testdata/at9.txtar") }

// TestAT10: a failure on the build session's side goes to the build session alone.
func TestAT10(t *testing.T) { scenariotest.Run(t, "testdata/at10.txtar") }

// TestAT11: the build session learns of an amendment.
func TestAT11(t *testing.T) { scenariotest.Run(t, "testdata/at11.txtar") }

// TestAT12: a revised spec's build session starts without its old tests.
func TestAT12(t *testing.T) { scenariotest.Run(t, "testdata/at12.txtar") }

// TestAT13: a change to a path the audit must read goes back to the audit.
func TestAT13(t *testing.T) { scenariotest.Run(t, "testdata/at13.txtar") }

// TestAT14: each session's prompt holds only its own work.
func TestAT14(t *testing.T) { scenariotest.Run(t, "testdata/at14.txtar") }
