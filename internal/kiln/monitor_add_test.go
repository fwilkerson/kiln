package kiln

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// TestMonitorAdd holds what kiln monitor add copies into .kiln/monitors/, and what it refuses before it writes. A
// source is a monitor kiln ships, by name, or a directory, by a path. kiln writes each script it runs executable, and
// each file the source holds executable. want marks those with a *, as ls -F does.
func TestMonitorAdd(t *testing.T) {
	team := t.TempDir()
	writeAll(t, team, map[string]string{"deps/command": "go get -u ./...", "deps/monitor.json": `{"hours": 168}`,
		"deps/notes/why.md": "Why deps.", "deps/lib/run": "go get -u ./...", "broken/check": "true",
		"vague/command": "true", "vague/agent.md": "---\neffort: extreme\n---\nFix it.\n"})
	if err := os.Chmod(filepath.Join(team, "deps/lib/run"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		source         string
		there          bool
		refused, usage bool
		want           []string
	}{
		"a monitor kiln ships": {source: "bugs", want: []string{"bugs/agent.md", "bugs/check*", "bugs/monitor.json"}},
		"one under its preset": {source: "go/docs", want: []string{"docs/agent.md", "docs/check*", "docs/monitor.json"}},
		"a team's directory": {source: team + "/deps",
			want: []string{"deps/command*", "deps/lib/run*", "deps/monitor.json", "deps/notes/why.md"}},
		"a preset, which is no monitor": {source: "go", refused: true, usage: true},
		"a name kiln ships nothing of":  {source: "deps", refused: true, usage: true},
		"a path to no directory":        {source: "./nowhere", refused: true, usage: true},
		"a monitor kiln could not run":  {source: team + "/broken", refused: true},
		"an agent kiln could not read":  {source: team + "/vague", refused: true},
		"a name already there":          {source: "bugs", there: true, refused: true, want: []string{"bugs/command*"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &Repo{top: t.TempDir()}
			writeAll(t, r.top, map[string]string{configFile: `{"gate": ["true"], "acceptance": "acceptance/{spec}/"}`})
			if tc.there {
				writeMonitor(t, r.top, "bugs", map[string]string{"command": "mine"})
			}

			err := r.MonitorAdd(tc.source, io.Discard)
			if (err != nil) != tc.refused || errors.Is(err, ErrUsage) != tc.usage {
				t.Fatalf("kiln monitor add %s answers %v; want a refusal %v, as a usage error %v", tc.source, err,
					tc.refused, tc.usage)
			}

			if got := filesUnder(t, filepath.Join(r.top, monitorsDir)); !slices.Equal(got, tc.want) {
				t.Errorf(".kiln/monitors/ holds %v; want %v", got, tc.want)
			}
		})
	}
}

// kilnOwn matches what names kiln's own repository, which a monitor any project may add never names.
var kilnOwn = regexp.MustCompile(`fwilkerson|docs/practices\.md|uat/|internal/|scenariotest`)

// TestShippedMonitors holds that kiln could run each monitor it ships, in a project whose tests it can guard. None
// names a path of kiln's own repository.
func TestShippedMonitors(t *testing.T) {
	c, err := parseConfig([]byte(`{"gate": ["true"], "acceptance": "acceptance/{spec}/"}`))
	if err != nil || len(shippedNames()) == 0 {
		t.Fatalf("kiln.json reads %v, and kiln ships %v", err, shippedNames())
	}

	for _, name := range shippedNames() {
		from, err := monitorSource(name)
		if err != nil {
			t.Fatal(err)
		}

		files, settings, err := monitorFiles(from)
		if err != nil {
			t.Fatal(err)
		}

		if _, refused := c.readMonitor(Spec(path.Base(name)), files, settings); len(refused) > 0 {
			t.Errorf("kiln ships %s, which it could not run: %v", name, refused)
		}

		for _, f := range files {
			if raw, _ := fs.ReadFile(from, f); kilnOwn.Match(raw) {
				t.Errorf("%s/%s names %q, of kiln's own repository", name, f, kilnOwn.Find(raw))
			}
		}
	}
}

// filesUnder lists each file under dir, by its slash path from dir, or nil when dir holds none. An executable file
// ends in a *.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(dir, p)
		info, err := d.Info()
		files = append(files, filepath.ToSlash(rel)+map[bool]string{true: "*"}[err == nil && info.Mode()&0o111 != 0])
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}

	return files
}
