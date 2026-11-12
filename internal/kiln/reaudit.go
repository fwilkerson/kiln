package kiln

import (
	"context"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// reaudits answers whether the work at now changed a path kiln.json's reaudit names otherwise than at read. read is
// the work the audit read. It compares each one's own change since it left main. Main's changes, brought in by a
// rebase or a merge, never count. kiln.json is read at main's tip, so a build cannot loosen it.
func (r *Repo) reaudits(ctx context.Context, read, now git.Rev) (bool, error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return false, err
	}

	c, err := r.config(ctx, main)
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

	diff, err := r.git.Diff(ctx, base, tip, paths...)
	var kept []string
	for line := range strings.Lines(diff) {
		if !strings.HasPrefix(line, "index ") && !strings.HasPrefix(line, "@@") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, ""), err
}
