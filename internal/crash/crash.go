// Package crash marks each step at which kiln changes durable state, so a test can stop kiln there. The test reads
// what a kill at the step would leave, and may kill kiln there.
//
//	crash.Step("state.json")       // before kiln writes its book of records
//	crash.Step("git worktree add") // before git adds a worktree
//
// kiln calls Step inside the few functions that change durable state, never at their callers:
//
//	state.json     each write of the book of records
//	ledger.jsonl   each append to the ledger, of the lines the book kept when it was written
//	git            each command that changes a ref, a worktree, the index or the config
//	clone remove   each removal of a session's clone, which is no worktree of git's
//	proc           each session's start and end, as RunSession or RunAttached runs it
//
// In the build of kiln itself, Step does nothing. A build with the tag crash asks a test at each step, and waits:
//
//	go build -tags crash ./cmd/kiln
//
//	KILN_CRASH_ASK  a directory the test made; with none, Step does nothing
//	<dir>/steps     the log each step appends a line to, as "<step> <pid> <what>"
//	<dir>/<step>    the test's answer to the step, in one line: GO, KILL or INT
//
// Steps count from 1 across every kiln that shares the directory. A kiln holds the log's lock from its step's line
// to the answer. So every kiln stands still while the test reads what a kill at the step would leave. KILL ends
// kiln by SIGKILL, before the step's change. INT sends kiln SIGINT, which reaches kiln's handler. The step's change
// then goes on, 50 ms later. A kiln whose step gets no answer in a minute has lost its test, and exits.
// TestKilledAtEachStep's doc says how a test proves recovery by the answers.
package crash
