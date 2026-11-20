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

// initHint is how a refusal says to start .kiln/. It names the preset, since kiln init with none writes Go's.
const initHint = "run kiln init --preset go or python, as the project is, and commit what it writes"

// initFile is kiln init's record of the defaults it last wrote, which its next run merges the project's edits against.
const initFile = kilnDir + "/init.json"

// InitOptions are what the developer chose for kiln init.
type InitOptions struct {
	// Preset names the preset Init writes: go, python, or a directory laid out as one. A directory's path holds a
	// slash, and a relative one is from the working directory. "" means the preset .kiln/init.json names, or go.
	Preset string
}

// Init brings .kiln/ in the checkout up to date with kiln's defaults, as opt's preset fills them. It writes each
// default that .kiln/ lacks, but a reviewer the project deleted from its reviewers folder. A folder holding only a
// .gitkeep keeps no reviewer. A file the project left as Init last wrote it takes the new default. Into a file the
// project changed, git merge-file merges the change between the last default and the new one. It leaves each
// conflict between the usual markers, and names the file. With no record of the last default, each difference from
// the new one is a conflict. It writes kiln.schema.json over, since kiln owns it. Init stages and commits nothing.
// It records each default of the preset, and the preset, in .kiln/init.json.
//
// kiln.json is the project's settings: its gate, and what a session reaches. A merge could change either unread, so
// Init never merges kiln.json. A kiln.json that is there gains only $schema and rounds, the keys every preset
// shares, where it lacks them.
//
// Init links /shape, and each reviewer's file in .kiln/reviewers/, into .claude/. It links each where .claude/ holds
// nothing of its name, or a link that reaches nothing. It removes each link in .claude/agents/ to a reviewer's file
// that is gone. It says so when the repository has no branch main, which each command that reads main then refuses.
// It prints kiln.json's gate, the commands the pre-commit hook runs on each commit to main. The hook refuses a commit
// to main that fails them. Last, Init installs the hook, or says why it installed none, as installHook says.
//
// Before it writes anything, Init refuses a preset it does not know or cannot use. A preset's kiln.json must hold
// rounds. Init refuses a reviewer Claude Code could not run before it writes anything too. Such a reviewer's
// frontmatter has no description, or a name other than its file's, without .md.
func (r *Repo) Init(ctx context.Context, out io.Writer, opt InitOptions) error {
	last, err := r.lastInit()
	if err != nil {
		return err
	}

	p, err := r.initPreset(opt.Preset, last.Preset)
	if err != nil {
		return err
	}

	// A refusal after the first write would leave .kiln/ written, and a re-run would not say to commit it.
	if _, err := r.reviewers(); err != nil {
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

// initPreset is the preset kiln init writes: the one named, else the one recorded, else the first built-in. A path
// named is from the working directory, and a path recorded is from the checkout's top.
func (r *Repo) initPreset(named, recorded string) (preset, error) {
	if named == "" {
		p, err := presetNamed(cmpOr(recorded, builtIns[0]), r.top)
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
	raw, found, err := r.git.Show(ctx, main, initFile)
	if err != nil {
		return err
	}

	if !found {
		_, set, err := r.git.Show(ctx, main, configFile)
		if set {
			fmt.Fprintf(out, "%s/ holds no record of what kiln init wrote: run kiln init, which leaves each "+
				"difference from its defaults as a conflict\n", kilnDir)
		}

		return err
	}

	from, behind, err := r.behind(raw)
	switch {
	case err != nil:
		fmt.Fprintf(out, "kiln could not tell whether %s/ is up to date: %v\n", kilnDir, err)
	case behind:
		fmt.Fprintf(out, "%s/ is behind the defaults of kiln init's preset %s: run kiln init, which merges them "+
			"with your edits\n", kilnDir, from)
	}

	return nil
}

// behind answers whether raw, a record of kiln init, differs from what kiln init would write now. It answers the
// preset the record names too, with its path from the checkout's top.
func (r *Repo) behind(raw []byte) (string, bool, error) {
	last, err := parseInit(raw)
	if err != nil {
		return "", false, err
	}

	from := cmpOr(last.Preset, builtIns[0])
	p, err := presetNamed(from, r.top)
	if err != nil {
		return from, false, err
	}

	files, err := presetFiles(p)
	now := newRecord(from, files)
	return from, !maps.EqualFunc(last.Files, now.Files, slices.Equal[[]string]), err
}

// initRecord is what initFile holds: the preset kiln init last wrote, and each default but kiln.json. A default is
// kept as its lines, so a diff of the record reads as a diff of the files.
type initRecord struct {
	Preset string              `json:"preset"`
	Files  map[string][]string `json:"files"`
}

// newRecord is the record of files, the defaults of the preset named, but kiln.json, which kiln init never merges.
func newRecord(preset string, files map[string][]byte) initRecord {
	rec := initRecord{Preset: preset, Files: map[string][]string{}}
	for name, raw := range files {
		if name != "kiln.json" {
			rec.Files[name] = strings.Split(string(raw), "\n")
		}
	}

	return rec
}

// file is the default kiln init last wrote at name, under .kiln/, or nil when the record holds none there.
func (rec initRecord) file(name string) []byte {
	lines, ok := rec.Files[name]
	if !ok {
		return nil
	}

	return []byte(strings.Join(lines, "\n"))
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

		line, err := r.writeDefault(ctx, name, last.file(name), files[name])
		if err != nil {
			return wrote, err
		}

		say(line)
	}

	line, err := r.writeRecord(newRecord(p.recorded(r.top), files))
	say(line)
	return wrote, err
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

// link links .claude/<p> to .kiln/<own>, unless .claude/ holds something of that name that reaches a file. It says so
// when that file is not .kiln/<own>, so the developer can rename one.
func (r *Repo) link(p, own string, out io.Writer) error {
	at := filepath.Join(r.top, ".claude", p)
	to := filepath.Join(r.top, kilnDir, own)
	_, err := os.Lstat(at)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case reachesNothing(at):
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

	fmt.Fprintf(out, "linked .claude/%s to %s\n", p, target)
	return nil
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

// reviewers names each reviewer's file in .kiln/reviewers/. It refuses one that badReviewer refuses.
func (r *Repo) reviewers() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(r.top, reviewersDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".md" {
			continue
		}

		rel := path.Join(reviewersDir, e.Name())
		raw, err := os.ReadFile(filepath.Join(r.top, rel))
		if err != nil {
			return nil, err
		}

		if err := badReviewer(e.Name(), string(raw)); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", errRefused, rel, err)
		}

		files = append(files, e.Name())
	}

	return files, nil
}

// badReviewer says why Claude Code could not run the reviewer that raw holds as a subagent, or answers nil. Claude Code
// needs a name and a description. The name must be the file's, without .md, so /shape and the link agree on it.
func badReviewer(file, raw string) error {
	front, _, err := frontmatter(raw)
	if err != nil {
		return err
	}

	keys := map[string]string{}
	for line := range strings.Lines(front) {
		if key, value, ok := field(line); ok {
			keys[key] = value
		}
	}

	switch name := strings.TrimSuffix(file, ".md"); {
	case keys["name"] != name:
		return refusals{{fmt.Sprintf("its frontmatter's name is %q", keys["name"]),
			fmt.Sprintf("make it %q, its file's name", name)}}
	case keys["description"] == "":
		return refusals{{"its frontmatter has no description, which Claude Code needs to run it",
			"add one that says what the reviewer reads for"}}
	}

	return nil
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
