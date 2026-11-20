package kiln

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// land rebases the build's work onto main as one commit, and gates it. Then it moves main
// there, only if main has not moved since the rebase; if it has, land starts again. from is the commit to rebase,
// which the build's checkout holds, or "" to start from main's tip. Before the gate, retire may add a commit. A path
// in allowed may change, though guarded names it.
//
// It answers the commit main moved to. Or it answers what kept the work from landing, for the session. That is a
// conflict, work that changes nothing on main, or the gate. A landing that doesn't move main puts the branch back
// where it stood, even after an error or an interrupt. Should kiln die mid-landing, the record keeps that commit,
// and the next kiln fire puts the branch back. A landing that main holds has landed, whatever moving main answered.
// The record keeps it until the build records it, so the next kiln records it if this one can't.
func (b *build) land(ctx context.Context, from git.Rev, retire retiring, allowed ...string) (landed git.Rev,
	back setback, err error) {
	before := from
	if before == "" {
		if before, err = b.r.git.Head(ctx, b.tree); err != nil {
			return "", setback{}, err
		}
	}

	b.set(func(r *record) { r.Before = before })
	defer func() {
		if landed == "" {
			landed, err = b.heldAnyway(context.WithoutCancel(ctx), err)
		}

		if landed == "" {
			err = errors.Join(err, b.unland(context.WithoutCancel(ctx)))
		} else {
			b.set(func(r *record) { r.Before = "" })
		}
	}()

	for {
		landed, back, err = b.landOnce(ctx, from, retire, allowed)
		if !errors.Is(err, errMainMoved) {
			return landed, back, err
		}

		b.say("main moved while the gate ran; the work is rebased onto it again")
	}
}

// heldAnyway answers the record's landing, and no error, when main holds it though the move answered err. An
// interrupt can cut the move short once main has moved. Otherwise it answers err.
func (b *build) heldAnyway(ctx context.Context, err error) (git.Rev, error) {
	held, found := b.r.holdsLanding(ctx, b.now())
	switch {
	case found != nil:
		return "", errors.Join(err, found)
	case !held:
		return "", err
	}

	b.onto = b.now().Onto
	return b.now().Landing, nil
}

// errMainMoved is a landing that main's move refused, since main moved while the gate ran.
var errMainMoved = errors.New("main moved while the gate ran")

// landOnce tries the landing once, onto main's tip. It answers errMainMoved when main moved meanwhile. It holds
// commandsLock from the read of main's tip until main moved, or the try answers. So the other lane moves no main
// under its gate. A setback goes back to the session with the lock free.
func (b *build) landOnce(ctx context.Context, from git.Rev, retire retiring, allowed []string) (git.Rev, setback,
	error) {
	held, err := b.r.lockCommands(ctx, b.rec.Name, b.out)
	if err != nil {
		return "", setback{}, err
	}

	defer held.release()
	onto, err := b.r.mainTip(ctx)
	if err != nil {
		return "", setback{}, err
	}

	head, back, err := b.prepare(ctx, from, onto, retire)
	if err != nil || back.why != "" {
		return "", back, err
	}

	g := b.treeGuard(allowed...)
	g.head = head
	v, err := b.gated(ctx, held, nil, g)
	if err != nil || !v.passed {
		return "", setback{whyLanding, v.report, v.failing}, err
	}

	landed, back, err := b.move(ctx, head, onto)
	if landed != "" {
		b.onto = onto
	}

	return landed, back, err
}

// retiring retires a spec in its landing, on held, the commit that last holds it. It answers the commit that
// retires it.
type retiring func(ctx context.Context, held git.Rev) (git.Rev, error)

// prepare puts the work from from onto onto, and retires the spec if retire is set. It
// answers the commit that would land. Or it answers what kept the work from landing: a conflict, or no change.
//
// Each step answers the commit it leaves checked out. Only kiln writes the build's worktree while it lands, so none
// is read again.
func (b *build) prepare(ctx context.Context, from, onto git.Rev, retire retiring) (git.Rev, setback, error) {
	start := cmpOr(from, onto)
	if err := b.r.git.Reset(ctx, b.tree, start); err != nil {
		return "", setback{}, err
	}

	head, back, err := b.rebase(ctx, start, onto)
	if err != nil || back.why != "" {
		return "", back, err
	}

	if retire != nil {
		if head, err = retire(ctx, head); err != nil {
			return "", setback{}, err
		}
	}

	return b.changes(ctx, onto, head)
}

// rebase rebases the work at start onto onto, as the one commit that lands, unless start is onto already. It answers
// the commit checked out then.
func (b *build) rebase(ctx context.Context, start, onto git.Rev) (git.Rev, setback, error) {
	if start == onto {
		return start, setback{}, nil
	}

	rebased, conflict, err := b.r.git.Rebase(ctx, b.tree, onto, b.landing())
	if err != nil || conflict == nil {
		return rebased, setback{}, err
	}

	report := fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n"))
	return "", setback{whyConflict, report, conflict.Files}, nil
}

// changes answers head, unless it changes nothing since onto. A session that ended with no result may leave nothing,
// and main would "move" to the commit it holds.
func (b *build) changes(ctx context.Context, onto, head git.Rev) (git.Rev, setback, error) {
	changed, err := b.r.git.Changed(ctx, onto, head)
	switch {
	case err != nil:
		return "", setback{}, err
	case len(changed) == 0:
		return "", setback{why: whyUnchanged, report: unchanged}, nil
	}

	return head, setback{}, nil
}

// move moves main to head, only if main is still at onto. It answers errMainMoved if main moved.
func (b *build) move(ctx context.Context, head, onto git.Rev) (git.Rev, setback, error) {
	b.set(func(r *record) { r.Landing, r.Onto = head, onto })
	way, err := b.r.git.Move(ctx, mainBranch, head, onto)
	switch {
	case err != nil:
		return "", setback{}, err
	case way == nil:
		return head, setback{}, nil
	case way.Tip != "":
		return "", setback{}, errMainMoved
	}

	return "", setback{}, fmt.Errorf("main's checkout is in the way of the landing: busy in %q, or with changes to %s",
		way.Busy, strings.Join(way.Files, ", "))
}

// unland puts the branch back at the commit it stood at before its landing began, if a landing began.
func (b *build) unland(ctx context.Context) error {
	if b.rec.Before == "" {
		return nil
	}

	if err := b.r.git.Reset(ctx, b.tree, b.rec.Before); err != nil {
		return err
	}

	b.set(func(r *record) { r.Before, r.Landing, r.Onto = "", "", "" })
	return nil
}

// foldAmendments commits, at checkout, the spec with its amendments folded in, and the amendment file removed. It
// answers that commit.
func (b *build) foldAmendments(ctx context.Context, checkout git.Worktree) (git.Rev, error) {
	name := b.rec.Name
	head, err := b.r.git.Head(ctx, checkout)
	if err != nil {
		return "", err
	}

	spec, _, err := b.r.git.Show(ctx, head, name.file())
	if err != nil {
		return "", err
	}

	amendments, _, err := b.r.git.Show(ctx, head, name.amendPath())
	if err != nil {
		return "", err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot fold into %s: %w", errRefused, name.amendPath(), name.file(), err)
	}

	if err := os.WriteFile(filepath.Join(string(checkout), name.file()), []byte(folded), 0o644); err != nil {
		return "", err
	}

	if err := os.Remove(filepath.Join(string(checkout), name.amendPath())); err != nil {
		return "", err
	}

	return b.r.git.Commit(ctx, checkout, fmt.Sprintf("docs(%s): fold the amendments into the spec\n", name))
}

func (b *build) checkoutMain(ctx context.Context) error {
	main, err := b.r.mainTip(ctx)
	if err != nil {
		return err
	}

	return b.r.git.Checkout(ctx, b.tree, b.rec.Name.branch(), main)
}

// retire deletes the spec and its amendments, and adds its line to PRODUCT.md. The line holds the command that shows
// the spec at held, the commit that last held it. It answers the commit that retires the spec.
func (b *build) retire(ctx context.Context, held git.Rev) (git.Rev, error) {
	name := b.rec.Name
	title, err := b.title()
	if err != nil {
		return "", err
	}

	if err := b.addProduct(fmt.Sprintf("- %s: `git show %s:%s`\n", title, short(held), name.file())); err != nil {
		return "", err
	}

	for _, p := range []string{name.file(), name.amendPath()} {
		if err := os.Remove(b.path(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	return b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): retire the accepted spec\n", name))
}

// title is the spec's title, from its first line, or its name if that line is no title.
func (b *build) title() (string, error) {
	spec, err := os.ReadFile(b.path(b.rec.Name.file()))
	if err != nil {
		return "", err
	}

	line, _, _ := strings.Cut(string(spec), "\n")
	if title, ok := strings.CutPrefix(line, "# "); ok {
		return title, nil
	}

	return string(b.rec.Name), nil
}

// addProduct adds line to PRODUCT.md, which it starts if the worktree holds none.
func (b *build) addProduct(line string) error {
	productPath := b.path(product)
	text, err := os.ReadFile(productPath)
	if errors.Is(err, fs.ErrNotExist) {
		text, err = []byte(productHead), nil
	}

	if err != nil {
		return err
	}

	return os.WriteFile(productPath, append(text, line...), 0o644)
}

const productHead = "# Product\n\nEach spec the developer accepted, and the command that shows it in the commit " +
	"that last held it.\n\n"
