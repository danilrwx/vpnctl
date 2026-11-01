package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// pushedState builds a repository with a bare origin and one pushed commit.
func pushedState(t *testing.T) (string, *gitsync.Repo) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := gitsync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AddRemote(bare); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "hosts.yaml"), "hosts: {}\n")
	if err := repo.Commit("init: state"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}
	return dir, repo
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGuardsPushedState(t *testing.T) {
	dir, _ := pushedState(t)
	st := &state.State{Dir: dir}
	if err := Guards(st, &Opts{}); err != nil {
		t.Fatalf("a clean pushed state was rejected: %v", err)
	}
}

func TestGuardsUnpushedState(t *testing.T) {
	dir, repo := pushedState(t)
	write(t, filepath.Join(dir, "users.yaml"), "users: {}\n")
	if err := repo.Commit("users: probe"); err != nil {
		t.Fatal(err)
	}
	st := &state.State{Dir: dir}
	err := Guards(st, &Opts{})
	if err == nil {
		t.Fatal("an unpushed commit was accepted")
	}
	if !strings.Contains(err.Error(), "is not pushed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := Guards(st, &Opts{Unpushed: true}); err != nil {
		t.Fatalf("--unpushed was rejected: %v", err)
	}
}

func TestGuardsDirtyTree(t *testing.T) {
	dir, _ := pushedState(t)
	write(t, filepath.Join(dir, "hosts.yaml"), "hosts: {}\nextra: 1\n")
	st := &state.State{Dir: dir}
	if err := Guards(st, &Opts{}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("a dirty worktree was accepted: %v", err)
	}
}

func TestCheckAncestors(t *testing.T) {
	dir, repo := pushedState(t)
	a, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "users.yaml"), "users: {}\n")
	if err := repo.Commit("users: probe"); err != nil {
		t.Fatal(err)
	}
	b, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	// the host runs an older commit that is in this history: fine
	if err := checkAncestors(repo, b, []plan.HostPlan{{Name: "h", Marker: a}}, []string{"h"}); err != nil {
		t.Fatalf("an ancestor marker was rejected: %v", err)
	}
	// the host runs a newer commit than this state: refuse, another device is ahead
	if err := checkAncestors(repo, a, []plan.HostPlan{{Name: "h", Marker: b}}, []string{"h"}); err == nil {
		t.Fatal("a marker ahead of HEAD was accepted")
	}
	// an unpushed marker cannot be checked and must not block
	if err := checkAncestors(repo, a, []plan.HostPlan{{Name: "h", Marker: b, Unpushed: true}}, []string{"h"}); err != nil {
		t.Fatalf("an unpushed marker blocked apply: %v", err)
	}
	// a host that is not being applied is ignored
	if err := checkAncestors(repo, a, []plan.HostPlan{{Name: "other", Marker: b}}, []string{"h"}); err != nil {
		t.Fatalf("an unrelated host blocked apply: %v", err)
	}
}
