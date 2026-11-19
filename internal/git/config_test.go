package git_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestConfig holds the package doc's promise: the developer's git config changes no call, but for signing its commits.
func TestConfig(t *testing.T) {
	for name, tc := range map[string]struct {
		repoConfig, globalConfig string
		call                     func(t *testing.T, s scratch) error
	}{
		"merge.verifySignatures moves main to an unsigned commit": {
			repoConfig: "git config merge.verifySignatures true",
			call:       moveUnsigned,
		},
		"a global merge.ff=only merges main into a clone": {
			globalConfig: "[merge]\n\tff = only\n",
			call:         mergeDiverged,
		},
		"a global clone.defaultRemoteName clones": {
			globalConfig: "[clone]\n\tdefaultRemoteName = upstream\n",
			call: func(t *testing.T, s scratch) error {
				return s.git.Clone(t.Context(), git.Worktree(filepath.Join(t.TempDir(), "clone")), "main", "x")
			},
		},
		"merge.directoryRenames=true still conflicts on a file added in a renamed directory": {
			repoConfig: "git config merge.directoryRenames true",
			call:       mergeIntoRenamed,
		},
		"log.showSignature keeps a signed commit's author": {
			repoConfig: signingKey("") + " && git config log.showSignature true",
			call:       landSigned,
		},
		"commit.gpgSign signs the landing": {
			repoConfig: signingKey("") + " && git config commit.gpgSign true",
			call: func(t *testing.T, s scratch) error {
				landed, err := land(t, s)
				if err != nil {
					return err
				}

				if signed := sh(t, s.repo, "git log -1 --format=%G? "+string(landed)); signed != "G" {
					return fmt.Errorf("the landing's signature check is %q; want G", signed)
				}

				return nil
			},
		},
		"a signing key behind a passphrase fails the landing at once": {
			repoConfig: signingKey("secret") + " && git config commit.gpgSign true",
			call: func(t *testing.T, s scratch) error {
				_, err := land(t, s)
				if err == nil || !strings.Contains(err.Error(), "could not sign") {
					return fmt.Errorf("the landing's error is %v; want git could not sign", err)
				}

				return nil
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := makeScratch(t, tc.repoConfig, tc.globalConfig)
			if err := tc.call(t, s); err != nil {
				t.Error(err)
			}
		})
	}
}

func moveUnsigned(t *testing.T, s scratch) error {
	first := git.Rev(sh(t, s.repo, "git rev-parse HEAD"))
	sh(t, s.repo, "git commit --quiet --allow-empty -m landed")
	landed := git.Rev(sh(t, s.repo, "git rev-parse HEAD"))
	sh(t, s.repo, "git reset --quiet --hard "+string(first))
	way, err := s.git.Move(t.Context(), "main", landed, first)
	if way != nil {
		return fmt.Errorf("refused: %+v", way)
	}

	return err
}

// mergeDiverged merges main into a clone whose branch and main each have a commit the other lacks.
func mergeDiverged(t *testing.T, s scratch) error {
	clone := filepath.Join(t.TempDir(), "clone")
	if err := s.git.Clone(t.Context(), git.Worktree(clone), "main", "x"); err != nil {
		return err
	}

	sh(t, clone, "echo x >x && git add x && git commit --quiet -m x")
	sh(t, s.repo, "echo m >m && git add m && git commit --quiet -m m")
	if err := s.git.Pull(t.Context(), git.Worktree(clone), "main"); err != nil {
		return err
	}

	conflict, err := s.git.Merge(t.Context(), git.Worktree(clone), "main", "merge main")
	if conflict != nil {
		return fmt.Errorf("conflict: %+v", conflict)
	}

	return err
}

// mergeIntoRenamed merges main into a branch that moved todo/ to done/, after main added todo/b. Neither side's
// commit says where todo/b belongs, so the merge conflicts, and changes nothing.
func mergeIntoRenamed(t *testing.T, s scratch) error {
	sh(t, s.repo, "mkdir todo && echo a >todo/a && git add todo && git commit --quiet -m a")
	tree := filepath.Join(t.TempDir(), "tree")
	if err := s.git.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		return err
	}

	sh(t, tree, "mkdir done && git mv todo/a done/a && git commit --quiet -m move")
	sh(t, s.repo, "echo b >todo/b && git add todo && git commit --quiet -m b")
	conflict, err := s.git.Merge(t.Context(), git.Worktree(tree), "main", "merge main")
	if err != nil || conflict == nil {
		return fmt.Errorf("the merge answered %+v, %v; want a conflict", conflict, err)
	}

	for _, p := range []string{"todo/b", "done/b"} {
		if _, err := os.Stat(filepath.Join(tree, p)); err == nil {
			return fmt.Errorf("the conflicting merge left %s", p)
		}
	}

	return nil
}

// landSigned rebases a commit signed with the repository's key, and checks the landing's author.
func landSigned(t *testing.T, s scratch) error {
	tree := filepath.Join(t.TempDir(), "tree")
	if err := s.git.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		return err
	}

	sh(t, tree, "git commit --quiet --allow-empty -S -m signed")
	// A git hook's GIT_AUTHOR_NAME outranks user.name, so the signed commit names the author to keep.
	author := sh(t, tree, "git log -1 --no-show-signature --format=%an")
	onto := git.Rev(sh(t, s.repo, "git rev-parse main"))
	landed, _, err := s.git.Rebase(t.Context(), git.Worktree(tree), onto, "land")
	if err != nil {
		return err
	}

	if by := sh(t, s.repo, "git log -1 --no-show-signature --format=%an "+string(landed)); by != author {
		return fmt.Errorf("the landing's author is %q; want %q", by, author)
	}

	return nil
}

// signingKey is the shell command that makes an SSH key behind passphrase, and has the repository sign with it.
func signingKey(passphrase string) string {
	return "ssh-keygen -q -t ed25519 -N '" + passphrase + "' -C t -f key && echo \"t@t $(cat key.pub)\" >signers && " +
		"git config gpg.format ssh && git config user.signingKey \"$PWD/key.pub\" && " +
		"git config gpg.ssh.allowedSignersFile \"$PWD/signers\""
}

// land rebases an unsigned commit on kiln/x onto main, and answers the landing.
func land(t *testing.T, s scratch) (git.Rev, error) {
	tree := filepath.Join(t.TempDir(), "tree")
	if err := s.git.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		return "", err
	}

	sh(t, tree, "echo x >x && git add x && git commit --quiet --no-gpg-sign -m x")
	onto := git.Rev(sh(t, s.repo, "git rev-parse main"))
	landed, _, err := s.git.Rebase(t.Context(), git.Worktree(tree), onto, "land")
	return landed, err
}

// scratch is a repository with one commit on main, and a git whose HOME holds only the global config given.
type scratch struct {
	git  *git.Git
	repo string
}

func makeScratch(t *testing.T, repoConfig, globalConfig string) scratch {
	s := scratch{repo: t.TempDir()}
	sh(t, s.repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	sh(t, s.repo, "echo first >first && git add first && git commit --quiet -m first")
	if repoConfig != "" {
		sh(t, s.repo, repoConfig)
	}

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(globalConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	g, err := git.New(s.repo, append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config")))
	if err != nil {
		t.Fatal(err)
	}

	s.git = g
	return s
}

// TestCommitUnmerged holds that Commit never commits a merge whose conflicts remain, which would commit their markers.
// It commits a merge whose conflicts were all resolved, as the merge.
func TestCommitUnmerged(t *testing.T) {
	for name, tc := range map[string]struct {
		resolve string
		// refused names the files the refusal names, and parents counts the parents of the commit at HEAD after.
		refused []string
		parents int
	}{
		"a conflict left as git left it": {resolve: "true", refused: []string{"first"}, parents: 1},
		"a conflict resolved and added":  {resolve: "echo both >first && git add first", parents: 2},
	} {
		t.Run(name, func(t *testing.T) {
			s := makeScratch(t, "", "")
			tree := filepath.Join(t.TempDir(), "tree")
			if err := s.git.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
				t.Fatal(err)
			}

			sh(t, tree, "echo work >first && git commit --quiet -am work")
			sh(t, s.repo, "echo main >first && git commit --quiet -am main")
			sh(t, tree, "git merge --quiet main >/dev/null 2>&1 || true; "+tc.resolve)
			_, err := s.git.Commit(t.Context(), git.Worktree(tree), "the session's work")
			var refused []string
			if u, ok := errors.AsType[*git.Unmerged](err); ok {
				refused, err = u.Files, nil
			}

			parents := len(strings.Fields(sh(t, tree, "git log -1 --format=%P")))
			got := fmt.Sprintf("error %v, refused %q, %d parents", err, refused, parents)
			if want := fmt.Sprintf("error <nil>, refused %q, %d parents", tc.refused, tc.parents); got != want {
				t.Errorf("Commit answered %s; want %s", got, want)
			}
		})
	}
}
