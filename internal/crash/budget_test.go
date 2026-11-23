package crash_test

import (
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestBudget holds when TestKilledAtEachStep ends by its own budget, and that its failure names the budget.
func TestBudget(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		left, timeout time.Duration
		said          string
	}{
		"more than a tenth is left": {61 * time.Second, 10 * time.Minute, ""},
		"a tenth is left":           {time.Minute, 10 * time.Minute, "its budget of 9m0s, nine tenths of go test's -timeout of 10m0s"},
	} {
		got := outlasted(c.left, c.timeout)
		if (got == "") != (c.said == "") || !strings.Contains(got, c.said) {
			t.Errorf("%s: with %s left of %s, the test fails with %q, not %q", name, c.left, c.timeout, got, c.said)
		}
	}
}

// keepBudget fails t once, when t ends over its budget. Each run that drive cut short only skips.
func keepBudget(t *testing.T) {
	// The subtests of t run in parallel once its function returns. Only a cleanup sees the last one end.
	t.Cleanup(func() {
		if why := overBudget(t); why != "" {
			t.Error(why)
		}
	})
}

// overBudget answers what TestKilledAtEachStep fails with once the test outlasts its budget, and "" before then.
// With no -timeout, the test has no budget.
func overBudget(t *testing.T) string {
	deadline, limited := t.Deadline()
	if !limited {
		return ""
	}

	timeout := flag.Lookup("test.timeout").Value.(flag.Getter).Get().(time.Duration)
	return outlasted(time.Until(deadline), timeout)
}

// outlasted answers what TestKilledAtEachStep fails with once it outlasts its budget, nine tenths of timeout. It
// answers "" before then. left is the time before the timeout of go test ends the run. In the last tenth, each command
// at work ends, so the panic of go test never names the failure.
func outlasted(left, timeout time.Duration) string {
	if left > timeout/10 {
		return ""
	}

	return fmt.Sprintf("TestKilledAtEachStep outlasted its budget of %s, nine tenths of go test's -timeout of %s. "+
		"A busy machine slows the test, so this failure names no defect of kiln's. "+
		"Run the test again once the machine is quiet, or with a longer -timeout.", timeout-timeout/10, timeout)
}
