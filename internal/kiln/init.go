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

// defaults are what kiln init writes into .kiln/, written for any project. The project owns them from then on.
//
//go:embed defaults
var defaults embed.FS

// linked are the defaults kiln init links into .claude/, so a session the developer runs has /shape and its
// reviewers. Neither the build agent nor the audit is linked: a developer who wants one as a subagent links it.
var linked = []string{"skills/shape", "agents/zoom-out.md", "agents/comprehension.md", "agents/critic.md"}

// Init writes kiln's defaults into .kiln/ in the checkout, each where .kiln/ holds no file of its name. To
// a kiln.json that is there, it adds only the keys the file lacks. It links /shape and its reviewers into .claude/,
// each where .claude/ holds nothing of its name. It installs the pre-commit hook. The hook refuses a commit to main
// that fails kiln.json's commands. Init refuses to write over another pre-commit hook.
func (r *Repo) Init(ctx context.Context, out io.Writer) error {
	wrote, err := r.writeDefaults(out)
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

	return r.installHook(ctx, out)
}

// writeDefaults writes each default that .kiln/ lacks, and adds to kiln.json the keys it lacks. It answers whether it
// wrote anything.
func (r *Repo) writeDefaults(out io.Writer) (wrote bool, err error) {
	err = fs.WalkDir(defaults, "defaults", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		raw, err := defaults.ReadFile(p)
		if err != nil {
			return err
		}

		rel := path.Join(kilnDir, p[len("defaults/"):])
		changed, err := r.writeDefault(rel, raw)
		if changed {
			wrote = true
			fmt.Fprintf(out, "wrote %s\n", rel)
		}

		return err
	})

	return wrote, err
}

// writeDefault writes raw at rel, unless the checkout holds a file there. Into kiln.json, it adds only the keys the
// file lacks. It answers whether it changed the file.
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

// addKeys adds to the JSON object old each key of from that it lacks. It answers nil when old lacks none.
func addKeys(old, from []byte) ([]byte, error) {
	var have, want map[string]json.RawMessage
	if err := json.Unmarshal(old, &have); err != nil {
		return nil, fmt.Errorf("%s: %w", configFile, err)
	}

	if err := json.Unmarshal(from, &want); err != nil {
		return nil, err
	}

	added := false
	for _, key := range slices.Sorted(maps.Keys(want)) {
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
