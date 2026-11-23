package kiln

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/gittest"
	"github.com/fwilkerson/practices/claim"
)

// TestInitUpdates holds how a second kiln init brings .kiln/ up to date with a newer kiln's defaults. ship changes
// the preset, as a newer kiln would, and edit changes .kiln/, as the developer would. The second run names no preset,
// so it takes the one .kiln/generated/init.json records.
func TestInitUpdates(t *testing.T) {
	t.Parallel()
	const build = ".kiln/agents/build.md"
	newCheck := "printf -- '- Run make check.\\n' >build-checks.md"
	tests := []struct {
		name, ship, edit string
		said             string   // a line kiln init prints
		holds            []string // what build.md holds after
		lacks            []string
	}{
		{name: "an unchanged file takes the new default", ship: newCheck,
			said: "updated " + build, holds: []string{"- Run make check.\n"}, lacks: []string{"go vet"}},
		{name: "an edited file keeps the edit, and takes the new default", ship: newCheck,
			edit: "sed -i '' 's/^You build one spec: /You build one spec, by house rules: /' " + build,
			said: "merged " + build + "\n", holds: []string{"by house rules", "- Run make check.\n"},
			lacks: []string{"go vet", "<<<<<<<"}},
		{name: "an edit kiln's change touches is a conflict", ship: newCheck,
			edit:  "sed -i '' 's/with .gofmt./with gofumpt/' " + build,
			said:  "merged " + build + ", with conflicts: resolve them before you commit it",
			holds: []string{"<<<<<<< " + build + "\n", "gofumpt", "- Run make check.\n", ">>>>>>> kiln init's new default"}},
		{name: "with no record, each difference is a conflict",
			edit: "echo 'House rule.' >>" + build + " && rm .kiln/generated/init.json && touch go.mod",
			said: "merged " + build + ", with conflicts", holds: []string{"<<<<<<< " + build + "\nHouse rule.\n"}},
		{name: "a deleted default is written again, and kiln.json is the project's",
			ship: "echo '{\"gate\": [\"make\"], \"rounds\": 2, \"acceptanceTests\": \"a/{spec}/\"}' >kiln.json",
			edit: "rm " + build, said: "wrote " + build, holds: []string{"go vet"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, repo := scratchRepo(t)
			team := teamPreset(t, t.TempDir())
			if err := r.Init(t.Context(), new(strings.Builder), InitOptions{Preset: team}); err != nil {
				t.Fatal(err)
			}

			config := shell(t, repo, "cat .kiln/kiln.json")
			shell(t, team, cmpOr(tt.ship, "true"))
			shell(t, repo, cmpOr(tt.edit, "true"))
			var out strings.Builder
			if err := r.Init(t.Context(), &out, InitOptions{}); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(out.String(), tt.said) || shell(t, repo, "cat .kiln/kiln.json") != config {
				t.Errorf("kiln init said:\n%s\nwant a line %q, and kiln.json kept", out.String(), tt.said)
			}

			wantHolds(t, shell(t, repo, "cat "+build)+"\n", tt.holds, tt.lacks)
		})
	}
}

// wantHolds holds that got holds each of holds, and none of lacks.
func wantHolds(t *testing.T, got string, holds, lacks []string) {
	t.Helper()
	for _, want := range holds {
		if !strings.Contains(got, want) {
			t.Errorf("the file lacks %q:\n%s", want, got)
		}
	}

	for _, unwanted := range lacks {
		if strings.Contains(got, unwanted) {
			t.Errorf("the file holds %q:\n%s", unwanted, got)
		}
	}
}

// TestInitAgain holds what kiln init writes: the defaults, the reviewers, the schema, and its record of each. A
// second run writes nothing. Without a record, an edited reviewer merges, and a deleted one stays deleted. An edited
// schema is written over, since kiln owns it.
func TestInitAgain(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	var first, again, third strings.Builder
	if err := r.Init(t.Context(), &first, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	rec, err := r.lastInit()
	for _, name := range []string{"agents/build.md", "reviewers/critic.md", "generated/kiln.schema.json"} {
		if _, ok := rec.Files[name]; err != nil || !ok {
			t.Errorf("kiln init's record lacks %s, %v; it records %v", name, err, slices.Sorted(maps.Keys(rec.Files)))
		}
	}

	if err := r.Init(t.Context(), &again, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(again.String(), "wrote") || strings.Contains(again.String(), "linked") {
		t.Errorf("a second kiln init said:\n%s\nwant it to write and link nothing", again.String())
	}

	shell(t, repo, "rm .kiln/generated/init.json .kiln/reviewers/zoom-out.md && echo 'House rule.' "+
		">>.kiln/reviewers/critic.md && echo '{}' >.kiln/generated/kiln.schema.json")
	if err := r.Init(t.Context(), &third, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	wantHolds(t, third.String(), []string{"merged .kiln/reviewers/critic.md, with conflicts",
		"updated .kiln/generated/kiln.schema.json\n", "wrote .kiln/generated/init.json\n"},
		[]string{"wrote .kiln/reviewers/zoom-out.md"})
}

// TestOwnInitRecord holds that kiln's own .kiln/generated/init.json records the defaults kiln init writes now. So kiln
// status never says that kiln's own .kiln/ is behind.
func TestOwnInitRecord(t *testing.T) {
	top, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	own, err := os.ReadFile(filepath.Join(top, initFile))
	if err != nil {
		t.Fatal(err)
	}

	rec, err := parseInit(own)
	var stale []string
	if err == nil {
		stale, err = (&Repo{top: top}).behind(rec, nil)
	}

	if err != nil || len(stale) > 0 {
		t.Errorf("kiln's own %s is behind %v, %v: run kiln init in kiln's checkout, and commit what it merges",
			initFile, stale, err)
	}
}

// TestStatusSaysInitBehind holds that kiln status says when .kiln/ at main's tip is behind kiln init's defaults. It
// says when main lacks kiln init's record, and says nothing while .kiln/ is up to date. A record may name a directory.
func TestStatusSaysInitBehind(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	// The preset is named through a link, which the record must not keep.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	team := teamPreset(t, filepath.Join(link, "team"))
	if err := r.Init(t.Context(), new(strings.Builder), InitOptions{Preset: team}); err != nil {
		t.Fatal(err)
	}

	commit := " && git add -A && git commit --quiet --no-verify -m x"
	for _, step := range []struct{ change, want string }{
		{"true", ""},
		{"echo '- Run make check.' >team/build-checks.md",
			".kiln/ is behind the defaults of kiln init's preset ./team: run kiln init, which merges them with your " +
				"edits\n"},
		{"git rm --quiet .kiln/generated/init.json", ".kiln/ holds no record of what kiln init wrote: run kiln init, " +
			"which leaves each difference from its defaults as a conflict\n"},
	} {
		shell(t, repo, step.change+commit)
		var out strings.Builder
		if err := r.Status(t.Context(), &out); err != nil {
			t.Fatal(err)
		}

		if got, _ := strings.CutPrefix(out.String(), "kiln holds no approved spec\n"); got != step.want {
			t.Errorf("after %s, kiln status said:\n%s\nwant:\n%s", step.change, out.String(), step.want)
		}
	}
}

// TestInitRefusesPreset holds that kiln init refuses a preset it can't use before it writes anything. A usage error
// is the developer's argument alone, never what .kiln/generated/init.json records.
func TestInitRefusesPreset(t *testing.T) {
	t.Parallel()
	for _, tt := range []refusedInit{
		{name: "a kiln.json kiln can't read", config: "{"},
		{name: "a name kiln doesn't know", named: "rust", usage: true},
		// The test runs in internal/kiln, which holds presets/go, a directory laid out as a preset.
		{name: "a path that starts with neither . nor /", named: "presets/go", usage: true,
			said: "or a directory, by a path that starts with . or /, such as ./presets/go"},
		{name: "a recorded name kiln doesn't know", record: `{"preset": "rust"}`},
		{name: "the checkout's kiln.json, with a key kiln does not read", checkout: `{"gate": ["true"], "rouns": 3}`,
			said: `holds "rouns", a key kiln does not read: name it "rounds"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, repo, named := tt.setUp(t)
			err := r.Init(t.Context(), new(strings.Builder), InitOptions{Preset: named})
			_, stat := os.Stat(filepath.Join(repo, agentFile(buildRole)))
			said := strings.Contains(fmt.Sprint(err), tt.said)
			if !claim.Holds(err != nil, stat != nil, errors.Is(err, ErrUsage) == tt.usage, said) {
				t.Errorf("kiln init: %v, and wrote build.md: %v; want refused, before it writes, usage %v, saying %q",
					err, stat == nil, tt.usage, tt.said)
			}
		})
	}
}

// TestInitFillsSharedKeys holds that a preset's kiln.json may lack $schema and rounds. kiln init writes each into the
// project's kiln.json, as a built-in preset holds it, and keeps every other key.
func TestInitFillsSharedKeys(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	team := teamPreset(t, t.TempDir())
	shell(t, team, `echo '{"gate": ["make"], "acceptanceTests": "a/{spec}/"}' >kiln.json`)
	if err := r.Init(t.Context(), new(strings.Builder), InitOptions{Preset: team}); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	raw, err := os.ReadFile(filepath.Join(repo, configFile))
	if err == nil {
		err = json.Unmarshal(raw, &got)
	}

	want := map[string]any{"$schema": "./generated/kiln.schema.json", "rounds": 3.0, "gate": []any{"make"},
		"acceptanceTests": "a/{spec}/"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("kiln init wrote kiln.json:\n%s\n%v; want %v", raw, err, want)
	}
}

// TestOpenOutsideRepo holds that kiln refuses a directory in no git repository with its own words and fix, never
// git's error.
func TestOpenOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(t.Context(), dir, os.Environ())
	want := dir + " is in no git repository: run git init there, or run kiln in a repository"
	if !errors.Is(err, errRefused) || !strings.HasSuffix(fmt.Sprint(err), want) {
		t.Errorf("kiln opens %s: %v; want it refused, saying %q", dir, err, want)
	}
}

// refusedInit is a row of TestInitRefusesPreset. A row that names no preset, and records none, names a team's.
type refusedInit struct {
	name     string
	config   string // the team preset's kiln.json
	record   string // .kiln/generated/init.json
	checkout string // the checkout's own kiln.json
	named    string
	usage    bool
	said     string // a part of the refusal
}

// setUp is the repository the row starts from, at its path, and the preset kiln init is named.
func (tt refusedInit) setUp(t *testing.T) (r *Repo, repo, named string) {
	r, repo = historyRepo(t, gittest.New(t))
	team := teamPreset(t, t.TempDir())
	for dir, f := range map[string][2]string{team: {tt.config, "kiln.json"}, repo: {tt.record, initFile}} {
		if f[0] != "" {
			writeAll(t, dir, map[string]string{f[1]: f[0] + "\n"})
		}
	}

	if tt.checkout != "" {
		writeAll(t, repo, map[string]string{configFile: tt.checkout + "\n"})
	}

	if tt.named == "" && tt.record == "" {
		return r, repo, team
	}

	return r, repo, tt.named
}

// TestInitPicksPreset holds which preset kiln init writes when the developer names none. It picks the one built-in
// preset whose files the repository's top holds, every one, and says so. It refuses none, or two, before it writes.
// It never picks over a preset named or recorded.
func TestInitPicksPreset(t *testing.T) {
	t.Parallel()
	rows := append([]pickRow{
		{name: "nothing matches", said: "refused: no preset matches the repository, by the files at its top (go: go.mod; "},
		{name: "a preset matches only when every file is there", files: "uv.lock",
			said: "refused: no preset matches the repository"},
		{name: "two presets match", files: "go.mod pyproject.toml uv.lock",
			said: "refused: more than one preset matches the repository, by the files at its top (go: go.mod; " +
				"python: pyproject.toml, uv.lock): name one with --preset"},
		{name: "a named preset wins", files: "go.mod", named: "python", want: "python", said: "wrote "},
		{name: "a recorded preset wins", files: "go.mod", record: `{"preset": "python"}`, want: "python", said: "wrote "},
	}, pickedByOwnFiles(t)...)

	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			r, repo := scratchRepo(t)
			shell(t, repo, "touch "+cmpOr(tt.files, "/dev/null"))
			if tt.record != "" {
				shell(t, repo, "mkdir -p "+generatedDir+" && echo '"+tt.record+"' >"+initFile)
			}

			var out strings.Builder
			err := r.Init(t.Context(), &out, InitOptions{Preset: tt.named})
			got := out.String()
			if err != nil {
				got = err.Error()
			}

			config, _ := os.ReadFile(filepath.Join(repo, configFile))
			_, made := os.Stat(filepath.Join(repo, kilnDir))
			if !claim.Holds(strings.HasPrefix(got, tt.said), string(config) == presetConfig(t, tt.want),
				(err == nil) == (tt.want != ""), (made == nil) == (tt.want != "")) {
				t.Errorf("kiln init said:\n%s\nand wrote kiln.json:\n%s\nwant it to start %q, from preset %q, and "+
					".kiln/ only from a preset", got, config, tt.said, tt.want)
			}
		})
	}
}

// pickRow is a row of TestInitPicksPreset: the files at the repository's top, and what kiln init then does.
type pickRow struct {
	name, files, record, named string
	want                       string // the preset whose kiln.json kiln init writes
	said                       string // what kiln init prints first, or the refusal
}

// pickedByOwnFiles is a row for each built-in preset, in a repository that holds the files its detectFile lists.
func pickedByOwnFiles(t *testing.T) []pickRow {
	var rows []pickRow
	for _, name := range builtIns {
		p, err := presetNamed(name, "")
		if err != nil {
			t.Fatal(err)
		}

		paths, _, err := p.detects("")
		if err != nil || len(paths) == 0 {
			t.Fatalf("preset %s detects %q: %v; want a file that names its paths", name, paths, err)
		}

		rows = append(rows, pickRow{name: name + " by its files", files: strings.Join(paths, " "), want: name,
			said: "picked the preset " + name + ", since the repository holds " + strings.Join(paths, " and ") + "\n"})
	}

	return rows
}

// presetConfig is the kiln.json of the built-in preset name, or "" for none.
func presetConfig(t *testing.T, name string) string {
	t.Helper()
	if name == "" {
		return ""
	}

	p, err := presetNamed(name, "")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := fs.ReadFile(p.files, "kiln.json")
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// teamPreset copies Go's preset into dir, as a team keeps a preset of its own, and answers dir.
func teamPreset(t *testing.T, dir string) string {
	t.Helper()
	p, err := presetNamed("go", "")
	if err == nil {
		err = os.CopyFS(dir, p.files)
	}

	if err != nil {
		t.Fatal(err)
	}

	return dir
}
