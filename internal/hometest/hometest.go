// Package hometest gives tests a HOME of their own, which holds only the git config that tests need:
//
//	func TestMain(m *testing.M) { hometest.Main(m) }
//
// The developer's git config then changes no git that a test runs. That config may sign each commit with a key
// behind an agent, at the cost of a call to ssh-keygen. kiln's git never sees GIT_CONFIG_GLOBAL, so HOME and
// XDG_CONFIG_HOME keep the config from it. kiln's cache moves under that HOME too. A test that needs a HOME of its
// own still sets one.
//
// The config turns off git's automatic maintenance. A commit would otherwise start it in the background. There it
// prunes worktrees and repacks while the test works in the same repository.
package hometest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Main runs m's tests with HOME set to a directory that Seed filled, XDG_CONFIG_HOME under it, and
// GIT_CONFIG_NOSYSTEM set. It removes the directory once the tests end, and exits with their code.
func Main(m *testing.M) {
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

// Seed writes the git config of a test's HOME into home, a directory that exists.
func Seed(home string) error {
	return os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitConfig), 0o644)
}

// gitConfig turns off automatic maintenance, which a commit would start in the background.
const gitConfig = "[maintenance]\n\tauto = false\n"
