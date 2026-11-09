// Package git is kiln's git: worktrees for builds, the commits a build lands as, and the one move of main.
//
//	g, err := git.New(repo, os.Environ())
//	err = g.Checkout(ctx, tree, "kiln/x", "main")      // a branch made from main, in its own worktree
//	_, err = g.Commit(ctx, tree, "a session's work")    // whatever the session left uncommitted
//	onto, err := g.Tip(ctx, "main")
//	landed, conflict, err := g.Rebase(ctx, tree, onto, "feat(x): land the approved spec")
//	// ... gate landed ...
//	way, err := g.Move(ctx, "main", landed, onto)       // refused, with way.Tip, if main moved meanwhile
//
// Every call runs with hooks, signing, autostash and background work off. The developer's config and the repository's
// cannot turn them on. A branch is named by its short name, as refs/heads/ holds it. A relative path is the main
// checkout's.
//
// Its errors name the git command that failed and what it printed. Rebase answers a conflict as a [Conflict], not an
// error. Move answers a move it refused as an [InTheWay], not an error.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/fwilkerson/kiln/internal/proc"
)

// pinned overrides, for every call, what config could otherwise make git do. It could run a hook or a monitor, or sign
// and so wait on a passphrase. It could stash the developer's changes, move other branches in a rebase, or start work
// in the background.
var pinned = [][2]string{
	{"core.hooksPath", "/dev/null"},
	{"core.fsmonitor", "false"},
	{"commit.gpgSign", "false"},
	{"merge.autoStash", "false"},
	{"rebase.autoStash", "false"},
	{"rebase.updateRefs", "false"},
	{"submodule.recurse", "false"},
	{"gc.auto", "0"},
	{"maintenance.auto", "false"},
}

// events are githooks(5)'s at git 2.55. hook.<event>.enabled=false turns off a hook defined in config, which
// core.hooksPath does not.
var events = []string{
	"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit", "pre-merge-commit", "prepare-commit-msg",
	"commit-msg", "post-commit", "pre-rebase", "post-checkout", "post-merge", "pre-push", "pre-receive", "update",
	"proc-receive", "post-receive", "post-update", "reference-transaction", "push-to-checkout", "pre-auto-gc",
	"post-rewrite", "sendemail-validate", "fsmonitor-watchman", "p4-changelist", "p4-prepare-changelist",
	"p4-post-changelist", "p4-pre-submit", "post-index-change",
}

// configured passes every pin as GIT_CONFIG_KEY_<n>, since each -c adds to a call's time.
var configured = func() []string {
	pairs := slices.Clone(pinned)
	for _, event := range events {
		pairs = append(pairs, [2]string{"hook." + event + ".enabled", "false"})
	}

	env := make([]string, 0, 2*len(pairs)+1)
	for i, kv := range pairs {
		key, value := fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1])
		env = append(env, key, value)
	}

	return append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(pairs)))
}()

// Git runs git on one repository, from its main checkout.
type Git struct {
	repo      string
	found     string
	bin       string
	resolve   sync.Once
	env       []string
	worktrees sync.Mutex
}

// New returns the git found on env's PATH, working on the repository whose main checkout is repo. Its only error is a
// *[Unusable].
func New(repo string, env []string) (*Git, error) {
	bin, err := proc.Look(env, "git")
	if missing, ok := errors.AsType[*proc.NotOnPath](err); ok {
		return nil, &Unusable{Path: missing.Path}
	}

	return &Git{repo: repo, found: bin, env: environment(env)}, nil
}

// Unusable is a git kiln cannot use: none on the PATH, or one before 2.55, which lacks hook.<event>.enabled.
type Unusable struct {
	Found string
	Said  string
	Path  string
}

func (e *Unusable) Error() string {
	if e.Found == "" {
		return fmt.Sprintf("git is not on the PATH given: %q; kiln needs git 2.55 or later", e.Path)
	}

	return fmt.Sprintf("the git at %s says %q; kiln needs git 2.55 or later", e.Found, e.Said)
}

// Check refuses a git that cannot say its version, or one before 2.55, with an *[Unusable].
func (g *Git) Check(ctx context.Context) error {
	said, err := g.line(ctx, g.repo, "version")
	if refused, ok := errors.AsType[*failed](err); ok {
		return &Unusable{Found: g.binary(ctx), Said: strings.TrimSpace(refused.Stderr)}
	}

	if start, ok := errors.AsType[*fs.PathError](err); ok && start.Op == "fork/exec" {
		return &Unusable{Found: g.binary(ctx), Said: start.Err.Error()}
	}

	if err != nil {
		return err
	}

	if !supported(said) {
		return &Unusable{Found: g.binary(ctx), Said: said}
	}

	return nil
}

func supported(said string) bool {
	version, ok := strings.CutPrefix(said, "git version ")
	major, rest, _ := strings.Cut(version, ".")
	minor, _, _ := strings.Cut(rest, ".")
	x, xErr := strconv.Atoi(major)
	y, yErr := strconv.Atoi(minor)
	return ok && xErr == nil && yErr == nil && (x > 2 || x == 2 && y >= 55)
}

// shim asks xcrun for the developer tools' git on every call, which near doubles each call's time.
const shim = "/usr/bin/git"

func (g *Git) binary(ctx context.Context) string {
	g.resolve.Do(func() { g.bin = throughShim(ctx, g.found, g.env) })
	return g.bin
}

// throughShim keeps the shim when xcrun names no git, so a call fails in git as it did before.
func throughShim(ctx context.Context, bin string, env []string) string {
	if bin != shim {
		return bin
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/xcrun", "--find", "git")
	cmd.Env = env
	out, err := cmd.Output()
	found := strings.TrimSpace(string(out))
	if err != nil || !filepath.IsAbs(found) || found == shim || !proc.Executable(found) {
		return bin
	}

	return found
}

func environment(base []string) []string {
	return append(proc.Git.Env(base), "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}

type failed struct {
	Args   []string
	Exit   int
	Stderr string
}

func (f *failed) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(f.Args, " "), f.Exit, strings.TrimSpace(f.Stderr))
}

type call struct {
	dir   string
	stdin string
	env   []string
}

// run's own process group keeps an interrupt meant for kiln from ending its git.
func (g *Git) run(ctx context.Context, c call, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.binary(ctx), args...)
	proc.OwnGroup(cmd)
	cmd.Dir = c.dir
	cmd.Env = slices.Concat(g.env, configured, c.env)
	cmd.Stdin = strings.NewReader(c.stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return stdout.String(), &failed{Args: args, Exit: exit.ExitCode(), Stderr: stderr.String()}
		}

		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

func (g *Git) in(ctx context.Context, dir string, args ...string) (string, error) {
	return g.run(ctx, call{dir: g.at(dir)}, args...)
}

func (g *Git) line(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.in(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func (g *Git) at(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(g.repo, path)
}

// CommonDir is the repository's git directory that every worktree shares.
func (g *Git) CommonDir(ctx context.Context) (string, error) {
	return g.line(ctx, g.repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// Checkout puts branch, made from the commit at, in a new worktree at path. If that fails part way, it removes what
// it made.
func (g *Git) Checkout(ctx context.Context, path, branch, at string) error {
	g.worktrees.Lock()
	defer g.worktrees.Unlock()
	path = g.at(path)
	if _, err := g.in(ctx, g.repo, "worktree", "prune"); err != nil {
		return err
	}

	_, there := os.Lstat(path)
	err := g.addWorktree(ctx, path, "-b", branch, path, at)
	if err == nil {
		return nil
	}

	return errors.Join(err, g.unmade(ctx, path, there != nil, branch))
}

// worktree add makes the branch before the tree, and can fail between them.
func (g *Git) unmade(ctx context.Context, path string, madePath bool, branch string) error {
	var err error
	if madePath {
		err = os.RemoveAll(path)
	}

	if _, pruned := g.in(ctx, g.repo, "worktree", "prune"); pruned != nil {
		return errors.Join(err, pruned)
	}

	if tip, found := g.Tip(ctx, branch); found == nil && tip != "" {
		_, deleted := g.in(ctx, g.repo, "branch", "--quiet", "-D", branch)
		err = errors.Join(err, deleted)
	}

	return err
}

// Detached puts commit in a worktree at path with no branch, making the worktree if it is not there yet. It removes
// every file the commit does not hold, ignored ones included, so what runs there sees only the commit.
func (g *Git) Detached(ctx context.Context, path, commit string) error {
	g.worktrees.Lock()
	defer g.worktrees.Unlock()
	path = g.at(path)
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if _, err := g.in(ctx, g.repo, "worktree", "prune"); err != nil {
			return err
		}

		return g.addWorktree(ctx, path, "--detach", path, commit)
	}

	for _, args := range [][]string{{"checkout", "--quiet", "--force", "--detach", commit}, {"clean", "-qfdx"}} {
		if _, err := g.in(ctx, path, args...); err != nil {
			return err
		}
	}

	return nil
}

// addWorktree runs git worktree add with args. It checks the worktree out with each symbolic link as a plain file
// that holds its target. Claude Code's sandbox refuses a shell's write to where a link under .claude/ points. With
// links, a session could not change guidance/ behind .claude/agents. The main checkout keeps its links.
func (g *Git) addWorktree(ctx context.Context, path string, args ...string) error {
	if _, err := g.in(ctx, g.repo, "config", "set", "extensions.worktreeConfig", "true"); err != nil {
		return err
	}

	add := append([]string{"worktree", "add", "--quiet", "--no-checkout"}, args...)
	if _, err := g.in(ctx, g.repo, add...); err != nil {
		return err
	}

	if _, err := g.in(ctx, path, "config", "set", "--worktree", "core.symlinks", "false"); err != nil {
		return err
	}

	_, err := g.in(ctx, path, "reset", "--quiet", "--hard")
	return err
}

// Link stages, at checkout, a symbolic link at path to target, and writes it as checkout keeps links. A commit takes
// it as a link, though checkout holds a plain file.
func (g *Git) Link(ctx context.Context, checkout, path, target string) error {
	blob, err := g.run(ctx, call{dir: g.at(checkout), stdin: target}, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}

	info := "120000," + strings.TrimSpace(blob) + "," + path
	if _, err := g.in(ctx, checkout, "update-index", "--add", "--cacheinfo", info); err != nil {
		return err
	}

	_, err = g.in(ctx, checkout, "checkout-index", "--force", "--", path)
	return err
}

// Head is the commit checked out at checkout.
func (g *Git) Head(ctx context.Context, checkout string) (string, error) {
	return g.line(ctx, checkout, "rev-parse", "--verify", "HEAD^{commit}")
}

// Commit commits every change at checkout, untracked files included, and answers the commit at HEAD. With no change it
// commits nothing.
func (g *Git) Commit(ctx context.Context, checkout, message string) (string, error) {
	out, err := g.in(ctx, checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", err
	}

	if out != "" {
		if _, err := g.in(ctx, checkout, "add", "--all"); err != nil {
			return "", err
		}

		c := call{dir: g.at(checkout), stdin: message}
		if _, err := g.run(ctx, c, "commit", "--quiet", "--no-verify", "--cleanup=verbatim", "-F", "-"); err != nil {
			return "", err
		}
	}

	return g.Head(ctx, checkout)
}

// Amend folds every change at checkout into the commit at HEAD, and answers the commit at HEAD. With no change it
// amends nothing.
func (g *Git) Amend(ctx context.Context, checkout string) (string, error) {
	out, err := g.in(ctx, checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", err
	}

	if out == "" {
		return g.Head(ctx, checkout)
	}

	if _, err := g.in(ctx, checkout, "add", "--all"); err != nil {
		return "", err
	}

	if _, err := g.in(ctx, checkout, "commit", "--quiet", "--amend", "--no-edit", "--no-verify"); err != nil {
		return "", err
	}

	return g.Head(ctx, checkout)
}

// Status lists each path at checkout that differs from the commit at HEAD, untracked files included.
func (g *Git) Status(ctx context.Context, checkout string) ([]string, error) {
	out, err := g.in(ctx, checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	var found []string
	for _, p := range paths(out) {
		found = append(found, p[min(3, len(p)):])
	}

	return found, err
}

// Reset puts the branch at checkout, and its files, back at commit, and removes every untracked file.
func (g *Git) Reset(ctx context.Context, checkout, commit string) error {
	for _, args := range [][]string{{"reset", "--quiet", "--hard", commit}, {"clean", "-qfd"}} {
		if _, err := g.in(ctx, checkout, args...); err != nil {
			return err
		}
	}

	return nil
}

// Conflict is a merge that git could not make alone, and the files it conflicts in.
type Conflict struct {
	Files []string
}

// Rebase makes one commit, on onto, of what the branch at checkout changed since it left onto's line. It moves the
// branch and its worktree there, and answers the commit. The commit takes its author from HEAD, and message.
//
// A merge that conflicts is answered as a [Conflict], and changes nothing.
func (g *Git) Rebase(ctx context.Context, checkout, onto, message string) (string, *Conflict, error) {
	head, err := g.Head(ctx, checkout)
	if err != nil {
		return "", nil, err
	}

	base, err := g.line(ctx, checkout, "merge-base", onto, head)
	if err != nil {
		return "", nil, err
	}

	tree, conflict, err := g.mergeTree(ctx, checkout, base, onto, head)
	if err != nil || conflict != nil {
		return "", conflict, err
	}

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	rebased, err := g.commitTree(ctx, checkout, tree, onto, head, message)
	if err != nil {
		return "", nil, err
	}

	if _, err := g.in(ctx, checkout, "reset", "--quiet", "--keep", rebased); err != nil {
		return "", nil, err
	}

	return rebased, nil, nil
}

func (g *Git) commitTree(ctx context.Context, checkout, tree, parent, author, message string) (string, error) {
	by, err := g.in(ctx, checkout, "show", "--no-patch", "--date=raw", "--format=%an%x00%ae%x00%ad", author+"^{commit}")
	if err != nil {
		return "", err
	}

	name, rest, _ := strings.Cut(strings.TrimSuffix(by, "\n"), "\x00")
	email, date, _ := strings.Cut(rest, "\x00")
	made, err := g.run(ctx, call{
		dir:   g.at(checkout),
		stdin: message,
		env:   []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_AUTHOR_DATE=" + date},
	}, "commit-tree", tree, "-p", parent, "-F", "-")
	return strings.TrimSpace(made), err
}

func (g *Git) mergeTree(ctx context.Context, checkout, base, onto, head string) (string, *Conflict, error) {
	args := []string{"--attr-source=" + onto, "merge-tree", "--write-tree", "-z", "--name-only", "--merge-base", base}
	out, err := g.in(ctx, checkout, append(args, onto, head)...)
	refused, ok := errors.AsType[*failed](err)
	if err != nil && (!ok || refused.Exit != 1) {
		return "", nil, err
	}

	fields := strings.Split(out, "\x00")
	if err == nil {
		return fields[0], nil, nil
	}

	var files []string
	for _, f := range fields[1:] {
		if f == "" {
			break
		}

		if !slices.Contains(files, f) {
			files = append(files, f)
		}
	}

	return "", &Conflict{Files: files}, nil
}

// Changed lists the paths that differ between the commits from and to.
func (g *Git) Changed(ctx context.Context, from, to string) ([]string, error) {
	out, err := g.in(ctx, g.repo, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", from, to, "--")
	return paths(out), err
}

// MergeBase is the newest commit that both a and b descend from.
func (g *Git) MergeBase(ctx context.Context, a, b string) (string, error) {
	return g.line(ctx, g.repo, "merge-base", a, b)
}

// Show reads the file at path in the commit at, and false when the commit holds no file there.
func (g *Git) Show(ctx context.Context, at, path string) ([]byte, bool, error) {
	listed, err := g.in(ctx, g.repo, "--literal-pathspecs", "ls-tree", "-z", "--full-tree", at+"^{commit}", "--", path)
	if err != nil || listed == "" {
		return nil, false, err
	}

	about, _, _ := strings.Cut(listed, "\t")
	if fields := strings.Fields(about); len(fields) != 3 || fields[1] != "blob" {
		return nil, false, nil
	}

	content, err := g.in(ctx, g.repo, "show", at+":"+path)
	return []byte(content), err == nil, err
}

func paths(out string) []string {
	var found []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			found = append(found, p)
		}
	}

	return found
}
