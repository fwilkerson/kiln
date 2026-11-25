package kiln

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/gittest"
	"github.com/fwilkerson/kiln/internal/tui"
	"github.com/fwilkerson/practices/claim"
)

// TestMonitorConfig holds which monitors kiln may run, by the files a monitor's directory holds and its
// monitor.json. A monitor is refused for each rule it breaks, each refusal on a line of its own.
func TestMonitorConfig(t *testing.T) {
	for name, tc := range map[string]struct {
		name                            Spec
		files                           string
		settings, acceptance, testFiles string
		refused                         []string
	}{
		"a command alone":                  {files: "command"},
		"an agent alone":                   {files: "agent.md"},
		"a check, a command and keys":      {files: "check command", settings: `{"hours": 1, "recheck": false, "mustAccept": true}`},
		"a gauge and its line":             {files: "gauge agent.md", settings: `{"atLeast": 10}`},
		"a gauge out of the gate":          {files: "gauge agent.md", settings: `{"atMost": 0, "recheck": false}`},
		"a helper in a subdirectory":       {files: "check agent.md lib/lost lib/notes/why.md"},
		"a file kiln does not read":        {files: "command README.md", refused: []string{"holds README.md, a file kiln does not read: keep only check, gauge"}},
		"chek beside agent.md":             {files: "chek agent.md", refused: []string{"holds chek, a file kiln does not read"}},
		"a name no spec could have":        {name: "Deps", files: "command", refused: []string{".kiln/monitors/Deps/ has a name no spec could have: name it in lower-case"}},
		"half an hour":                     {files: "command", settings: `{"hours": 0.5}`, refused: []string{"monitor.json: "}},
		"a negative interval":              {files: "command", settings: `{"hours": -1}`, refused: []string{"monitor.json: "}},
		"a word for an interval":           {files: "command", settings: `{"hours": "daily"}`, refused: []string{"monitor.json: "}},
		"over the decoder's limit":         {files: "command", settings: `{"hours": 65536}`, refused: []string{"monitor.json: "}},
		"a key kiln does not know":         {files: "check command", settings: `{"drain": true}`, refused: []string{"monitor.json: "}},
		"a check alone":                    {files: "check", refused: []string{"holds no command and no agent.md, so it fixes nothing"}},
		"a check and a gauge":              {files: "check gauge agent.md", settings: `{"atLeast": 10}`, refused: []string{"holds both a check and a gauge"}},
		"a gauge with no line":             {files: "gauge agent.md", refused: []string{"holds a gauge, but its monitor.json names both atLeast and atMost, or neither"}},
		"a gauge with both sides":          {files: "gauge agent.md", settings: `{"atLeast": 1, "atMost": 2}`, refused: []string{"holds a gauge, but its monitor.json names both atLeast and atMost, or neither"}},
		"a line with no gauge":             {files: "check agent.md", settings: `{"atMost": 0}`, refused: []string{"names atLeast or atMost, a gauge's line, but holds no gauge"}},
		"a gauge alone":                    {files: "gauge monitor.json", settings: `{"hours": 1, "atMost": 0}`},
		"a gauge alone, no tests":          {files: "gauge", settings: `{"atMost": 0}`, acceptance: `[]`},
		"a gauge alone with fix keys":      {files: "gauge", settings: `{"atMost": 0, "writesTests": true, "recheck": false, "mustAccept": false}`, refused: []string{"names mustAccept in its monitor.json, a key that steers a fix, but holds no command and no agent.md to fix with: drop mustAccept, or add a command or an agent.md", "names recheck", "names writesTests"}},
		"a line alone":                     {files: "monitor.json", settings: `{"atMost": 0}`, refused: []string{"names atLeast or atMost, a gauge's line, but holds no gauge: drop atLeast and atMost, or add a gauge"}},
		"a monitor.json alone":             {files: "monitor.json", settings: `{"hours": 1}`, refused: []string{"holds no command and no agent.md, so it fixes nothing: add one, or keep a gauge alone"}},
		"tests kiln could not guard":       {files: "command", acceptance: `"{spec}_test/"`, refused: []string{"may not write the tests"}},
		"tests the monitor may write":      {files: "command", settings: `{"writesTests": true}`, acceptance: `"{spec}_test/"`},
		"tests under no path to guard":     {files: "command", acceptance: `[]`, refused: []string{"may not write the tests"}},
		"tests under one of two paths":     {files: "command", acceptance: `["uat/{spec}/", "{spec}_test/"]`, refused: []string{"may not write the tests"}},
		"each rule a monitor breaks":       {name: "Deps", files: "check README", refused: []string{"a name no spec could have", "holds README", "holds no command and no agent.md"}},
		"a verify first":                   {files: "check verify.md agent.md", settings: `{"workflow": "verify-first"}`},
		"one session, named":               {files: "check agent.md", settings: `{"workflow": "build"}`},
		"a workflow kiln does not know":    {files: "check agent.md", settings: `{"workflow": "tdd"}`, refused: []string{`names, in its monitor.json, the workflow "tdd", which kiln does not know: name "build", "build-and-verify", "verify-first", or drop workflow`}},
		"side by side":                     {files: "check verify.md agent.md", settings: `{"workflow": "build-and-verify"}`},
		"side by side with no verify.md":   {files: "check agent.md", settings: `{"workflow": "build-and-verify"}`, refused: []string{"names the workflow build-and-verify, which needs a check, verify.md and agent.md"}},
		"a verify first with no verify.md": {files: "check agent.md", settings: `{"workflow": "verify-first"}`, refused: []string{"names the workflow verify-first, which needs a check, verify.md and agent.md"}},
		"a verify first with no agent.md":  {files: "check verify.md", settings: `{"workflow": "verify-first"}`, refused: []string{"holds no command and no agent.md", "which needs a check, verify.md and agent.md", "names workflow in its monitor.json, a key that steers a fix"}},
		"a verify first of a gauge":        {files: "gauge verify.md agent.md", settings: `{"workflow": "verify-first", "atMost": 0}`, refused: []string{"which needs a check, verify.md and agent.md"}},
		"a verify first with a command":    {files: "check verify.md agent.md command", settings: `{"workflow": "verify-first"}`, refused: []string{"names the workflow verify-first, but holds a command"}},
		"a verify.md no workflow runs":     {files: "check verify.md agent.md", refused: []string{`holds verify.md, which only the workflows build-and-verify and verify-first run: say "workflow": "verify-first"`}},
		"a workflow in a watch alone":      {files: "gauge", settings: `{"atMost": 0, "workflow": "build"}`, refused: []string{"names workflow in its monitor.json, a key that steers a fix"}},
		"a verify first that writes tests": {files: "check verify.md agent.md", settings: `{"workflow": "verify-first", "writesTests": true}`, refused: []string{"names the workflow verify-first and writesTests, but its verify session alone writes the tests: drop writesTests"}},
		"a verify first with no testFiles": {files: "check verify.md agent.md", settings: `{"workflow": "verify-first"}`, testFiles: `[]`, refused: []string{"names the workflow verify-first, but .kiln/kiln.json names no testFiles, so kiln cannot tell the tests from the fix: add testFiles"}},
		"one session with no testFiles":    {files: "check agent.md", testFiles: `[]`},
	} {
		t.Run(name, func(t *testing.T) {
			acceptance, tests := cmpOr(tc.acceptance, `"acceptance/{spec}/"`), cmpOr(tc.testFiles, `["*_test.go"]`)
			c, err := parseConfig([]byte(`{"gate": ["true"], "rounds": 1, "acceptanceTests": ` + acceptance +
				`, "testFiles": ` + tests + `}`))
			if err != nil {
				t.Fatal(err)
			}

			var raw []byte
			if tc.settings != "" {
				raw = []byte(tc.settings)
			}

			name := cmpOr(tc.name, "deps")
			_, got := c.readMonitor(name, monitor{Name: name}.path(""), strings.Fields(tc.files), raw)
			wantRefused(t, got, tc.refused)
		})
	}
}

// wantRefused holds that got holds a refusal for each of want, in order, which says it.
func wantRefused(t *testing.T, got refusals, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("refused %q, want %d refusals: %q", got, len(want), want)
	}

	for i, says := range want {
		if !strings.Contains(got[i].String(), says) {
			t.Errorf("refusal %d is %q, want one that says %q", i+1, got[i], says)
		}
	}
}

// TestMonitorDirs holds that kiln reads each directory under .kiln/monitors/ at a commit as a monitor, by name. One
// with no monitor.json runs daily. A script kiln runs that git holds without the executable bit is refused. So is a
// file directly under .kiln/monitors/, since a monitor is a directory.
func TestMonitorDirs(t *testing.T) {
	t.Parallel()
	h := gittest.New(t)
	r, repo := historyRepo(t, h)
	writeAll(t, repo, map[string]string{configFile: `{"gate": ["true"], "acceptanceTests": "uat/{spec}/"}` + "\n"})
	writeMonitor(t, repo, "b", map[string]string{"command": "true"})
	writeMonitor(t, repo, "a", map[string]string{"check": "true", "agent.md": "Fix it.", "monitor.json": `{"hours": 1}`,
		"notes/lost": "true"})
	h.CommitDir("main", repo)
	_, reads, err := r.monitors(t.Context(), "main")
	monitors := runnable(reads)
	if err != nil || len(monitors) != 2 {
		t.Fatalf("main holds monitors %v, %v; want a and b", reads, err)
	}

	a, b := monitors[0], monitors[1]
	if !claim.Holds(a.Name == "a", a.finder == checkScript, a.agent, a.interval() == time.Hour, a.tree != "") {
		t.Errorf("a reads as %+v, want a check, an agent, every hour, and its directory's ID", a)
	}

	if b.interval() != 24*time.Hour {
		t.Errorf("b, with no monitor.json, runs every %v, want every 24h", b.interval())
	}

	if names, err := r.Takes(t.Context(), "monitor run"); !slices.Equal(names, []string{"a", "b"}) {
		t.Errorf("kiln complete monitor run answers %v, %v; want a and b", names, err)
	}

	if err := os.Chmod(filepath.Join(repo, monitorsDir, "b", "command"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeAll(t, repo, map[string]string{monitorsDir + "/stray": "true\n"})
	h.CommitDir("main", repo)
	_, reads, err = r.monitors(t.Context(), "main")
	for name, want := range map[Spec]string{"a": "", "b": "chmod +x", "stray": "is a file"} {
		_, found, refused := monitorNamed(reads, name)
		said := strings.Contains(fmt.Sprint(refused), want)
		if !claim.Holds(err == nil, found, said, (refused == nil) == (want == "")) {
			t.Errorf("%s reads as refused %v, %v; want it refused, saying %q, while the rest read", name, refused, err,
				want)
		}
	}

	if names, _ := r.Takes(t.Context(), "monitor run"); !slices.Equal(names, []string{"a", "b", "stray"}) {
		t.Errorf("kiln complete monitor run answers %v; want each, since kiln monitor run says why it refuses one",
			names)
	}
}

// TestContractGauge holds that a fix's contract says the gate reads the monitor's gauge only where the gate does.
func TestContractGauge(t *testing.T) {
	for name, tc := range map[string]struct {
		m    monitor
		says bool
	}{
		"a gauge in the gate":     {m: monitor{finder: gaugeScript}, says: true},
		"a gauge out of the gate": {m: monitor{finder: gaugeScript, Recheck: new(false)}},
		"a check in the gate":     {m: monitor{finder: checkScript}},
	} {
		f := &fixing{monitor: tc.m}
		if says := strings.Contains(f.contract("m"), "reads the monitor's gauge"); says != tc.says {
			t.Errorf("%s: the contract says the gate reads the gauge: %v, want %v", name, says, tc.says)
		}
	}
}

// writeMonitor writes the monitor name's directory under .kiln/monitors/ in repo, with each of files by its path.
// Each script kiln runs is executable.
func writeMonitor(t *testing.T, repo string, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(repo, monitorsDir, name)
	writeAll(t, dir, files)
	for _, script := range roleScripts {
		if _, there := files[script]; there {
			if err := os.Chmod(filepath.Join(dir, script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestStatusNeverRan holds that kiln status says once that a monitor never ran, then when it runs.
func TestStatusNeverRan(t *testing.T) {
	h := gittest.New(t)
	r, repo := historyRepo(t, h)
	writeAll(t, repo, map[string]string{configFile: `{"gate": ["true"], "acceptanceTests": "a/{spec}/"}`})
	writeMonitor(t, repo, "deps", map[string]string{checkScript: checkHealthy, commandScript: "true\n"})
	h.CommitDir("main", repo)
	var out strings.Builder
	err := r.monitorStatus(t.Context(), &out)
	const want = "monitors:\ndeps: never ran, runs at the next kiln monitor run\n"
	if err != nil || out.String() != want {
		t.Errorf("kiln status shows the monitors:\n%s\n%v; want:\n%s", out.String(), err, want)
	}
}

// TestMonitorRunRefused holds that kiln monitor run runs each monitor kiln can read, beside one it refuses. It says
// why it refused that one, and notifies of the refusal once. A run by the refused one's name refuses with the refusal.
// kiln status and kiln tui list it as refused, with why, and the gauges still read the healthy monitor's.
func TestMonitorRunRefused(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	writeAll(t, repo, map[string]string{configFile: `{"gate": ["true"], "acceptanceTests": "a/{spec}/", ` +
		`"notify": "printf '%s\\n' \"$KILN_NOTICE\" >>.git/notices"}`})
	writeMonitor(t, repo, "good", map[string]string{gaugeScript: "echo 0\n", commandScript: "true\n",
		settingsFile: `{"atMost": 0}`})
	writeMonitor(t, repo, "bad", map[string]string{checkScript: checkHealthy, commandScript: "true\n"})
	shell(t, repo, "chmod -x .kiln/monitors/bad/check && git add -A && git commit --quiet -m start")
	const why = "run chmod +x .kiln/monitors/bad/check, and commit it"
	for run, good := range []string{"good: healthy", "good: not due"} {
		var out strings.Builder
		err := r.MonitorRun(t.Context(), "", &out)
		notices := shell(t, repo, "cat .git/notices || true")
		if !claim.Holds(err == nil, strings.Contains(out.String(), good), strings.Contains(out.String(),
			"bad: refused: "), strings.Contains(out.String(), why), strings.Count(notices, `"name":"bad"`) == 1) {
			t.Errorf("run %d: %v, and kiln said:\n%s\nnotices:\n%s\nwant %q, bad refused saying %q, and one "+
				"notice of it", run+1, err, out.String(), notices, good, why)
		}
	}

	if err := r.MonitorRun(t.Context(), "bad", io.Discard); !strings.Contains(fmt.Sprint(err), why) {
		t.Errorf("kiln monitor run bad: %v; want it refused, saying %q", err, why)
	}

	var status strings.Builder
	err := r.monitorStatus(t.Context(), &status)
	if !claim.Holds(err == nil, strings.Contains(status.String(), "bad: refused\n"),
		strings.Contains(status.String(), why)) {
		t.Errorf("kiln status shows the monitors:\n%s\n%v; want bad refused, saying %q", status.String(), err, why)
	}

	shown, err := view{r}.Monitors(t.Context())
	bad := tui.Spec{Monitor: &tui.Monitor{}}
	if i := slices.IndexFunc(shown, func(s tui.Spec) bool { return s.Name == "bad" }); i >= 0 {
		bad = shown[i]
	}

	if !claim.Holds(err == nil, bad.State == tui.Refused, bad.Monitor.Result == foundRefused,
		strings.Contains(bad.Monitor.Summary, why)) {
		t.Errorf("kiln tui shows the monitors %+v, %v; want bad refused, saying %q", shown, err, why)
	}

	_, gauges, err := r.gauges(t.Context())
	var names []string
	for _, g := range gauges {
		names = append(names, string(g.Name))
	}

	if !claim.Holds(err == nil, slices.Equal(names, []string{"good"})) {
		t.Errorf("the gauges read are %v, %v; want good's", names, err)
	}
}

// TestMonitorConflict holds that kiln refuses a monitor at main's tip whose script or monitor.json holds a conflict's
// marker. kiln init leaves one there in a merge. It is refused as an agent's file that holds one is.
func TestMonitorConflict(t *testing.T) {
	for file, marker := range map[string]string{commandScript: "<<<<<<< ours", settingsFile: ">>>>>>> theirs"} {
		t.Run(file, func(t *testing.T) {
			h := gittest.New(t)
			r, repo := historyRepo(t, h)
			writeAll(t, repo, map[string]string{configFile: `{"gate": ["true"], "acceptanceTests": "a/{spec}/"}`})
			files := map[string]string{commandScript: "true\n", settingsFile: "{}\n"}
			files[file] += marker + "\n"
			writeMonitor(t, repo, "deps", files)
			h.CommitDir("main", repo)
			_, reads, err := r.monitors(t.Context(), "main")
			if err == nil {
				_, _, err = monitorNamed(reads, "deps")
			}

			want := fmt.Sprintf("%s holds a conflict's marker, %q: resolve the conflict, then commit the file",
				monitor{Name: "deps"}.path(file), marker)
			if fmt.Sprint(err) != want {
				t.Errorf("kiln reads the monitors: %v; want %s", err, want)
			}
		})
	}
}

// writeAll writes each of files under dir, by its path from dir, with the directories it needs.
func writeAll(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		target := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDue holds when a monitor is due. It is when it never ran, its check couldn't run, or a usage limit ended its
// fix. It is when its fix was cut short. Otherwise, it is due once its interval, less half an hour, has passed.
func TestDue(t *testing.T) {
	ran := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	daily := monitor{Name: "deps", Hours: defaultHours}
	for name, tc := range map[string]struct {
		found, outcome string
		later          time.Duration
		due, past      bool
	}{
		"healthy a moment ago":              {found: foundHealthy, later: time.Minute},
		"its check ran past the timeout":    {found: foundCouldNot, past: true, later: time.Minute},
		"that, its interval later":          {found: foundCouldNot, past: true, later: 24 * time.Hour, due: true},
		"healthy, its interval less slack":  {found: foundHealthy, later: 23*time.Hour + 30*time.Minute, due: true},
		"healthy, just short of that":       {found: foundHealthy, later: 23*time.Hour + 29*time.Minute},
		"its check couldn't run":            {found: foundCouldNot, later: time.Minute, due: true},
		"held a moment ago":                 {found: foundHeld, later: time.Minute},
		"a fix a usage limit ended":         {found: foundFix, outcome: outLimit, later: time.Minute, due: true},
		"a fix that lost its connection":    {found: foundFix, outcome: outOffline, later: time.Minute, due: true},
		"a fix cut short":                   {found: foundFix, outcome: outDropped, later: time.Minute, due: true},
		"a gated check couldn't run":        {found: foundFix, outcome: outCouldNot, later: time.Minute, due: true},
		"a session that failed":             {found: foundFix, outcome: outFailed, later: time.Minute},
		"a session that failed, a day ago":  {found: foundFix, outcome: outFailed, later: 24 * time.Hour, due: true},
		"a fix that landed a moment ago":    {found: foundFix, outcome: outLanded, later: time.Minute},
		"a fix that stopped, its interval":  {found: foundFix, outcome: outStopped, later: 24 * time.Hour, due: true},
		"a fix waiting on the developer":    {found: foundFix, outcome: outWaits, later: time.Minute},
		"never run":                         {due: true},
		"a fix that changed nothing, today": {found: foundFix, outcome: outUnchanged, later: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			var entries []entry
			if tc.found != "" {
				line := entry{Kind: monitorKind, Monitor: "deps", At: ran, Found: tc.found, Build: "b", Past: tc.past}
				entries = append(entries, line)
			}

			if tc.outcome != "" {
				entries = append(entries, entry{Kind: outcomeKind, Build: "b", Outcome: tc.outcome})
			}

			if due := histories(entries)["deps"].due(daily, ran.Add(tc.later)); due != tc.due {
				t.Errorf("due %v, want %v", due, tc.due)
			}
		})
	}
}

// TestStuck holds that a check or gauge past the timeout holds its monitor until main or its directory changes. A run
// held for it keeps it held, and any other run since releases it.
func TestStuck(t *testing.T) {
	stuck := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundCouldNot, Past: true}
	heldFor := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundHeld, Past: true}
	healthy := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundHealthy}
	for name, tc := range map[string]struct {
		lines     []entry
		main, ent string
		held      bool
	}{
		"the same main and entry":     {lines: []entry{stuck}, main: "a", ent: "e", held: true},
		"main moved":                  {lines: []entry{stuck}, main: "b", ent: "e"},
		"its entry changed":           {lines: []entry{stuck}, main: "a", ent: "f"},
		"a run held for it since":     {lines: []entry{stuck, heldFor}, main: "a", ent: "e", held: true},
		"a run that finished since":   {lines: []entry{stuck, healthy}, main: "a", ent: "e"},
		"a check that never ran past": {lines: []entry{healthy}, main: "a", ent: "e"},
	} {
		now := entry{Main: git.Rev(tc.main), Entry: tc.ent}
		if held := histories(tc.lines)["m"].stuckAt(now); held != tc.held {
			t.Errorf("%s: held %v, want %v", name, held, tc.held)
		}
	}
}

// TestHold holds what releases a monitor its last fix holds. That is a change to main's tip, its entry, or what its
// check or gauge printed. The fix's outcome and the monitor's check or gauge say which.
func TestHold(t *testing.T) {
	gated := monitor{Name: "m", finder: checkScript, command: true}
	outside := monitor{Name: "m", finder: checkScript, agent: true, Recheck: new(false)}
	unchecked := monitor{Name: "m", agent: true}
	gauged := monitor{Name: "m", finder: gaugeScript, agent: true}
	for name, tc := range map[string]struct {
		m                    monitor
		outcome              string
		main, entry, printed bool
		held                 bool
	}{
		"stopped, nothing changed":           {m: gated, outcome: outStopped, held: true},
		"stopped, main moved":                {m: gated, outcome: outStopped, main: true},
		"stopped, its entry changed":         {m: gated, outcome: outStopped, entry: true},
		"stopped, its check printed another": {m: gated, outcome: outStopped, printed: true},
		"rejected, nothing changed":          {m: gated, outcome: outRejected, held: true},
		"rejected, main moved":               {m: gated, outcome: outRejected, main: true},
		"asked, main moved":                  {m: outside, outcome: outNeedsYou, main: true, held: true},
		"asked, its check printed another":   {m: outside, outcome: outNeedsYou, printed: true},
		"asked, its entry changed":           {m: outside, outcome: outNeedsYou, entry: true},
		"asked with no check, main moved":    {m: unchecked, outcome: outNeedsYou, main: true},
		"asked with no check, nothing moved": {m: unchecked, outcome: outNeedsYou, held: true},
		"unchanged, main moved":              {m: outside, outcome: outUnchanged, main: true, held: true},
		"unchanged, its check printed other": {m: outside, outcome: outUnchanged, printed: true},
		"unchanged with no check":            {m: unchecked, outcome: outUnchanged},
		"landed, its check out of the gate":  {m: outside, outcome: outLanded, main: true, held: true},
		"landed, and its check printed more": {m: outside, outcome: outLanded, printed: true},
		"landed, its check in the gate":      {m: gated, outcome: outLanded},
		"its session failed":                 {m: gated, outcome: outFailed},
		"a usage limit":                      {m: gated, outcome: outLimit},
		"cut short":                          {m: gated, outcome: outDropped},
		"its gated check couldn't run":       {m: gated, outcome: outCouldNot},
		"landed, its gauge printed the same": {m: gauged, outcome: outLanded, main: true, held: true},
		"landed, its gauge printed another":  {m: gauged, outcome: outLanded, main: true, printed: true},
		"landed, its gauge's entry changed":  {m: gauged, outcome: outLanded, main: true, entry: true},
		"unchanged, its gauge the same":      {m: gauged, outcome: outUnchanged, main: true, held: true},
		"asked, its gauge the same":          {m: gauged, outcome: outNeedsYou, main: true, held: true},
	} {
		t.Run(name, func(t *testing.T) {
			was := entry{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b", Main: "a", Entry: "e",
				Printed: "p"}
			now := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Printed: "p"}
			for _, change := range []struct {
				changed bool
				field   *string
			}{{tc.main, (*string)(&now.Main)}, {tc.entry, &now.Entry}, {tc.printed, &now.Printed}} {
				if change.changed {
					*change.field += "'"
				}
			}

			h := histories([]entry{was, {Kind: outcomeKind, Build: "b", Outcome: tc.outcome}})["m"]
			if held := h.holds(tc.m, now) != ""; held != tc.held {
				t.Errorf("held %v, want %v", held, tc.held)
			}
		})
	}
}

// TestWatchHold holds what releases a monitor that fixes nothing from its last needs you. That is a change to its entry
// or to what its gauge printed, or a healthy run since. A move of main's tip alone releases none.
func TestWatchHold(t *testing.T) {
	watching := monitor{Name: "m", finder: gaugeScript}
	needed := entry{Kind: monitorKind, Monitor: "m", Found: outNeedsYou, Main: "a", Entry: "e", Printed: "p"}
	healthy := entry{Kind: monitorKind, Monitor: "m", Found: foundHealthy, Main: "a", Entry: "e", Printed: "q"}
	for name, tc := range map[string]struct {
		lines []entry
		now   entry
		held  bool
	}{
		"the same reading, main moved":  {lines: []entry{needed}, now: entry{Main: "b", Entry: "e", Printed: "p"}, held: true},
		"another reading":               {lines: []entry{needed}, now: entry{Main: "a", Entry: "e", Printed: "r"}},
		"its entry changed":             {lines: []entry{needed}, now: entry{Main: "a", Entry: "f", Printed: "p"}},
		"the same, after a healthy run": {lines: []entry{needed, healthy}, now: entry{Main: "a", Entry: "e", Printed: "p"}},
	} {
		if held := histories(tc.lines)["m"].holds(watching, tc.now) != ""; held != tc.held {
			t.Errorf("%s: held %v, want %v", name, held, tc.held)
		}
	}
}

// TestCheck holds how kiln reads a check: its last line of stdout says whether to fix. A non-zero exit, or a last
// line that is no such object, couldn't run. kiln then keeps the last line it printed on either stream.
func TestCheck(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		command        string
		ran, fix       bool
		summary, found string
		limit          time.Duration
	}{
		"healthy":                {command: `echo '{"fix": false, "summary": "fine"}'`, ran: true, summary: "fine"},
		"a finding above it":     {command: `echo CVE-1; echo '{"fix": true, "summary": "one"}'`, ran: true, fix: true, summary: "one", found: "CVE-1"},
		"progress on stderr":     {command: `echo working >&2; echo '{"fix": true, "summary": "s"}'`, ran: true, fix: true, summary: "s"},
		"a non-zero exit":        {command: `echo '{"fix": true, "summary": "s"}'; sleep 0.2; echo dial tcp: refused >&2; exit 1`, summary: "dial tcp: refused"},
		"a last line of text":    {command: `echo all good`, summary: "the check's last line, \"all good\", is no verdict" + noVerdictFix},
		"an object with no fix":  {command: `echo '{"summary": "s"}'`, summary: `the check's last line, "{\"summary\": \"s\"}", is no verdict` + noVerdictFix},
		"nothing at all":         {command: `true`, summary: "the check printed nothing" + noVerdictFix},
		"an exit with no output": {command: `exit 3`, summary: "the check failed: exit status 3"},
		"one that runs past":     {command: `echo '{"fix": true, "summary": "s"}'; sleep 30`, limit: 500 * time.Millisecond, summary: "the check ran past kiln.json's timeout of 500ms, so kiln killed it"},
	} {
		t.Run(name, func(t *testing.T) {
			s := config{Timeout: timeout(tc.limit)}.shell(os.Environ())
			c, err := s.runMonitorCheck(t.Context(), git.Worktree(t.TempDir()), tc.command)
			if err != nil {
				t.Fatal(err)
			}

			type found struct {
				ran, fix       bool
				summary, found string
			}
			got := found{ran: c.ran, fix: c.fix, summary: c.summary, found: c.found}
			want := found{ran: tc.ran, fix: tc.fix, summary: tc.summary, found: tc.found}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestAsk holds how kiln reads an agent's reply that asks for the developer. It holds how a session's end ends a fix.
func TestAsk(t *testing.T) {
	for name, tc := range map[string]struct {
		text    string
		failed  bool
		outcome string
		summary string
	}{
		"a reply that asks":         {text: "It needs a person.\n{\"ask\": true, \"summary\": \"look\"}", outcome: outNeedsYou, summary: "look"},
		"a reply that fixed it":     {text: "Done."},
		"an ask that is false":      {text: `{"ask": false, "summary": "no"}`},
		"a session that failed":     {text: "OAuth token has expired", failed: true, outcome: outFailed, summary: "OAuth token has expired"},
		"a failure with no result":  {failed: true, outcome: outFailed, summary: "kiln: not signed in"},
		"an ask inside other lines": {text: "{\"ask\": true, \"summary\": \"x\"}\nThen I fixed it."},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fixing{}
			ended := f.endedBy(claudeEnded(tc.text, tc.failed), "line 1\nkiln: not signed in\n")
			type ending struct {
				ended            bool
				outcome, summary string
			}
			got, want := ending{ended, f.outcome, f.summary}, ending{tc.outcome != "", tc.outcome, tc.summary}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}

			if tc.outcome == outNeedsYou && !strings.Contains(f.report, "needs a person") {
				t.Errorf("the report is %q, want the reply above its last line", f.report)
			}
		})
	}
}

// TestTitle holds which titles an agent's reply gives the commit that lands its fix.
func TestTitle(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"a title on the last line": {text: "Fixed it.\n{\"title\": \"fix(tui): keep each row linear\"}",
			want: "fix(tui): keep each row linear"},
		"one with no scope":         {text: `{"title": "perf: cache the gauge rows"}`, want: "perf: cache the gauge rows"},
		"a reply with no title":     {text: "Fixed it."},
		"a title above other lines": {text: "{\"title\": \"fix(tui): x\"}\nThen I checked it."},
		"a title of 73 characters":  {text: `{"title": "fix(tui): ` + strings.Repeat("x", 63) + `"}`},
		"a title of 72 characters": {text: `{"title": "fix(tui): ` + strings.Repeat("x", 62) + `"}`,
			want: "fix(tui): " + strings.Repeat("x", 62)},
		"a title with no type": {text: `{"title": "Fixed the gauge rows"}`},
		"a title of two lines": {text: `{"title": "fix(tui): x\n\nmore"}`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := titleOf(tc.text); got != tc.want {
				t.Errorf("titleOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// claudeEnded is a session's end, with text as its result.
func claudeEnded(text string, failed bool) claude.Ended {
	return claude.Ended{Text: text, Failed: failed}
}

// TestNamedRun holds that kiln monitor run <name> runs held or not, but never starts a second fix of a monitor.
func TestNamedRun(t *testing.T) {
	m := monitor{Name: "m", finder: checkScript, command: true}
	afterStop := histories([]entry{{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b"},
		{Kind: outcomeKind, Build: "b", Outcome: outStopped}})["m"]
	for name, tc := range map[string]struct {
		named bool
		rec   *record
		held  bool
	}{
		"due, after a fix that stopped":     {rec: &record{Name: "m", State: stopped, Monitor: true}, held: true},
		"named, after a fix that stopped":   {named: true, rec: &record{Name: "m", State: stopped, Monitor: true}},
		"named, while a fix waits":          {named: true, rec: &record{State: waiting, Monitor: true}, held: true},
		"named, while an accepted fix is":   {named: true, rec: &record{State: queued, Accepted: true}, held: true},
		"named, with a stopped landing":     {named: true, rec: &record{State: stopped, Accepted: true}, held: true},
		"due, while a fix waits, unchanged": {rec: &record{State: waiting, Monitor: true}, held: true},
	} {
		t.Run(name, func(t *testing.T) {
			run := &monitorRunning{named: tc.named}
			if held := run.held(m, afterStop, tc.rec, entry{}); held != tc.held {
				t.Errorf("held %v, want %v", held, tc.held)
			}
		})
	}
}

// TestAcceptedLandingStops holds that an accepted fix's landing only lands or stops. An error of any kind stops it,
// so its record and branch stay for kiln accept. So does a session that asks for the developer, or fails, with its
// summary and report as why. Only an interrupt leaves it as it was.
func TestAcceptedLandingStops(t *testing.T) {
	for name, tc := range map[string]struct {
		accepted bool
		err      error
		outcome  string
		why      string
	}{
		"a landing whose check couldn't run":  {accepted: true, err: &checkFailed{line: "dial tcp"}, why: "dial tcp"},
		"a landing kiln could not go on with": {accepted: true, err: errors.New("no disk"), why: "no disk"},
		"a landing that lost its connection": {accepted: true, err: &lostConnection{why: "Connection error"},
			why: "Connection error"},
		"a landing whose session asks":       {accepted: true, outcome: outNeedsYou, why: "look\nthe report"},
		"a landing whose session fails":      {accepted: true, outcome: outFailed, why: "look\nthe report"},
		"a landing that landed":              {accepted: true, outcome: outLanded},
		"a fix, not accepted, that couldn't": {err: &checkFailed{line: "dial tcp"}},
		"a fix, not accepted, that asks":     {outcome: outNeedsYou},
	} {
		t.Run(name, func(t *testing.T) {
			b := &build{rec: record{Accepted: tc.accepted}, fix: &fixing{}}
			b.fix.end(tc.outcome, "look", "the report")
			stop, stops := errors.AsType[*outOfRounds](b.landingStops(t.Context(), tc.err))
			why := ""
			if stops {
				why = stop.Why
			}

			if stops != (tc.why != "") || !strings.HasSuffix(why, tc.why) {
				t.Errorf("stops %v, as %q; want a stop %v, as %q", stops, why, tc.why != "", tc.why)
			}
		})
	}
}

// TestMonitorStatusLines holds what kiln status shows below a monitor's line. A stopped fix shows what failed. The
// next step follows the fix's record where the book holds one, such as an accepted landing that stopped.
func TestMonitorStatusLines(t *testing.T) {
	stoppedFix := standing{outcome: outStopped, summary: "3 updates",
		report: "the gate failed:\nacceptance/a/x refused"}
	for name, tc := range map[string]struct {
		s    standing
		rec  *record
		want string
	}{
		"a stopped fix shows what failed": {s: stoppedFix, rec: &record{Name: "m", State: stopped, Monitor: true},
			want: "acceptance/a/x refused"},
		"and is tended by name": {s: stoppedFix, rec: &record{Name: "m", State: stopped, Monitor: true},
			want: "kiln tend m"},
		"an accepted landing that stopped is accepted again": {s: stoppedFix,
			rec: &record{Name: "m", State: stopped, Accepted: true, Monitor: true}, want: "kiln accept m"},
		"a waiting fix is accepted or rejected": {s: standing{outcome: outWaits},
			rec: &record{Name: "m", State: waiting, Monitor: true}, want: "kiln reject m"},
		"an agent's report stays out of status": {s: standing{result: outNeedsYou, outcome: outNeedsYou, fixed: true, summary: "look",
			report: "the long report"}, want: "kiln tend m"},
		"a refused monitor is tended":     {s: standing{result: foundRefused, summary: "why"}, want: "kiln tend m"},
		"one that couldn't run is tended": {s: standing{result: "held: " + foundCouldNot}, want: "kiln tend m"},
	} {
		t.Run(name, func(t *testing.T) {
			lines := strings.Join(tc.s.lines("m", tc.rec), "\n")
			if !strings.Contains(lines, tc.want) {
				t.Errorf("%q holds no %q", lines, tc.want)
			}

			if tc.s.outcome == outNeedsYou && strings.Contains(lines, tc.s.report) {
				t.Errorf("%q holds the agent's report", lines)
			}
		})
	}
}

// TestMonitorState holds where kiln tui shows a monitor standing, by its last runs. A hold keeps what held it.
func TestMonitorState(t *testing.T) {
	ran := func(found string) entry { return entry{Kind: monitorKind, Monitor: "m", Found: found} }
	fix, past := ran(foundFix), ran(foundHeld)
	fix.Build, past.Past = "b", true
	ended := func(outcome string) entry { return entry{Kind: outcomeKind, Build: "b", Outcome: outcome} }
	for name, tc := range map[string]struct {
		lines []entry
		want  tui.State
	}{
		"never ran":                {want: tui.NeverRun},
		"healthy":                  {lines: []entry{ran(foundHealthy)}, want: tui.Healthy},
		"its check failed":         {lines: []entry{ran(foundCouldNot)}, want: tui.CouldNotRun},
		"held past a timeout":      {lines: []entry{past}, want: tui.CouldNotRun},
		"a fix that needs you":     {lines: []entry{fix, ended(outNeedsYou)}, want: tui.NeedsYou},
		"held by a fix that waits": {lines: []entry{fix, ended(outWaits), ran(foundHeld)}, want: tui.Waits},
	} {
		t.Run(name, func(t *testing.T) {
			s := histories(tc.lines)["m"].standing(monitor{Name: "m"}, nil)
			if got := s.state(); got != tc.want {
				t.Errorf("stands %v, as %q; want %v", got, s.result, tc.want)
			}
		})
	}
}

// TestAcceptedLanding holds that a monitor with an accepted fix stands as the book holds the fix. The ledger still
// says the fix waits.
func TestAcceptedLanding(t *testing.T) {
	h := histories([]entry{{Kind: monitorKind, Monitor: "docs", Found: foundFix, Build: "b"},
		{Kind: outcomeKind, Build: "b", Outcome: outWaits}})["docs"]
	for name, tc := range map[string]struct {
		landing state
		want    tui.State
	}{
		"its landing queued": {landing: queued, want: tui.Queued},
		"its landing builds": {landing: building, want: tui.Halted},
	} {
		t.Run(name, func(t *testing.T) {
			s := h.standing(monitor{Name: "docs"}, &record{Name: "docs", State: tc.landing, Accepted: true,
				Monitor: true, Build: "b"})
			if got := s.state(); got != tc.want || strings.Contains(s.result, "waiting") {
				t.Errorf("stands %q, as %q; want %q, and no wait on the developer", got, s.result, tc.want)
			}
		})
	}
}

// TestLandingSession holds that kiln status names why the session of an accepted fix's landing runs, while it runs.
func TestLandingSession(t *testing.T) {
	conflict := entry{Kind: startKind, Build: "b", Session: "s",
		Why: whyConflict, Round: 1, Rounds: 3}
	for name, tc := range map[string]struct {
		lines []entry
		want  string
	}{
		"while the session works":  {lines: []entry{conflict}, want: "accepted, landing: conflict with main, round 1 of 3"},
		"once its turn has ended":  {lines: []entry{conflict, {Kind: endKind, Build: "b", Session: "s"}}, want: "accepted, landing"},
		"before the session works": {want: "accepted, landing"},
	} {
		t.Run(name, func(t *testing.T) {
			entries := append([]entry{{Kind: monitorKind, Monitor: "docs", Found: foundFix, Build: "b"},
				{Kind: outcomeKind, Build: "b", Outcome: outWaits}}, tc.lines...)
			rec := &record{Name: "docs", State: building, Accepted: true, Monitor: true, Build: "b",
				PID: os.Getpid()}
			if got := histories(entries)["docs"].standing(monitor{Name: "docs"}, rec).result; got != tc.want {
				t.Errorf("kiln status shows %q; want %q", got, tc.want)
			}
		})
	}
}
