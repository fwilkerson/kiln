package kiln

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctor holds what kiln doctor finds in a project's files, once a change breaks a rule.
// Each row names the lines Doctor prints, in order, by a part of each. A line that starts "note:" is no problem.
func TestDoctor(t *testing.T) {
	project := doctorProject(t)
	const monitor = "mkdir -p .kiln/monitors/deps && cd .kiln/monitors/deps && "
	const launchd = "note: .kiln/monitors/ holds monitors, but no launchd job runs them in any checkout of this " +
		"repository: run kiln monitor install, or run kiln monitor run by hand"
	for name, tc := range map[string]doctorCase{
		"kiln init's files": {want: []string{"found no problem"}},
		"each refusal of kiln.json at once": {
			change: `sed -i '' 's|"rounds": 3|"rounds": -1|; s|"proxy.golang.org"|"https://x.org/"|' .kiln/kiln.json`,
			want: []string{`names -1 rounds: name 0 or more`, `names the host "https://x.org/": name a bare host`,
				"note: .kiln/kiln.json differs from main's in hosts, rounds: kiln acts on main's"},
		},
		"a key of kiln.json kiln renamed": {change: withKeys(`"workflow": "split",`),
			want: []string{`holds "workflow", a key kiln no longer reads: say "testSession": true in its place`}},
		"a key of monitor.json kiln renamed": {change: monitor + "echo Fix it. >agent.md && echo '{\"accept\": true}' >monitor.json",
			want: []string{`.kiln/monitors/deps/monitor.json holds "accept", a key kiln no longer reads: name it "mustAccept"`,
				launchd}},
		"kiln.json that is no JSON": {change: "echo '{' >.kiln/kiln.json", want: []string{"fix its JSON"}},
		"kiln.json the working tree lacks": {change: "rm .kiln/kiln.json",
			want: []string{"the working tree holds no .kiln/kiln.json: run kiln init"}},
		"acceptance kiln queue refuses": {change: acceptance(`"{spec}/"`),
			want: []string{"starts with {spec}, so a spec's name would decide where it is: put a directory of its own",
				"note: .kiln/kiln.json differs from main's in acceptance"}},
		"a key spelled in other letters": {change: withKeys(`"Workflow": "split",`),
			want: []string{`"Workflow"`}},
		"scripts the shell expands or ends early": {
			change: `sed -i '' 's|"go vet ./..."|"~/bin/vet ./...", "./x.sh;go vet ./..."|' .kiln/kiln.json && ` +
				`printf '#!/bin/sh\n' >x.sh && chmod +x x.sh`,
			want: []string{"note: .kiln/kiln.json differs from main's in gate", "found no problem"}},
		"an agent the working tree lacks": {change: "rm .kiln/agents/audit.md",
			want: []string{"the working tree holds no .kiln/agents/audit.md: run kiln init"}},
		"each refusal of an agent's file": {
			change: `printf -- '---\ntools: [Read]\neffort: huge\n---\nBuild.\n' >.kiln/agents/build.md`,
			want: []string{"build.md, as the working tree holds it, its tools are not tool names",
				`build.md, as the working tree holds it, its effort, "huge", is none of`},
		},
		"each refusal of a monitor, and of its agent": {
			change: monitor + "touch README && echo '{\"atLeast\": 1}' >monitor.json && " +
				"printf -- '---\neffort: huge\n---\nFix it.\n' >agent.md",
			want: []string{".kiln/monitors/deps/ holds README, a file kiln does not read",
				".kiln/monitors/deps/ names atLeast or atMost, a gauge's line, but holds no gauge",
				`.kiln/monitors/deps/agent.md, as the working tree holds it, its effort, "huge", is none of`, launchd}},
		"a file directly under .kiln/monitors/": {change: "mkdir -p .kiln/monitors && touch .kiln/monitors/deps",
			want: []string{".kiln/monitors/deps is a file, where a monitor is a directory: move it into"}},
		"a monitor whose launchd job runs in another checkout": {
			change: monitor + "printf 'true\\n' >command && chmod +x command && cd - && " +
				`mkdir -p sub "$HOME/Library/LaunchAgents" && printf ` +
				`'<key>WorkingDirectory</key>\n  <string>%s/sub</string>\n' "$PWD" ` +
				`>"$HOME/Library/LaunchAgents/dev.kiln.monitor.other.plist"`,
			want: []string{"found no problem"}},
		"a monitor's script that is not executable, beside a file git ignores": {
			change: monitor + "touch command .DS_Store && echo .DS_Store >../../../.gitignore",
			want: []string{"the working tree holds .kiln/monitors/deps/command without the executable bit, so kiln " +
				"cannot run it: run chmod +x .kiln/monitors/deps/command, and commit it", launchd},
		},
		"a notify program the working tree lacks": {change: withKeys(`"notify": ".kiln/notify-me",`),
			want: []string{".kiln/kiln.json runs .kiln/notify-me, which the working tree lacks: write it",
				"note: .kiln/kiln.json differs from main's in notify"}},
		"each refusal of a reviewer, and a conflict kiln init left": {
			change: "printf -- '---\\nname: other\\n---\\n' >.kiln/reviewers/critic.md && " +
				"printf '<<<<<<< .kiln/skills/shape/SKILL.md\\nmine\\n' >>.kiln/skills/shape/SKILL.md",
			want: []string{`.kiln/reviewers/critic.md, its frontmatter's name is "other": make it "critic"`,
				".kiln/reviewers/critic.md, its frontmatter has no description",
				`.kiln/skills/shape/SKILL.md holds a conflict's marker, "<<<<<<< .kiln/skills/shape/SKILL.md": resolve`}},
		"a reviewer whose link reaches another file": {change: "ln -sf ../../.kiln/agents/build.md .claude/agents/critic.md",
			want: []string{".claude/agents/critic.md links to ../../.kiln/agents/build.md, not .kiln/reviewers/critic.md, " +
				"so /shape runs another file: remove the link, then run kiln init, which links the reviewer"}},
		"a record of kiln init that is no JSON": {change: "echo '{' >.kiln/init.json",
			want: []string{".kiln/init.json: "}},
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

// TestDoctorJobWithoutDir holds that a launchd job whose file names no working directory runs in no checkout. kiln
// doctor, run in the repository, still notes that no job runs its monitors.
func TestDoctorJobWithoutDir(t *testing.T) {
	project := doctorProject(t)
	job := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", launchdPrefix+"nodir.plist")
	t.Cleanup(func() { _ = os.Remove(job) })
	shell(t, project, "mkdir -p .kiln/monitors/deps "+filepath.Dir(job)+" && printf 'true\\n' "+
		">.kiln/monitors/deps/command && chmod +x .kiln/monitors/deps/command && printf '<key>Label</key>\\n' >"+job)
	// A job that names no directory would run git where kiln runs, which is this repository.
	t.Chdir(project)
	doctorCase{want: []string{"note: .kiln/monitors/ holds monitors, but no launchd job runs them",
		"found no problem"}}.doctor(t, project)
}

// TestDoctorAddedConflict holds that kiln doctor reads each file kiln monitor add recorded, for a conflict kiln init
// left there.
func TestDoctorAddedConflict(t *testing.T) {
	t.Parallel()
	project := doctorProject(t)
	team := filepath.Join(t.TempDir(), "deps")
	writeAll(t, team, map[string]string{commandScript: "lib/run\n", "lib/run": "true\n"})
	r, err := Open(t.Context(), project, os.Environ())
	if err == nil {
		err = r.MonitorAdd(team, io.Discard)
	}

	if err != nil {
		t.Fatal(err)
	}

	shell(t, project, "chmod +x .kiln/monitors/deps/command && printf '<<<<<<< .kiln/monitors/deps/lib/run\\n' "+
		">>.kiln/monitors/deps/lib/run")
	doctorCase{want: []string{`.kiln/monitors/deps/lib/run holds a conflict's marker, ` +
		`"<<<<<<< .kiln/monitors/deps/lib/run": resolve the conflict, then commit the file`,
		"note: .kiln/monitors/ holds monitors, but no launchd job runs them"}}.doctor(t, project)
}

// TestDoctorMainAndTools holds what kiln doctor finds missing outside .kiln/: main, its kiln.json, claude and the
// pre-commit hook. It notes where main's kiln.json differs.
func TestDoctorMainAndTools(t *testing.T) {
	t.Parallel()
	project := doctorProject(t)
	for name, tc := range map[string]doctorCase{
		"an object's keys reordered, and nothing else": {
			change: withKeys(`"mcp": {"docs": {"command": "a", "args": []}},`) +
				` && git commit --quiet --no-verify -am mcp && ` +
				`sed -i '' 's|{"command": "a", "args": \\[\\]}|{"args": [], "command": "a"}|' .kiln/kiln.json`,
			want: []string{"found no problem"}},
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
	if err := r.Init(t.Context(), &bytes.Buffer{}, InitOptions{Preset: "go"}); err != nil {
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
		t.Parallel()
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
	t.Parallel()
	r, repo := scratchRepo(t)
	if err := r.Init(t.Context(), &bytes.Buffer{}, InitOptions{Preset: "go"}); err != nil {
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

// TestTakesDoctor holds that kiln complete doctor offers each spec the working tree holds, by a name a spec may have.
func TestTakesDoctor(t *testing.T) {
	t.Parallel()
	repo := doctorProject(t)
	shell(t, repo, "touch .kiln/specs/Notes.md .kiln/specs/form.amend.md")
	r, err := Open(t.Context(), repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	if names, err := r.Takes(t.Context(), "doctor"); err != nil || strings.Join(names, " ") != "form" {
		t.Errorf("kiln complete doctor offers %q, %v; want only form", names, err)
	}
}
