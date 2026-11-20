package kiln

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctor holds what kiln doctor finds in a project's files, once a change breaks a rule.
// Each row names the lines Doctor prints, in order, by a part of each. A line that starts "note:" is no problem.
func TestDoctor(t *testing.T) {
	project := doctorProject(t)
	const monitor = `{"name": "deps", "every": "24h", "check": ".kiln/monitors/deps", "agent": "deps.md"}`
	const launchd = "names monitors, but no launchd job runs them: run kiln monitor install"
	for name, tc := range map[string]doctorCase{
		"kiln init's files": {want: []string{"found no problem"}},
		"each refusal of kiln.json at once": {
			change: withKeys(`"workflow": "both", "gauges": [{"name": "g", "command": "true"}],`) +
				` && sed -i '' 's|"proxy.golang.org"|"https://x.org/"|' .kiln/kiln.json`,
			want: []string{`names the workflow "both": name "split"`, `names the host "https://x.org/": name a bare host`,
				`names a gauge "g" that names both min and max, or neither`,
				"note: .kiln/kiln.json differs from main's in gauges, hosts, workflow: kiln acts on main's"},
		},
		"kiln.json that is no JSON": {change: "echo '{' >.kiln/kiln.json", want: []string{"fix its JSON"}},
		"kiln.json the working tree lacks": {change: "rm .kiln/kiln.json",
			want: []string{"the working tree holds no .kiln/kiln.json: run kiln init"}},
		"acceptance kiln queue refuses": {change: acceptance(`"{spec}/"`),
			want: []string{"starts with {spec}", "note: .kiln/kiln.json differs from main's in acceptance"}},
		"an agent the working tree lacks": {change: "rm .kiln/agents/audit.md",
			want: []string{"the working tree holds no .kiln/agents/audit.md: run kiln init"}},
		"each refusal of an agent's file": {
			change: `printf -- '---\ntools: [Read]\neffort: huge\n---\nBuild.\n' >.kiln/agents/build.md`,
			want: []string{"build.md, as the working tree holds it, its tools are not tool names",
				`build.md, as the working tree holds it, its effort, "huge", is none of`},
		},
		"a monitor's script and agent the working tree lacks": {change: withKeys(`"monitors": [` + monitor + `],`),
			want: []string{"the working tree holds no .kiln/agents/deps.md: write it, or name another agent",
				"runs .kiln/monitors/deps, which the working tree lacks", launchd, "note:"}},
		"a monitor's script that is not executable": {
			change: withKeys(`"monitors": [`+monitor+`],`) + " && mkdir .kiln/monitors && touch .kiln/monitors/deps " +
				"&& echo Fix. >.kiln/agents/deps.md",
			want: []string{"runs .kiln/monitors/deps, which is not executable: run chmod +x .kiln/monitors/deps",
				launchd, "note:"},
		},
		"a spec that leaves the form": {spec: "form",
			want: []string{"form.md leaves /shape's template: its acceptance test \"2. <Title>\" is not ### 1.",
				"form.md leaves /shape's template: its acceptance test 1 holds no Then",
				"form.md leaves /shape's template: it lacks the section ## Goals"},
		},
		"a spec the working tree lacks": {spec: "x",
			want: []string{"the working tree holds no .kiln/specs/x.md: write it with /shape"}},
	} {
		t.Run(name, tc.check(project))
	}
}

// TestDoctorMainAndTools holds what kiln doctor finds missing outside .kiln/: main, its kiln.json, claude and the
// pre-commit hook. It notes where main's kiln.json differs.
func TestDoctorMainAndTools(t *testing.T) {
	project := doctorProject(t)
	for name, tc := range map[string]doctorCase{
		"kiln.json main lacks": {change: "git rm --quiet --cached .kiln/kiln.json && git commit --quiet --no-verify -m x",
			want: []string{"main holds no .kiln/kiln.json, which kiln reads there: commit it to main"}},
		"a key kiln.json changed, and no problem": {change: `sed -i '' 's/"rounds": 3/"rounds": 2/' .kiln/kiln.json`,
			want: []string{"note: .kiln/kiln.json differs from main's in rounds", "found no problem"}},
		"no branch main": {change: "git branch -m master", want: []string{"kiln builds on the branch main"}},
		"no claude":      {noClaude: true, want: []string{"claude is not on the PATH"}},
		"no pre-commit hook": {change: "rm .git/hooks/pre-commit",
			want: []string{"pre-commit is missing, so no commit to main runs the gate: run kiln init"}},
		"a hook of the developer's own that runs no kiln": {
			change: "printf '#!/bin/sh\nexit 0\n' >.git/hooks/pre-commit",
			want:   []string{"pre-commit does not run the gate: add this line to it: kiln precommit"},
		},
		"a hook in core.hooksPath that runs kiln precommit, but may not run": {
			change: "mkdir .githooks && echo 'kiln precommit' >.githooks/pre-commit && git config core.hooksPath .githooks",
			want:   []string{".githooks/pre-commit is not executable, so git skips it"},
		},
	} {
		t.Run(name, tc.check(project))
	}
}

// doctorCase is a change to a project kiln init set up on main, and the lines kiln doctor then prints.
type doctorCase struct {
	change, spec string
	noClaude     bool
	want         []string
}

// doctorProject is a project kiln init set up on main, which each row of a test reads. Its working tree holds a
// spec, form, that leaves /shape's template, which a row names to read it.
func doctorProject(t *testing.T) string {
	r, repo := scratchRepo(t)
	if err := r.Init(t.Context(), &bytes.Buffer{}, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	shell(t, repo, "git add -A && git commit --quiet --no-verify -m start && mkdir .kiln/specs && "+
		"sed '/^## Goals/d; s/^### 1\\./### 2./; /Then/d' .kiln/skills/shape/template.md >.kiln/specs/form.md")
	return repo
}

// check runs kiln doctor on project, and on tc's spec when it names one. A row that changes the project changes a
// copy of it, so the rows that change nothing share it.
func (tc doctorCase) check(project string) func(t *testing.T) {
	return func(t *testing.T) {
		repo := project
		if tc.change != "" {
			repo = t.TempDir()
			shell(t, project, "cp -Rp . "+repo)
			shell(t, repo, tc.change)
		}

		tc.doctor(t, repo)
	}
}

// doctor runs kiln doctor in repo, and holds that it prints tc's lines.
func (tc doctorCase) doctor(t *testing.T, repo string) {
	r, err := Open(t.Context(), repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	if !tc.noClaude {
		shell(t, bin, "printf '#!/bin/sh\\n' >claude && chmod +x claude")
	}

	r.env = append(r.env, "PATH="+bin)
	var out bytes.Buffer
	err = r.Doctor(t.Context(), Spec(tc.spec), &out)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != len(tc.want) {
		t.Fatalf("doctor printed %d lines, want %d:\n%s", len(lines), len(tc.want), out.String())
	}

	problems := false
	for i, want := range tc.want {
		problems = problems || !strings.HasPrefix(want, "note:") && want != "found no problem"
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d is %q, want one that says %q", i+1, lines[i], want)
		}
	}

	if (err != nil) != problems {
		t.Errorf("doctor answered %v, want an error %v", err, problems)
	}
}

// withKeys is a shell command that adds keys, JSON members each followed by a comma, to the top of kiln.json.
func withKeys(keys string) string {
	return `sed -i '' '1s|{|{ ` + keys + `|' .kiln/kiln.json`
}

// TestDoctorWritesNothing holds that kiln doctor leaves the working tree, and git's directory, as they were.
func TestDoctorWritesNothing(t *testing.T) {
	r, repo := scratchRepo(t)
	if err := r.Init(t.Context(), &bytes.Buffer{}, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	before := shell(t, repo, "git add -A && git commit --quiet --no-verify -m start && echo x >>.kiln/kiln.json && "+
		"find . -newer .kiln/kiln.json | sort")
	if err := r.Doctor(t.Context(), "x", &bytes.Buffer{}); err == nil {
		t.Fatal("doctor found no problem in a kiln.json that is no JSON")
	}

	if after := shell(t, repo, "find . -newer .kiln/kiln.json | sort"); after != before {
		t.Errorf("doctor changed files: before\n%s\nafter\n%s", before, after)
	}

	if _, err := os.Stat(filepath.Join(repo, ".git", "kiln")); !os.IsNotExist(err) {
		t.Errorf("doctor made kiln's own directory: %v", err)
	}
}
