// Package roles_test holds the code of each acceptance test of spec 7, project-roles. TestAT<N> runs
// testdata/at<N>.txtar, whose comments quote test N's Given, When and Then. A Then about a session reads what kiln
// passed to the scripted claude: its arguments, system prompt and settings.
package roles_test

import (
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

func TestMain(m *testing.M) { scenariotest.Main(m) }

// TestAT1: an agent's file on main sets its session.
func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }

// TestAT2: a build cannot change kiln's files.
func TestAT2(t *testing.T) { scenariotest.Run(t, "testdata/at2.txtar") }

// TestAT3: kiln's contract follows the agent's own prompt.
func TestAT3(t *testing.T) { scenariotest.Run(t, "testdata/at3.txtar") }

// TestAT4: an agent's file cannot cross kiln's lines.
func TestAT4(t *testing.T) { scenariotest.Run(t, "testdata/at4.txtar") }

// TestAT5: a project names its own hosts and paths.
func TestAT5(t *testing.T) { scenariotest.Run(t, "testdata/at5.txtar") }

// TestAT6: a new project starts from kiln's files.
func TestAT6(t *testing.T) { scenariotest.Run(t, "testdata/at6.txtar") }

// TestAT7: kiln refuses a spec that leaves the template.
func TestAT7(t *testing.T) { scenariotest.Run(t, "testdata/at7.txtar") }
