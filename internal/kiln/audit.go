package kiln

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

var tally = regexp.MustCompile(`found (\d+)\s*$`)

// audit runs the audit agent on the directories the build changed, and its acceptance tests. It answers the findings,
// or "" for none. An audit that names no finding count is all findings. An audit that ends with no result runs again,
// in a new session, up to auditTries times. Once the audit answers, the record's Audited names the commit it read. A
// build that changed nothing to read records no read.
func (b *build) audit(ctx context.Context) (string, error) {
	head, dirs, files, err := b.changedFiles(ctx)
	if err != nil || len(dirs) == 0 {
		return "", err
	}

	b.say("the audit reads %s", strings.Join(dirs, " "))
	about, subject := auditing, b.rec.Name.file()
	switch {
	case b.fix != nil && b.rec.Accepted:
		about, subject = auditingLanding, string(b.rec.Name)
	case b.fix != nil:
		about, subject = auditingFix, string(b.rec.Name)
	}

	prompt := fmt.Sprintf(about, "`"+strings.Join(dirs, "`, `")+"`", subject, strings.Join(files, ", "))

	harness, err := b.auditedHarness(ctx)
	if err != nil {
		return "", err
	}

	if len(harness) > 0 {
		prompt += fmt.Sprintf(auditingHarness, strings.Join(harness, ", "))
	}

	// The audit's own worktree keeps what it writes out of the build's, even when kiln stops mid-audit.
	l, err := b.agentLaunch(ctx, auditRole, claude.Launch{Prompt: prompt, Dir: string(b.r.auditTree(b.rec.Name))})
	if err != nil {
		return "", err
	}

	findings, err := b.auditTries(ctx, l, head)
	if err == nil {
		b.set(func(r *record) { r.Audited = head })
	}

	// recount reads the worktree of an audit kiln stopped, so it stays until then.
	if ctx.Err() == nil {
		err = errors.Join(err, b.r.git.Remove(ctx, []git.Worktree{git.Worktree(l.Dir)}))
	}

	return findings, err
}

// auditTries runs the audit l launches on head until it ends with a result, up to auditTries times.
func (b *build) auditTries(ctx context.Context, l claude.Launch, head git.Rev) (string, error) {
	for try := 1; ; try++ {
		findings, err := b.auditTry(ctx, l, head, try)
		if !errors.Is(err, errNoResult) {
			return findings, err
		}

		b.say("the audit ended with no result; it runs again")
	}
}

// errNoResult is an audit run that ended with no result, with tries left.
var errNoResult = errors.New("the audit ended with no result")

// auditTry runs the audit l launches, in a new session, on the build's commit head. It answers its findings, or
// errNoResult for a try that ended with no result before the last.
func (b *build) auditTry(ctx context.Context, l claude.Launch, head git.Rev, try int) (string, error) {
	if err := b.r.git.Detached(ctx, git.Worktree(l.Dir), head); err != nil {
		return "", err
	}

	b.auditing = head
	l.Session = claude.NewID()
	ended, err := b.talk(ctx, l, reason{Why: whyAudit, Try: try})
	switch {
	case err != nil:
		return "", err
	case !ended.Failed:
		return findingsOf(ended.Text), nil
	case try < auditTries:
		return "", errNoResult
	}

	return "", fmt.Errorf("the audit ended with no result %d times", auditTries)
}

// findingsOf is the findings an audit's text names, or "" for none. A text that names no finding count is all
// findings.
func findingsOf(text string) string {
	text = strings.TrimSpace(text)
	if m := tally.FindStringSubmatch(text); m != nil && m[1] == "0" {
		return ""
	}

	return text
}

// changedFiles answers the build's commit, and the files its work changed since it left main, outside .kiln/. In
// an accepted fix's landing, they are the files changed since kiln accept, as changedSince lists them. It answers
// their directories too, as changedDirs names them.
func (b *build) changedFiles(ctx context.Context) (head git.Rev, dirs, files []string, err error) {
	head, err = b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", nil, nil, err
	}

	changed, err := b.ownFiles(ctx, head)
	if err != nil {
		return "", nil, nil, err
	}

	files = slices.DeleteFunc(changed, func(p string) bool { return !outsideKiln(p) })
	dirs, err = b.changedDirs(ctx, head, files)
	return head, dirs, files, err
}

// ownFiles lists the files the work at head changed since it left main. An accepted fix's landing lists those changed
// since kiln accept.
func (b *build) ownFiles(ctx context.Context, head git.Rev) ([]string, error) {
	if b.fix != nil && b.rec.Accepted {
		return b.r.changedSince(ctx, b.rec.AcceptedAt, head)
	}

	main, err := b.r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	return b.r.git.Branched(ctx, main, head)
}

// changedDirs are the directories at head that hold paths, sorted, each once: . for the top, or the directory's path.
// A directory the work emptied gives way to the nearest one above it that head holds. A deletion is audited too.
// It names no file extension, so it serves a project in any language.
func (b *build) changedDirs(ctx context.Context, head git.Rev, paths []string) ([]string, error) {
	var dirs []string
	for _, p := range paths {
		dir, err := b.heldDir(ctx, head, path.Dir(p))
		if err != nil {
			return nil, err
		}

		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	slices.Sort(dirs)
	return dirs, nil
}

// heldDir is dir, or the nearest directory above it, that holds a file at head. The top always counts.
func (b *build) heldDir(ctx context.Context, head git.Rev, dir string) (string, error) {
	for ; dir != "."; dir = path.Dir(dir) {
		files, err := b.r.git.Files(ctx, head, dir)
		if err != nil || len(files) > 0 {
			return dir, err
		}
	}

	return ".", nil
}

const auditTries = 3

// auditedHarness lists each harness file the build session changed, for the audit. A fix has no split sides.
func (b *build) auditedHarness(ctx context.Context) ([]string, error) {
	if b.fix != nil {
		return nil, nil
	}

	return b.harnessChanged(ctx)
}
