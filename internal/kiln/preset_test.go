package kiln

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

// TestPresetsFillMarkers holds that each built-in preset fills every marker in the defaults, and leaves none behind.
// Each file of a built-in preset, but kiln.json, fills a marker.
func TestPresetsFillMarkers(t *testing.T) {
	for _, name := range builtIns {
		p, err := presetNamed(name, "")
		if err != nil {
			t.Fatal(err)
		}

		files, err := presetFiles(p)
		if err != nil {
			t.Errorf("preset %s: %v", name, err)
			continue
		}

		for _, name := range []string{"kiln.json", "agents/build.md", "agents/verify.md", "agents/audit.md"} {
			if len(files[name]) == 0 {
				t.Errorf("preset %s writes no %s", p.name, name)
			}
		}

		for name, raw := range files {
			if bytes.Contains(raw, []byte("kiln:preset")) {
				t.Errorf("preset %s leaves a marker in %s", p.name, name)
			}
		}

		wantMarked(t, p)
	}
}

// wantMarked holds that a marker in the defaults names each file of p but kiln.json.
func wantMarked(t *testing.T, p preset) {
	t.Helper()
	entries, err := fs.ReadDir(p.files, ".")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		named := false
		stem := strings.TrimSuffix(e.Name(), ".md")
		_ = fs.WalkDir(defaults, "defaults", func(name string, d fs.DirEntry, err error) error {
			raw, _ := defaults.ReadFile(name)
			named = named || bytes.Contains(raw, []byte("<!-- kiln:preset "+stem+" -->"))
			return err
		})

		if e.Name() != "kiln.json" && !named {
			t.Errorf("preset %s: no marker names %s", p.name, e.Name())
		}
	}
}
