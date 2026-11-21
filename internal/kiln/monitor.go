package kiln

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// monitorsDir holds a directory for each monitor, named for the monitor.
const monitorsDir = kilnDir + "/monitors"

// The files of a monitor's directory. kiln runs each script by its path, at the top of a worktree. So its first line
// picks its language, and git holds it with the executable bit.
const (
	// settingsFile is how often the monitor runs, its gauge's line, and what its fix may do.
	settingsFile = "monitor.json"
	// checkScript finds a problem. Its last line of stdout is a verdict.
	checkScript = "check"
	// gaugeScript finds a problem by a number, the monitor's gauge. kiln reads it after each landing too.
	gaugeScript = "gauge"
	// commandScript fixes the problem with no model, outside the sandbox.
	commandScript = "command"
	// monitorAgent is the agent that fixes the problem in a session, written as any agent's file is.
	monitorAgent = "agent.md"
)

// roleScripts are the files of a monitor's directory that kiln runs.
var roleScripts = []string{checkScript, gaugeScript, commandScript}

// roleFiles are the files kiln reads directly in a monitor's directory. Any other file there sits in a subdirectory.
var roleFiles = slices.Concat(roleScripts, []string{monitorAgent, settingsFile})

// monitor is a directory under .kiln/monitors/, which kiln monitor run runs once its interval has passed. Its check or
// its gauge finds a problem, and its command, its agent or both fix it. One with a gauge alone fixes nothing: it
// watches the gauge.
type monitor struct {
	// Name is the directory's name, by a spec's name's rule. Its fix builds on kiln/<name>.
	Name Spec
	settings
	// finder is checkScript or gaugeScript, whichever the directory holds, or "" for neither.
	finder string
	// command and agent say whether the directory holds commandScript and monitorAgent.
	command, agent bool
	// tree is git's ID of the directory, which a change to any file in it changes. The hold compares it.
	tree string
}

// settings is how a monitor runs, and what its fix may do, in .kiln/monitors/<name>/monitor.json. A monitor with a
// gauge names atLeast or atMost, and every other key is optional. The keys recheck, mustAccept and writesTests apply
// to a fix. kiln refuses each of them in a monitor with no command and no agent.md, since that monitor makes no fix.
type settings struct {
	// Each field's doc is the hover an editor shows for its key, as TestSchema builds it. So each stands alone.

	// Schema is the path of the JSON Schema that an editor checks this file against: "../../monitor.schema.json".
	// kiln init writes that schema to .kiln/. kiln ignores this key.
	Schema string `json:"$schema,omitempty"`
	// Hours is the number of whole hours between runs of the monitor, from 0 to 65535, such as 1. With 0, every
	// kiln monitor run runs the monitor. Absent, it is 24.
	Hours uint16 `json:"hours"`
	// Recheck says whether the gate of the monitor's fix runs the monitor's check or gauge again. Absent or true, the
	// gate runs it, and fails while it still finds the problem. Set it false when no fix clears the problem until the
	// fix is deployed. kiln refuses recheck in a monitor with no command and no agent.md.
	Recheck *bool `json:"recheck"`
	// MustAccept, when true, keeps each fix that passes on its branch until the developer runs kiln accept on it.
	// Absent or false, a fix that passes lands on main. kiln refuses mustAccept in a monitor with no command and no
	// agent.md.
	MustAccept bool `json:"mustAccept"`
	// WritesTests, when true, lets a fix write the acceptance tests of every spec. Those are the files under the
	// part of each acceptance path before {spec}, such as uat/ for "uat/{spec}/". Absent or false, a fix writes
	// nothing there. kiln then refuses the monitor while kiln.json's acceptance is absent, or names a path that
	// starts with {spec}. kiln refuses writesTests in a monitor with no command and no agent.md.
	WritesTests bool `json:"writesTests"`
	// AtLeast is the lowest reading of the monitor's gauge that needs no fix, such as 80. A reading below it starts
	// a fix. In a monitor with no command and no agent.md, such a reading notifies the developer instead. A monitor
	// with a gauge names atLeast or atMost, never both. kiln refuses atLeast in a monitor with no gauge.
	AtLeast *float64 `json:"atLeast"`
	// AtMost is the highest reading of the monitor's gauge that needs no fix, such as 0. A reading above it starts a
	// fix. In a monitor with no command and no agent.md, such a reading notifies the developer instead. A monitor
	// with a gauge names atLeast or atMost, never both. kiln refuses atMost in a monitor with no gauge.
	AtMost *float64 `json:"atMost"`
}

// line is the gauge's line that s names.
func (s settings) line() gaugeLine {
	return gaugeLine{AtLeast: s.AtLeast, AtMost: s.AtMost}
}

// defaultHours is the hours of a monitor whose monitor.json names none, or which holds no monitor.json.
const defaultHours = 24

// interval is how often m runs.
func (m monitor) interval() time.Duration {
	return time.Duration(m.Hours) * time.Hour
}

// gated is whether m's check or gauge joins its fix's gate.
func (m monitor) gated() bool {
	return m.finds() && (m.Recheck == nil || *m.Recheck)
}

// finds is whether m has a finding to compare: what its check or its gauge printed.
func (m monitor) finds() bool {
	return m.finder != ""
}

// fixes is whether m fixes what it finds, with its command or its agent. One that does not watches its gauge.
func (m monitor) fixes() bool {
	return m.command || m.agent
}

// path is the path of file in m's directory, from the repository's top.
func (m monitor) path(file string) string {
	return path.Join(monitorsDir, string(m.Name), file)
}

// reading is value, a reading of m's gauge, as a ledger's line holds it: named for m, beside m's line. A nil value is
// unknown.
func (m monitor) reading(value *float64) entry {
	return entry{Gauge: string(m.Name), Value: value, gaugeLine: m.line()}
}

// hashOf is a short hash of text.
func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// monitors reads kiln.json at the commit at, where work was queued or started, and the monitors there. It reads each
// file as pastFiles does, and each monitor as monitorsIn does.
func (r *Repo) monitors(ctx context.Context, at git.Rev) (config, []monitorRead, error) {
	c, err := r.config(ctx, at)
	if err != nil {
		return c, nil, err
	}

	reads, err := r.monitorsIn(ctx, c, r.pastFiles(at), at)
	return c, reads, err
}

// mainMonitors answers main's tip and kiln.json there, as mainConfig reads them, and the monitors there, as
// monitorsIn reads them.
func (r *Repo) mainMonitors(ctx context.Context) (git.Rev, config, []monitorRead, error) {
	main, c, err := r.mainConfig(ctx)
	if err != nil {
		return main, c, nil, err
	}

	reads, err := r.monitorsIn(ctx, c, r.filesAt(main), main)
	return main, c, reads, err
}

// monitorsIn reads each entry under .kiln/monitors/ at the commit at, by name. c is kiln.json there, and f reads the
// files there. It reads each as a monitorRead, so one monitor kiln refuses never keeps it from the rest.
func (r *Repo) monitorsIn(ctx context.Context, c config, f files, at git.Rev) ([]monitorRead, error) {
	dirs, err := r.monitorDirs(ctx, at)
	if err != nil {
		return nil, err
	}

	reads := make([]monitorRead, len(dirs))
	for i, d := range dirs {
		if reads[i], err = c.readAt(ctx, f, d); err != nil {
			return nil, err
		}
	}

	return reads, nil
}

// monitorRead is an entry under .kiln/monitors/ in a commit, by its name and git's ID of it. It holds the monitor kiln
// runs, or refused: why kiln refuses the entry, each refusal with its fix. Read the monitor only where refused is nil.
type monitorRead struct {
	name    Spec
	tree    string
	monitor monitor
	refused error
}

// readAt reads d, as f holds it, as a monitorRead. A file in a monitor's place is refused. It answers an error only
// where git fails.
func (c config) readAt(ctx context.Context, f files, d monitorDir) (monitorRead, error) {
	read := monitorRead{name: d.name, tree: d.tree}
	if d.file {
		read.refused = refusals{strayMonitor(string(d.name))}
		return read, nil
	}

	m, err := c.readDir(ctx, f, d)
	var refused refusals
	if err := refused.add(err); err != nil {
		return read, err
	}

	read.monitor, read.refused = m, refused.err()
	return read, nil
}

// monitorDir is a monitor's directory in a commit or the working tree. It holds the directory's name, its files, and
// git's ID of it in a commit. plain is a script of roleScripts that is not executable, or "". file is whether a
// commit holds a file in the directory's place.
type monitorDir struct {
	name        Spec
	tree, plain string
	files       []string
	file        bool
}

// monitorDirs lists each monitor's directory under .kiln/monitors/ at the commit at, by name, with each file under
// it. It lists each file directly under .kiln/monitors/ too, as a file, since a monitor is a directory.
func (r *Repo) monitorDirs(ctx context.Context, at git.Rev) ([]monitorDir, error) {
	entries, err := r.git.Tree(ctx, at, monitorsDir)
	if err != nil {
		return nil, err
	}

	var dirs []monitorDir
	files, plain := map[string][]string{}, map[string]string{}
	for _, e := range entries {
		name, file, inside := strings.Cut(e.Path, "/")
		switch {
		case !inside:
			dirs = append(dirs, monitorDir{name: Spec(name), tree: e.ID, file: !e.Dir})
		case !e.Dir:
			files[name] = append(files[name], file)
			if !e.Exec && slices.Contains(roleScripts, file) {
				plain[name] = file
			}
		}
	}

	for i, d := range dirs {
		dirs[i].files, dirs[i].plain = files[string(d.name)], plain[string(d.name)]
	}

	return dirs, nil
}

// strayMonitor refuses the file name directly under .kiln/monitors/, since a monitor is a directory.
func strayMonitor(name string) refusal {
	return refusal{fmt.Sprintf("%s/%s is a file, where a monitor is a directory", monitorsDir, name),
		fmt.Sprintf("move it into a monitor's directory, such as %s/deps/", monitorsDir)}
}

// readDir reads the monitor whose directory is d, with its monitor.json as f holds it. c is kiln.json beside it. It
// refuses a script kiln runs that is not executable, beside each refusal of readMonitor. It refuses a script or a
// monitor.json that holds a conflict's marker, as an agent's file is refused.
func (c config) readDir(ctx context.Context, f files, d monitorDir) (monitor, error) {
	raw, refused, err := roleConflicts(ctx, f, d)
	if err != nil {
		return monitor{}, err
	}

	m, broke := c.readMonitor(d.name, monitor{Name: d.name}.path(""), d.files, raw)
	refused = append(refused, broke...)
	m.tree = d.tree
	if d.plain != "" {
		refused = append(refused, refusal{fmt.Sprintf("%s holds %s without the executable bit, so kiln cannot run "+
			"it", f.at, m.path(d.plain)), fmt.Sprintf("run chmod +x %s, and commit it", m.path(d.plain))})
	}

	return m, refused.err()
}

// roleConflicts refuse each script and monitor.json in d, as f holds it, that holds a conflict's marker. They answer
// the monitor.json, or nil where d lacks one. One that holds a marker answers nil too, since nothing more is read
// there.
func roleConflicts(ctx context.Context, f files, d monitorDir) ([]byte, refusals, error) {
	var settings []byte
	var refused refusals
	for _, role := range slices.Concat(roleScripts, []string{settingsFile}) {
		if !slices.Contains(d.files, role) {
			continue
		}

		file := monitor{Name: d.name}.path(role)
		raw, _, err := f.read(ctx, file)
		if err != nil {
			return nil, nil, err
		}

		conflict := conflictIn(string(raw)).about(file)
		if role == settingsFile && len(conflict) == 0 {
			settings = raw
		}

		refused = append(refused, conflict...)
	}

	return settings, refused, nil
}

// readMonitor reads the monitor name from the files its directory, dir, holds, and from raw, its monitor.json or nil.
// c is kiln.json. It refuses each rule of the package doc that the monitor breaks, so kiln could not run it. Each
// refusal names dir, which kiln monitor add gives as its source.
func (c config) readMonitor(name Spec, dir string, files []string, raw []byte) (monitor, refusals) {
	m := monitor{Name: name, Hours: defaultHours, command: slices.Contains(files, commandScript),
		agent: slices.Contains(files, monitorAgent)}
	var keys map[string]jsontext.Value
	if raw != nil {
		// A monitor.json that is no object fails the strict read below, which words why.
		_ = jsonv2.Unmarshal(raw, &keys)
		file := path.Join(dir, settingsFile)
		if renamed := oldKeys(file, keys, renamedSetting); len(renamed) > 0 {
			return m, renamed
		}

		// json/v2 matches a key's case exactly, as kiln.json is read.
		if err := jsonv2.Unmarshal(raw, &m.settings, jsonv2.RejectUnknownMembers(true)); err != nil {
			return m, refusals{{fmt.Sprintf("%s: %v", file, err),
				"fix its JSON, and name only the keys kiln help monitors lists"}}
		}
	}

	for _, finder := range []string{checkScript, gaugeScript} {
		if slices.Contains(files, finder) {
			m.finder = finder
		}
	}

	return m, c.monitorRefusals(m, files, slices.Sorted(maps.Keys(keys))).about(dir + "/")
}

// renamedSettings are the keys of monitor.json that kiln once read, each with the key that took its place.
var renamedSettings = map[string]string{"gates": "recheck", "accept": "mustAccept", "tests": "writesTests",
	"min": "atLeast", "max": "atMost"}

// oldKeys refuse each of keys, those of file, that kiln once read there. went answers the fix for such a key, and ""
// for any other.
func oldKeys(file string, keys map[string]jsontext.Value, went func(key string) string) refusals {
	var refused refusals
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		if fix := went(key); fix != "" {
			refused = append(refused, refusal{fmt.Sprintf("%s holds %q, a key kiln no longer reads", file, key), fix})
		}
	}

	return refused
}

// renamedSetting is the fix for key of a monitor.json, where kiln renamed it, or "".
func renamedSetting(key string) string {
	if now, ok := renamedSettings[key]; ok {
		return fmt.Sprintf("name it %q", now)
	}

	return ""
}

// fixKeys are the keys of monitor.json that steer a fix, which a monitor that fixes nothing may not name.
var fixKeys = []string{"recheck", "mustAccept", "writesTests"}

// monitorRefusals say why kiln may not run m, whose directory holds files, and whose monitor.json names keys.
func (c config) monitorRefusals(m monitor, files, keys []string) refusals {
	stray := strayFile(files)
	gauged := m.finder == gaugeScript
	// A line with neither a check nor a gauge means a gauge, whose own refusal says it is missing.
	meantGauge := gauged || m.finder == "" && m.line().sides() > 0
	refused := broken(
		rule{!specName.MatchString(string(m.Name)), refusal{"has a name no spec could have",
			"name it in lower-case letters, digits and hyphens, such as deps"}},
		rule{stray != "", refusal{fmt.Sprintf("holds %s, a file kiln does not read", stray),
			fmt.Sprintf("keep only %s there, and move a helper into a subdirectory, such as lib/",
				strings.Join(roleFiles, ", "))}},
		rule{slices.Contains(files, checkScript) && gauged, refusal{
			"holds both a check and a gauge, where it may hold one", "remove one"}},
		rule{gauged && m.line().sides() != 1, refusal{"holds a gauge, but its monitor.json names both atLeast and atMost, " +
			"or neither", "name one, the gauge's line"}},
		rule{!gauged && m.line().sides() > 0, refusal{"names atLeast or atMost, a gauge's line, but holds no gauge",
			"drop atLeast and atMost, or add a gauge"}},
		rule{!m.fixes() && !meantGauge, refusal{"holds no command and no agent.md, so it fixes nothing",
			"add one, or keep a gauge alone, with its line in monitor.json, for kiln to watch"}},
		rule{m.fixes() && !m.WritesTests && c.unguarded(), refusal{"may not write the tests, while acceptance names no " +
			"path before {spec} for kiln to guard", "give each acceptance path a part before {spec}, or say " +
			"\"writesTests\": true in its monitor.json"}},
	)
	for _, key := range keys {
		if !m.fixes() && slices.Contains(fixKeys, key) {
			refused = append(refused, refusal{fmt.Sprintf("names %s in its monitor.json, a key that steers a fix, "+
				"but holds no command and no agent.md to fix with", key),
				fmt.Sprintf("drop %s, or add a command or an agent.md", key)})
		}
	}

	return refused
}

// strayFile is the first of files that sits directly in a monitor's directory, and is none of roleFiles, or "".
func strayFile(files []string) string {
	for _, f := range files {
		if !strings.Contains(f, "/") && !slices.Contains(roleFiles, f) {
			return f
		}
	}

	return ""
}

// monitorNamed is the monitor of reads named name. It answers found false where reads holds none of that name, and
// the refusal where kiln refuses it.
func monitorNamed(reads []monitorRead, name Spec) (m monitor, found bool, refused error) {
	i := slices.IndexFunc(reads, func(d monitorRead) bool { return d.name == name })
	if i < 0 {
		return monitor{}, false, nil
	}

	return reads[i].monitor, true, reads[i].refused
}

// runnable are the monitors of reads that kiln runs, leaving out each it refuses.
func runnable(reads []monitorRead) []monitor {
	var monitors []monitor
	for _, d := range reads {
		if d.refused == nil {
			monitors = append(monitors, d.monitor)
		}
	}

	return monitors
}

// nowVar names a time, as RFC 3339, that kiln monitor run takes as now.
const nowVar = "KILN_NOW"

// now is the time a monitor's run starts at: the machine's clock, or the time $KILN_NOW names.
func (r *Repo) now() time.Time {
	if at, err := time.Parse(time.RFC3339, envValue(r.env, nowVar)); err == nil {
		return at
	}

	return time.Now()
}

// envValue is the value of the variable name in env, or "".
func envValue(env []string, name string) string {
	for _, kv := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value
		}
	}

	return ""
}

// checked is what a check found.
type checked struct {
	// ran is false for a check that couldn't run: it exited non-zero, or its last line is no verdict.
	ran bool
	// fix is whether the check asks for a fix.
	fix bool
	// summary is the verdict's summary, or the last line of output of a check that couldn't run.
	summary string
	// drain is whether the verdict asks kiln to check again at once, once the fix it starts lands.
	drain bool
	// printed is the check's stdout, which the hold compares, and found what it printed above its last line.
	printed, found string
	// reading is a gauge's reading, as a monitor line holds it: the gauge's name, its value and its line. Its Gauge
	// is "" for a check.
	reading entry
	// past is whether the check or gauge couldn't run because it ran past kiln.json's timeout.
	past bool
}

// verdictLine is a check's last line of stdout.
type verdictLine struct {
	Fix     *bool  `json:"fix"`
	Summary string `json:"summary"`
	Drain   bool   `json:"drain"`
}

// runMonitorCheck runs command in dir, as a gauge runs, and reads what it found. It answers an error only
// once kiln is interrupted. A check that ran past s's limit couldn't run.
func (s projectShell) runMonitorCheck(ctx context.Context, dir git.Worktree, command string) (checked, error) {
	out, err := s.run(ctx, dir, command)
	if ctx.Err() != nil {
		return checked{}, ctx.Err()
	}

	c := checked{printed: out.stdout}
	last, found := lastOf(c.printed)
	c.found = found
	_, past := errors.AsType[*ranPast](err)
	var v verdictLine
	switch {
	case past:
		c.summary, c.past = "the check "+err.Error(), true
	case err != nil:
		c.summary = cmpOr(out.last(), fmt.Sprintf("the check failed: %v", err))
	case last == "" && out.last() == "":
		c.summary = "the check printed nothing" + noVerdictFix
	case !jsonLine(last, &v) || v.Fix == nil:
		// A check that printed only on stderr is named by its last line there.
		c.summary = fmt.Sprintf("the check's last line, %q, is no verdict", cmpOr(last, out.last())) + noVerdictFix
	default:
		c.ran, c.fix, c.summary, c.drain = true, *v.Fix, v.Summary, v.Drain
	}

	return c, nil
}

// noVerdictFix is the fix a check's summary names when the last line of its output is no verdict.
const noVerdictFix = `: end its output with a JSON object, such as {"fix": false, "summary": "all clear"}`

// lastOf splits out, trimmed, into its last line and what comes above it.
func lastOf(out string) (last, above string) {
	out = strings.TrimRight(out, "\r\n\t ")
	i := strings.LastIndex(out, "\n")
	return strings.TrimSpace(out[i+1:]), strings.TrimRight(out[:max(i, 0)], "\r\n")
}

// checkTree is the worktree, under the cache directory, where a monitor's check or gauge runs. Its run reads main's
// tip, and its fix's gate reads the fix's commit there. A spec's name never starts with a dot, so it is no build's
// tree.
const checkTree = ".monitor-check"

// firstCheck runs m's check, or reads its gauge, in a worktree of its own at main. c says how long a command may run.
// A monitor with neither asks for a fix each time.
func (held commands) firstCheck(ctx context.Context, c config, m monitor, main git.Rev) (checked, error) {
	r := held.r
	if !m.finds() {
		return checked{ran: true, fix: true}, nil
	}

	s := c.shell(r.env)
	tree, err := held.scratchTree(ctx, checkTree, main)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(m, fmt.Sprintf("kiln could not check out main for the check: %v", err)), nil
	case m.finder == checkScript:
		return s.runMonitorCheck(ctx, tree, m.path(checkScript))
	}

	empty, err := r.git.EmptyTree(ctx)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(m, fmt.Sprintf("kiln could not name git's empty tree: %v", err)), nil
	}

	return s.measured(ctx, tree, m, empty)
}

// couldNotCheck is a check of m that couldn't run, for why. Its reading names m's gauge, where m has one.
func couldNotCheck(m monitor, why string) checked {
	c := checked{summary: why}
	if m.finder == gaugeScript {
		c.reading = m.reading(nil)
	}

	return c
}

// measured reads m's gauge in tree, from base, as a check's verdict. On its line is healthy, and crossed asks for a
// fix. A gauge that read nothing is healthy, with no reading, as after a landing. A reading kiln could not take
// couldn't run. It answers an error only once kiln is interrupted.
func (s projectShell) measured(ctx context.Context, tree git.Worktree, m monitor, base git.Rev) (checked, error) {
	rd, err := s.readGauge(ctx, tree, m, base)
	switch {
	case err != nil:
		return checked{}, err
	case rd.nothing:
		return checked{ran: true, summary: fmt.Sprintf("%s: nothing to read in the whole repository, as %s", m.Name,
			rd.why), printed: rd.printed}, nil
	}

	e := m.reading(rd.value)
	return checked{ran: rd.value != nil, fix: m.line().crossed(rd.value), summary: rd.summary(e), printed: rd.printed,
		found: rd.found, reading: e, past: rd.past != nil}, nil
}
