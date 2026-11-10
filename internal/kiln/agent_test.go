package kiln

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

func TestAgentFile(t *testing.T) {
	for name, tc := range map[string]struct {
		file, want string
		refused    bool
	}{
		"a file with no frontmatter is all body, run as every session ran before": {
			file: "Build it.\n",
			want: `"Build it." [] opus medium fable`,
		},
		"the frontmatter names the tools, model, effort and advisor": {
			file: "---\ntools: Read, Grep\nmodel: sonnet\neffort: high\nadvisor: opus\n---\n\nAudit it.\n",
			want: `"Audit it." ["Read" "Grep"] sonnet high opus`,
		},
		"other keys and # lines are ignored, and a value may be quoted": {
			file: "---\n# model: haiku\nname: audit\ndescription: reads: packages\neffort: \"max\"\n---\nAudit.\n",
			want: `"Audit." [] opus max fable`,
		},
		"an empty frontmatter names nothing": {
			file: "---\n---\nBuild.",
			want: `"Build." [] opus medium fable`,
		},
		"a frontmatter with no closing line is refused":  {file: "---\nmodel: opus\nBuild it.\n", refused: true},
		"an effort Claude Code does not take is refused": {file: "---\neffort: huge\n---\nBuild.\n", refused: true},
		"tools as a YAML block list are refused":         {file: "---\ntools:\n  - Read\n---\nA.\n", refused: true},
		"tools as a YAML flow list are refused":          {file: "---\ntools: [Read, Grep]\n---\nA.\n", refused: true},
		"tools that name nothing are refused":            {file: "---\ntools: \"\"\n---\nA.\n", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			a, err := parseAgent(tc.file)
			if refused := err != nil; refused != tc.refused {
				t.Fatalf("refused: %v; want refused %v", err, tc.refused)
			}

			if tc.refused {
				return
			}

			tools := "[]"
			if a.tools != nil {
				tools = fmt.Sprintf("%q", a.tools)
			}

			if got := fmt.Sprintf("%q %s %s %s %s", a.body, tools, a.model, a.effort, a.advisor); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSpecForm(t *testing.T) {
	const tests = "## Acceptance tests\n\n### 1. One\n\n- **Given**: a.\n- **When**: b.\n- **Then**: c.\n\n"
	head := "# Spec 1: x\n\nStatus: Approved\n\n## Overview\n\n## Goals\n\n## Non-goals\n\n## Open Questions\n\n"
	tail := "## Technical Plan\n\n## Alternatives Considered\n"
	for name, tc := range map[string]struct {
		spec    string
		refused bool
	}{
		"the template itself":                 {spec: template},
		"every section, in order":             {spec: head + tests + tail},
		"a section of the spec's own between": {spec: head + "## Terms\n\n" + tests + "## Own\n\n" + tail},
		"a section missing":                   {spec: head + tests + "## Technical Plan\n", refused: true},
		"sections out of order":               {spec: head + tail + tests, refused: true},
		"a section only inside a fence": {
			spec: head + tests + "```text\n## Technical Plan\n```\n## Alternatives Considered\n", refused: true,
		},
		"tests numbered from 2": {spec: head + strings.Replace(tests, "### 1.", "### 2.", 1) + tail, refused: true},
		"a test skipped": {
			spec:    head + tests + "### 3. Three\n\n- **Given**: a.\n- **When**: b.\n- **Then**: c.\n\n" + tail,
			refused: true,
		},
		"a test with no Then":     {spec: head + strings.Replace(tests, "- **Then**: c.\n", "", 1) + tail, refused: true},
		"a test with no number":   {spec: head + strings.Replace(tests, "### 1. One", "### One", 1) + tail, refused: true},
		"no acceptance test":      {spec: head + "## Acceptance tests\n\n" + tail, refused: true},
		"a Then before its Given": {spec: head + strings.Replace(tests, "Given", "Then", 1) + tail, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := formOf(tc.spec); (err != nil) != tc.refused {
				t.Errorf("refused: %v; want refused %v", err, tc.refused)
			}
		})
	}
}

// TestSpecBeforeKiln holds where kiln reads a spec at an old commit. It is under .kiln/specs/, or under specs/
// where the commit holds no spec under .kiln/.
func TestSpecBeforeKiln(t *testing.T) {
	r, repo := scratchRepo(t)
	shell(t, repo, "mkdir specs && echo old >specs/x.md && git add -A && git commit --quiet -m old")
	old := git.Rev(shell(t, repo, "git rev-parse HEAD"))
	shell(t, repo, "mkdir -p .kiln/specs && echo new >.kiln/specs/x.md && git add -A && git commit --quiet -m new")
	moved := git.Rev(shell(t, repo, "git rev-parse HEAD"))
	for at, want := range map[git.Rev]string{old: "old\n", moved: "new\n"} {
		spec, found, err := r.specAt(t.Context(), at, "x")
		if err != nil || !found || string(spec) != want {
			t.Errorf("at %s, the spec is %q, %v, %v; want %q", short(at), spec, found, err, want)
		}
	}
}

// TestInitKeeps holds what kiln init leaves as the project made it. It adds to kiln.json only the keys it lacks. It
// writes no default over a file of its name, and links nothing over one in .claude/.
func TestInitKeeps(t *testing.T) {
	r, repo := scratchRepo(t)
	shell(t, repo, `mkdir -p .kiln/agents .claude/agents && `+
		`echo '{"gate": ["true"], "rounds": 1}' >.kiln/kiln.json && `+
		`echo mine >.kiln/agents/build.md && echo mine >.claude/agents/critic.md`)
	if err := r.Init(t.Context(), io.Discard); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(repo, configFile))
	if err != nil {
		t.Fatal(err)
	}

	var c config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}

	if fmt.Sprint(c.Gate, c.Rounds) != "[true] 1" || len(c.Hosts) == 0 || len(c.Writable) == 0 {
		t.Errorf("kiln.json is %s; want its gate and rounds kept, with hosts and writable added", raw)
	}

	for p, want := range map[string]string{".kiln/agents/build.md": "mine\n", ".claude/agents/critic.md": "mine\n"} {
		if got, err := os.ReadFile(filepath.Join(repo, p)); err != nil || string(got) != want {
			t.Errorf("%s holds %q, %v; want %q", p, got, err, want)
		}
	}

	if _, err := os.Stat(filepath.Join(repo, agentFile(auditRole))); err != nil {
		t.Errorf("kiln init wrote no audit agent beside the project's build agent: %v", err)
	}

	link, err := os.Readlink(filepath.Join(repo, ".claude/agents/zoom-out.md"))
	if link != "../../.kiln/agents/zoom-out.md" {
		t.Errorf(".claude/agents/zoom-out.md links to %q, %v", link, err)
	}
}

// scratchRepo is a new repository, with no commit, opened.
func scratchRepo(t *testing.T) (*Repo, string) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	shell(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	r, err := Open(t.Context(), repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	return r, repo
}

// TestQueueNeedsInit holds that kiln queue refuses while main lacks a file of .kiln/ it needs, and names kiln init.
func TestQueueNeedsInit(t *testing.T) {
	for _, missing := range []string{configFile, agentFile(buildRole), agentFile(auditRole)} {
		t.Run(missing, func(t *testing.T) {
			r, repo := scratchRepo(t)
			if err := r.Init(t.Context(), io.Discard); err != nil {
				t.Fatal(err)
			}

			shell(t, repo, "mkdir .kiln/specs && cp .kiln/skills/shape/template.md .kiln/specs/x.md && "+
				"sed -i '' 's/^Status: Proposed/Status: Approved/' .kiln/specs/x.md && rm "+missing+
				" && git add -A && git commit --quiet --no-verify -m start")
			err := r.Queue(t.Context(), "x", io.Discard)
			if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "kiln init") {
				t.Errorf("kiln queue answered %v; want a refusal that names kiln init", err)
			}
		})
	}
}

// TestAuditedPackages holds which packages the audit reads. A Go file at the top names the package ".". A directory
// whose last Go file the work deleted names none.
func TestAuditedPackages(t *testing.T) {
	b, repo, _ := landingBuild(t)
	shell(t, repo, "mkdir gone && echo 'package gone' >gone/g.go && git add -A && git commit --quiet -m gone")
	if err := b.r.git.Reset(t.Context(), b.tree, git.Rev(shell(t, repo, "git rev-parse HEAD"))); err != nil {
		t.Fatal(err)
	}

	shell(t, string(b.tree), "git rm --quiet gone/g.go && mkdir sub && echo 'package sub' >sub/s.go && "+
		"echo 'package top' >top.go && echo notes >notes.md && git add -A && git commit --quiet -m work")
	_, packages, err := b.changedPackages(t.Context())
	if got := fmt.Sprint(packages); err != nil || got != "[./sub .]" {
		t.Errorf("the audit reads %s, %v; want [./sub .]", got, err)
	}
}
