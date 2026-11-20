package kiln

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// presetFS holds each built-in preset's kiln.json, and its text for each marker in the defaults' prompts.
//
//go:embed presets
var presetFS embed.FS

// preset is the language-specific part of what kiln init writes: kiln.json, and the "Tailor this" sections.
type preset struct {
	// name is a built-in preset's name, or the absolute path of a directory laid out as one.
	name  string
	files fs.FS
}

// builtIns are the presets kiln holds, the first when the developer names none.
var builtIns = []string{"go", "python"}

// marker is a line in a default prompt that kiln init replaces with a preset's file of the marker's name.
var marker = regexp.MustCompile(`(?m)^<!-- kiln:preset ([a-z-]+) -->\n`)

// presetFiles are the files kiln init writes for p, by path under .kiln/. They are p's kiln.json, and every default,
// with each marker replaced. It fails when a marker names a file p lacks.
func presetFiles(p preset) (map[string][]byte, error) {
	config, err := fs.ReadFile(p.files, "kiln.json")
	if err == nil {
		err = usableConfig(config)
	}

	if err != nil {
		return nil, fmt.Errorf("preset %s: %w", p.name, err)
	}

	files := map[string][]byte{"kiln.json": config}
	err = fs.WalkDir(defaults, "defaults", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		raw, err := defaults.ReadFile(name)
		if err != nil {
			return err
		}

		files[strings.TrimPrefix(name, "defaults/")], err = p.fill(raw)
		return err
	})

	return files, err
}

// usableConfig refuses raw, a preset's kiln.json, when kiln could not use it, or it lacks a key every preset shares.
// kiln init then refuses the preset before it writes anything.
func usableConfig(raw []byte) error {
	if _, err := parseConfig(raw); err != nil {
		return fmt.Errorf("kiln.json: %w", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return err
	}

	for _, key := range sharedKeys {
		if _, ok := keys[key]; !ok {
			return fmt.Errorf("kiln.json lacks %s, which every preset holds", key)
		}
	}

	return nil
}

// fill replaces each marker in raw with p's file of the marker's name.
func (p preset) fill(raw []byte) ([]byte, error) {
	var failed error
	filled := marker.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(marker.FindSubmatch(m)[1]) + ".md"
		text, err := fs.ReadFile(p.files, name)
		if err != nil {
			failed = cmpOr(failed, fmt.Errorf("preset %s fills no marker %s: %w", p.name, name, err))
		}

		return text
	})

	return filled, failed
}

// ErrUsage is the kind of a command kiln refuses for its arguments. Init returns it for a preset it does not know.
// Hook returns it for arguments kiln never writes.
var ErrUsage = errors.New("usage")

// presetNamed is the preset name names: a built-in one's name, or a directory laid out as one. A directory's path
// holds a slash, and a relative one is from dir. presetNamed refuses any other name, and names each built-in.
func presetNamed(name, dir string) (preset, error) {
	switch {
	case filepath.IsAbs(name):
		// The checkout's top has its links resolved, so a path recorded from it needs them resolved too.
		real, err := filepath.EvalSymlinks(name)
		if err != nil {
			return preset{}, fmt.Errorf("preset %s: %w", name, err)
		}

		return preset{name: real, files: os.DirFS(real)}, nil
	case strings.Contains(name, "/"):
		return presetNamed(filepath.Join(dir, name), dir)
	case slices.Contains(builtIns, name):
		files, err := fs.Sub(presetFS, path.Join("presets", name))
		return preset{name: name, files: files}, err
	}

	return preset{}, fmt.Errorf("no preset %q: name one of %s, or a directory by a path that holds a slash", name,
		strings.Join(builtIns, ", "))
}

// recorded is p's name as kiln init's record keeps it, where a directory's path is from top, the checkout's top.
func (p preset) recorded(top string) string {
	rel, err := filepath.Rel(top, p.name)
	switch {
	case !filepath.IsAbs(p.name), err != nil:
		return p.name
	case !strings.Contains(rel, "/"):
		// A name with no slash would read back as a built-in preset's.
		return "./" + rel
	}

	return rel
}
