package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/proc"
)

// Doctor prints each problem kiln's commands would refuse or fail on, in the project as its working tree holds it.
// Each names its fix. Doctor reads kiln.json, and refuses it as every kiln command would. It reads the build,
// verify and audit agents as kiln queue reads them, and each monitor's agent. Where a command of kiln.json starts
// with a path, Doctor reads the script there. It needs the branch main, a claude on the PATH, and a pre-commit hook
// that runs kiln precommit. It needs a launchd job when kiln.json names a monitor. With a name, it reads that spec as
// kiln queue would, but not its Status. It notes each key of kiln.json that differs from main's, which kiln acts on.
// Doctor writes nothing, and calls no model and no network. It fails when it found a problem.
func (r *Repo) Doctor(ctx context.Context, name Spec, out io.Writer) error {
	d, err := r.diagnose(ctx, name)
	if err != nil {
		return err
	}

	for _, why := range d.found {
		fmt.Fprintln(out, why)
	}

	for _, note := range d.notes {
		fmt.Fprintln(out, "note: "+note)
	}

	if len(d.found) > 0 {
		return fmt.Errorf("found %s", plural(len(d.found), "problem"))
	}

	fmt.Fprintln(out, "found no problem")
	return nil
}

// diagnosis is what Doctor found: each problem, and each note, which is no problem.
type diagnosis struct {
	found refusals
	notes []string
}

// diagnose finds each problem and note Doctor prints. It answers an error only when kiln could not read a file or
// run git.
func (r *Repo) diagnose(ctx context.Context, name Spec) (diagnosis, error) {
	var d diagnosis
	raw, found, err := r.workingTree().read(ctx, configFile)
	if err != nil {
		return d, err
	}

	var c config
	readable := false
	if found {
		c, readable = d.config(raw)
	} else {
		d.found = append(d.found, fmt.Sprintf("the working tree holds no %s: %s", configFile, initHint))
	}

	if readable {
		if err := r.diagnoseProject(ctx, c, &d); err != nil {
			return d, err
		}
	}

	if err := r.diagnoseSpec(ctx, c, name, &d); err != nil {
		return d, err
	}

	if err := r.diagnoseMain(ctx, raw, readable, &d); err != nil {
		return d, err
	}

	return d, r.diagnoseTools(ctx, c, &d)
}

// config reads raw, the working tree's kiln.json, and adds its refusals to d. It answers false for a file with no
// keys kiln can read, which then has nothing more to check.
func (d *diagnosis) config(raw []byte) (config, bool) {
	c, err := parseConfig(raw)
	if err := d.found.add(err); err != nil {
		d.found = append(d.found, err.Error())
		return c, false
	}

	return c, true
}

// diagnoseProject adds to d what kiln queue refuses in c and the agents' files. It adds each monitor's agent kiln
// cannot read. It adds each script of kiln.json's commands that is missing, or not executable.
func (r *Repo) diagnoseProject(ctx context.Context, c config, d *diagnosis) error {
	tree := r.workingTree()
	refused, err := c.readiness(ctx, tree)
	if err != nil {
		return err
	}

	d.found = append(d.found, refused...)
	for _, role := range c.monitorRoles() {
		_, err := tree.agent(ctx, role, "write it, or name another agent in the monitor's entry")
		if err := d.found.add(err); err != nil {
			return err
		}
	}

	d.found = append(d.found, c.scriptRefusals(r.top)...)
	return nil
}

// monitorRoles are the roles of c's monitors' agents, each once, but the roles kiln queue reads. A role whose file
// kiln.json may not name is none.
func (c config) monitorRoles() []string {
	seen := []string{"", buildRole, verifyRole, auditRole}
	var roles []string
	for _, m := range c.Monitors {
		if role := m.role(); !slices.Contains(seen, role) {
			seen, roles = append(seen, role), append(roles, role)
		}
	}

	return roles
}

// diagnoseSpec adds to d what kiln queue refuses in the working tree's spec name, but its Status. c is the working
// tree's kiln.json. It adds nothing for the name "".
func (r *Repo) diagnoseSpec(ctx context.Context, c config, name Spec, d *diagnosis) error {
	if name == "" {
		return nil
	}

	if refused := name.misnamed(); refused != nil {
		d.found = append(d.found, refused...)
		return nil
	}

	spec, found, err := r.workingTree().read(ctx, name.file())
	switch {
	case err != nil:
		return err
	case !found:
		d.found = append(d.found, fmt.Sprintf("the working tree holds no %s: write it with /shape", name.file()))
	default:
		d.found = append(d.found, c.specRefusals(name, spec)...)
	}

	return nil
}

// diagnoseMain adds to d what main lacks that kiln reads there. When readable, it notes each key of tree, the working
// tree's kiln.json, that differs from main's.
func (r *Repo) diagnoseMain(ctx context.Context, tree []byte, readable bool, d *diagnosis) error {
	main, err := r.mainTip(ctx)
	if errors.Is(err, errNoMain) {
		d.found = append(d.found, noMain)
		return nil
	}

	if err != nil {
		return err
	}

	raw, found, err := r.git.Show(ctx, main, configFile)
	switch {
	case err != nil:
		return err
	case !found:
		d.found = append(d.found, fmt.Sprintf("main holds no %s, which kiln reads there: commit it to main",
			configFile))
	case readable:
		if keys := drift(tree, raw); len(keys) > 0 {
			d.notes = append(d.notes, fmt.Sprintf("%s differs from main's in %s: kiln acts on main's, so commit it "+
				"to main to act on yours", configFile, strings.Join(keys, ", ")))
		}
	}

	return nil
}

// drift names, in order, each key whose value differs between kiln.json files a and b. Space and order inside a value
// count for nothing. A file that is no JSON object reads as one with no keys.
func drift(a, b []byte) []string {
	var x, y map[string]json.RawMessage
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	keys := slices.Sorted(maps.Keys(x))
	keys = slices.Compact(slices.Sorted(slices.Values(append(keys, slices.Collect(maps.Keys(y))...))))
	var differ []string
	for _, k := range keys {
		if !bytes.Equal(compact(x[k]), compact(y[k])) {
			differ = append(differ, k)
		}
	}

	return differ
}

// compact is raw with its insignificant space removed, or nil for no value.
func compact(raw json.RawMessage) []byte {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return nil
	}

	return b.Bytes()
}

// diagnoseTools adds to d each program kiln runs that is missing. That is claude, the pre-commit hook, and the
// launchd job when c names a monitor.
func (r *Repo) diagnoseTools(ctx context.Context, c config, d *diagnosis) error {
	if _, err := proc.Look(r.env, claudeProgram); err != nil {
		d.found = append(d.found, fmt.Sprintf("%v: install Claude Code, and put the directory that holds %s on "+
			"the PATH", err, claudeProgram))
	}

	refused, err := r.hookRefusals(ctx)
	if err != nil {
		return err
	}

	d.found = append(d.found, refused...)
	if len(c.Monitors) == 0 {
		return nil
	}

	_, plist, err := r.launchdPlist()
	if err != nil {
		return err
	}

	if _, err := os.Stat(plist); errors.Is(err, fs.ErrNotExist) {
		d.found = append(d.found, fmt.Sprintf("%s names monitors, but no launchd job runs them: run kiln monitor "+
			"install", configFile))
	}

	return nil
}

// hookRefusals say why no pre-commit hook runs kiln precommit. The hook git runs is missing, runs no kiln, or is not
// executable.
func (r *Repo) hookRefusals(ctx context.Context) (refusals, error) {
	hooks, err := r.hooksDir(ctx)
	if err != nil {
		return nil, err
	}

	hook := filepath.Join(hooks, "pre-commit")
	raw, err := os.ReadFile(hook)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return refusals{fmt.Sprintf("%s is missing, so no commit to main runs the gate: run kiln init, which "+
			"installs it, or names the line to add to your own hook", hook)}, nil
	case err != nil:
		return nil, err
	}

	runs := bytes.Contains(raw, []byte(hookMark)) || bytes.Contains(raw, []byte(precommitLine))
	return broken(
		rule{!runs, fmt.Sprintf("%s does not run the gate: add this line to it: %s", hook, precommitLine)},
		rule{!proc.Executable(hook), fmt.Sprintf("%s is not executable, so git skips it: run chmod +x %s", hook,
			hook)},
	), nil
}

// scriptRefusals say which of c's commands starts with a script the working tree at top lacks, or may not execute. A
// command whose first word holds no slash runs a program on the PATH, which Doctor leaves alone.
func (c config) scriptRefusals(top string) refusals {
	var refused refusals
	var seen []string
	for _, command := range c.commands() {
		script := firstWord(command)
		if !scriptPath(script) || slices.Contains(seen, script) {
			continue
		}

		seen = append(seen, script)
		at := script
		if !filepath.IsAbs(at) {
			at = filepath.Join(top, filepath.FromSlash(script))
		}

		_, err := os.Stat(at)
		refused = append(refused, broken(
			rule{err != nil, fmt.Sprintf("%s runs %s, which the working tree lacks: write it, or change the "+
				"command", configFile, script)},
			rule{err == nil && !proc.Executable(at), fmt.Sprintf("%s runs %s, which is not executable: run chmod "+
				"+x %s, and commit it", configFile, script, script)},
		)...)
	}

	return refused
}

// commands are the commands of c that kiln runs with sh -c. They are the gate's, writable's, each gauge's, and each
// monitor's check and command.
func (c config) commands() []string {
	commands := slices.Concat(c.Gate, c.Writable)
	for _, g := range c.Gauges {
		commands = append(commands, g.Command)
	}

	for _, m := range c.Monitors {
		commands = append(commands, m.Check, m.Command)
	}

	return commands
}

// scriptPath is whether word, the first of a command, names a script by its path. It holds a slash, and nothing the
// shell would expand or unquote.
func scriptPath(word string) bool {
	return strings.Contains(word, "/") && !strings.ContainsAny(word, "$\"'`=\\")
}

// firstWord is the first word of command, as the shell splits it at space, or "".
func firstWord(command string) string {
	word, _, _ := strings.Cut(strings.TrimSpace(command), " ")
	return word
}
