package git_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestShow holds what Show answers for each entry a commit may hold at a path. A commit git lacks is an error, never
// a missing file.
func TestShow(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	sh(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	sh(t, repo, "mkdir dir && echo in >dir/f && ln -s dir/f link && printf 'a\\nb' >'with space'")
	sh(t, repo, "git add -A && git commit --quiet -m first")
	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		at   git.Rev
		path string
		want string
	}{
		{"a file", "main", "dir/f", "\"in\\n\" true, no error"},
		{"a link, as its target", "main", "link", "\"dir/f\" true, no error"},
		{"a name with a space", "main", "with space", "\"a\\nb\" true, no error"},
		{"a directory", "main", "dir", "\"\" false, no error"},
		{"a path the commit lacks", "main", "none", "\"\" false, no error"},
		{"a commit git lacks", git.Rev(strings.Repeat("1", 40)), "dir/f", "\"\" false, an error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, found, err := g.Show(t.Context(), tc.at, tc.path)
			failed := map[bool]string{false: "no error", true: "an error"}[err != nil]
			if got := fmt.Sprintf("%q %v, %s", content, found, failed); got != tc.want {
				t.Errorf("Show(%s, %q) answered %s (%v); want %s", tc.at, tc.path, got, err, tc.want)
			}
		})
	}
}
