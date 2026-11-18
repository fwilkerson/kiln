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
)

// defaults are what kiln init writes into .kiln/ for every preset. A preset fills each marker in their prompts, and
// adds kiln.json. The project owns them from then on.
//
//go:embed defaults
var defaults embed.FS

// linked are the defaults kiln init links into .claude/, so a session the developer runs has /shape and its
// reviewers. Neither the build agent nor the audit is linked: a developer who wants one as a subagent links it.
var linked = []string{"skills/shape", "agents/zoom-out.md", "agents/comprehension.md", "agents/critic.md"}

// initHint is how a refusal says to start .kiln/. It names the preset, since kiln init with none writes Go's.
const initHint = "run kiln init --preset go or python, as the project is, and commit what it writes"

// InitOptions are what the developer chose for kiln init.
type InitOptions struct {
	// Preset names the preset Init writes, go or python. "" means go, even in a project that started from python.
	Preset string
}

// Init writes kiln's defaults into .kiln/ in the checkout, as opt's preset fills them. It writes each where .kiln/
// holds no file of its name. To a kiln.json that is there, it adds only rounds, the key every preset shares, if the
// file lacks it. It links /shape and its reviewers into .claude/, each where .claude/ holds nothing of its name.
// It installs the pre-commit hook, and prints kiln.json's gate, the commands the hook runs on each commit to main.
// The hook refuses a commit to main that fails them. Init refuses
// a preset it does not know before it writes anything, and refuses to write over another pre-commit hook. Last, it
// says so when the repository has no branch main, which each command that reads main then refuses.
func (r *Repo) Init(ctx context.Context, out io.Writer, opt InitOptions) error {
	p, err := presetNamed(opt.Preset)
	if err != nil {
		return err
	}

	wrote, err := r.writeDefaults(p, out)
	if err != nil {
		return err
	}

	for _, p := range linked {
		if err := r.link(p, out); err != nil {
			return err
		}
	}

	if wrote {
		fmt.Fprintf(out, "commit %s/ on main: kiln reads it there\n", kilnDir)
	}

	if err := r.installHook(ctx, out); err != nil {
		return err
	}

	if err := r.sayGate(out, wrote); err != nil {
		return err
	}

	if _, err := r.mainTip(ctx); errors.Is(err, errNoMain) {
		fmt.Fprintln(out, noMain)
	}

	return nil
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

// writeDefaults writes each of p's files that .kiln/ lacks, and adds to kiln.json the shared keys it lacks. It
// answers whether it wrote anything.
func (r *Repo) writeDefaults(p preset, out io.Writer) (wrote bool, err error) {
	files, err := presetFiles(p)
	if err != nil {
		return false, err
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		rel := path.Join(kilnDir, name)
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

// addKeys adds to the JSON object old each of sharedKeys that it lacks, as from holds it. It answers nil when old
// lacks none.
func addKeys(old, from []byte) ([]byte, error) {
	var have, want map[string]json.RawMessage
	if err := json.Unmarshal(old, &have); err != nil {
		return nil, fmt.Errorf("%s: %w", configFile, err)
	}

	if err := json.Unmarshal(from, &want); err != nil {
		return nil, err
	}

	added := false
	for _, key := range sharedKeys {
		if _, ok := have[key]; !ok {
			have[key], added = want[key], true
		}
	}

	if !added {
		return nil, nil
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(have)
	return b.Bytes(), err
}

// link links .claude/<p> to .kiln/<p>, unless .claude/ holds something of that name.
func (r *Repo) link(p string, out io.Writer) error {
	at := filepath.Join(r.top, ".claude", p)
	if _, err := os.Lstat(at); !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}

	target, err := filepath.Rel(filepath.Dir(at), filepath.Join(r.top, kilnDir, p))
	if err != nil {
		return err
	}

	if err := os.Symlink(target, at); err != nil {
		return err
	}

	fmt.Fprintf(out, "linked .claude/%s to %s\n", p, target)
	return nil
}
