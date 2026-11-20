package kiln

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// presetFS holds each preset's kiln.json, and its text for each marker in the defaults' prompts.
//
//go:embed presets
var presetFS embed.FS

// preset names the language-specific part of what kiln init writes: kiln.json, and the "Tailor this" sections.
type preset string

const (
	presetGo     preset = "go"
	presetPython preset = "python"
)

// presets are the presets kiln init writes, the first when the developer names none.
var presets = []preset{presetGo, presetPython}

// marker is a line in a default prompt that kiln init replaces with a preset's file of the marker's name.
var marker = regexp.MustCompile(`(?m)^<!-- kiln:preset ([a-z-]+) -->\n`)

// presetFiles are the files kiln init writes for p, by path under .kiln/. They are p's kiln.json, and every default,
// with each marker replaced. It fails when a marker names a file p lacks, or p holds a file no marker names.
func presetFiles(p preset) (map[string][]byte, error) {
	dir := path.Join("presets", string(p))
	config, err := presetFS.ReadFile(path.Join(dir, "kiln.json"))
	if err != nil {
		return nil, fmt.Errorf("preset %s: %w", p, err)
	}

	files := map[string][]byte{"kiln.json": config}
	var used []string
	err = fs.WalkDir(defaults, "defaults", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		raw, err := defaults.ReadFile(name)
		if err != nil {
			return err
		}

		filled, names, err := fill(raw, dir)
		files[strings.TrimPrefix(name, "defaults/")], used = filled, append(used, names...)
		return err
	})
	if err != nil {
		return nil, err
	}

	return files, unused(dir, used)
}

// fill replaces each marker in raw with the file of its name in dir. It answers the names it filled.
func fill(raw []byte, dir string) ([]byte, []string, error) {
	var names []string
	var failed error
	filled := marker.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(marker.FindSubmatch(m)[1]) + ".md"
		text, err := presetFS.ReadFile(path.Join(dir, name))
		if err != nil {
			failed = cmpOr(failed, fmt.Errorf("preset %s fills no marker %s: %w", path.Base(dir), name, err))
		}

		names = append(names, name)
		return text
	})

	return filled, names, failed
}

// unused fails when dir holds a file, but kiln.json, that no marker named.
func unused(dir string, used []string) error {
	entries, err := presetFS.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.Name() != "kiln.json" && !slices.Contains(used, e.Name()) {
			return fmt.Errorf("preset %s: no marker names %s", path.Base(dir), e.Name())
		}
	}

	return nil
}

// ErrUsage is the kind of a command kiln refuses for its arguments. Init returns it for a preset it does not know.
// Hook returns it for arguments kiln never writes. MonitorAdd returns it for a monitor kiln does not ship.
var ErrUsage = errors.New("usage")

// presetNamed is the preset of name, or the first for "". It refuses any other name, and names each preset.
func presetNamed(name string) (preset, error) {
	if name == "" {
		return presets[0], nil
	}

	if p := preset(name); slices.Contains(presets, p) {
		return p, nil
	}

	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = string(p)
	}

	return "", fmt.Errorf("%w: no preset %q: name one of %s", ErrUsage, name, strings.Join(names, ", "))
}
