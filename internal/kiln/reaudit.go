package kiln

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// reaudits answers whether the work at now changed a path kiln.json's reaudit names otherwise than at read. read is
// the work the audit read. It compares each one's own change since it left main. Main's changes, brought in by a
// rebase or a merge, never count. kiln.json is read at main's tip, so a build cannot loosen it.
func (r *Repo) reaudits(ctx context.Context, read, now git.Rev) (bool, error) {
	main, c, err := r.mainConfig(ctx)
	if err != nil || len(c.Reaudit) == 0 {
		return false, err
	}

	then, err := r.ownChange(ctx, main, read, c.Reaudit)
	if err != nil {
		return false, err
	}

	change, err := r.ownChange(ctx, main, now, c.Reaudit)
	return change != then, err
}

// ownChange is the work at tip's own change to paths, since it left main: the diff from their merge base. It drops
// what names the commits or the lines, so the same change reads the same on any base.
func (r *Repo) ownChange(ctx context.Context, main, tip git.Rev, paths []string) (string, error) {
	base, err := r.git.MergeBase(ctx, main, tip)
	if err != nil {
		return "", err
	}

	return r.changeOf(ctx, base, tip, paths)
}

// changeOf is the change to paths from base to tip, as ownChange reads it.
func (r *Repo) changeOf(ctx context.Context, base, tip git.Rev, paths []string) (string, error) {
	diff, err := r.git.Diff(ctx, base, tip, paths...)
	var kept []string
	for line := range strings.Lines(diff) {
		if !strings.HasPrefix(line, "index ") && !strings.HasPrefix(line, "@@") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, ""), err
}

// changedSince lists, sorted, each file whose own change, as ownChange reads it, differs between the works at from
// and to. Main's changes never count. It lists none when from is to, or from is "".
func (r *Repo) changedSince(ctx context.Context, from, to git.Rev) ([]string, error) {
	if from == "" || from == to {
		return nil, nil
	}

	main, err := r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	then, err := r.ownChanges(ctx, main, from)
	if err != nil {
		return nil, err
	}

	now, err := r.ownChanges(ctx, main, to)
	var changed []string
	for p := range maps.Keys(then) {
		if then[p] != now[p] {
			changed = append(changed, p)
		}
	}

	for p := range maps.Keys(now) {
		if _, found := then[p]; !found {
			changed = append(changed, p)
		}
	}

	slices.Sort(changed)
	return changed, err
}

// ownChanges is the work at tip's own change since it left main, by file, as ownChange reads it.
func (r *Repo) ownChanges(ctx context.Context, main, tip git.Rev) (map[string]string, error) {
	base, err := r.git.MergeBase(ctx, main, tip)
	if err != nil {
		return nil, err
	}

	files, err := r.git.Changed(ctx, base, tip)
	changes := make(map[string]string, len(files))
	for _, f := range files {
		if err != nil {
			break
		}

		changes[f], err = r.changeOf(ctx, base, tip, []string{f})
	}

	return changes, err
}
