package kiln

import (
	"bytes"
	"testing"
)

// TestPresetsFillMarkers holds that each preset fills every marker in the defaults, and leaves none behind.
func TestPresetsFillMarkers(t *testing.T) {
	for _, p := range presets {
		t.Run(string(p), func(t *testing.T) {
			files, err := presetFiles(p)
			if err != nil {
				t.Fatal(err)
			}

			for _, name := range []string{"kiln.json", "agents/build.md", "agents/audit.md"} {
				if len(files[name]) == 0 {
					t.Errorf("preset %s writes no %s", p, name)
				}
			}

			for name, raw := range files {
				if bytes.Contains(raw, []byte("kiln:preset")) {
					t.Errorf("preset %s leaves a marker in %s", p, name)
				}
			}
		})
	}
}
