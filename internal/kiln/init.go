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
	"path"
	"path/filepath"
	"slices"
	"strings"
)

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

// InitOptions are what the developer chose for kiln init.
type InitOptions struct {
	// Preset names the preset Init writes, go or python. "" means go, even in a project that started from python.
	Preset string
}

// Init writes kiln's defaults into .kiln/ in the checkout, as opt's preset fills them. It writes each where .kiln/
// holds no file of its name. It writes the default reviewers only where .kiln/ holds no reviewers folder. To a
// kiln.json that is there, it adds only rounds, the key every preset shares, if the file lacks it. It links /shape and
// each reviewer into .claude/, as linkReviewers says. It says so when the repository has no branch main, which each
// command that reads main then refuses. It prints kiln.json's gate, the commands the pre-commit hook runs on each
// commit to main. The hook refuses a commit to main that fails them. Last, Init installs the hook, or says why it
// installed none, as installHook says. Init refuses a preset it does not know before it writes anything.
func (r *Repo) Init(ctx context.Context, out io.Writer, opt InitOptions) error {
	p, err := presetNamed(opt.Preset)
	if err != nil {
		return err
	}

	wrote, err := r.writeDefaults(p, out)
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

// sayGate prints the gate's commands of the checkout's kiln.json, which the pre-commit hook runs on each commit to
// main. When Init wrote .kiln/, it says they must pass before .kiln/ is committed.
func (r *Repo) sayGate(out io.Writer, wrote bool) error {
	raw, err := os.ReadFile(filepath.Join(r.top, configFile))
	if err != nil {
		return err
	}

	c, err := parseConfig(raw)
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

// writeDefaults writes each of p's files that .kiln/ lacks, and adds to kiln.json the shared keys it lacks. It writes
// no default reviewer into a reviewers folder that is there, so a reviewer the project deleted stays deleted. It
// answers whether it wrote anything.
func (r *Repo) writeDefaults(p preset, out io.Writer) (wrote bool, err error) {
	files, err := presetFiles(p)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(filepath.Join(r.top, reviewersDir))
	keepReviewers := !errors.Is(err, fs.ErrNotExist)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		rel := path.Join(kilnDir, name)
		if keepReviewers && path.Dir(rel) == reviewersDir {
			continue
		}

		changed, err := r.writeDefault(rel, files[name])
		if err != nil {
			return wrote, err
		}

		if changed {
			wrote = true
			fmt.Fprintf(out, "wrote %s\n", rel)
		}
	}

	return wrote, nil
}

// writeDefault writes raw at rel, unless the checkout holds a file there. Into kiln.json, it adds only the shared keys
// the file lacks. It answers whether it changed the file.
func (r *Repo) writeDefault(rel string, raw []byte) (bool, error) {
	target := filepath.Join(r.top, rel)
	old, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, err
	case rel != configFile:
		return false, nil
	default:
		if raw, err = addKeys(old, raw); err != nil || raw == nil {
			return false, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}

	return true, os.WriteFile(target, raw, 0o644)
}

// sharedKeys are the keys of kiln.json whose default is the same in every preset. A re-run of kiln init adds only
// these, so it never adds one language's commands to a project in another.
var sharedKeys = []string{"rounds"}

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
		if _, ok := have[key]; ok {
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

// link links .claude/<p> to .kiln/<own>, unless .claude/ holds something of that name. It says so when that
// something reaches a file other than .kiln/<own>, so the developer can rename one.
func (r *Repo) link(p, own string, out io.Writer) error {
	at := filepath.Join(r.top, ".claude", p)
	to := filepath.Join(r.top, kilnDir, own)
	if _, err := os.Lstat(at); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}

		if !sameFile(at, to) {
			fmt.Fprintf(out, "linked nothing at .claude/%s, since another file is there; rename one to link %s/%s\n",
				p, kilnDir, own)
		}

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
		return fmt.Errorf("its frontmatter's name is %q; make it %q, its file's name", keys["name"], name)
	case keys["description"] == "":
		return errors.New("its frontmatter has no description, which Claude Code needs to run it")
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

// sameFile reports whether paths a and b reach one file, through any links on the way. A path that reaches nothing
// is no file's.
func sameFile(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(x, y)
}
