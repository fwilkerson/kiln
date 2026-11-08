package kiln

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/guidance"
	"github.com/fwilkerson/kiln/internal/claude"
)

// unknownReset is how long a usage limit that names no reset pauses a build.
const unknownReset = 5 * time.Minute

// Run builds each queued spec, one at a time, until none is queued. A build that was building when kiln last stopped
// goes on first, in the same session. Run refuses while another kiln run builds in the repository.
func (r *Repo) Run(ctx context.Context, out io.Writer) error {
	unlock, err := r.store.lock(runLock, false)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return fmt.Errorf("%w: another kiln run is building in this repository", errRefused)
	}

	if err != nil {
		return err
	}

	defer unlock()

	err = r.store.update(func(b *book) error {
		for _, s := range b.Specs {
			if s.State == Building {
				s.State = Queued
			}
		}

		slices.SortStableFunc(b.Specs, func(x, y *record) int { return goesOn(y) - goesOn(x) })
		return nil
	})
	if err != nil {
		return err
	}

	for {
		var rec record
		var found bool
		err := r.store.update(func(b *book) error {
			rec, found = b.next()
			return nil
		})
		if err != nil || !found {
			return err
		}

		b := &build{r: r, rec: rec, tree: r.tree(rec.Name), out: out}
		err = b.run(ctx)
		if s, ok := errors.AsType[*stopped](err); ok {
			b.stop(s.why)
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if err != nil {
			b.stop(fmt.Sprintf("kiln could not go on with the build: %v", err))
		}
	}
}

func goesOn(r *record) int {
	if r.State == Queued && r.Session != "" {
		return 1
	}

	return 0
}

// build is one build of a spec, or the landing of an accepted one.
type build struct {
	r    *Repo
	rec  record
	tree string
	out  io.Writer
}

// stopped is a build that its gate failed past its last round.
type stopped struct{ why string }

func (s *stopped) Error() string { return s.why }

func (b *build) run(ctx context.Context) error {
	if b.rec.Session == "" {
		if err := b.start(ctx); err != nil {
			return err
		}
	} else if _, err := b.talk(ctx, b.launch(true, interrupted)); err != nil {
		return err
	}

	for {
		if _, err := b.r.git.Commit(ctx, b.tree, b.message("the session's work")); err != nil {
			return err
		}

		v, err := b.r.gate(ctx, b.tree, b.rec.Name)
		if err != nil {
			return err
		}

		if b.rec.FailingAtMain == nil && len(v.atMain) > 0 {
			b.set(func(r *record) { r.FailingAtMain = v.atMain })
		}

		next, err := b.after(ctx, v)
		if err != nil || next == "" {
			return err
		}

		if _, err := b.talk(ctx, b.launch(true, next)); err != nil {
			return err
		}
	}
}

// after takes the build on from a gate's verdict. It answers what to tell the session next, or "" once the build has
// landed, or waits on its branch.
func (b *build) after(ctx context.Context, v verdict) (string, error) {
	if !v.passed {
		return b.round(ctx, v.report)
	}

	if !b.rec.Audited {
		b.set(func(r *record) { r.Audited = true })
		findings, err := b.audit(ctx)
		if err != nil || findings != "" {
			return fmt.Sprintf(audited, findings), err
		}
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	if amendments, found, err := b.r.git.Show(ctx, head, amendPath(b.rec.Name)); err != nil || found {
		b.set(func(r *record) { r.State, r.Amendments = Amended, string(amendments) })
		b.say("waits on %s, with amendments for UAT", branchOf(b.rec.Name))
		return "", err
	}

	landed, report, err := b.land(ctx, head, nil)
	if err != nil || report != "" {
		if err == nil {
			return b.round(ctx, report)
		}

		return "", err
	}

	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return "", err
	}

	b.set(func(r *record) { r.State = Landed })
	b.say("landed on main at %s, and waits for UAT", short(landed))
	return "", nil
}

// round sends a failed gate's report back to the session, or stops the build past its last round.
func (b *build) round(ctx context.Context, report string) (string, error) {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", err
	}

	c, err := b.r.config(ctx, main)
	if err != nil {
		return "", err
	}

	if b.rec.Rounds >= c.Rounds {
		return "", &stopped{why: "the gate still failed after its last round:\n" + report}
	}

	b.set(func(r *record) { r.Rounds++ })
	b.say("the gate failed; it goes back to the session, round %d of %d", b.rec.Rounds, c.Rounds)
	return fmt.Sprintf(gateFailed, b.rec.Rounds, c.Rounds, report), nil
}

func (b *build) start(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	// A branch an earlier build of the spec left would hold work from a spec as it was approved before.
	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return err
	}

	if err := b.r.git.Checkout(ctx, b.tree, branchOf(b.rec.Name), main); err != nil {
		return err
	}

	b.set(func(r *record) { r.Session = newSession() })
	b.say("building on %s, in %s", branchOf(b.rec.Name), b.tree)
	prompt := fmt.Sprintf(starting, specPath(b.rec.Name), short(b.rec.Approved), branchOf(b.rec.Name))
	_, err = b.talk(ctx, b.launch(false, prompt))
	return err
}

func (b *build) launch(resume bool, prompt string) claude.Launch {
	name := b.rec.Name
	hook := fmt.Sprintf("%s hook --spec %s --place %s", shellQuote(b.r.exe), name, shellQuote(b.tree))
	return b.sandboxed(claude.Launch{
		Session: b.rec.Session,
		Resume:  resume,
		Role:    "build",
		System:  fmt.Sprintf(buildRole, name),
		Prompt:  prompt,
		Dir:     b.tree,
		Hook:    hook,
	})
}

// sandboxed adds to l what every session of a build holds to.
func (b *build) sandboxed(l claude.Launch) claude.Launch {
	l.Deny = []string{"Bash(git push)", "Bash(git push *)", "Bash(git update-ref *)", "Bash(git branch -f *)",
		"Bash(git branch -D *)", "Bash(git checkout main)", "Bash(git switch main)", "Bash(git reset * main)"}
	l.Forbidden = []string{
		"Pushing to any remote, in any form.",
		"Writing, moving, resetting or deleting any ref but the branch checked out in the working directory, " +
			"and above all main.",
		fmt.Sprintf("Changing anything under specs/ but %s, by a file tool or a shell command.", amendPath(b.rec.Name)),
		"Changing .claude/settings.json or .claude/settings.local.json.",
	}
	l.Writable = b.r.goCaches(context.Background())
	l.Domains = []string{"proxy.golang.org", "sum.golang.org", "storage.googleapis.com"}
	return l
}

// talk runs the session l launches, and answers how it ended. A usage limit pauses the build until it resets, and the
// same session resumes.
func (b *build) talk(ctx context.Context, l claude.Launch) (claude.Ended, error) {
	for {
		ended, err := b.r.claude.Run(ctx, l)
		if err != nil || ended.Limit == nil {
			return ended, err
		}

		until := ended.Limit.Resets
		if until.IsZero() {
			until = time.Now().Add(unknownReset)
		}

		b.set(func(r *record) { r.Paused = until })
		b.say("paused for a usage limit until %s", until.Local().Format(time.Kitchen))
		select {
		case <-ctx.Done():
			return ended, ctx.Err()
		case <-time.After(time.Until(until)):
		}

		b.set(func(r *record) { r.Paused = time.Time{} })
		l.Resume, l.Prompt = true, limitReset
	}
}

var tally = regexp.MustCompile(`found (\d+)\s*$`)

// audit runs the audit agent on the packages the build changed, and its acceptance tests. It answers the findings, or
// "" for none. An audit that names no finding count is all findings.
func (b *build) audit(ctx context.Context) (string, error) {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", err
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	base, err := b.r.git.MergeBase(ctx, main, head)
	if err != nil {
		return "", err
	}

	changed, err := b.r.git.Changed(ctx, base, head)
	if err != nil {
		return "", err
	}

	var packages []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !slices.Contains(packages, "./"+path.Dir(p)) {
			packages = append(packages, "./"+path.Dir(p))
		}
	}

	if len(packages) == 0 {
		return "", nil
	}

	b.say("the audit reads %s", strings.Join(packages, " "))
	l := b.sandboxed(claude.Launch{
		Session: newSession(),
		Role:    "audit",
		System:  guidance.Audit(),
		Prompt:  fmt.Sprintf(auditing, strings.Join(packages, " "), specPath(b.rec.Name), b.rec.Name),
		Dir:     b.tree,
		Tools:   []string{"Read", "Grep", "Glob"},
	})
	ended, err := b.talk(ctx, l)
	text := strings.TrimSpace(ended.Text)
	if m := tally.FindStringSubmatch(text); err != nil || m != nil && m[1] == "0" {
		return "", err
	}

	return text, nil
}

// land rebases the build's work onto main as one commit, copies its guidance in, and gates it. Then it moves main
// there, only if main has not moved since the rebase; if it has, land starts again. from is the commit to rebase,
// or "" to start from main's tip. Before the gate, retire may add a commit. A path in allowed may change, though
// guarded names it.
//
// It answers the commit main moved to. Or it answers what kept the work from landing, a conflict or the gate, for the
// session.
func (b *build) land(ctx context.Context, from string, retire func(ctx context.Context, held string) error,
	allowed ...string) (landed, report string, err error) {
	for {
		onto, err := b.r.git.Tip(ctx, mainBranch)
		if err != nil {
			return "", "", err
		}

		start := cmpOr(from, onto)
		if err := b.r.git.Reset(ctx, b.tree, start); err != nil {
			return "", "", err
		}

		if start != onto {
			_, conflict, err := b.r.git.Rebase(ctx, b.tree, onto, b.message("land the approved spec"))
			if err != nil {
				return "", "", err
			}

			if conflict != nil {
				return "", fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n")), nil
			}
		}

		held, err := b.copyGuidance(ctx)
		if err != nil {
			return "", "", err
		}

		if retire != nil {
			if err := retire(ctx, held); err != nil {
				return "", "", err
			}
		}

		v, err := b.r.gate(ctx, b.tree, b.rec.Name, allowed...)
		if err != nil || !v.passed {
			return "", v.report, err
		}

		head, err := b.r.git.Head(ctx, b.tree)
		if err != nil {
			return "", "", err
		}

		way, err := b.r.git.Move(ctx, mainBranch, head, onto)
		switch {
		case err != nil:
			return "", "", err
		case way == nil:
			return head, "", nil
		case way.Tip != "":
			b.say("main moved while the gate ran; the work is rebased onto it again")
			continue
		}

		return "", "", fmt.Errorf("main's checkout is in the way of the landing: busy in %q, or with changes to %s",
			way.Busy, strings.Join(way.Files, ", "))
	}
}

// copyGuidance copies guidance/agents/ and guidance/skills/ to .claude/, into the commit at HEAD. It answers that
// commit.
func (b *build) copyGuidance(ctx context.Context) (string, error) {
	for _, dir := range []string{"agents", "skills"} {
		from := filepath.Join(b.tree, "guidance", dir)
		err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			rel, err := filepath.Rel(from, p)
			if err != nil {
				return err
			}

			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}

			to := filepath.Join(b.tree, ".claude", dir, rel)
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}

			return os.WriteFile(to, raw, 0o644)
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	return b.r.git.Amend(ctx, b.tree)
}

// foldAmendments commits, on the spec's branch, the spec with its amendments folded in, and the amendment file
// removed. It answers that commit.
func (b *build) foldAmendments(ctx context.Context) (string, error) {
	name := b.rec.Name
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	spec, _, err := b.r.git.Show(ctx, head, specPath(name))
	if err != nil {
		return "", err
	}

	amendments, _, err := b.r.git.Show(ctx, head, amendPath(name))
	if err != nil {
		return "", err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot fold into %s: %w", errRefused, amendPath(name), specPath(name), err)
	}

	if err := os.WriteFile(filepath.Join(b.tree, specPath(name)), []byte(folded), 0o644); err != nil {
		return "", err
	}

	if err := os.Remove(filepath.Join(b.tree, amendPath(name))); err != nil {
		return "", err
	}

	return b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): fold the amendments into the spec\n", name))
}

func (b *build) checkoutMain(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return err
	}

	return b.r.git.Checkout(ctx, b.tree, branchOf(b.rec.Name), main)
}

// retire deletes the spec and its amendments, and adds its line to PRODUCT.md. The line names held, the commit that
// last held the spec.
func (b *build) retire(ctx context.Context, held string) error {
	name := b.rec.Name
	spec, err := os.ReadFile(filepath.Join(b.tree, specPath(name)))
	if err != nil {
		return err
	}

	title := name
	if line, _, _ := strings.Cut(string(spec), "\n"); strings.HasPrefix(line, "# ") {
		title = strings.TrimPrefix(line, "# ")
	}

	productPath := filepath.Join(b.tree, product)
	text, err := os.ReadFile(productPath)
	if errors.Is(err, fs.ErrNotExist) {
		text, err = []byte(productHead), nil
	}

	if err != nil {
		return err
	}

	line := fmt.Sprintf("- %s: %s, last held in %s.\n", title, specPath(name), short(held))
	if err := os.WriteFile(productPath, append(text, line...), 0o644); err != nil {
		return err
	}

	for _, p := range []string{specPath(name), amendPath(name)} {
		if err := os.Remove(filepath.Join(b.tree, p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	_, err = b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): retire the accepted spec\n", name))
	return err
}

const productHead = "# Product\n\nEach spec the developer accepted, and the commit that last held it.\n\n"

// set changes the build's record, and the copy the build keeps of it.
func (b *build) set(change func(*record)) {
	change(&b.rec)
	if err := b.r.store.set(b.rec.Name, change); err != nil {
		b.say("kiln could not record it: %v", err)
	}
}

// stop records the build as stopped, and why.
func (b *build) stop(why string) {
	b.set(func(r *record) { r.State, r.Why, r.Paused = Stopped, why, time.Time{} })
	b.say("stopped: %s", strings.SplitN(why, "\n", 2)[0])
}

func (b *build) say(format string, args ...any) {
	fmt.Fprintf(b.out, "%s: %s\n", b.rec.Name, fmt.Sprintf(format, args...))
}

func (b *build) message(what string) string {
	return fmt.Sprintf("feat(%s): %s\n\nkiln built %s, approved at %s, in session %s.\n", b.rec.Name, what,
		specPath(b.rec.Name), short(b.rec.Approved), b.rec.Session)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func jsonIndent(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	return append(raw, '\n'), err
}

//go:embed build.md
var buildRole string

const (
	starting = "Build %s, approved at %s. Your worktree is on its branch, %s, made from main. When the build " +
		"is done, end your turn: kiln commits what you leave, and gates it."
	gateFailed = "The gate failed, so it comes back to you: round %d of %d. Fix what it found, and end your " +
		"turn.\n\n%s"
	audited = "The audit read the packages you changed, and your acceptance tests' code beside their Given, " +
		"When, Then. Fix each finding, or say in your reply why it is wrong. Then end your turn.\n\n%s"
	auditing  = "Audit these packages: %s. The spec is %s, and its acceptance tests' code is in acceptance/%s/."
	conflicts = "Main moved, and your work conflicts with it in these files:\n%s\n\nMerge main into your " +
		"branch with git merge main, resolve each conflict, commit, and end your turn."
	limitReset  = "The usage limit has reset. Go on where you stopped."
	interrupted = "kiln stopped while you worked. Go on where you stopped."
)
