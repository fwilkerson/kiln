package kiln

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/practices/claim"
)

// TestInitUpdates holds how a second kiln init brings .kiln/ up to date with a newer kiln's defaults. ship changes
// the preset, as a newer kiln would, and edit changes .kiln/, as the developer would. The second run names no preset,
// so it takes the one .kiln/init.json records.
func TestInitUpdates(t *testing.T) {
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
			edit: "echo 'House rule.' >>" + build + " && rm .kiln/init.json",
			said: "merged " + build + ", with conflicts", holds: []string{"<<<<<<< " + build + "\nHouse rule.\n"}},
		{name: "a deleted default is written again, and kiln.json is the project's",
			ship: "echo '{\"gate\": [\"make\"], \"rounds\": 2, \"acceptance\": \"a/{spec}/\"}' >kiln.json",
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
			t.Errorf("build.md lacks %q:\n%s", want, got)
		}
	}

	for _, unwanted := range lacks {
		if strings.Contains(got, unwanted) {
			t.Errorf("build.md holds %q:\n%s", unwanted, got)
		}
	}
}

// TestInitAgain holds what kiln init writes: the defaults, the reviewers, the schema, and its record of each. A
// second run writes nothing. Without a record, an edited reviewer merges, and a deleted one stays deleted. An edited
// schema is written over, since kiln owns it.
func TestInitAgain(t *testing.T) {
	r, repo := scratchRepo(t)
	var first, again, third strings.Builder
	if err := r.Init(t.Context(), &first, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	rec, err := r.lastInit()
	for _, name := range []string{"agents/build.md", "reviewers/critic.md", "kiln.schema.json"} {
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

	shell(t, repo, "rm .kiln/init.json .kiln/reviewers/zoom-out.md && echo 'House rule.' >>.kiln/reviewers/critic.md "+
		"&& echo '{}' >.kiln/kiln.schema.json")
	if err := r.Init(t.Context(), &third, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	wantHolds(t, third.String(), []string{"merged .kiln/reviewers/critic.md, with conflicts",
		"updated .kiln/kiln.schema.json\n", "wrote .kiln/init.json\n"}, []string{"wrote .kiln/reviewers/zoom-out.md"})
}

// TestOwnInitRecord holds that kiln's own .kiln/init.json records the defaults kiln init writes now. So kiln status
// never says that kiln's own .kiln/ is behind.
func TestOwnInitRecord(t *testing.T) {
	top, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	own, err := os.ReadFile(filepath.Join(top, initFile))
	if err != nil {
		t.Fatal(err)
	}

	r := &Repo{top: top}
	if _, behind, err := r.behind(own); err != nil || behind {
		t.Errorf("kiln's own %s is behind the defaults, %v: run kiln init in kiln's checkout, and commit what it "+
			"merges", initFile, err)
	}
}

// TestStatusSaysInitBehind holds that kiln status says when .kiln/ at main's tip is behind kiln init's defaults. It
// says when main lacks kiln init's record, and says nothing while .kiln/ is up to date. A record may name a directory.
func TestStatusSaysInitBehind(t *testing.T) {
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
		{"git rm --quiet .kiln/init.json", ".kiln/ holds no record of what kiln init wrote: run kiln init, which " +
			"leaves each difference from its defaults as a conflict\n"},
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
// is the developer's argument alone, never what .kiln/init.json records.
func TestInitRefusesPreset(t *testing.T) {
	for _, tt := range []struct {
		name, config, record, named string
		usage                       bool
	}{
		{name: "a kiln.json kiln can't read", config: "{"},
		{name: "a kiln.json with no rounds", config: `{"gate": ["make"], "acceptance": "a/{spec}/"}`},
		{name: "a name kiln doesn't know", named: "rust", usage: true},
		{name: "a recorded name kiln doesn't know", record: `{"preset": "rust"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, repo := scratchRepo(t)
			team := teamPreset(t, t.TempDir())
			if tt.config != "" {
				shell(t, team, "echo '"+tt.config+"' >kiln.json")
			}

			if tt.record != "" {
				shell(t, repo, "mkdir .kiln && echo '"+tt.record+"' >"+initFile)
			}

			named := tt.named
			if named == "" && tt.record == "" {
				named = team
			}

			err := r.Init(t.Context(), new(strings.Builder), InitOptions{Preset: named})
			_, stat := os.Stat(filepath.Join(repo, agentFile(buildRole)))
			if !claim.Holds(err != nil, stat != nil, errors.Is(err, ErrUsage) == tt.usage) {
				t.Errorf("kiln init: %v, and wrote build.md: %v; want refused, before it writes, usage %v", err,
					stat == nil, tt.usage)
			}
		})
	}
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
