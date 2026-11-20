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
// verify and audit agents as kiln queue reads them. It reads each monitor under .kiln/monitors/ as kiln monitor run
// reads it at main's tip. It reads the files git would commit there, and agent.md as the fix's session reads it.
// Where a command of kiln.json starts with a path, Doctor reads the script there. It needs the branch main, a claude
// on the PATH, and a pre-commit hook that runs kiln precommit. With a monitor, it needs a launchd job in a checkout
// of the repository.
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

	monitors, err := r.workingMonitors(ctx)
	if err := d.found.add(err); err != nil {
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
		if err := r.diagnoseProject(ctx, c, monitors, &d); err != nil {
			return d, err
		}
	}

	refused, err := r.diagnoseSpec(ctx, c, name, monitorNames(monitors))
	if err != nil {
		return d, err
	}

	d.found = append(d.found, refused...)

	if err := r.diagnoseMain(ctx, raw, readable, &d); err != nil {
		return d, err
	}

	return d, r.diagnoseTools(ctx, len(monitors) > 0, &d)
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

// diagnoseProject adds to d what kiln queue refuses in c and the agents' files. It adds what kiln monitor run refuses
// in each of monitors, and each agent.md a fix's session could not read. It adds each script of kiln.json's commands
// that is missing, or not executable.
func (r *Repo) diagnoseProject(ctx context.Context, c config, monitors []monitorDir, d *diagnosis) error {
	tree := r.workingTree()
	refused, err := c.readiness(ctx, tree)
	if err != nil {
		return err
	}

	d.found = append(d.found, refused...)
	for _, dir := range monitors {
		m, err := c.readDir(ctx, tree, dir)
		if err := d.found.add(err); err != nil {
			return err
		}

		if m.agent {
			_, err := tree.agent(ctx, m.path(monitorAgent), "write it")
			if err := d.found.add(err); err != nil {
				return err
			}
		}
	}

	d.found = append(d.found, c.scriptRefusals(r.top)...)
	return nil
}

// workingMonitors lists each monitor's directory under .kiln/monitors/ in the working tree, by name, with each file
// git would commit there. Those are the files git tracks, and those it does not ignore. It refuses each file directly
// under .kiln/monitors/, as monitorDirs does in a commit.
func (r *Repo) workingMonitors(ctx context.Context) ([]monitorDir, error) {
	out, err := plainGit(ctx, r.top, proc.Git.Env(r.env), "ls-files", "-z", "--cached", "--others",
		"--exclude-standard", "--", monitorsDir+"/")
	if err != nil {
		return nil, err
	}

	var dirs []monitorDir
	var refused refusals
	for p := range strings.SplitSeq(out, "\x00") {
		name, file, inside := strings.Cut(strings.TrimPrefix(p, monitorsDir+"/"), "/")
		at := filepath.Join(r.top, filepath.FromSlash(p))
		switch _, err := os.Lstat(at); {
		// git lists a file it tracks that the working tree deleted.
		case p == "", err != nil:
		case !inside:
			refused = append(refused, strayMonitor(name))
		default:
			dirs = withFile(dirs, Spec(name), file, slices.Contains(roleScripts, file) && !proc.Executable(at))
		}
	}

	return dirs, refused.err()
}

// withFile adds file to the directory of dirs named name, which it adds when dirs lacks it. plain says file is a
// script kiln runs that is not executable. git lists each directory's files together.
func withFile(dirs []monitorDir, name Spec, file string, plain bool) []monitorDir {
	if len(dirs) == 0 || dirs[len(dirs)-1].name != name {
		dirs = append(dirs, monitorDir{name: name})
	}

	d := &dirs[len(dirs)-1]
	d.files = append(d.files, file)
	if plain {
		d.plain = file
	}

	return dirs
}

// monitorNames are the names of monitors.
func monitorNames(monitors []monitorDir) []string {
	names := make([]string, len(monitors))
	for i, m := range monitors {
		names[i] = string(m.name)
	}

	return names
}

// diagnoseSpec answers what kiln queue refuses in the working tree's spec name, but its Status. c is the working
// tree's kiln.json, and monitors the names of the working tree's monitors. It answers none for the name "".
func (r *Repo) diagnoseSpec(ctx context.Context, c config, name Spec, monitors []string) (refusals, error) {
	if name == "" {
		return nil, nil
	}

	if refused := name.misnamed(); refused != nil {
		return refused, nil
	}

	spec, found, err := r.workingTree().read(ctx, name.file())
	switch {
	case err != nil:
		return nil, err
	case !found:
		return refusals{{"the working tree holds no " + name.file(), "write it with /shape"}}, nil
	}

	return c.specRefusals(name, spec, monitors), nil
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
// launchd job when monitored, so the working tree holds a monitor.
func (r *Repo) diagnoseTools(ctx context.Context, monitored bool, d *diagnosis) error {
	if _, err := proc.Look(r.env, claudeProgram); err != nil {
		d.found = append(d.found, refusal{err.Error(),
			"install Claude Code, and put the directory that holds " + claudeProgram + " on the PATH"})
	}

	refused, err := r.hookRefusals(ctx)
	if err != nil {
		return err
	}

	d.found = append(d.found, refused...)
	if !monitored {
		return nil
	}

	served, err := r.launchdServes(ctx)
	if err != nil || served {
		return err
	}

	d.found = append(d.found, refusal{monitorsDir + "/ holds monitors, but no launchd job runs them in any " +
		"checkout of this repository", "run kiln monitor install"})
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

// commands are the commands of c that kiln runs with sh -c: the gate's, writable's and each gauge's.
func (c config) commands() []string {
	commands := slices.Concat(c.Gate, c.Writable)
	for _, g := range c.Gauges {
		commands = append(commands, g.Command)
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
