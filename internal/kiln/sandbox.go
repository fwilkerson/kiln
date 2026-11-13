package kiln

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/proc"
)

// sandboxed adds to l what every session of a build holds to. No agent's file changes the denied commands and
// forbidden actions. The hosts and writable paths are kiln.json's, at main's tip.
func (b *build) sandboxed(ctx context.Context, l claude.Launch) (claude.Launch, error) {
	s, err := b.sides(ctx)
	if err != nil {
		return l, err
	}

	l.Deny = append([]string{"Bash(git push)", "Bash(git push *)", "Bash(git update-ref *)", "Bash(git branch -f *)",
		"Bash(git branch -D *)", "Bash(git checkout main)", "Bash(git switch main)", "Bash(git reset * main)"},
		l.Deny...)
	kept := fmt.Sprintf("Changing anything under .kiln/ but %s, by a file tool or a shell command.",
		b.rec.Name.amendPath())
	// A split build's amendments are the verify session's.
	if s.split && l.Role == buildRole {
		kept = fmt.Sprintf("Changing anything under .kiln/, or under %s, by a file tool or a shell command.",
			s.acceptance)
	}

	l.Forbidden = []string{
		"Pushing to any remote, in any form.",
		"Writing, moving, resetting or deleting any ref but the branch checked out in the working directory, " +
			"and above all main.",
		kept,
		"Changing .claude/settings.json or .claude/settings.local.json.",
	}
	return b.r.reached(ctx, l)
}

// reached adds to l what kiln.json at main's tip says a session reaches. Those are the hosts its commands reach, and
// the paths beside its worktree they write.
func (r *Repo) reached(ctx context.Context, l claude.Launch) (claude.Launch, error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return l, err
	}

	c, err := r.config(ctx, main)
	if err != nil {
		return l, err
	}

	for _, command := range c.Writable {
		paths, err := r.printed(ctx, command)
		if err != nil {
			return l, err
		}

		l.Writable = append(l.Writable, paths...)
	}

	l.Domains = c.Hosts
	return l, nil
}

// printed answers the paths command prints, a field each. It runs command once per kiln command, in the checkout
// kiln runs in, with sh -c. A command that fails is an error.
func (r *Repo) printed(ctx context.Context, command string) ([]string, error) {
	r.writableMu.Lock()
	defer r.writableMu.Unlock()
	if paths, ok := r.writable[command]; ok {
		return paths, nil
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = r.top, proc.Shell.Env(r.env)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s's writable command %q: %w", configFile, command, err)
	}

	if r.writable == nil {
		r.writable = map[string][]string{}
	}

	r.writable[command] = strings.Fields(string(out))
	return r.writable[command], nil
}
