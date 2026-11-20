// Package git is kiln's git: checkouts for builds, the commits a build lands as, and the one move of main.
//
//	g, err := git.New(repo, os.Environ())
//	err = g.Checkout(ctx, tree, "kiln/x", "main")      // a branch made from main, in its own worktree
//	_, err = g.Commit(ctx, tree, "a session's work")    // whatever the session left uncommitted
//	onto, err := g.Tip(ctx, "main")
//	landed, conflict, err := g.Rebase(ctx, tree, onto, "feat(x): land the approved spec")
//	// conflict != nil: git could not merge the branch onto onto alone, and nothing changed
//	// ... gate landed ...
//	way, err := g.Move(ctx, "main", landed, onto)
//	// way.Tip:   main moved meanwhile, as another landing moves it
//	// way.Busy:  a rebase, a bisect or another git is busy with main, in that worktree
//	// way.Files: the developer's files that moving main would overwrite
//	err = g.Remove(ctx, tree, "kiln/x")
//
// A conflict and a refused move are answers, not errors. An error names the git command that failed and what it
// printed. A branch is named by its short name, as refs/heads/ holds it. A relative path is the main checkout's.
//
// Every call runs with hooks, signature checks, autostash, update-refs, fsmonitor, submodule recursion and background
// maintenance off. A merge may make a merge commit, and a clone names its remote origin. A file one side added in a
// directory the other side renamed is a conflict. kiln passes that config in the environment, so neither the
// developer's config nor the repository's changes it. A commit's author comes from config or the environment. With
// neither, it fails rather than guess one from the host. A commit is signed as the developer's config says. A signing
// key that would ask for its passphrase fails the call at once, and the error says so. No call opens an editor,
// prompts for a password, or takes an optional lock. Every call runs in the C locale, so git's words are never
// translated. Each runs in a process group of its own, which the terminal's Ctrl-C does not
// reach. When ctx ends, the call's git is killed. [Git.Move]'s fast-forward alone runs to its end, so the checkout is
// never left half-updated.
//
// kiln needs git 2.55 or later, whose hook.<event>.enabled turns off a hook that config defines. kiln asks xcrun once
// for the git behind /usr/bin/git, since that shim near doubles each call's time.
//
// A call that adds or removes a worktree waits for every other such call, in this process or another kiln's. Meanwhile
// it holds the lock of <git common dir>/kiln/worktrees.lock, since each such call writes the shared config.
//
// A worktree or clone kiln makes holds each symbolic link as a plain file that holds its target. Claude Code's sandbox
// refuses a shell's write through a link under .claude/. The main checkout keeps its links, and so does each commit.
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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/fwilkerson/kiln/internal/proc"
)

// pinned overrides, for every call, what config could otherwise make git do. It could run a hook or a monitor. It
// could stash the developer's changes, move other branches in a rebase, or start work in the background. It could
// move a file one side added into a directory the other side renamed. It could refuse kiln's unsigned commits, or a
// merge that is not a fast-forward. It could name a clone's remote other than origin, or print a signature's check
// where kiln reads an author. Without an identity, it could guess one from the account and host, where git commit
// should refuse.
var pinned = [][2]string{
	{"core.hooksPath", "/dev/null"},
	{"core.fsmonitor", "false"},
	{"merge.autoStash", "false"},
	{"rebase.autoStash", "false"},
	{"rebase.updateRefs", "false"},
	{"submodule.recurse", "false"},
	{"gc.auto", "0"},
	{"maintenance.auto", "false"},
	{"merge.verifySignatures", "false"},
	{"merge.ff", "true"},
	{"merge.directoryRenames", "conflict"},
	{"clone.defaultRemoteName", "origin"},
	{"log.showSignature", "false"},
	{"user.useConfigOnly", "true"},
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

	// ssh-keygen asks the program in SSH_ASKPASS for a passphrase, never the terminal, and false refuses at once.
	return append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(pairs)), "SSH_ASKPASS=/usr/bin/false",
		"SSH_ASKPASS_REQUIRE=force")
}()

// Rev names a commit by its hash, or by anything else git rev-parse takes, such as a branch.
type Rev string

// Worktree is the directory of one of the repository's checkouts: its main one, or a linked worktree. A relative one
// is the main checkout's.
type Worktree string

// Branch is a branch's short name, as refs/heads/ holds it.
type Branch string

// commit names the commit itself, which a ref to a tag would not.
func (r Rev) commit() string {
	return string(r) + "^{commit}"
}

func (b Branch) ref() string {
	return "refs/heads/" + string(b)
}

// Git runs git on one repository, from its main checkout.
type Git struct {
	repo      Worktree
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

	return &Git{repo: Worktree(repo), found: bin, env: environment(env)}, nil
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
	switch {
	case err != nil, !filepath.IsAbs(found), found == shim, !proc.Executable(found):
		return bin
	}

	return found
}

// environment pins LC_ALL to C, since kiln reads git's words, such as --shortstat's counts. A git built with gettext
// would print them in the developer's language.
func environment(base []string) []string {
	return append(proc.Git.Env(base), "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

type failed struct {
	Args   []string
	Exit   int
	Stderr string
}

func (f *failed) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(f.Args, " "), f.Exit, strings.TrimSpace(f.Stderr))
}

// unsigned is err, from a git that could not sign the commit it made.
func unsigned(err error) error {
	return fmt.Errorf("%w: git could not sign the commit, as the git config asks: load the signing key into "+
		"ssh-agent with ssh-add, or turn commit.gpgSign off", err)
}

type call struct {
	dir   string
	stdin string
	env   []string
}

// run's own process group keeps the terminal's Ctrl-C from reaching its git. When ctx ends, run kills that group.
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
			f := &failed{Args: args, Exit: exit.ExitCode(), Stderr: stderr.String()}
			// git commit and git merge print this when the commit's signing failed.
			if strings.Contains(f.Stderr, "failed to write commit object") {
				return stdout.String(), unsigned(f)
			}

			return stdout.String(), f
		}

		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

func (g *Git) in(ctx context.Context, dir Worktree, args ...string) (string, error) {
	return g.run(ctx, call{dir: string(g.at(dir))}, args...)
}

func (g *Git) line(ctx context.Context, dir Worktree, args ...string) (string, error) {
	out, err := g.in(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func (g *Git) at(path Worktree) Worktree {
	if filepath.IsAbs(string(path)) {
		return path
	}

	return Worktree(filepath.Join(string(g.repo), string(path)))
}

// CommonDir is the repository's git directory that every worktree shares.
func (g *Git) CommonDir(ctx context.Context) (string, error) {
	return g.line(ctx, g.repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// Checkout puts branch, made from the commit at, in a new worktree at path. If that fails part way, it removes what
// it made.
func (g *Git) Checkout(ctx context.Context, path Worktree, branch Branch, at Rev) error {
	unlock, err := g.lockWorktrees(ctx)
	if err != nil {
		return err
	}

	defer unlock()
	path = g.at(path)
	if _, err := g.in(ctx, g.repo, "worktree", "prune"); err != nil {
		return err
	}

	_, there := os.Lstat(string(path))
	err = g.addWorktree(ctx, path, "-b", string(branch), string(path), string(at))
	if err == nil {
		return nil
	}

	return errors.Join(err, g.unmade(ctx, path, there != nil, branch))
}

// worktreesLock is the file a call locks while it adds or removes a worktree. It is in kiln's directory under the git
// common directory.
const worktreesLock = "kiln/worktrees.lock"

// lockWorktrees holds g's worktrees mutex, and the file lock worktreesLock, until unlock. Each call that adds or
// removes a worktree writes the repository's shared config. git fails a write that finds the config locked. The
// mutex holds for one process, and the file lock for each kiln of the repository.
func (g *Git) lockWorktrees(ctx context.Context) (unlock func(), err error) {
	g.worktrees.Lock()
	f, err := g.flockWorktrees(ctx)
	if err != nil {
		g.worktrees.Unlock()
		return nil, err
	}

	return func() { f.Close(); g.worktrees.Unlock() }, nil
}

// flockWorktrees opens worktreesLock, and waits until it holds the file's lock.
func (g *Git) flockWorktrees(ctx context.Context) (*os.File, error) {
	common, err := g.CommonDir(ctx)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(common, worktreesLock)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

// worktree add makes the branch before the tree, and can fail between them.
func (g *Git) unmade(ctx context.Context, path Worktree, madePath bool, branch Branch) error {
	var err error
	if madePath {
		err = os.RemoveAll(string(path))
	}

	if _, pruned := g.in(ctx, g.repo, "worktree", "prune"); pruned != nil {
		return errors.Join(err, pruned)
	}

	if tip, found := g.Tip(ctx, branch); found == nil && tip != "" {
		_, deleted := g.in(ctx, g.repo, "branch", "--quiet", "-D", string(branch))
		err = errors.Join(err, deleted)
	}

	return err
}

// Detached puts commit in a worktree at path with no branch, making the worktree if it is not there yet. It removes
// every file the commit does not hold, ignored ones included, so what runs there sees only the commit.
func (g *Git) Detached(ctx context.Context, path Worktree, commit Rev) error {
	unlock, err := g.lockWorktrees(ctx)
	if err != nil {
		return err
	}

	defer unlock()
	path = g.at(path)
	if _, err := os.Stat(filepath.Join(string(path), ".git")); err != nil {
		if _, err := g.in(ctx, g.repo, "worktree", "prune"); err != nil {
			return err
		}

		return g.addWorktree(ctx, path, "--detach", string(path), string(commit))
	}

	checkout := []string{"checkout", "--quiet", "--force", "--detach", string(commit)}
	for _, args := range [][]string{checkout, {"clean", "-qfdx"}} {
		if _, err := g.in(ctx, path, args...); err != nil {
			return err
		}
	}

	return nil
}

// addWorktree runs git worktree add with args. It checks the worktree out with each symbolic link as a plain file
// that holds its target. Claude Code's sandbox refuses a shell's write to where a link under .claude/ points. With
// links, a session could not change what .claude/agents links to. The main checkout keeps its links.
func (g *Git) addWorktree(ctx context.Context, path Worktree, args ...string) error {
	if err := g.unshare(ctx); err != nil {
		return err
	}

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

// unshare moves core.bare and core.worktree to the main worktree's config.worktree, as git-worktree(1) says to before
// extensions.worktreeConfig. Left shared, core.bare = true makes git read every linked worktree as bare.
func (g *Git) unshare(ctx context.Context) error {
	common, err := g.CommonDir(ctx)
	if err != nil {
		return err
	}

	shared := filepath.Join(common, "config")
	for _, key := range []string{"core.bare", "core.worktree"} {
		value, err := g.line(ctx, g.repo, "config", "get", "--file", shared, key)
		if unset, ok := errors.AsType[*failed](err); ok && unset.Exit == 1 {
			continue
		}

		if err != nil {
			return err
		}

		if value == "false" {
			continue
		}

		main := filepath.Join(common, "config.worktree")
		if _, err := g.in(ctx, g.repo, "config", "set", "--file", main, key, value); err != nil {
			return err
		}

		if _, err := g.in(ctx, g.repo, "config", "unset", "--file", shared, key); err != nil {
			return err
		}
	}

	return nil
}

// Head is the commit checked out at checkout.
func (g *Git) Head(ctx context.Context, checkout Worktree) (Rev, error) {
	head, err := g.line(ctx, checkout, "rev-parse", "--verify", "HEAD^{commit}")
	return Rev(head), err
}

// Commit commits every change at checkout, untracked files included, and answers the commit at HEAD. With no change it
// commits nothing. A merge whose conflicts remain is refused as an [*Unmerged], and nothing changes. A merge whose
// conflicts were all resolved is committed as the merge.
func (g *Git) Commit(ctx context.Context, checkout Worktree, message string) (Rev, error) {
	out, err := g.in(ctx, checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return "", err
	}

	if files := unmerged(out); len(files) > 0 {
		return "", &Unmerged{Files: files}
	}

	if out != "" {
		if _, err := g.in(ctx, checkout, "add", "--all"); err != nil {
			return "", err
		}

		c := call{dir: string(g.at(checkout)), stdin: message}
		if _, err := g.run(ctx, c, "commit", "--quiet", "--no-verify", "--cleanup=verbatim", "-F", "-"); err != nil {
			return "", err
		}
	}

	return g.Head(ctx, checkout)
}

// Unmerged is a commit that [Git.Commit] refused, since a merge at its checkout still conflicts in Files.
type Unmerged struct {
	Files []string
}

func (u *Unmerged) Error() string {
	return "a merge is unfinished, with conflicts in " + strings.Join(u.Files, ", ")
}

// unmerged lists the paths that git status -z --porcelain=v1 --no-renames printed as still in conflict.
func unmerged(status string) []string {
	var files []string
	for _, p := range paths(status) {
		if slices.Contains([]string{"DD", "AU", "UD", "UA", "DU", "AA", "UU"}, p[:min(2, len(p))]) {
			files = append(files, p[min(3, len(p)):])
		}
	}

	return files
}

// Status lists each path at checkout that differs from the commit at HEAD, untracked files included.
func (g *Git) Status(ctx context.Context, checkout Worktree) ([]string, error) {
	out, err := g.in(ctx, checkout, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	var found []string
	for _, p := range paths(out) {
		found = append(found, p[min(3, len(p)):])
	}

	return found, err
}

// Reset puts the branch at checkout, and its files, back at commit, and removes every untracked file.
func (g *Git) Reset(ctx context.Context, checkout Worktree, commit Rev) error {
	for _, args := range [][]string{{"reset", "--quiet", "--hard", string(commit)}, {"clean", "-qfd"}} {
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
func (g *Git) Rebase(ctx context.Context, checkout Worktree, onto Rev, message string) (Rev, *Conflict, error) {
	head, err := g.Head(ctx, checkout)
	if err != nil {
		return "", nil, err
	}

	base, err := g.line(ctx, checkout, "merge-base", string(onto), string(head))
	if err != nil {
		return "", nil, err
	}

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	r := &rebase{checkout: checkout, base: Rev(base), onto: onto, head: head, message: message}
	tree, conflict, err := g.mergeTree(ctx, r)
	if err != nil || conflict != nil {
		return "", conflict, err
	}

	rebased, err := g.commitTree(ctx, r, tree)
	if err != nil {
		return "", nil, err
	}

	if _, err := g.in(ctx, checkout, "reset", "--quiet", "--keep", string(rebased)); err != nil {
		return "", nil, err
	}

	return rebased, nil, nil
}

// rebase is one Rebase: the commit it makes takes head's changes since base, on onto, with head's author.
type rebase struct {
	checkout Worktree
	base     Rev
	onto     Rev
	head     Rev
	message  string
}

func (g *Git) commitTree(ctx context.Context, r *rebase, tree string) (Rev, error) {
	by, err := g.in(ctx, r.checkout, "show", "--no-patch", "--date=raw", "--format=%an%x00%ae%x00%ad", r.head.commit())
	if err != nil {
		return "", err
	}

	name, rest, _ := strings.Cut(strings.TrimSuffix(by, "\n"), "\x00")
	email, date, _ := strings.Cut(rest, "\x00")
	args := []string{"commit-tree", tree, "-p", string(r.onto), "-F", "-"}
	// commit-tree ignores commit.gpgSign, which git commit and git merge read.
	sign, _ := g.line(ctx, r.checkout, "config", "--type=bool", "commit.gpgSign")
	if sign == "true" {
		args = append(args, "-S")
	}

	made, err := g.run(ctx, call{
		dir:   string(g.at(r.checkout)),
		stdin: r.message,
		env:   []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_AUTHOR_DATE=" + date},
	}, args...)
	if _, ok := errors.AsType[*failed](err); ok && sign == "true" {
		err = unsigned(err)
	}

	return Rev(strings.TrimSpace(made)), err
}

func (g *Git) mergeTree(ctx context.Context, r *rebase) (string, *Conflict, error) {
	onto := string(r.onto)
	args := []string{"--attr-source=" + onto, "merge-tree", "--write-tree", "-z", "--name-only", "--merge-base"}
	out, err := g.in(ctx, r.checkout, append(args, string(r.base), onto, string(r.head))...)
	if err != nil && !conflicted(err) {
		return "", nil, err
	}

	fields := strings.Split(out, "\x00")
	if err == nil {
		return fields[0], nil, nil
	}

	return "", &Conflict{Files: conflicts(fields[1:])}, nil
}

// conflicted says whether git merge-tree failed only because the merge conflicts, which it says by its exit code 1.
func conflicted(err error) bool {
	refused, ok := errors.AsType[*failed](err)
	return ok && refused.Exit == 1
}

// conflicts are the files merge-tree names after its tree, up to the empty field that ends them.
func conflicts(fields []string) []string {
	var files []string
	for _, f := range fields {
		if f == "" {
			break
		}

		if !slices.Contains(files, f) {
			files = append(files, f)
		}
	}

	return files
}

// Changed lists the paths that differ between the commits from and to.
func (g *Git) Changed(ctx context.Context, from, to Rev) ([]string, error) {
	diff := []string{"diff", "--name-only", "-z", "--no-renames", "--no-ext-diff"}
	out, err := g.in(ctx, g.repo, append(diff, string(from), string(to), "--")...)
	return paths(out), err
}

// Diff is the patch from the commit from to the commit to, of paths alone, with no line of context. A binary file's
// patch holds its content.
func (g *Git) Diff(ctx context.Context, from, to Rev, paths ...string) (string, error) {
	diff := []string{"--literal-pathspecs", "diff", "--no-renames", "--no-ext-diff", "--no-color", "--unified=0",
		"--binary"}
	return g.in(ctx, g.repo, append(append(diff, string(from), string(to), "--"), paths...)...)
}

// Shortstat is how much a diff changed, as git diff --shortstat counts it.
type Shortstat struct {
	Files, Insertions, Deletions int
}

var shortstat = regexp.MustCompile(`(\d+) (file|insertion|deletion)`)

// Shortstat counts what changed between the commits from and to.
func (g *Git) Shortstat(ctx context.Context, from, to Rev) (Shortstat, error) {
	diff := []string{"diff", "--shortstat", "--no-renames", "--no-ext-diff"}
	out, err := g.in(ctx, g.repo, append(diff, string(from), string(to), "--")...)
	var s Shortstat
	for _, m := range shortstat.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "file":
			s.Files = n
		case "insertion":
			s.Insertions = n
		case "deletion":
			s.Deletions = n
		}
	}

	return s, err
}

// EmptyTree is the ID of the tree that holds no file, in the repository's own hash. A diff from it names every file.
func (g *Git) EmptyTree(ctx context.Context) (Rev, error) {
	id, err := g.line(ctx, g.repo, "hash-object", "-t", "tree", "/dev/null")
	return Rev(id), err
}

// MergeBase is the newest commit that both a and b descend from.
func (g *Git) MergeBase(ctx context.Context, a, b Rev) (Rev, error) {
	base, err := g.line(ctx, g.repo, "merge-base", string(a), string(b))
	return Rev(base), err
}

// Files lists the names of the entries directly in dir, in the commit at. A commit with no dir holds none.
func (g *Git) Files(ctx context.Context, at Rev, dir string) ([]string, error) {
	out, err := g.in(ctx, g.repo, "--literal-pathspecs", "ls-tree", "-z", "--name-only", at.commit(), "--", dir+"/")
	found := paths(out)
	for i, p := range found {
		found[i] = strings.TrimPrefix(p, dir+"/")
	}

	return found, err
}

// Entry is a file or a directory that a commit holds.
type Entry struct {
	// Path is the entry's path from the directory Git.Tree lists.
	Path string
	// Dir is true of a directory.
	Dir bool
	// ID is git's ID of the entry's content. A directory's changes with any file under it.
	ID string
}

// Tree lists each file and directory under dir in the commit at, each directory before what it holds. A commit with
// no dir holds none.
func (g *Git) Tree(ctx context.Context, at Rev, dir string) ([]Entry, error) {
	out, err := g.in(ctx, g.repo, "--literal-pathspecs", "ls-tree", "-r", "-t", "-z", "--full-tree", at.commit(), "--",
		dir+"/")
	var entries []Entry
	for _, line := range paths(out) {
		about, p, _ := strings.Cut(line, "\t")
		fields := strings.Fields(about)
		// -t lists the directories above dir too, which hold no path under it.
		if rel, under := strings.CutPrefix(p, dir+"/"); under && len(fields) == 3 {
			entries = append(entries, Entry{Path: rel, Dir: fields[1] == "tree", ID: fields[2]})
		}
	}

	return entries, err
}

// Show reads the file at path in the commit at, and false when the commit holds no file there.
func (g *Git) Show(ctx context.Context, at Rev, path string) ([]byte, bool, error) {
	listed, err := g.in(ctx, g.repo, "--literal-pathspecs", "ls-tree", "-z", "--full-tree", at.commit(), "--", path)
	if err != nil || listed == "" {
		return nil, false, err
	}

	about, _, _ := strings.Cut(listed, "\t")
	if fields := strings.Fields(about); len(fields) != 3 || fields[1] != "blob" {
		return nil, false, nil
	}

	content, err := g.in(ctx, g.repo, "show", string(at)+":"+path)
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
