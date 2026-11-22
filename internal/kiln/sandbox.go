package kiln

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// sandboxContract tells every session where the sandbox lets its shell commands write.
//
//go:embed contract/sandbox.md
var sandboxContract string

// sandboxed adds to l what every session of a build holds to. No agent's file changes the denied commands and
// forbidden actions. What the session reaches is kiln.json's, at main's tip. The system prompt ends with
// sandboxContract.
func (b *build) sandboxed(ctx context.Context, l claude.Launch) (claude.Launch, error) {
	s, err := b.sides(ctx)
	if err != nil {
		return l, err
	}

	l.Deny = append([]string{"Bash(git push)", "Bash(git push *)", "Bash(git update-ref *)", "Bash(git branch -f *)",
		"Bash(git branch -D *)", "Bash(git checkout main)", "Bash(git switch main)", "Bash(git reset * main)"},
		l.Deny...)
	kept := b.kept(s, l.Role)
	l.System = strings.TrimSpace(l.System + "\n\n" + sandboxContract)
	l.Forbidden = []string{
		"Pushing to any remote, in any form.",
		"Writing, moving, resetting or deleting any ref but the branch checked out in the working directory, " +
			"and above all main.",
		kept,
		"Changing anything in a .claude directory, by a file tool or a shell command.",
	}
	return b.r.reached(ctx, l)
}

// kept is the forbidden action that keeps role's session off .kiln/, and off what is not its own.
func (b *build) kept(s sides, role string) string {
	switch {
	case b.fix != nil:
		return b.fix.kept(role)
	case s.split && role == buildRole:
		// A split build's amendments are the verify session's.
		return fmt.Sprintf("Changing anything under .kiln/, or under %s, by a file tool or a shell command.",
			strings.Join(s.acceptance, ", "))
	}

	return fmt.Sprintf("Changing anything under .kiln/ but %s, by a file tool or a shell command.",
		b.rec.Name.amendPath())
}

// reached adds to l what kiln.json at main's tip says a session reaches. Those are the hosts its commands reach, the
// paths beside its worktree they write, and the MCP servers it calls. Its commands may reach Unix sockets
// too. It adds how long the session may print nothing.
func (r *Repo) reached(ctx context.Context, l claude.Launch) (claude.Launch, error) {
	_, c, err := r.mainConfig(ctx)
	if err != nil {
		return l, err
	}

	for _, command := range c.WritablePathCommands {
		paths, err := r.printed(ctx, c.shell(r.env), command)
		if err != nil {
			return l, err
		}

		l.Writable = append(l.Writable, paths...)
	}

	l.Domains, l.UnixSockets, l.MCP, l.Silence = c.AllowedHosts, c.AllowUnixSockets, c.MCPServers, c.killAfter()
	return l, nil
}

// printed answers the paths command prints, a field each. It runs command once per kiln command, in the checkout
// kiln runs in, as s runs it. A command that fails, or prints a path that is not absolute, is an error.
func (r *Repo) printed(ctx context.Context, s projectShell, command string) ([]string, error) {
	r.writableMu.Lock()
	defer r.writableMu.Unlock()
	if paths, ok := r.writable[command]; ok {
		return paths, nil
	}

	out, err := s.run(ctx, git.Worktree(r.top), command)
	if err != nil {
		return nil, fmt.Errorf("the command %q of %s's writablePathCommands: %w", command, configFile, err)
	}

	paths := strings.Fields(out.stdout)
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("the command %q of %s's writablePathCommands printed %q: print only absolute paths",
				command, configFile, p)
		}
	}

	if r.writable == nil {
		r.writable = map[string][]string{}
	}

	r.writable[command] = paths
	return r.writable[command], nil
}
