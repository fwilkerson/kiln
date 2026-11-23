package kiln

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/gittest"
	"github.com/fwilkerson/practices/claim"
)

func TestAgentFile(t *testing.T) {
	for name, tc := range map[string]struct {
		file, want string
		refused    bool
	}{
		"a file with no frontmatter is all body, and names no effort or advisor": {
			file: "Build it.\n",
			want: `"Build it." [] "opus" "" ""`,
		},
		"the frontmatter names the tools, model, effort and advisor": {
			file: "---\ntools: Read, Grep\nmodel: sonnet\neffort: high\nadvisor: opus\n---\n\nAudit it.\n",
			want: `"Audit it." ["Read" "Grep"] "sonnet" "high" "opus"`,
		},
		"other keys and # lines are ignored, and a value may be quoted": {
			file: "---\n# model: haiku\nname: audit\ndescription: reads: packages\neffort: \"max\"\n---\nAudit.\n",
			want: `"Audit." [] "opus" "max" ""`,
		},
		"an empty frontmatter names nothing": {
			file: "---\n---\nBuild.",
			want: `"Build." [] "opus" "" ""`,
		},
		"a frontmatter with no closing line is refused":  {file: "---\nmodel: opus\nBuild it.\n", refused: true},
		"an effort Claude Code does not take is refused": {file: "---\neffort: huge\n---\nBuild.\n", refused: true},
		"tools as a YAML block list are refused":         {file: "---\ntools:\n  - Read\n---\nA.\n", refused: true},
		"tools as a YAML flow list are refused":          {file: "---\ntools: [Read, Grep]\n---\nA.\n", refused: true},
		"tools that name nothing are refused":            {file: "---\ntools: \"\"\n---\nA.\n", refused: true},
		"a conflict's markers are refused": {
			file:    "Build.\n<<<<<<< .kiln/agents/build.md\nmine\n=======\nkiln's\n>>>>>>> kiln init's new default\n",
			refused: true,
		},
		"a heading underlined with = is no marker": {file: "Build\n=======\n", want: `"Build\n=======" [] "opus" "" ""`},
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

			if got := fmt.Sprintf("%q %s %q %q %q", a.body, tools, a.model, a.effort, a.advisor); got != tc.want {
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
		refused int
	}{
		"the template itself":                 {spec: template},
		"every section, in order":             {spec: head + tests + tail},
		"a section of the spec's own between": {spec: head + "## Terms\n\n" + tests + "## Own\n\n" + tail},
		"a section missing":                   {spec: head + tests + "## Technical Plan\n", refused: 1},
		"sections out of order":               {spec: head + tail + tests, refused: 2},
		"a section only inside a fence": {
			spec: head + tests + "```text\n## Technical Plan\n```\n## Alternatives Considered\n", refused: 1,
		},
		"tests numbered from 2": {spec: head + strings.Replace(tests, "### 1.", "### 2.", 1) + tail, refused: 1},
		"a test skipped": {
			spec:    head + tests + "### 3. Three\n\n- **Given**: a.\n- **When**: b.\n- **Then**: c.\n\n" + tail,
			refused: 1,
		},
		"a test with no Then":     {spec: head + strings.Replace(tests, "- **Then**: c.\n", "", 1) + tail, refused: 1},
		"a test with no number":   {spec: head + strings.Replace(tests, "### 1. One", "### One", 1) + tail, refused: 1},
		"no acceptance test":      {spec: head + "## Acceptance tests\n\n" + tail, refused: 1},
		"a Then before its Given": {spec: head + strings.Replace(tests, "Given", "Then", 1) + tail, refused: 1},
		"each way it leaves the form": {
			spec: head + strings.Replace(tests, "- **Then**: c.\n", "", 1) + "## Technical Plan\n", refused: 2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if refused := formOf(tc.spec); len(refused) != tc.refused {
				t.Errorf("refused: %q; want %d refusals", refused, tc.refused)
			}
		})
	}
}

// TestSpecBeforeKiln holds where kiln reads a spec at an old commit. It is under .kiln/specs/, or under specs/
// where the commit holds no spec under .kiln/.
func TestSpecBeforeKiln(t *testing.T) {
	t.Parallel()
	h := gittest.New(t)
	r, _ := historyRepo(t, h)
	old := h.CommitFiles("main", gittest.Files{"specs/x.md": "old\n"})
	moved := h.CommitFiles("main", gittest.Files{".kiln/specs/x.md": "new\n"})
	for at, want := range map[git.Rev]string{old: "old\n", moved: "new\n"} {
		spec, found, err := r.specAt(t.Context(), at, "x")
		if err != nil || !claim.Holds(found, string(spec) == want) {
			t.Errorf("at %s, the spec is %q, %v, %v; want %q", short(at), spec, found, err, want)
		}
	}
}

// TestInitKeeps holds what kiln init leaves as the project made it. It keeps kiln.json's gate and rounds, and adds no
// allowedHosts or writablePathCommands key, since those differ by preset. With no record, it keeps the project's own
// file of a default. The difference is left as a conflict. It links nothing over a file in .claude/. It names each
// such file, unless that path already reaches kiln's own file through a link.
func TestInitKeeps(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	shell(t, repo, `mkdir -p .kiln/agents .claude/agents && `+
		`echo '{"gate": ["true"], "rounds": 1}' >.kiln/kiln.json && `+
		`echo mine >.kiln/agents/build.md && echo mine >.claude/agents/critic.md && `+
		`ln -s ../../.kiln/reviewers/comprehension.md .claude/agents && ln -s ../.kiln/skills .claude/skills`)
	var out strings.Builder
	if err := r.Init(t.Context(), &out, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	const skipped = "linked nothing at .claude/agents/critic.md, since another file is there; " +
		"rename one to link .kiln/reviewers/critic.md\n"
	if got := strings.Count(out.String(), "linked nothing"); got != 1 || !strings.Contains(out.String(), skipped) {
		t.Errorf("kiln init said:\n%s\nwant the one line %q", out.String(), skipped)
	}

	wantConfigKept(t, repo)
	for p, want := range map[string]string{".kiln/agents/build.md": "<<<<<<< .kiln/agents/build.md\nmine\n",
		".claude/agents/critic.md": "mine\n"} {
		if got, err := os.ReadFile(filepath.Join(repo, p)); err != nil || !strings.HasPrefix(string(got), want) {
			t.Errorf("%s holds %q, %v; want it to start %q", p, got, err, want)
		}
	}

	if _, err := os.Stat(filepath.Join(repo, agentFile(auditRole))); err != nil {
		t.Errorf("kiln init wrote no audit agent beside the project's build agent: %v", err)
	}

	link, err := os.Readlink(filepath.Join(repo, ".claude/agents/zoom-out.md"))
	if link != "../../.kiln/reviewers/zoom-out.md" {
		t.Errorf(".claude/agents/zoom-out.md links to %q, %v", link, err)
	}
}

// TestInitReviewers holds how kiln init links the reviewers a project keeps. It refuses a reviewer that Claude Code
// could not run, before it writes anything. It writes no default reviewer into a reviewers folder that is there. It
// links each reviewer there, in place of a link that reaches nothing. It removes each link to a reviewer that is gone.
func TestInitReviewers(t *testing.T) {
	t.Parallel()
	r, repo := scratchRepo(t)
	shell(t, repo, `mkdir -p .kiln/reviewers .kiln/agents .claude/agents && touch .kiln/reviewers/.gitkeep && `+
		`printf -- '---\nname: upgrade\n---\n' >.kiln/reviewers/upgrade.md && `+
		`ln -s ../../.kiln/agents/upgrade.md .claude/agents && `+
		`ln -s ../../.kiln/reviewers/critic.md .claude/agents && ln -s ../../.kiln/agents/gone.md .claude/agents`)
	err := r.Init(t.Context(), io.Discard, InitOptions{Preset: "go"})
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), ".kiln/reviewers/upgrade.md") {
		t.Errorf("kiln init with a reviewer that has no description: %v; want it refused, naming the file", err)
	}

	if _, err := os.Stat(filepath.Join(repo, configFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("kiln init refused a reviewer, but wrote %s: %v", configFile, err)
	}

	shell(t, repo, "printf -- '---\nname: upgrade\ndescription: Reads a spec.\n---\nRead it.\n' "+
		">.kiln/reviewers/upgrade.md")
	var out strings.Builder
	if err := r.Init(t.Context(), &out, InitOptions{Preset: "go"}); err != nil {
		t.Fatal(err)
	}

	const line = "unlinked .claude/agents/critic.md, since .kiln/reviewers/critic.md is gone\n"
	if !strings.Contains(out.String(), line) {
		t.Errorf("kiln init said:\n%s\nwant the line %q", out.String(), line)
	}

	for p, want := range map[string]bool{
		".kiln/reviewers/critic.md": false, ".claude/agents/critic.md": false,
		".claude/agents/upgrade.md": true, ".claude/agents/gone.md": true,
	} {
		if _, err := os.Lstat(filepath.Join(repo, p)); (err == nil) != want {
			t.Errorf("%s: %v; want it there: %v", p, err, want)
		}
	}

	link, err := os.Readlink(filepath.Join(repo, ".claude/agents/upgrade.md"))
	if link != "../../.kiln/reviewers/upgrade.md" {
		t.Errorf(".claude/agents/upgrade.md links to %q, %v", link, err)
	}
}

// TestBadReviewer holds which reviewers kiln init refuses. Claude Code could not run one, or would run it by another
// name than its file's.
func TestBadReviewer(t *testing.T) {
	for name, tc := range map[string]struct {
		file    string
		refused bool
	}{
		"a name and a description":           {file: "---\nname: x\ndescription: Reads.\n---\nRead.\n"},
		"a quoted name, and other keys":      {file: "---\nname: \"x\"\nrerun: true\ndescription: 'Reads.'\n---\n"},
		"no frontmatter":                     {file: "Read.\n", refused: true},
		"a name other than the file's":       {file: "---\nname: y\ndescription: Reads.\n---\n", refused: true},
		"no description":                     {file: "---\nname: x\n---\nRead.\n", refused: true},
		"a description only in a # line":     {file: "---\nname: x\n# description: Reads.\n---\n", refused: true},
		"a frontmatter with no closing line": {file: "---\nname: x\ndescription: Reads.\n", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := badReviewer("x.md", tc.file); (err != nil) != tc.refused {
				t.Errorf("refused: %v; want refused %v", err, tc.refused)
			}
		})
	}
}

// wantConfigKept holds that kiln.json keeps the gate and rounds TestInitKeeps wrote, with no sandbox key added.
func wantConfigKept(t *testing.T, repo string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, configFile))
	if err != nil {
		t.Fatal(err)
	}

	var c config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}

	if !claim.Holds(fmt.Sprint(c.Gate, c.Rounds) == "[true] 1", c.AllowedHosts == nil, c.WritablePathCommands == nil) {
		t.Errorf("kiln.json is %s; want its gate and rounds kept, with no sandbox key added", raw)
	}
}

// scratchRepo is a new repository, with no commit, opened.
func scratchRepo(t *testing.T) (*Repo, string) {
	repo := t.TempDir()
	shell(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	r, err := Open(t.Context(), repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	return r, repo
}

// historyRepo is a repository whose git is h, the stand-in of package gittest, and its checkout, an empty directory.
// No git repository holds the checkout. kiln's PATH names an empty directory, so kiln finds no program by name, such
// as claude. kiln still runs /bin/sh by its path, and plainGit finds git on the test's own PATH. h fails the test where
// kiln asks it what it does not answer.
func historyRepo(t *testing.T, h *gittest.History) (*Repo, string) {
	top := t.TempDir()
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
	r, err := newRepo(top, filepath.Join(top, ".git"), h, append(env, "PATH="+t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}

	return r, top
}

// TestQueueNeedsInit holds that kiln queue refuses while main lacks a file of .kiln/ it needs, and names kiln init.
func TestQueueNeedsInit(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{configFile, agentFile(buildRole), agentFile(verifyRole), agentFile(auditRole)} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			h, _ := approvedX(t)
			r, _ := historyRepo(t, h)
			h.CommitRemoved(mainBranch, missing)
			err := r.Queue(t.Context(), "x", io.Discard)
			if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "kiln init") {
				t.Errorf("kiln queue answered %v; want a refusal that names kiln init", err)
			}
		})
	}
}

// TestQueueNeedsTestsPlace holds that kiln queue refuses a kiln.json that names no place for the acceptance tests.
// It refuses paths that two specs could share. It refuses a spec whose paths meet a testHelpers path. It refuses a
// build agent that says where the tests live.
func TestQueueNeedsTestsPlace(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		// config changes kiln.json, and build ends the build agent.
		config         func(string) string
		build, refusal string
	}{
		"kiln init's files are taken":                {},
		"kiln.json names no acceptance":              {config: noAcceptance, refusal: "names no acceptance"},
		"its acceptance starts with the spec's name": {config: acceptanceOf(`"{spec}/"`), refusal: "starts with {spec}"},
		"a list of two paths is taken":               {config: acceptanceOf(`["uat/{spec}/", "integration/{spec}/"]`)},
		"an empty list":                              {config: acceptanceOf(`[]`), refusal: "names no acceptance"},
		"a path without the spec's name":             {config: acceptanceOf(`["uat/{spec}/", "integration/"]`), refusal: "lacks {spec}"},
		"a path not in its clean form":               {config: acceptanceOf(`["./uat/{spec}/"]`), refusal: "not a clean path"},
		"a path above the repository":                {config: acceptanceOf(`["../{spec}/"]`), refusal: "not a clean path"},
		"a path from the root":                       {config: acceptanceOf(`["/uat/{spec}/"]`), refusal: "not a clean path"},
		"a fixed part that starts another's":         {config: acceptanceOf(`["uat/{spec}/", "uat/integration/{spec}/"]`), refusal: "could make two"},
		"one fixed part twice, in other letters":     {config: acceptanceOf(`["test_{spec}.py", "Test_{spec}-it.py"]`), refusal: "could make two"},
		"a fixed part that only shares a prefix":     {config: acceptanceOf(`["uat/{spec}/", "uat-it/{spec}/"]`)},
		"the spec's path is a helpers path":          {config: helpersOf(`acceptance/x/`), refusal: "meets the testHelpers path"},
		"a helpers path is under the spec's path":    {config: helpersOf(`acceptance/x/h/`), refusal: "meets the testHelpers path"},
		"the spec's path is under a helpers path":    {config: helpersOf(`acceptance/`), refusal: "meets the testHelpers path"},
		"a helpers path beside the spec's":           {config: helpersOf(`acceptance/xh/`)},
		"the build agent names where the tests live": {build: "\nWrite them in acceptance/x/.\n",
			refusal: "move that text to .kiln/agents/verify.md"},
		"the build agent names the second path": {config: acceptanceOf(`["uat/{spec}/", "integration/{spec}/"]`),
			build: "\nRun the suite in (integration/).\n", refusal: "move that text to .kiln/agents/verify.md"},
		"the build agent names a path that only ends in a part": {
			config: acceptanceOf(`["uat/{spec}/", "git/{spec}/"]`), build: "\nThe wrapper is in internal/git/.\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, tree := approvedX(t)
			r, _ := historyRepo(t, h)
			config, build := readFile(t, tree, configFile), readFile(t, tree, agentFile(buildRole))
			if tc.config != nil {
				config = tc.config(config)
			}

			h.CommitFiles(mainBranch, gittest.Files{configFile: config, agentFile(buildRole): build + tc.build})
			err := r.Queue(t.Context(), "x", io.Discard)
			switch {
			case tc.refusal == "" && err != nil:
				t.Errorf("kiln queue answered %v; want it queued", err)
			case tc.refusal != "" &&
				!claim.Holds(errors.Is(err, errRefused), strings.Contains(fmt.Sprint(err), tc.refusal)):
				t.Errorf("kiln queue answered %v; want a refusal that says %q", err, tc.refusal)
			}
		})
	}
}

// acceptance is a shell command that sets kiln.json's acceptanceTests to raw, a JSON value.
func acceptance(raw string) string {
	return `sed -i '' 's|"acceptanceTests": .*|"acceptanceTests": ` + raw + `|' .kiln/kiln.json`
}

// acceptanceOf changes kiln.json, as acceptance does, without a shell.
func acceptanceOf(raw string) func(config string) string {
	return func(config string) string {
		return acceptanceKey.ReplaceAllLiteralString(config, `"acceptanceTests": `+raw)
	}
}

// acceptanceKey matches kiln.json's acceptanceTests, to the end of its line.
var acceptanceKey = regexp.MustCompile(`"acceptanceTests": .*`)

// helpersOf changes kiln.json to name one testHelpers path, p.
func helpersOf(p string) func(config string) string {
	return func(config string) string {
		return strings.Replace(config, `"acceptanceTests": `, `"testHelpers": ["`+p+`"], "acceptanceTests": `, 1)
	}
}

// noAcceptance is kiln.json without its acceptanceTests.
func noAcceptance(config string) string {
	return acceptanceLine.ReplaceAllLiteralString(config, "")
}

// acceptanceLine matches kiln.json's line of acceptanceTests, with the comma that ends the line above it.
var acceptanceLine = regexp.MustCompile(`,\n  "acceptanceTests": [^\n]*`)

// inited is a checkout that kiln init set up with the preset go, in a repository of its own. Tests that read it share
// it, and none changes it.
var inited = sync.OnceValues(func() (string, error) {
	repo, err := os.MkdirTemp(os.Getenv("HOME"), "inited-")
	if err != nil {
		return "", err
	}

	cmd := exec.Command("/bin/sh", "-c", "git init --quiet -b main && git config user.name t && "+
		"git config user.email t@t")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init: %w: %s", err, out)
	}

	r, err := Open(context.Background(), repo, os.Environ())
	if err == nil {
		err = r.Init(context.Background(), io.Discard, InitOptions{Preset: "go"})
	}

	return repo, err
})

// approvedX is gittest's History whose main holds what kiln init wrote in inited, then the spec x. x is /shape's
// template, Approved. approvedX answers inited's checkout too.
func approvedX(t *testing.T) (*gittest.History, string) {
	tree, err := inited()
	if err != nil {
		t.Fatal(err)
	}

	h := gittest.New(t)
	h.CommitDir(mainBranch, tree)
	template := readFile(t, tree, ".kiln/skills/shape/template.md")
	h.CommitFiles(mainBranch, gittest.Files{Spec("x").file(): proposed.ReplaceAllLiteralString(template,
		"Status: Approved")})
	return h, tree
}

// proposed matches a spec's Status line, while the spec is Proposed.
var proposed = regexp.MustCompile(`(?m)^Status: Proposed`)

// readFile is the file at p, from dir.
func readFile(t *testing.T, dir, p string) string {
	raw, err := os.ReadFile(filepath.Join(dir, p))
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// TestAuditedDirs holds which directories the audit reads. A file at the top, in any language, names "." for the top.
// A directory that the work emptied of files names the nearest directory above it. A file under .kiln/ names none.
func TestAuditedDirs(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ work, want string }{
		"files in several directories and languages": {
			work: "git rm --quiet deep/gone/g.go && mkdir sub && echo 'package sub' >sub/s.go && " +
				"echo 'package top' >top.go && echo notes >notes.md && mkdir app && echo 'x = 1' >app/models.py && " +
				"mkdir -p .kiln/specs && echo amended >.kiln/specs/x.amend.md",
			want: "[. app deep sub]",
		},
		"only a deletion": {work: "git rm --quiet deep/gone/g.go", want: "[deep]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, repo, _ := landingBuild(t)
			shell(t, repo, "mkdir -p deep/gone && echo 'package gone' >deep/gone/g.go && echo d >deep/d.md && "+
				"git add -A && git commit --quiet -m gone")
			if err := b.r.git.Reset(t.Context(), b.tree, git.Rev(shell(t, repo, "git rev-parse HEAD"))); err != nil {
				t.Fatal(err)
			}

			shell(t, string(b.tree), tc.work+" && git add -A && git commit --quiet -m work")
			_, dirs, _, err := b.changedFiles(t.Context())
			if got := fmt.Sprint(dirs); err != nil || got != tc.want {
				t.Errorf("the audit reads %s, %v; want %s", got, err, tc.want)
			}
		})
	}
}

// TestAuditReadsLaterWork holds that only the audit's read records it. An audit with nothing to read records none, so
// the session's later work gets the audit before it lands. An audit that answers nothing runs again.
func TestAuditReadsLaterWork(t *testing.T) {
	t.Parallel()
	b, repo, _ := landingBuild(t)
	shell(t, repo, "mkdir .kiln/agents && echo 'Audit the work.' >.kiln/agents/audit.md && git add -A && "+
		"git commit --quiet -m agents")
	main := git.Rev(shell(t, repo, "git rev-parse HEAD"))
	if err := b.r.git.Reset(t.Context(), b.tree, main); err != nil {
		t.Fatal(err)
	}

	b.set(func(r *record) { r.Approved = main })
	program := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(program, []byte(strings.ReplaceAll(replying, "REPLIES", "er")), 0o700); err != nil {
		t.Fatal(err)
	}

	b.r.claude = claude.New(claude.Config{Program: program, Sessions: filepath.Join(t.TempDir(), "sessions")})
	for _, step := range []struct{ work, findings string }{
		{work: "true"},
		{work: "echo 'package x' >x.go && git add x.go && git commit --quiet -m x", findings: "Done."},
	} {
		shell(t, string(b.tree), step.work)
		findings, _, err := b.findings(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		head := git.Rev(shell(t, string(b.tree), "git rev-parse HEAD"))
		read := map[bool]git.Rev{true: head, false: ""}[step.findings != ""]
		if !claim.Holds(findings == step.findings, b.now().Audited == read) {
			t.Errorf("after %q, the audit found %q and the record says it read %q; want %q, and %q", step.work,
				findings, b.now().Audited, step.findings, read)
		}
	}
}
