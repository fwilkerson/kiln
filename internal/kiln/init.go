package kiln

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// schemaFile is kiln.json's JSON Schema, which an editor reads by kiln.json's "$schema". kiln init writes it from the
// defaults, where TestSchema builds it from config.
const schemaFile = kilnDir + "/kiln.schema.json"

// defaults are what kiln init writes into .kiln/ for every preset. A preset fills each marker in their prompts, and
// adds kiln.json. The project owns them from then on.
//
//go:embed defaults
var defaults embed.FS

// reviewersDir holds the reviewers /shape runs on a spec, each a Claude Code agent's file. It lies apart from
// .kiln/agents/, so no reviewer is taken for an agent that kiln's builds run.
const reviewersDir = kilnDir + "/reviewers"

// initHint is how a refusal says to start .kiln/.
const initHint = "run kiln init, and commit what it writes"

// initFile is kiln init's record of the defaults it last wrote, which its next run merges the project's edits against.
const initFile = kilnDir + "/init.json"

// InitOptions are what the developer chose for kiln init.
type InitOptions struct {
	// Preset names the preset Init writes, as the package doc says under Presets. "" means the preset .kiln/init.json
	// names, or else the built-in one the repository's files match.
	Preset string
}

// Init brings .kiln/ in the checkout up to date with kiln's defaults, as opt's preset fills them. The package doc
// says how, under Presets. Init adds the keys a preset's kiln.json lacks as sharedDefaults holds them. Last, it
// installs the hook, or says why it installed none, as installHook says.
func (r *Repo) Init(ctx context.Context, out io.Writer, opt InitOptions) error {
	last, err := r.lastInit()
	if err != nil {
		return err
	}

	p, err := r.initPreset(opt.Preset, last.Preset, out)
	if err != nil {
		return err
	}

	// A refusal after the first write would leave .kiln/ written, and a re-run would not say to commit it.
	if err := r.initRefusal(); err != nil {
		return err
	}

	wrote, err := r.writeDefaults(ctx, p, last, out)
	if err != nil {
		return err
	}

	// Neither the build agent nor the audit is linked: a developer who wants one as a subagent links it.
	if err := r.link("skills/shape", "skills/shape", out); err != nil {
		return err
	}

	if err := r.linkReviewers(out); err != nil {
		return err
	}

	if wrote {
		fmt.Fprintf(out, "commit %s/ on main: kiln reads it there\n", kilnDir)
	}

	if _, err := r.mainTip(ctx); errors.Is(err, errNoMain) {
		fmt.Fprintln(out, noMain)
	}

	if err := r.sayGate(out, wrote); err != nil {
		return err
	}

	return r.installHook(ctx, out)
}

// initRefusal refuses what Init would refuse after its first write. That is a reviewer Claude Code could not run, or a
// checkout's kiln.json that kiln could not use. A checkout with no kiln.json passes, since Init writes one.
func (r *Repo) initRefusal() error {
	if _, err := r.reviewers(); err != nil {
		return err
	}

	raw, err := os.ReadFile(filepath.Join(r.top, configFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}

	_, err = parseConfig(raw)
	return err
}

// initPreset is the preset kiln init writes: the one named, else the one recorded, else the one pickPreset picks. A
// path named is from the working directory, and a path recorded is from the checkout's top.
func (r *Repo) initPreset(named, recorded string, out io.Writer) (preset, error) {
	switch {
	case named == "" && recorded == "":
		return r.pickPreset(out)
	case named == "":
		p, err := presetNamed(recorded, r.top)
		if err != nil {
			return p, fmt.Errorf("%w: %s names a preset kiln can't use: %w; name one with --preset", errRefused,
				initFile, err)
		}

		return p, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return preset{}, err
	}

	p, err := presetNamed(named, wd)
	if err != nil {
		return p, fmt.Errorf("%w: %w", ErrUsage, err)
	}

	return p, nil
}

// pickPreset is the one built-in preset whose detectFile's paths the checkout's top holds, every one. It prints
// which, and by which files. It refuses none, or more than one, and names each candidate's files.
func (r *Repo) pickPreset(out io.Writer) (preset, error) {
	var matched []preset
	var all, found []string
	var why string
	for _, name := range builtIns {
		p, err := presetNamed(name, "")
		if err != nil {
			return p, err
		}

		paths, holds, err := p.detects(r.top)
		if err != nil {
			return p, err
		}

		files := name + ": " + strings.Join(paths, ", ")
		all = append(all, files)
		if holds {
			matched, found = append(matched, p), append(found, files)
			why = fmt.Sprintf("picked the preset %s, since the repository holds %s", name, strings.Join(paths, " and "))
		}
	}

	switch len(matched) {
	case 0:
		return preset{}, fmt.Errorf("%w: no preset matches the repository, by the files at its top (%s): name one "+
			"with --preset", errRefused, strings.Join(all, "; "))
	case 1:
		fmt.Fprintln(out, why)
		return matched[0], nil
	}

	return preset{}, fmt.Errorf("%w: more than one preset matches the repository, by the files at its top (%s): "+
		"name one with --preset", errRefused, strings.Join(found, "; "))
}

// sayGate prints the gate's commands of the checkout's kiln.json, which the pre-commit hook runs on each commit to
// main. When Init wrote .kiln/, it says they must pass before .kiln/ is committed.
func (r *Repo) sayGate(out io.Writer, wrote bool) error {
	c, err := r.checkoutConfig()
	if err != nil {
		return err
	}

	fmt.Fprint(out, "the pre-commit hook runs the gate on each commit to main")
	if wrote {
		fmt.Fprintf(out, ", so make its commands pass before you commit %s/", kilnDir)
	}

	fmt.Fprintln(out, ":")
	for _, cmd := range c.Gate {
		fmt.Fprintf(out, "  %s\n", cmd)
	}

	return nil
}

// initStatus says, in kiln status, when .kiln/ at main's tip is behind the defaults kiln init writes now. Where main
// holds kiln.json but no record of kiln init, it says so.
func (r *Repo) initStatus(ctx context.Context, main git.Rev, out io.Writer) error {
	last, err := r.mainRecord(ctx, main)
	if err == nil && len(last.Files) == 0 {
		_, set, err := r.git.Show(ctx, main, configFile)
		if err != nil {
			return err
		}

		if set {
			fmt.Fprintf(out, "%s/ holds no record of what kiln init wrote: run kiln init, which leaves each "+
				"difference from its defaults as a conflict\n", kilnDir)
		}
	}

	var present []string
	if err == nil {
		present, err = r.git.Files(ctx, main, monitorsDir)
	}

	var stale []string
	if err == nil {
		stale, err = r.behind(last, present)
	}

	switch {
	case err != nil:
		fmt.Fprintf(out, "kiln could not tell whether %s/ is up to date: %v\n", kilnDir, err)
	case len(stale) > 0:
		fmt.Fprintf(out, "%s/ is behind %s: run kiln init, which merges them with your edits\n", kilnDir,
			strings.Join(stale, ", and "))
	}

	return nil
}

// mainRecord is kiln init's record at main, or an empty record where main holds none.
func (r *Repo) mainRecord(ctx context.Context, main git.Rev) (initRecord, error) {
	raw, found, err := r.git.Show(ctx, main, initFile)
	if err != nil || !found {
		return initRecord{}, err
	}

	return parseInit(raw)
}

// behind names what in last, a record of kiln init, differs from what kiln init would write now. It reads the defaults
// of the preset initPreset finds. It reads each monitor kiln monitor add wrote, of those present names.
func (r *Repo) behind(last initRecord, present []string) ([]string, error) {
	var stale []string
	if len(last.Files) > 0 {
		p, err := r.initPreset("", last.Preset, io.Discard)
		if err != nil {
			return nil, err
		}

		files, err := presetFiles(p)
		if err != nil {
			return nil, err
		}

		from := p.recorded(r.top)
		if !maps.EqualFunc(last.Files, newRecord(from, files).Files, slices.Equal[[]string]) {
			stale = append(stale, "the defaults of kiln init's preset "+from)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(last.Monitors)) {
		m := last.Monitors[name]
		if !slices.Contains(present, string(name)) {
			continue
		}

		_, files, err := r.sourceFiles(m.Source)
		if err != nil {
			return stale, err
		}

		if !maps.EqualFunc(m.Files, linesOf(files), slices.Equal[[]string]) {
			stale = append(stale, fmt.Sprintf("the monitor %s, from %s", name, m.Source))
		}
	}

	return stale, nil
}

// initRecord is what initFile holds: the preset kiln init last wrote, and each default but kiln.json. A default is
// kept as its lines, so a diff of the record reads as a diff of the files.
type initRecord struct {
	Preset string    `json:"preset"`
	Files  fileLines `json:"files"`
	// Monitors holds each monitor kiln monitor add wrote, by its name, as kiln init last brought it up to date.
	Monitors map[Spec]addedMonitor `json:"monitors,omitempty"`
}

// addedMonitor is a monitor kiln monitor add wrote, as kiln init keeps it. It holds the source, and each file's lines.
type addedMonitor struct {
	// Source is a monitor kiln ships, such as go/docs, or a directory's path from the checkout's top. That path
	// starts with a dot or a slash, as one kiln monitor add takes does.
	Source string    `json:"source"`
	Files  fileLines `json:"files"`
}

// fileLines holds files as a record keeps them, each by its lines. A diff of the record then reads as a diff of them.
type fileLines map[string][]string

// linesOf is files as a record keeps them.
func linesOf(files map[string][]byte) fileLines {
	lines := fileLines{}
	for name, raw := range files {
		lines[name] = strings.Split(string(raw), "\n")
	}

	return lines
}

// file is the file f keeps at name, or nil when f keeps none there.
func (f fileLines) file(name string) []byte {
	lines, ok := f[name]
	if !ok {
		return nil
	}

	return []byte(strings.Join(lines, "\n"))
}

// added records the monitor name, as kiln monitor add or kiln init wrote it.
func (rec *initRecord) added(name Spec, m addedMonitor) {
	if rec.Monitors == nil {
		rec.Monitors = map[Spec]addedMonitor{}
	}

	rec.Monitors[name] = m
}

// newRecord is the record of files, the defaults of the preset named, but kiln.json, which kiln init never merges.
func newRecord(preset string, files map[string][]byte) initRecord {
	merged := maps.Clone(files)
	delete(merged, "kiln.json")
	return initRecord{Preset: preset, Files: linesOf(merged)}
}

// parseInit reads raw, the content of initFile.
func parseInit(raw []byte) (initRecord, error) {
	var rec initRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return rec, fmt.Errorf("%s: %w", initFile, err)
	}

	return rec, nil
}

// lastInit reads initFile in the checkout. With no file there, it answers an empty record.
func (r *Repo) lastInit() (initRecord, error) {
	raw, err := os.ReadFile(filepath.Join(r.top, initFile))
	if errors.Is(err, fs.ErrNotExist) {
		return initRecord{}, nil
	}

	if err != nil {
		return initRecord{}, err
	}

	return parseInit(raw)
}

// checkoutConfig reads kiln.json in the checkout, which may differ from main's. It refuses a checkout with none.
func (r *Repo) checkoutConfig() (config, error) {
	raw, err := os.ReadFile(filepath.Join(r.top, configFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return config{}, fmt.Errorf("%w: the checkout holds no %s: %s", errRefused, configFile, initHint)
	case err != nil:
		return config{}, err
	}

	return parseConfig(raw)
}

// writeDefaults brings each of p's files in .kiln/ up to date, as writeDefault says, then records them in initFile.
// It writes no default reviewer that deletedReviewers names. last is the record kiln init left before.
// writeDefaults prints a line for each file it changed, and answers whether it changed any.
func (r *Repo) writeDefaults(ctx context.Context, p preset, last initRecord, out io.Writer) (wrote bool, err error) {
	files, err := presetFiles(p)
	if err != nil {
		return false, err
	}

	deleted := r.deletedReviewers(files)
	say := func(line string) {
		if line != "" {
			wrote = true
			fmt.Fprintln(out, line)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		if deleted[name] {
			continue
		}

		line, err := r.writeDefault(ctx, name, last.Files.file(name), files[name])
		if err != nil {
			return wrote, err
		}

		say(line)
	}

	rec := newRecord(p.recorded(r.top), files)
	for _, name := range slices.Sorted(maps.Keys(last.Monitors)) {
		m, kept, err := r.updateMonitor(ctx, name, last.Monitors[name], say)
		if err != nil {
			return wrote, err
		}

		if kept {
			rec.added(name, m)
		}
	}

	line, err := r.writeRecord(rec)
	say(line)
	return wrote, err
}

// updateMonitor brings the monitor name, which kiln monitor add wrote as m, up to date with its source. It merges
// each file as writeDefault merges a default. It writes a file the source gains as kiln monitor add would. say
// prints each line writeDefault answers. A monitor whose directory the project deleted stays deleted: updateMonitor
// answers false, so the record drops it. One whose source kiln cannot read stays as it is, and say says why.
func (r *Repo) updateMonitor(ctx context.Context, name Spec, m addedMonitor, say func(string)) (addedMonitor, bool,
	error) {
	dir := monitor{Name: name}.path("")
	switch _, err := os.Lstat(filepath.Join(r.top, dir)); {
	case errors.Is(err, fs.ErrNotExist):
		return m, false, nil
	case err != nil:
		return m, false, err
	}

	from, files, err := r.sourceFiles(m.Source)
	if err != nil {
		say(fmt.Sprintf("kiln init left %s/ as it is, since it cannot read its source %s: %v", dir, m.Source, err))
		return m, true, nil
	}

	for _, f := range slices.Sorted(maps.Keys(files)) {
		rel := path.Join(dir, f)
		line, err := r.writeDefault(ctx, strings.TrimPrefix(rel, kilnDir+"/"), m.Files.file(f), files[f])
		if err == nil && strings.HasPrefix(line, "wrote ") {
			err = os.Chmod(filepath.Join(r.top, rel), addedMode(from, f))
		}

		if err != nil {
			return m, false, err
		}

		say(line)
	}

	return addedMonitor{Source: m.Source, Files: linesOf(files)}, true, nil
}

// deletedReviewers names each default reviewer in files that the project deleted, so kiln init writes it no more.
// Where .kiln/ has no reviewers folder, the project deleted none.
func (r *Repo) deletedReviewers(files map[string][]byte) map[string]bool {
	deleted := map[string]bool{}
	if _, err := os.Stat(filepath.Join(r.top, reviewersDir)); errors.Is(err, fs.ErrNotExist) {
		return deleted
	}

	for name := range files {
		_, err := os.Lstat(filepath.Join(r.top, kilnDir, name))
		deleted[name] = path.Dir(name) == "reviewers" && errors.Is(err, fs.ErrNotExist)
	}

	return deleted
}

// writeDefault brings the file at name, under .kiln/, to want, the default kiln init writes now. It keeps the
// project's edits since base, the default kiln init last wrote there. It writes over the schema, which kiln owns. It
// answers the line kiln init prints, or "" when it changed nothing.
func (r *Repo) writeDefault(ctx context.Context, name string, base, want []byte) (string, error) {
	rel := path.Join(kilnDir, name)
	target := filepath.Join(r.top, rel)
	have, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "wrote " + rel, writeFile(target, want)
	case err != nil:
		return "", err
	case rel == configFile:
		return keepConfig(target, have, want)
	case rel == schemaFile && !bytes.Equal(have, want):
		return "updated " + rel, os.WriteFile(target, want, 0o644)
	case bytes.Equal(have, want), bytes.Equal(base, want):
		return "", nil
	case bytes.Equal(have, base):
		return "updated " + rel, os.WriteFile(target, want, 0o644)
	}

	return r.mergeDefault(ctx, rel, base, want)
}

// keepConfig adds to have, the kiln.json at target, the shared keys it lacks, as want holds them. It answers the line
// kiln init prints, or "" when have lacks none.
func keepConfig(target string, have, want []byte) (string, error) {
	raw, err := addKeys(have, want)
	if err != nil || raw == nil {
		return "", err
	}

	return "wrote " + configFile, os.WriteFile(target, raw, 0o644)
}

// mergeDefault merges into the file at rel the change from base to want, with git merge-file. It leaves each
// conflict between the usual markers, and the line it answers says so.
func (r *Repo) mergeDefault(ctx context.Context, rel string, base, want []byte) (string, error) {
	dir, err := os.MkdirTemp("", "kiln-init-")
	if err != nil {
		return "", err
	}

	defer os.RemoveAll(dir)
	sides := []string{filepath.Join(dir, "base"), filepath.Join(dir, "default")}
	for i, raw := range [][]byte{base, want} {
		if err := os.WriteFile(sides[i], raw, 0o600); err != nil {
			return "", err
		}
	}

	_, err = plainGit(ctx, r.top, proc.Git.Env(r.env), "merge-file", "-L", rel, "-L", "kiln init's last default",
		"-L", "kiln init's new default", rel, sides[0], sides[1])
	// git merge-file exits with its count of conflicts, up to 127, and higher only when it fails.
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() < 128 {
		return "merged " + rel + ", with conflicts: resolve them before you commit it", nil
	}

	return "merged " + rel, err
}

// writeRecord writes rec to initFile, unless the file holds it already. It answers the line kiln init prints, or ""
// when it wrote nothing.
func (r *Repo) writeRecord(rec initRecord) (string, error) {
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return "", err
	}

	target := filepath.Join(r.top, initFile)
	if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, raw.Bytes()) {
		return "", nil
	}

	return "wrote " + initFile, writeFile(target, raw.Bytes())
}

// writeFile writes raw at target, with the directories above it.
func writeFile(target string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	return os.WriteFile(target, raw, 0o644)
}

// sharedKeys are the keys of kiln.json whose default is the same in every preset. A re-run of kiln init adds only
// these, so it never adds one language's commands to a project in another.
var sharedKeys = []string{"$schema", "rounds"}

// sharedDefaults holds the default of each of sharedKeys, which kiln init adds to a preset's kiln.json that lacks it.
const sharedDefaults = `{"$schema": "./kiln.schema.json", "rounds": 3}`

// addKeys adds to the JSON object old each of sharedKeys that it lacks, as from holds it. It keeps each byte of old,
// and puts each added key after the last, indented as that key. It answers nil when old lacks none.
func addKeys(old, from []byte) ([]byte, error) {
	var have, want map[string]json.RawMessage
	if err := json.Unmarshal(old, &have); err != nil {
		return nil, fmt.Errorf("%s: %w", configFile, err)
	}

	if err := json.Unmarshal(from, &want); err != nil {
		return nil, err
	}

	at, indent, err := lastMember(old)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", configFile, err)
	}

	var add bytes.Buffer
	for _, key := range sharedKeys {
		_, has := have[key]
		if _, ok := want[key]; has || !ok {
			continue
		}

		if len(have) > 0 || add.Len() > 0 {
			add.WriteByte(',')
		}

		fmt.Fprintf(&add, "%s%q: %s", indent, key, want[key])
	}

	if add.Len() == 0 {
		return nil, nil
	}

	return slices.Concat(old[:at], add.Bytes(), old[at:]), nil
}

// lastMember answers the offset just past the object obj's last value, and the white space before that value's key.
// With no member, at is the offset just past the opening brace, and indent is empty.
func lastMember(obj []byte) (at int, indent string, err error) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil {
		return 0, "", err
	}

	for at = int(dec.InputOffset()); dec.More(); at = int(dec.InputOffset()) {
		rest := obj[at:]
		if r := bytes.TrimLeft(rest, " \t\r\n"); len(r) > 0 && r[0] == ',' {
			rest = r[1:]
		}

		key := bytes.TrimLeft(rest, " \t\r\n")
		indent = string(rest[:len(rest)-len(key)])
		var value json.RawMessage
		if _, err := dec.Token(); err != nil {
			return 0, "", err
		}

		if err := dec.Decode(&value); err != nil {
			return 0, "", err
		}
	}

	return at, indent, nil
}

// link links .claude/<p> to .kiln/<own>, unless .claude/ holds something of that name that reaches a file. It links
// in place of a link to a reviewer's old file in .kiln/agents/, and says that file is unused. It says so when another
// file is there, so the developer can rename one.
func (r *Repo) link(p, own string, out io.Writer) error {
	at := filepath.Join(r.top, ".claude", p)
	to := filepath.Join(r.top, kilnDir, own)
	old := r.oldReviewer(at, own)
	_, err := os.Lstat(at)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case reachesNothing(at), old != "":
		// A link to a file moved away holds nothing of the developer's, so kiln's link takes its place.
		if err := os.Remove(at); err != nil {
			return err
		}
	case !sameFile(at, to):
		fmt.Fprintf(out, "linked nothing at .claude/%s, since another file is there; rename one to link %s/%s\n",
			p, kilnDir, own)
		return nil
	default:
		return nil
	}

	target, err := filepath.Rel(filepath.Dir(at), to)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}

	if err := os.Symlink(target, at); err != nil {
		return err
	}

	if old != "" {
		fmt.Fprintf(out, "linked .claude/%s to %s, in place of its link to %s, which kiln no longer reads: delete "+
			"that file\n", p, target, old)
		return nil
	}

	fmt.Fprintf(out, "linked .claude/%s to %s\n", p, target)
	return nil
}

// oldReviewer is the reviewer's file in .kiln/agents/ that the link at reaches, from before kiln read reviewers in
// .kiln/reviewers/. own is the reviewer's file there. It answers "" when at reaches no such file.
func (r *Repo) oldReviewer(at, own string) string {
	dir, file := path.Split(own)
	old := path.Join(kilnDir, "agents", file)
	if dir != "reviewers/" || !sameFile(at, filepath.Join(r.top, old)) {
		return ""
	}

	return old
}

// linkReviewers links .claude/agents/<file> to each reviewer's file in .kiln/reviewers/, as link does. It removes
// each link there to a reviewer whose file is gone. It refuses a reviewer that badReviewer refuses, before it links
// any.
func (r *Repo) linkReviewers(out io.Writer) error {
	files, err := r.reviewers()
	if err != nil {
		return err
	}

	for _, f := range files {
		if err := r.link(path.Join("agents", f), path.Join("reviewers", f), out); err != nil {
			return err
		}
	}

	return r.unlinkGone(out)
}

// unlinkGone removes each link in .claude/agents/ to a reviewer whose file is gone, and says so.
func (r *Repo) unlinkGone(out io.Writer) error {
	dir := filepath.Join(r.top, ".claude", "agents")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	for _, e := range entries {
		gone, ok := r.goneReviewer(filepath.Join(dir, e.Name()))
		if !ok {
			continue
		}

		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}

		fmt.Fprintf(out, "unlinked .claude/agents/%s, since %s is gone\n", e.Name(), gone)
	}

	return nil
}

// reviewers names each reviewer's file in .kiln/reviewers/. It refuses each that badReviewer refuses, with each reason.
func (r *Repo) reviewers() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(r.top, reviewersDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	var files []string
	var refused refusals
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".md" {
			continue
		}

		rel := path.Join(reviewersDir, e.Name())
		raw, err := os.ReadFile(filepath.Join(r.top, rel))
		if err != nil {
			return nil, err
		}

		refused = append(refused, badReviewer(e.Name(), string(raw)).about(rel+",")...)
		files = append(files, e.Name())
	}

	if len(refused) > 0 {
		return nil, fmt.Errorf("%w: %w", errRefused, refused)
	}

	return files, nil
}

// badReviewer says why Claude Code could not run the reviewer that raw holds as a subagent. Claude Code needs a name
// and a description. The name must be the file's, without .md, so /shape and the link agree on it.
func badReviewer(file, raw string) refusals {
	front, _, err := frontmatter(raw)
	// frontmatter refuses only with refusals.
	if refused, ok := errors.AsType[refusals](err); ok {
		return refused
	}

	keys := map[string]string{}
	for line := range strings.Lines(front) {
		if key, value, ok := field(line); ok {
			keys[key] = value
		}
	}

	name := strings.TrimSuffix(file, ".md")
	return broken(
		rule{keys["name"] != name, refusal{fmt.Sprintf("its frontmatter's name is %q", keys["name"]),
			fmt.Sprintf("make it %q, its file's name", name)}},
		rule{keys["description"] == "", refusal{"its frontmatter has no description, which Claude Code needs to run it",
			"add one that says what the reviewer reads for"}},
	)
}

// goneReviewer answers the reviewer's file that the link at reaches, from the checkout's top, when that file is gone.
// It answers false when at is no link, or reaches anything else.
func (r *Repo) goneReviewer(at string) (string, bool) {
	target, err := os.Readlink(at)
	if err != nil {
		return "", false
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(at), target)
	}

	if filepath.Dir(target) != filepath.Join(r.top, reviewersDir) {
		return "", false
	}

	_, err = os.Lstat(target)
	return path.Join(reviewersDir, filepath.Base(target)), errors.Is(err, fs.ErrNotExist)
}

// reachesNothing reports whether the link at reaches no file.
func reachesNothing(at string) bool {
	_, err := os.Stat(at)
	return errors.Is(err, fs.ErrNotExist)
}

// sameFile reports whether paths a and b reach one file, through any links on the way. A path that reaches nothing
// is no file's.
func sameFile(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(x, y)
}
