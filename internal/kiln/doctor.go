package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/proc"
)

// Doctor prints each problem kiln's commands would refuse or fail on, in the project as its working tree holds it.
// Each names its fix. Doctor reads kiln.json, and refuses it as every kiln command would. It reads the build,
// verify and audit agents as kiln queue reads them, and each monitor's agent. Where a command of kiln.json starts
// with a path, Doctor reads the script there. It needs the branch main, a claude on the PATH, and a pre-commit hook
// that runs kiln precommit. When kiln.json names a monitor, it needs a launchd job in a checkout of the repository.
// With a name, it reads that spec as kiln queue would, but not its Status. It notes each key of kiln.json that
// differs from main's, which kiln acts on. Doctor writes nothing, and calls no model and no network. It fails when it
// found a problem.
func (r *Repo) Doctor(ctx context.Context, name Spec, out io.Writer) error {
	d, err := r.diagnose(ctx, name)
	if err != nil {
		return err
	}

	for _, why := range d.found {
		fmt.Fprintln(out, why.String())
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
		d.found = append(d.found, refusal{"the working tree holds no " + configFile, initHint})
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

// config reads raw, the working tree's kiln.json, and adds its refusals to d. It answers false for a file kiln cannot
// read, which then has nothing more to check.
func (d *diagnosis) config(raw []byte) (config, bool) {
	c, err := parseConfig(raw)
	if u, ok := errors.AsType[unreadable](err); ok {
		d.found = append(d.found, u.refusals...)
		return c, false
	}

	// parseConfig's only other error is its refusals.
	_ = d.found.add(err)
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
		d.found = append(d.found, refusal{"the working tree holds no " + name.file(), "write it with /shape"})
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
		d.found = append(d.found, noMainRefusal)
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
		d.found = append(d.found, refusal{"main holds no " + configFile + ", which kiln reads there",
			"commit it to main"})
	case readable:
		if keys := drift(tree, raw); len(keys) > 0 {
			d.notes = append(d.notes, fmt.Sprintf("%s differs from main's in %s: kiln acts on main's, so commit it "+
				"to main to act on yours", configFile, strings.Join(keys, ", ")))
		}
	}

	return nil
}

// drift names, in order, each key whose value differs between kiln.json files a and b. It compares values as JSON
// reads them, so space, and the order of an object's keys, count for nothing. A file that is no JSON object reads as
// one with no keys.
func drift(a, b []byte) []string {
	var x, y map[string]any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	keys := slices.Concat(slices.Collect(maps.Keys(x)), slices.Collect(maps.Keys(y)))
	var differ []string
	for _, k := range slices.Compact(slices.Sorted(slices.Values(keys))) {
		if !reflect.DeepEqual(x[k], y[k]) {
			differ = append(differ, k)
		}
	}

	return differ
}

// diagnoseTools adds to d each program kiln runs that is missing. That is claude, the pre-commit hook, and the
// launchd job when c names a monitor.
func (r *Repo) diagnoseTools(ctx context.Context, c config, d *diagnosis) error {
	if _, err := proc.Look(r.env, claudeProgram); err != nil {
		d.found = append(d.found, refusal{err.Error(),
			"install Claude Code, and put the directory that holds " + claudeProgram + " on the PATH"})
	}

	refused, err := r.hookRefusals(ctx)
	if err != nil {
		return err
	}

	d.found = append(d.found, refused...)
	if len(c.Monitors) == 0 {
		return nil
	}

	served, err := r.launchdServes(ctx)
	if err != nil || served {
		return err
	}

	d.found = append(d.found, refusal{configFile + " names monitors, but no launchd job runs them in any checkout " +
		"of this repository", "run kiln monitor install"})
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
		return refusals{{hook + " is missing, so no commit to main runs the gate",
			"run kiln init, which installs it, or names the line to add to your own hook"}}, nil
	case err != nil:
		return nil, err
	}

	runs := bytes.Contains(raw, []byte(hookMark)) || bytes.Contains(raw, []byte(precommitLine))
	return broken(
		rule{!runs, refusal{hook + " does not run the gate", "add this line to it: " + precommitLine}},
		rule{!proc.Executable(hook), refusal{hook + " is not executable, so git skips it", "run chmod +x " + hook}},
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
			rule{err != nil, refusal{fmt.Sprintf("%s runs %s, which the working tree lacks", configFile, script),
				"write it, or change the command"}},
			rule{err == nil && !proc.Executable(at), refusal{fmt.Sprintf("%s runs %s, which is not executable",
				configFile, script), fmt.Sprintf("run chmod +x %s, and commit it", script)}},
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
// shell would expand, such as ~ or a glob, or unquote.
func scriptPath(word string) bool {
	return strings.Contains(word, "/") && !strings.ContainsAny(word, "$\"'`=\\~*?[{")
}

// firstWord is the first word of command, up to the first space or other character that ends a shell word.
func firstWord(command string) string {
	command = strings.TrimSpace(command)
	if end := strings.IndexAny(command, " \t\n;&|<>()"); end >= 0 {
		return command[:end]
	}

	return command
}

// launchdServes is whether a launchd job of kiln monitor install runs in any checkout of r's repository. It reads
// each job's working directory, and asks git for its common directory.
func (r *Repo) launchdServes(ctx context.Context) (bool, error) {
	_, plist, err := r.launchdPlist()
	if err != nil {
		return false, err
	}

	jobs, err := filepath.Glob(filepath.Join(filepath.Dir(plist), launchdPrefix+"*.plist"))
	if err != nil {
		return false, err
	}

	for _, job := range jobs {
		// A job whose checkout is gone, or is no git checkout, names no directory, and so no repository.
		other, _ := plainGit(ctx, jobDir(job), proc.Git.Env(r.env), "rev-parse", "--path-format=absolute",
			"--git-common-dir")
		if sameFile(other, r.common) {
			return true, nil
		}
	}

	return false, nil
}

// jobWorkingDir finds the working directory in the property list that launchdJob writes.
var jobWorkingDir = regexp.MustCompile(`<key>WorkingDirectory</key>\s*<string>([^<]*)</string>`)

// jobDir is the working directory the launchd job in file plist names, or "" when kiln reads none.
func jobDir(plist string) string {
	raw, err := os.ReadFile(plist)
	m := jobWorkingDir.FindSubmatch(raw)
	if err != nil || m == nil {
		return ""
	}

	var dir string
	if xml.Unmarshal(append(append([]byte("<s>"), m[1]...), "</s>"...), &dir) != nil {
		return ""
	}

	return dir
}
