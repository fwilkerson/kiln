package kiln

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/practices/claim"
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
		says           string // a part of the refusal
	}{
		"a monitor kiln ships": {source: "bugs", want: []string{"bugs/agent.md", "bugs/check*", "bugs/monitor.json"}},
		"one under its preset": {source: "go/docs", want: []string{"docs/agent.md", "docs/check*", "docs/monitor.json"}},
		"a team's directory": {source: team + "/deps",
			want: []string{"deps/command*", "deps/lib/run*", "deps/monitor.json", "deps/notes/why.md"}},
		"a preset, which is no monitor": {source: "go", refused: true, usage: true},
		"a name kiln ships nothing of": {source: "deps", refused: true, usage: true,
			says: "or a directory, by a path that starts with . or /, such as ./deps"},
		"a path to no directory": {source: "./nowhere", refused: true, usage: true},
		"a monitor kiln could not run": {source: team + "/broken", refused: true,
			says: team + "/broken/ holds no command and no agent.md"},
		"an agent kiln could not read": {source: team + "/vague", refused: true, says: team + "/vague/agent.md, its effort"},
		"a name already there":         {source: "bugs", there: true, refused: true, want: []string{"bugs/command*"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &Repo{top: t.TempDir()}
			writeAll(t, r.top, map[string]string{configFile: `{"gate": ["true"], "acceptance": "acceptance/{spec}/"}`})
			if tc.there {
				writeMonitor(t, r.top, "bugs", map[string]string{"command": "mine"})
			}

			err := r.MonitorAdd(tc.source, io.Discard)
			says := strings.Contains(fmt.Sprint(err), tc.says)
			if !claim.Holds((err != nil) == tc.refused, errors.Is(err, ErrUsage) == tc.usage, says) {
				t.Fatalf("kiln monitor add %s answers %v; want a refusal %v, as a usage error %v, that says %q",
					tc.source, err, tc.refused, tc.usage, tc.says)
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

		if _, refused := c.readMonitor(Spec(path.Base(name)), name, files, settings); len(refused) > 0 {
			t.Errorf("kiln ships %s, which it could not run: %v", name, refused)
		}

		for _, f := range files {
			if raw, _ := fs.ReadFile(from, f); kilnOwn.Match(raw) {
				t.Errorf("%s/%s names %q, of kiln's own repository", name, f, kilnOwn.Find(raw))
			}
		}
	}
}

// TestShippedDocsCheck holds the summary of the docs monitor kiln ships, which kiln status and each notice show.
func TestShippedDocsCheck(t *testing.T) {
	for changed, want := range map[string]string{
		"a":   `"summary": "1 package changed since `,
		"a b": `"summary": "2 packages changed since `,
	} {
		t.Run(changed, func(t *testing.T) {
			r, repo := scratchRepo(t)
			if err := r.Init(t.Context(), io.Discard, InitOptions{Preset: "go"}); err != nil {
				t.Fatal(err)
			}

			if err := r.MonitorAdd("go/docs", io.Discard); err != nil {
				t.Fatal(err)
			}

			shell(t, repo, "git add -A && git commit --quiet --no-verify -m start")
			for pkg := range strings.FieldsSeq(changed) {
				writeAll(t, repo, map[string]string{pkg + "/" + pkg + ".go": "package " + pkg + "\n"})
			}

			out := shell(t, repo, "git add -A && git commit --quiet --no-verify -m change && .kiln/monitors/docs/check")
			if last := out[strings.LastIndex(out, "\n")+1:]; !strings.Contains(last, want) {
				t.Errorf("the check's verdict is %s; want one that holds %s", last, want)
			}
		})
	}
}

// TestShippedFit holds which monitors kiln ships fit a project, by the preset .kiln/init.json records. The shell
// completes only those. kiln monitor add with no source lists each, with the preset it fits.
func TestShippedFit(t *testing.T) {
	all := []string{"bugs", "go/deps", "go/docs", "go/gate-time"}
	for preset, tc := range map[string]struct {
		fits []string
		says string
	}{
		"go":     {fits: all, says: "go/docs        for the preset go, which .kiln/init.json records\n"},
		"python": {fits: []string{"bugs"}, says: "go/docs        for the preset go; .kiln/init.json records python\n"},
		"":       {fits: all, says: "go/docs        for the preset go\n"},
	} {
		t.Run(cmpOr(preset, "no record"), func(t *testing.T) {
			r, _ := scratchRepo(t)
			if preset != "" {
				if err := r.Init(t.Context(), io.Discard, InitOptions{Preset: preset}); err != nil {
					t.Fatal(err)
				}
			}

			fits, err := r.Takes(t.Context(), "monitor add")
			var out strings.Builder
			if err == nil {
				err = r.ShippedMonitors(&out)
			}

			listed := out.String()
			anyProject := strings.HasPrefix(listed, "bugs           for any project\n")
			if !claim.Holds(err == nil, slices.Equal(fits, tc.fits), anyProject, strings.Contains(listed, tc.says)) {
				t.Errorf("the shell completes %v, %v, and kiln monitor add lists:\n%s\nwant %v, and the line %q", fits,
					err, listed, tc.fits, tc.says)
			}
		})
	}
}

// TestCompleteSkipsAdded holds that the shell completes no monitor kiln ships whose directory the checkout holds.
// kiln monitor add would refuse it.
func TestCompleteSkipsAdded(t *testing.T) {
	r, _ := scratchRepo(t)
	err := r.Init(t.Context(), io.Discard, InitOptions{Preset: "go"})
	if err == nil {
		err = r.MonitorAdd("go/docs", io.Discard)
	}

	if err != nil {
		t.Fatal(err)
	}

	fits, err := r.Takes(t.Context(), "monitor add")
	if want := []string{"bugs", "go/deps", "go/gate-time"}; err != nil || !slices.Equal(fits, want) {
		t.Errorf("the shell completes %v, %v; want %v", fits, err, want)
	}
}

// TestMonitorUpdates holds how kiln init brings a monitor up to date with the directory kiln monitor add wrote it
// from. ship changes the directory, as a team's repository would, and edit changes the project's copy. Until kiln
// init runs, kiln status says the monitor is behind.
func TestMonitorUpdates(t *testing.T) {
	const check = ".kiln/monitors/deps/check"
	allFine := "sed -i '' 's/\"fine\"/\"all fine\"/' check"
	tests := []struct {
		name, ship, edit string
		said             string   // a line kiln init prints
		behind           bool     // whether kiln status says the monitor is behind
		dropped          bool     // whether the record drops the monitor
		holds            []string // what check holds after
		lacks            []string
	}{
		{name: "an unchanged file takes the new source", ship: allFine, said: "updated " + check, behind: true,
			holds: []string{"all fine"}},
		{name: "an edited file keeps the edit, and takes the new source", ship: allFine,
			edit: "sed -i '' 's/^# The check of the deps monitor./# Our check./' " + check, said: "merged " + check + "\n",
			behind: true, holds: []string{"# Our check.", "all fine"}, lacks: []string{"<<<<<<<"}},
		{name: "an edit the source's change touches is a conflict", ship: allFine,
			edit: "sed -i '' 's/\"fine\"/\"ok\"/' " + check, behind: true,
			said:  "merged " + check + ", with conflicts: resolve them before you commit it",
			holds: []string{"<<<<<<< " + check + "\n", "\"ok\"", "all fine"}},
		{name: "a file the source gains is written, as kiln monitor add would",
			ship: "mkdir lib && printf '#!/bin/sh\\n' >lib/old && chmod +x lib/old", behind: true,
			said: "wrote .kiln/monitors/deps/lib/old", holds: []string{"\"fine\"", "lib/old\n"}},
		{name: "a deleted monitor stays deleted", ship: allFine, edit: "git rm -rq .kiln/monitors/deps", dropped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, repo, team := teamMonitor(t)
			shell(t, team, tt.ship)
			shell(t, repo, cmpOr(tt.edit, "true")+" && git add -A && git commit --quiet --no-verify --allow-empty -m x")
			status, said := statusThenInit(t, r)
			rec, err := r.lastInit()
			_, recorded := rec.Monitors["deps"]
			behind := strings.Contains(status, "is behind the monitor deps, from ")
			if !claim.Holds(err == nil, behind == tt.behind, strings.Contains(said, tt.said), recorded != tt.dropped) {
				t.Errorf("kiln status said:\n%s\nkiln init said:\n%s\nthe record names deps %v, %v; want the monitor "+
					"behind %v, a line %q, and the record dropping it %v", status, said, recorded, err, tt.behind,
					tt.said, tt.dropped)
			}

			if tt.holds != nil {
				wantHolds(t, shell(t, repo, "cat "+check+" && find .kiln/monitors/deps -perm -u+x -type f"),
					append(tt.holds, check), tt.lacks)
			}
		})
	}
}

// teamMonitor is a project from the go preset, and team, a directory that holds the monitor deps. kiln monitor add
// added deps from team, and main holds all of it.
func teamMonitor(t *testing.T) (r *Repo, repo, team string) {
	t.Helper()
	r, repo = scratchRepo(t)
	team = filepath.Join(t.TempDir(), "deps")
	writeAll(t, team, map[string]string{"command": "#!/bin/sh\ngo get -u ./...\n", "check": "#!/bin/sh\n" +
		"# The check of the deps monitor.\n#\n# It asks for no fix.\nset -eu\n" +
		"echo '{\"fix\": false, \"summary\": \"fine\"}'\n"})
	if err := r.Init(t.Context(), io.Discard, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	if err := r.MonitorAdd(team, io.Discard); err != nil {
		t.Fatal(err)
	}

	shell(t, repo, "git add -A && git commit --quiet --no-verify -m kiln")
	return r, repo, team
}

// statusThenInit answers what kiln status says, then what kiln init says, with no preset named.
func statusThenInit(t *testing.T, r *Repo) (status, said string) {
	t.Helper()
	var s, out strings.Builder
	if err := r.Status(t.Context(), &s); err != nil {
		t.Fatal(err)
	}

	if err := r.Init(t.Context(), &out, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	return s.String(), out.String()
}

// TestShippedMonitorUpdates holds that kiln init brings a monitor kiln ships up to date with this kiln's. An older
// kiln's check stands in the record and in the checkout.
func TestShippedMonitorUpdates(t *testing.T) {
	r, repo := scratchRepo(t)
	if err := r.Init(t.Context(), io.Discard, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	if err := r.MonitorAdd("bugs", io.Discard); err != nil {
		t.Fatal(err)
	}

	rec, err := r.lastInit()
	if err != nil {
		t.Fatal(err)
	}

	added := rec.Monitors["bugs"]
	older := append([]string{"# An older kiln's check."}, added.Files["check"]...)
	added.Files["check"] = older
	writeAll(t, repo, map[string]string{".kiln/monitors/bugs/check": strings.Join(older, "\n")})
	if _, err := r.writeRecord(rec); err != nil {
		t.Fatal(err)
	}

	shell(t, repo, "chmod +x .kiln/monitors/bugs/check && git add -A && git commit --quiet --no-verify -m x")
	status, said := statusThenInit(t, r)
	shipped, _ := fs.ReadFile(shipped, "monitors/bugs/check")
	have := shell(t, repo, "test -x .kiln/monitors/bugs/check && cat .kiln/monitors/bugs/check")
	if !claim.Holds(strings.Contains(status, "is behind the monitor bugs, from bugs:"),
		strings.Contains(said, "updated .kiln/monitors/bugs/check\n"), have == strings.TrimSpace(string(shipped))) {
		t.Errorf("kiln status said:\n%s\nkiln init said:\n%s\nwant bugs behind, then updated to what kiln ships",
			status, said)
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
