// Package hometest gives tests a HOME of their own, which holds only the git config that tests need:
//
//	func TestMain(m *testing.M) { hometest.Main(m) }
//
// The developer's global git config then changes no git that a test runs. That config may sign each commit with a key
// behind an agent, at the cost of a call to ssh-keygen. kiln's git never sees GIT_CONFIG_GLOBAL, so HOME and
// XDG_CONFIG_HOME keep the config from it. kiln's cache moves under that HOME too. A test that needs a HOME of its
// own still sets one. GIT_CONFIG_NOSYSTEM keeps the system's git config from the test's own git. kiln's git drops
// that variable, as proc.Git says, so kiln's git still reads the system's config.
//
// The config turns off git's automatic maintenance, which a commit would otherwise start in the background. Background
// maintenance prunes worktrees and repacks while the test works in the same repository.
//
// A test binary that git rebase --exec or a hook starts inherits GIT_DIR. Each git the test runs then works on that
// repository. [DropGitVars] drops GIT_DIR, with every other GIT_* variable that kiln's own git drops.
package hometest

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/proc"
)

// Main runs m's tests without the variables [DropGitVars] drops, in a HOME that Seed filled. XDG_CONFIG_HOME lies
// under it, and GIT_CONFIG_NOSYSTEM is set. It removes the HOME once the tests end, and exits with their code.
func Main(m *testing.M) {
	DropGitVars()
	home, err := os.MkdirTemp("", "hometest-")
	if err == nil {
		err = Seed(home)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// DropGitVars removes from the process's environment each variable that kiln's own git never sees, as [proc.Git]
// says. Those are the GIT_* variables but the four that name who commits. GIT_DIR, GIT_WORK_TREE or GIT_INDEX_FILE
// would aim a test's git at another repository.
func DropGitVars() {
	kept := proc.Git.Env(os.Environ())
	for _, kv := range os.Environ() {
		if !slices.Contains(kept, kv) {
			name, _, _ := strings.Cut(kv, "=")
			os.Unsetenv(name)
		}
	}
}

// Seed writes the git config of a test's HOME into home, a directory that exists.
func Seed(home string) error {
	return os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitConfig), 0o644)
}

// gitConfig turns off automatic maintenance, which a commit would start in the background.
const gitConfig = "[maintenance]\n\tauto = false\n"
