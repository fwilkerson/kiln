// Package scenariotest runs kiln's acceptance tests. Each is a testscript file that runs kiln on a scratch
// repository, with a scripted claude as the model.
//
//	func TestMain(m *testing.M) { scenariotest.Main(m) }
//
//	func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }
//
//	func TestUAT(t *testing.T) { scenariotest.RunAll(t, "testdata") }
//
// Run runs one script, for a spec's test N. RunAll runs each script of each feature, testdata/<feature>/*.txtar.
//
// This script lands a spec x, with one scripted build session and one audit:
//
//	exec kiln queue x
//	exec kiln fire
//	stdout '^x: landed on main'
//	grep 'Built x' .claude-calls/build/1/1/stream.jsonl
//
//	-- claude.json --
//	{
//	  "build": {"1": {"1": [
//	    {"tool": {"name": "Bash", "input": {"command": "cp -R \"$WORK/_given/x/.\" ."}}},
//	    {"result": {"result": "Built x."}}
//	  ]}},
//	  "audit": {"1": {"1": [{"result": {"result": "read 1 directory, found 0"}}]}}
//	}
//	-- .kiln/specs/x.md --
//	### 1. X answers
//
//	- **Given**: nothing.
//	- **When**: X is called.
//	- **Then**: it answers 42.
//	-- _given/x/x.go --
//	package scratch
//
//	func X() int { return 42 }
//
// The script runs in a scratch repository, with the scripted claude in place of the real one:
//
//	$WORK/                       the main checkout, on main; its first commit holds what the script brings
//	  claude.json                the scripted claude's steps, a ClaudeScript, by role, attempt and call
//	  _given/                    the work a session copies in, which that commit and Go's ./... leave out
//	  .claude-calls/<role>/<attempt>/<call>/
//	                             each call's record, in the files that ClaudeCallFile and its kin name
//	$WORK-home/                  HOME, so kiln's worktrees and caches are the script's own; its git config is
//	                             hometest's, with no automatic maintenance
//
//	PATH              kiln, built from cmd/kiln, then claude, the scripted one, and await
//	KILN_REPO         the checkout of kiln the test runs in, for a script that works on kiln's own repository
//	GOCACHE           the developer's, with GOMODCACHE and GOPATH; Go runs offline, as GOPROXY=off,
//	                  GOTOOLCHAIN=local and GOFLAGS=-buildvcs=false say
//	GOFLAGS           -trimpath too, so a gate reuses what another script's gate compiled and vetted, from
//	                  another directory
//	SCENARIOTEST_PID  the test process, whose end ends each await and scripted claude
//
// A script waits for an event with await, never with a count of tries or a sleep. Only the test's deadline bounds the
// wait, so a loaded machine makes a script slow, never wrong:
//
//	exec await .tmp/fixing                 until the file exists
//	exec await .tmp/mon.out '^x: waits'    until a line of the file matches
//
// await is on the PATH of kiln, of its sessions and of its gates too. A scripted step or a gate waits the same way.
// await looks at its file every 20 ms. A scripted claude has no limit of its own. Every await and scripted claude
// ends once the test process ends, so none outlives the run. Where kqueue cannot watch the test process, an await
// or a scripted claude asks once a second whether it runs.
//
// The scripted claude speaks as claude -p --output-format stream-json --verbose, and reads its prompt on standard
// input. A call with no -p is an interactive session, as kiln tend starts one. Its last argument is its first
// message, which its record keeps as the prompt. It prints no stream, and its tools run with the settings Claude
// Code reads for the developer.
//
// Main refuses to run any test, exiting 2, when the first claude on the PATH is not the scripted one. A real claude
// would call the model.
//
// A script brings only what its Given names. One that brings anything under .kiln/ gets the rest of a Go project that
// kiln builds:
//
//   - .kiln/kiln.json gets each of three keys it lacks. They are acceptanceTests, as acceptance/{spec}/, gate, as go
//     vet and go test, and rounds, as 1. With that gate comes go.mod, for the module example.com/scratch.
//   - Each agent's file it lacks: a build agent, a verify agent and an audit, each a line of prompt. The audit has
//     the tools Read, Grep, Glob and Bash.
//   - A spec under .kiln/specs/ that holds only its acceptance tests, from "### 1." on, gets the other sections of
//     /shape's template, empty, and Status Approved. It is spec N, by its name's place among the script's specs.
//   - Each check, gauge and command under .kiln/monitors/ gets the executable bit, as kiln runs each by its path.
//
// A script drives a full-screen program in a pseudo-terminal, such as kiln's terminal UI. Its commands are term, keys,
// click, wheel, screen and color. No sandboxed session can open a pseudo-terminal, so term skips such a script inside
// one. It skips only where SANDBOX_RUNTIME is set, as Claude Code's sandbox sets it. Elsewhere the script fails.
package scenariotest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/hometest"
	"github.com/rogpeppe/go-internal/testscript"
)

// Main runs the tests of m, with kiln and the scripted claude first on the PATH. Call it from TestMain. It first
// drops the variables that would aim git at another repository, as [hometest.DropGitVars] says.
func Main(m *testing.M) {
	hometest.DropGitVars()
	testscript.Main(tests{m}, map[string]func(){
		"claude": func() { os.Exit(claudeMain()) },
		"await":  func() { os.Exit(awaitMain()) },
	})
}

type tests struct {
	m *testing.M
}

// goEnv holds Go's caches, which each script shares with the developer, since a script's Go runs offline.
var goEnv = map[string]string{}

func (t tests) Run() int {
	// testscript puts its commands first on the PATH. A claude elsewhere would be the real one, which calls the model.
	first := filepath.SplitList(os.Getenv("PATH"))[0]
	if claude, err := exec.LookPath("claude"); err != nil || filepath.Dir(claude) != first {
		fmt.Fprintf(os.Stderr, "scenariotest: the first claude on PATH is %q, not the harness's own: %v\n", claude, err)
		return 2
	}

	bin, err := os.MkdirTemp("", "kiln-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "scenariotest:", err)
		return 2
	}

	defer os.RemoveAll(bin)

	if err := buildKiln(bin); err != nil {
		fmt.Fprintln(os.Stderr, "scenariotest:", err)
		return 2
	}

	os.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	if err := readGoEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "scenariotest:", err)
		return 2
	}

	return t.m.Run()
}

// buildKiln builds kiln into bin, and lists the files it is built from in kilnSource.
func buildKiln(bin string) error {
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "kiln"), kilnCommand)
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building kiln: %w\n%s", err, out)
	}

	var err error
	kilnSource, err = sourceOf(kilnCommand)
	return err
}

// readGoEnv sets goEnv to Go's caches, and kilnRepo to the module's directory.
func readGoEnv() error {
	names := []string{"GOCACHE", "GOMODCACHE", "GOPATH"}
	out, err := exec.Command("go", append([]string{"env"}, names...)...).Output()
	if err != nil {
		return fmt.Errorf("go env: %w", err)
	}

	for i, value := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		goEnv[names[i]] = value
	}

	mod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return fmt.Errorf("go env GOMOD: %w", err)
	}

	kilnRepo = filepath.Dir(strings.TrimSpace(string(mod)))
	return nil
}

const kilnCommand = "github.com/fwilkerson/kiln/cmd/kiln"

// kilnRepo is the checkout of kiln the test runs in.
var kilnRepo string

// kilnSource is each file the kiln on the PATH is built from, but Go's own packages.
var kilnSource []string

// sourceOf lists the Go and embedded files that pkg is built from, with its dependencies, but Go's own packages.
func sourceOf(pkg string) ([]string, error) {
	format := `{{if not .Standard}}{{range .GoFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}
{{end}}{{end}}`
	out, err := exec.Command("go", "list", "-deps", "-f", format, pkg).Output()
	if err != nil {
		return nil, fmt.Errorf("listing the source of %s: %w", pkg, err)
	}

	return strings.Fields(string(out)), nil
}

// readSource reads kilnSource once. go test caches a test by its binary, and by the files it opens while it runs.
// The scripts run a kiln built apart. Since the test reads kiln's source, go test drops the cached result when kiln
// changes.
var readSource = sync.OnceValue(func() error {
	for _, p := range kilnSource {
		if _, err := os.ReadFile(p); err != nil {
			return err
		}
	}

	return nil
})

// Run runs the script in file, beside the other tests that call Run or RunAll.
func Run(t *testing.T, file string) {
	run(t, testscript.Params{Files: []string{file}})
}

// RunAll runs each script at dir/<feature>/<scenario>.txtar as the test <feature>/<scenario>, beside the others.
func RunAll(t *testing.T, dir string) {
	features, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal("scenariotest:", err)
	}

	for _, feature := range features {
		t.Run(feature.Name(), func(t *testing.T) {
			run(t, testscript.Params{Dir: filepath.Join(dir, feature.Name())})
		})
	}
}

func run(t *testing.T, scripts testscript.Params) {
	t.Parallel()
	if len(goEnv) == 0 {
		t.Fatal("scenariotest: this package's TestMain must call scenariotest.Main(m)")
	}

	if err := readSource(); err != nil {
		t.Fatal("scenariotest:", err)
	}

	waits := screenWaits(t)
	scripts.Setup = func(env *testscript.Env) error { return setUp(env, waits) }
	scripts.RequireExplicitExec, scripts.Cmds = true, terminalCommands
	testscript.Run(t, scripts)
}

// screenWaits answers the context that ends a script's waits for its screen. It ends as testscript ends a script's
// commands, two grace periods before t's deadline, each 5% of the time left. A wait's failure then shows the screen.
func screenWaits(t *testing.T) context.Context {
	deadline, ok := t.Deadline()
	if !ok {
		return context.Background()
	}

	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(-time.Until(deadline)/10))
	t.Cleanup(cancel)
	return ctx
}

// givenDir holds the files a scripted session copies in, which the repository leaves out.
const givenDir = "_given"

func setUp(env *testscript.Env, waits context.Context) error {
	home := env.WorkDir + "-home"
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}

	if err := hometest.Seed(home); err != nil {
		return err
	}

	env.Setenv("HOME", home)
	for name, value := range goEnv {
		env.Setenv(name, value)
	}

	env.Setenv("GOTOOLCHAIN", "local")
	env.Setenv("GOPROXY", "off")
	env.Setenv("GOFLAGS", "-buildvcs=false -trimpath")
	env.Setenv("KILN_REPO", kilnRepo)
	env.Setenv(testPIDEnv, strconv.Itoa(os.Getpid()))
	if err := seed(env.WorkDir); err != nil {
		return err
	}

	excluded := strings.Join([]string{"/" + ClaudeScriptFile, "/" + ClaudeCallsDir + "/", "/" + givenDir + "/"}, `\n`)
	script := "git init --quiet -b main && git config user.name kiln-test && " +
		"git config user.email kiln-test@example.com && printf '" + excluded + `\n' >>.git/info/exclude && ` +
		"git add -A && git commit --quiet --allow-empty -m start"
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir, cmd.Env = env.WorkDir, env.Vars
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scenariotest: making the scratch repository: %w\n%s", err, out)
	}

	t := &terminal{skip: env.T().Skip, waits: waits}
	for _, kv := range env.Vars {
		name, _, _ := strings.Cut(kv, "=")
		t.names = append(t.names, name)
	}

	env.Values[terminalKey{}] = t
	return nil
}
