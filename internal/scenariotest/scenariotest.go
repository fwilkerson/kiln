// Package scenariotest runs kiln's acceptance tests. Each is a testscript file that runs kiln on a scratch
// repository, with a scripted claude as the model.
//
//	func TestMain(m *testing.M) { scenariotest.Main(m) }
//
//	func TestAT1(t *testing.T) { scenariotest.Run(t, "testdata/at1.txtar") }
//
// A script's $WORK is the scratch repository's main checkout, on main. Its first commit holds every file the script
// brings, but claude.json and what is under _given/. A session copies in the work it builds from _given/, which Go's
// ./... leaves out too. HOME is a directory of the script's own, so kiln's worktrees are the script's own too.
// $KILN_REPO is the checkout of kiln the test runs in, for a script that works on kiln's own repository.
//
// On the PATH, kiln is built from cmd/kiln, and claude is the scripted one. It follows $WORK/claude.json, a
// [ClaudeScript], and records each call under $WORK/.claude-calls/<role>/<attempt>/<call>/. The record holds the
// files that [ClaudeCallFile] and the constants beside it name. Go runs offline, from the developer's caches.
//
// A script drives a full-screen program in a pseudo-terminal, such as kiln tui. Its commands are term, keys, click,
// wheel, screen and color. No sandboxed session can open a pseudo-terminal, so such a script runs only outside one.
package scenariotest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// Main runs the tests of m, with kiln and the scripted claude first on the PATH. Call it from TestMain.
func Main(m *testing.M) {
	testscript.Main(tests{m}, map[string]func(){"claude": func() { os.Exit(claudeMain()) }})
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
// The scripts run a kiln built apart. Reading its source lets a change to kiln end the cached result.
var readSource = sync.OnceValue(func() error {
	for _, p := range kilnSource {
		if _, err := os.ReadFile(p); err != nil {
			return err
		}
	}

	return nil
})

// Run runs the script in file, beside the other tests that call Run.
func Run(t *testing.T, file string) {
	t.Parallel()
	if len(goEnv) == 0 {
		t.Fatal("scenariotest: this package's TestMain must call scenariotest.Main(m)")
	}

	if err := readSource(); err != nil {
		t.Fatal("scenariotest:", err)
	}

	testscript.Run(t, testscript.Params{Files: []string{file}, Setup: setUp, RequireExplicitExec: true,
		Cmds: terminalCommands})
}

// givenDir holds the files a scripted session copies in, which the repository leaves out.
const givenDir = "_given"

func setUp(env *testscript.Env) error {
	home := env.WorkDir + "-home"
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}

	env.Setenv("HOME", home)
	for name, value := range goEnv {
		env.Setenv(name, value)
	}

	env.Setenv("GOTOOLCHAIN", "local")
	env.Setenv("GOPROXY", "off")
	env.Setenv("GOFLAGS", "-buildvcs=false")
	env.Setenv("KILN_REPO", kilnRepo)
	excluded := strings.Join([]string{"/" + ClaudeScriptFile, "/" + ClaudeCallsDir + "/", "/" + givenDir + "/"}, `\n`)
	script := "git init --quiet -b main && git config user.name kiln-test && " +
		"git config user.email kiln-test@example.com && printf '" + excluded + `\n' >>.git/info/exclude && ` +
		"git add -A && git commit --quiet --allow-empty -m start"
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir, cmd.Env = env.WorkDir, env.Vars
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scenariotest: making the scratch repository: %w\n%s", err, out)
	}

	t := &terminal{}
	for _, kv := range env.Vars {
		name, _, _ := strings.Cut(kv, "=")
		t.names = append(t.names, name)
	}

	env.Values[terminalKey{}] = t
	return nil
}
