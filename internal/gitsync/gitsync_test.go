package gitsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// TestPushFetchAncestry walks the life of a state repository the way the
// guards use it: a commit, a push, a second clone that fetches, and the
// questions apply asks before it touches a host.
func TestPushFetchAncestry(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	// main, not master: a bare repo whose HEAD names a branch nobody pushed
	// makes every clone fail with "reference not found"
	if _, err := git.PlainInitWithOptions(bare, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(root, "first")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Init(first)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.AddRemote(bare); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(first, "hosts.yaml"), []byte("hosts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state: a host"); err != nil {
		t.Fatal(err)
	}

	head1, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(root, "second")
	if _, err := git.PlainClone(second, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatal(err)
	}

	repo2, err := Open(second)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo2.Fetch(); err != nil {
		t.Fatal(err)
	}

	origin, err := repo2.OriginMain()
	if err != nil {
		t.Fatal(err)
	}

	if origin != head1 {
		t.Fatalf("origin/main %s, want %s", origin, head1)
	}

	// the second clone moves ahead: the guard asks whether the host is behind
	if err := os.WriteFile(filepath.Join(second, "users.yaml"), []byte("users: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo2.Commit("state: a user"); err != nil {
		t.Fatal(err)
	}

	head2, err := repo2.Head()
	if err != nil {
		t.Fatal(err)
	}

	if ok, err := repo2.IsAncestor(head1, head2); err != nil || !ok {
		t.Fatalf("head1 must be an ancestor of head2: %v %v", ok, err)
	}

	if ok, err := repo2.IsAncestor(head2, head1); err != nil || ok {
		t.Fatalf("head2 must not be an ancestor of head1: %v %v", ok, err)
	}

	// FilesAt is what drift renders the old commit from
	files, err := repo2.FilesAt(head1)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := files["hosts.yaml"]; !ok {
		t.Fatalf("FilesAt at the first commit: %v", files)
	}

	clean, err := repo2.Clean()
	if err != nil || !clean {
		t.Fatalf("a committed worktree must be clean: %v %v", clean, err)
	}
}

// TestCloneWithAuth: a phone has no key file, so the clone takes an auth method
// from the Keystore signer. The shape is the same as `git clone` on a laptop.
func TestCloneWithAuth(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")

	if _, err := git.PlainInitWithOptions(bare, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "src")

	r, err := Init(src)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.AddRemote(bare); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(src, "hosts.yaml"), []byte("hosts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.Commit("state: a host"); err != nil {
		t.Fatal(err)
	}

	if err := r.Push(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "dst")

	cloned, err := Clone(bare, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dst, "hosts.yaml")); err != nil {
		t.Fatalf("the clone is empty: %v", err)
	}

	if _, err := cloned.Head(); err != nil {
		t.Fatalf("head: %v", err)
	}
}

// TestFastForwardMovesAStaleClone: a phone that only pushed stayed on the state
// it cloned. Pull has to move the worktree, not just the remote-tracking ref.
func TestFastForwardMovesAStaleClone(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")

	if _, err := git.PlainInitWithOptions(bare, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(root, "first")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Init(first)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.AddRemote(bare); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(first, "hosts.yaml"), []byte("hosts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state: one"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(root, "second")
	if _, err := git.PlainClone(second, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatal(err)
	}

	repo2, err := Open(second)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(first, "hosts.yaml"), []byte("hosts:\n  ru-msk-1: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state: two"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	if err := repo2.Fetch(); err != nil {
		t.Fatal(err)
	}

	head, err := repo2.FastForward()
	if err != nil {
		t.Fatal(err)
	}

	want, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	if head != want {
		t.Fatalf("fast-forwarded to %s, want %s", head, want)
	}

	body, err := os.ReadFile(filepath.Join(second, "hosts.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "ru-msk-1") {
		t.Fatalf("the worktree did not move: %q", body)
	}
}

// TestFastForwardRefusesADivergedState: a merge is not something a phone can
// resolve, so it is refused instead of guessed.
func TestFastForwardRefusesADivergedState(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")

	if _, err := git.PlainInitWithOptions(bare, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(root, "first")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Init(first)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.AddRemote(bare); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(first, "hosts.yaml"), []byte("hosts: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state: one"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(root, "second")
	if _, err := git.PlainClone(second, false, &git.CloneOptions{URL: bare}); err != nil {
		t.Fatal(err)
	}

	repo2, err := Open(second)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(second, "users.yaml"), []byte("users: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo2.Commit("state: local"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(first, "users.yaml"), []byte("users:\n  a: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state: remote"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	if err := repo2.Fetch(); err != nil {
		t.Fatal(err)
	}

	if _, err := repo2.FastForward(); err == nil {
		t.Fatal("a diverged state was fast-forwarded")
	}
}
