package termtest_test

import (
	"testing"

	"kilntui/termtest"
)

func TestShell(t *testing.T) {
	term := termtest.Start(t, 40, 5, "sh", "-c", `printf 'ready\n'; read x; printf 'got %s\n' "$x"; sleep 5`)
	term.WaitFor("ready")
	term.Type("abc\r")
	term.WaitFor("got abc")
}
