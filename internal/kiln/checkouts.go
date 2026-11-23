package kiln

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// clone is the clone in which role's session of a build of name works: the verify session's. A spec's name never
// starts with a dot, so it is no build's tree.
func (r *Repo) clone(name Spec, role string) git.Worktree {
	return git.Worktree(sideClone(r.tree(name), name, role))
}

// sideBranch is the branch role's session of a build of the spec works on, in its clone.
func (s Spec) sideBranch(role string) git.Branch { return git.Branch("kiln/" + string(s) + "." + role) }

// dropCheckouts removes every checkout and branch a build of name made. They are the build's, the audit's and the
// gate's worktrees, and the verify session's clone. The .main worktree stays, since every gate shares it.
func (r *Repo) dropCheckouts(ctx context.Context, name Spec) error {
	trees := []git.Worktree{r.tree(name), r.auditTree(name), r.gateTree(name), r.clone(name, verifyRole)}
	return r.git.Remove(ctx, trees, name.branch(), name.sideBranch(verifyRole))
}

// sideBySideStart removes a revised spec's earlier tests from kiln/<name>, before the code session's first run beside
// the verify session. Those tests are on main, and would mislead that run. It answers whether main held any.
func (b *build) sideBySideStart(ctx context.Context, acceptance []string) (bool, error) {
	if b.order != sideBySide {
		return false, nil
	}

	return b.removeOldTests(ctx, acceptance)
}

// removeOldTests commits the removal of what main holds under each of acceptance, on kiln/<name>. It makes one
// commit, and answers whether main held any.
func (b *build) removeOldTests(ctx context.Context, acceptance []string) (bool, error) {
	held := false
	for _, p := range acceptance {
		at := b.path(p)
		if _, err := os.Lstat(at); err != nil {
			continue
		}

		if err := os.RemoveAll(at); err != nil {
			return false, err
		}

		held = true
	}

	if !held {
		return false, nil
	}

	message := fmt.Sprintf("chore(%s): remove the old acceptance tests, which the verify session writes anew\n",
		b.rec.Name)
	_, err := b.r.git.Commit(ctx, b.tree, message)
	return true, err
}

// putBack puts each acceptance path back on kiln/<name>, in a commit of kiln's, before a join merges tested. It puts
// it as it stands at the merge base of the two. Then the verify session's changes there merge cleanly. Merged
// straight, a revised spec's removed tests conflict with each test the verify session changed. Each it left alone is
// deleted. Once put back, the work changes no acceptance path since that base, so it puts back nothing.
func (b *build) putBack(ctx context.Context, tested git.Rev) error {
	if !b.now().OldTests {
		return nil
	}

	tip, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return err
	}

	base, err := b.r.git.MergeBase(ctx, tip, tested)
	if err != nil {
		return err
	}

	changed, err := b.r.git.Changed(ctx, base, tip)
	removed := slices.DeleteFunc(changed, func(p string) bool { return !underAny(p, b.side.acceptance) })
	if err != nil || len(removed) == 0 {
		return err
	}

	if err := b.r.git.Restore(ctx, b.tree, base, removed...); err != nil {
		return err
	}

	message := fmt.Sprintf("chore(%s): put the old acceptance tests back, for the verify session's to merge onto\n",
		b.rec.Name)
	_, err = b.r.git.Commit(ctx, b.tree, message)
	return err
}

// amendedSpec is the spec as the commit at holds it, with any amendments there folded in. A fold the developer
// accepted then reads the same as the amendments it folded.
func (b *build) amendedSpec(ctx context.Context, at git.Rev) (string, error) {
	spec, _, err := b.r.git.Show(ctx, at, b.rec.Name.file())
	if err != nil {
		return "", err
	}

	amendments, found, err := b.r.git.Show(ctx, at, b.rec.Name.amendPath())
	if err != nil || !found {
		return string(spec), err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return string(spec) + "\n\n" + string(amendments), nil
	}

	return folded, nil
}

// blinds are what the session in own, a checkout of name's build, may not read. rival is where the session it must
// not see works. The sandbox denies a command the whole of kiln's checkouts, and allowRead lets own back in. It
// denies the verify session the repository's main checkout and its git directory too, which hold every branch. The
// code session works in a worktree, whose git directory is the repository's. The Read tool's rules deny each other
// checkout by name, and rival's transcripts.
func (r *Repo) blinds(name Spec, own, rival git.Worktree) (denyRead, allowRead, deny []string) {
	others := []string{string(rival), string(r.tree(name)), string(r.auditTree(name)), string(r.gateTree(name)),
		filepath.Join(r.trees, mainTree), r.top, r.common}
	denyRead = []string{r.trees}
	if own != r.tree(name) {
		denyRead = append(denyRead, r.top, r.common)
	}

	if config := r.claudeConfig(); config != "" {
		transcripts := filepath.Join(config, "projects", projectDir(string(rival)))
		others = append(others, transcripts)
		denyRead = append(denyRead, transcripts)
	}

	for _, p := range slices.DeleteFunc(others, func(p string) bool { return p == string(own) }) {
		deny = append(deny, "Read(/"+p+"/**)")
	}

	return denyRead, []string{string(own)}, deny
}

// claudeConfig is the folder where a session keeps its transcripts, read from the environment kiln gives it. It is
// CLAUDE_CONFIG_DIR, else .claude in the home folder, and empty when neither is known.
func (r *Repo) claudeConfig() string {
	if dir := envValue(r.env, "CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}

	home := envValue(r.env, "HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}

	if home == "" {
		return ""
	}

	return filepath.Join(home, ".claude")
}

var notNamed = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectDir names the directory, in the projects folder of [Repo.claudeConfig], of the transcripts of a session in
// dir.
func projectDir(dir string) string {
	return notNamed.ReplaceAllString(dir, "-")
}

// lastLine is what follows prefix on the reply's last line that starts with it.
func lastLine(text, prefix string) (string, bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range slices.Backward(lines) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSpace(rest), true
		}
	}

	return "", false
}

// harnessChanged lists each file of kiln.json's testHelpers that differs from main's with the proved commit's change
// applied. Those are each the code session changed. The audit reads each for a weakened check. It lists none where no
// proof stands.
func (b *build) harnessChanged(ctx context.Context) ([]string, error) {
	tested := b.now().Tested
	helpers := b.sideOf(codeSeat).helpers
	if tested == "" || len(helpers) == 0 {
		return nil, nil
	}

	g := b.treeGuard()
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return nil, err
	}

	g.head = head
	main, err := b.r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	changed, err := b.r.judgedPaths(ctx, g, main)
	if err != nil {
		return nil, err
	}

	changed = slices.DeleteFunc(changed, func(p string) bool { return !underAny(p, helpers) })
	proved, err := b.r.proved(ctx, g, main, changed)
	if err != nil {
		return nil, err
	}

	return b.r.differs(ctx, head, proved, changed)
}

// differs lists each of paths that head holds otherwise than proved does, as Repo.proved reads proved.
func (r *Repo) differs(ctx context.Context, head git.Rev, proved map[string][]byte, paths []string) ([]string,
	error) {
	var found []string
	for _, p := range paths {
		was, wasHeld := proved[p]
		is, held, err := r.git.Show(ctx, head, p)
		if err != nil {
			return nil, err
		}

		if held != wasHeld || string(is) != string(was) {
			found = append(found, p)
		}
	}

	return found, nil
}
