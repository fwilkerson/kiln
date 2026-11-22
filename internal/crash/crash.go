// Package crash marks each step at which kiln changes durable state, so a test can kill kiln there. The test then
// runs the command a developer would run next, and checks that it recovers.
//
//	crash.Step("state.json")       // before kiln writes its book of records
//	crash.Step("git worktree add") // before git adds a worktree
//
// kiln calls Step inside the few functions that change durable state, never at their callers:
//
//	state.json     each write of the book of records
//	ledger.jsonl   each line appended
//	git            each command that changes a ref, a worktree, the index or the config
//	proc           each session's start and end, as RunSession or RunAttached runs it
//
// In kiln's own build, Step does nothing. A build with the tag crash counts the steps of each kiln sharing one log.
// At each step named, it kills kiln, or snapshots the directory that holds what kiln changes:
//
//	go build -tags crash ./cmd/kiln
//
//	KILN_CRASH_LOG        the file each step appends a line to, as "<step> <pid> <what>"; with none, Step does nothing
//	KILN_CRASH_AT         the steps named, counted from 1 across every kiln that appends to the log, split by commas;
//	                      3+8 names 3, 11, 19 and on
//	KILN_CRASH_SIGNAL     INT, which takes kiln's graceful path, or KILL, as when unset
//	KILN_CRASH_SNAPSHOTS  a directory: kiln kills nothing, and clones KILN_CRASH_ROOT to <dir>/<step> at each
//	                      step named
//	KILN_CRASH_ROOT       the directory that holds the repository, its worktrees and HOME, which a snapshot clones
//
// SIGKILL ends kiln before the step's change. SIGINT reaches kiln's handler, then the step's change goes on, 50 ms
// later. A snapshot holds what a SIGKILL there would leave, but at the step alone, as the test's doc says.
package crash
