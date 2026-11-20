// Package hometest runs a package's tests in a HOME of their own, which holds no config:
//
//	func TestMain(m *testing.M) { hometest.Main(m) }
//
// The developer's git config then changes no git that a test runs. That config may sign each commit with a key
// behind an agent, at the cost of a call to ssh-keygen. kiln's git never sees GIT_CONFIG_GLOBAL, so HOME and
// XDG_CONFIG_HOME keep the config from it. kiln's cache moves under that HOME too. A test that needs a HOME of its
// own still sets one.
package hometest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Main runs m's tests with HOME set to an empty directory, XDG_CONFIG_HOME under it, and GIT_CONFIG_NOSYSTEM set.
// It removes the directory once the tests end, and exits with their code.
func Main(m *testing.M) {
	home, err := os.MkdirTemp("", "hometest-")
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
