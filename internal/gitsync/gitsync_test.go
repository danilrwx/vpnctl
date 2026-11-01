package gitsync

import (
	"os"
	"path/filepath"
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
