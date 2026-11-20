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

// schema is kiln.json's JSON Schema, which an editor reads by kiln.json's "$schema". TestSchema builds it from config.
//
//go:embed kiln.schema.json
var schema []byte

// schemaFile is where kiln init writes schema.
const schemaFile = kilnDir + "/kiln.schema.json"

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
// holds no file of its name. It writes kiln.schema.json over, since kiln owns it. A kiln.json that is there gains
// only $schema and rounds, the keys every preset shares, where it lacks them. It links /shape and its reviewers into
// .claude/, each where .claude/ holds nothing of its name. It says so when the repository has no branch main, which
// each command that reads main then refuses. It prints kiln.json's gate, the commands the pre-commit hook runs on each
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

	for _, p := range linked {
		if err := r.link(p, out); err != nil {
			return err
		}
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

	// A file kiln cannot read is written over, as a missing one is.
	if old, _ := os.ReadFile(filepath.Join(r.top, schemaFile)); bytes.Equal(old, schema) {
		return wrote, nil
	}

	if err := os.WriteFile(filepath.Join(r.top, schemaFile), schema, 0o644); err != nil {
		return wrote, err
	}

	fmt.Fprintf(out, "wrote %s\n", schemaFile)
	return true, nil
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

// link links .claude/<p> to .kiln/<p>, unless .claude/ holds something of that name. It says so when that
// something reaches a file other than .kiln/<p>, so the developer can rename one.
func (r *Repo) link(p string, out io.Writer) error {
	at := filepath.Join(r.top, ".claude", p)
	own := filepath.Join(r.top, kilnDir, p)
	if _, err := os.Lstat(at); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}

		if !sameFile(at, own) {
			fmt.Fprintf(out, "linked nothing at .claude/%s, since another file is there; rename one to link %s/%s\n",
				p, kilnDir, p)
		}

		return nil
	}

	target, err := filepath.Rel(filepath.Dir(at), own)
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

// sameFile reports whether paths a and b reach one file, through any links on the way. A path that reaches nothing
// is no file's.
func sameFile(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(x, y)
}
