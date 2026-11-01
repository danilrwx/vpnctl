// Package gitsync keeps the state directory in git: commit after every
// change, pull before writing, push on demand.
package gitsync

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Repo wraps an opened state repository.
type Repo struct {
	r *git.Repository
}

// Open opens an existing repository in dir.
func Open(dir string) (*Repo, error) {
	r, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	return &Repo{r: r}, nil
}

// Init creates a repository in dir when there is none yet.
func Init(dir string) (*Repo, error) {
	if r, err := git.PlainOpen(dir); err == nil {
		return &Repo{r: r}, nil
	}
	// main, not the go-git default master: apply refuses anything else
	r, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		return nil, fmt.Errorf("init repo: %w", err)
	}
	return &Repo{r: r}, nil
}

// AddRemote registers origin, replacing an existing one.
func (r *Repo) AddRemote(url string) error {
	if _, err := r.r.Remote("origin"); err == nil {
		if err := r.r.DeleteRemote("origin"); err != nil {
			return fmt.Errorf("delete remote: %w", err)
		}
	}
	if _, err := r.r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}}); err != nil {
		return fmt.Errorf("add remote: %w", err)
	}
	return nil
}

// Commit stages every change and commits it with msg.
func (r *Repo) Commit(msg string) error {
	w, err := r.r.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("stage: %w", err)
	}
	st, err := w.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if st.IsClean() {
		return nil
	}
	_, err = w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "vpnctl", Email: "vpnctl@localhost", When: time.Now()},
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Head returns the short hash of HEAD.
func (r *Repo) Head() (string, error) {
	h, err := r.r.Head()
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return h.Hash().String()[:7], nil
}

// Clean reports whether the worktree has no uncommitted changes.
func (r *Repo) Clean() (bool, error) {
	w, err := r.r.Worktree()
	if err != nil {
		return false, fmt.Errorf("worktree: %w", err)
	}
	st, err := w.Status()
	if err != nil {
		return false, fmt.Errorf("status: %w", err)
	}
	return st.IsClean(), nil
}

// Push sends the current branch to origin.
func (r *Repo) Push() error {
	if err := r.r.Push(&git.PushOptions{}); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

// Pull updates the worktree from origin.
func (r *Repo) Pull() error {
	w, err := r.r.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if err := w.Pull(&git.PullOptions{RemoteName: "origin"}); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("pull: %w", err)
	}
	return nil
}

// Branch is the current branch name.
func (r *Repo) Branch() (string, error) {
	h, err := r.r.Head()
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return h.Name().Short(), nil
}

// Tag marks HEAD: applied/<timestamp> records what the hosts run.
func (r *Repo) Tag(name string) error {
	h, err := r.r.Head()
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	if _, err := r.r.CreateTag(name, h.Hash(), nil); err != nil {
		return fmt.Errorf("tag %s: %w", name, err)
	}
	return nil
}

// FilesAt is the file tree of a commit: the state as it was, for drift detection.
func (r *Repo) FilesAt(rev string) (map[string][]byte, error) {
	hash, err := r.r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", rev, err)
	}
	commit, err := r.r.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", rev, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree %s: %w", rev, err)
	}
	files := map[string][]byte{}
	it := tree.Files()
	defer it.Close()
	err = it.ForEach(func(f *object.File) error {
		body, err := f.Contents()
		if err != nil {
			return err
		}
		files[f.Name] = []byte(body)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", rev, err)
	}
	return files, nil
}

// Fetch updates the remote-tracking refs from origin.
func (r *Repo) Fetch() error {
	if err := r.r.Fetch(&git.FetchOptions{RemoteName: "origin"}); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// OriginMain is the short hash of origin/main.
func (r *Repo) OriginMain() (string, error) {
	h, err := r.r.Reference(plumbing.NewRemoteReferenceName("origin", "main"), true)
	if err != nil {
		return "", fmt.Errorf("origin/main: %w", err)
	}
	return h.Hash().String()[:7], nil
}

// IsAncestor reports whether anc is reachable from desc: the host applied a
// state that this device still has in its history.
func (r *Repo) IsAncestor(anc, desc string) (bool, error) {
	a, err := r.commitOf(anc)
	if err != nil {
		return false, err
	}
	d, err := r.commitOf(desc)
	if err != nil {
		return false, err
	}
	return a.IsAncestor(d)
}

func (r *Repo) commitOf(rev string) (*object.Commit, error) {
	hash, err := r.r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", rev, err)
	}
	c, err := r.r.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", rev, err)
	}
	return c, nil
}

// SSHAuth is the credential for an ssh remote: the agent first, then ~/.ssh keys.
func SSHAuth() (transport.AuthMethod, error) {
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		if a, err := gitssh.NewSSHAgentAuth("git"); err == nil {
			return a, nil
		}
	}
	home, _ := os.UserHomeDir()
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if a, err := gitssh.NewPublicKeysFromFile("git", p, ""); err == nil {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no ssh key for the remote: no agent and no key in ~/.ssh")
}

// ListRemote lists the refs of a remote without cloning it.
func ListRemote(url string, auth transport.AuthMethod) ([]*plumbing.Reference, error) {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := rem.List(&git.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", url, err)
	}
	return refs, nil
}

// FetchAuth fetches with an explicit credential.
func (r *Repo) FetchAuth(auth transport.AuthMethod) error {
	if err := r.r.Fetch(&git.FetchOptions{RemoteName: "origin", Auth: auth}); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// PushRefs pushes the given refspecs, optionally forcing.
func (r *Repo) PushRefs(auth transport.AuthMethod, force bool, refs ...config.RefSpec) error {
	opts := &git.PushOptions{RemoteName: "origin", Auth: auth, Force: force}
	opts.RefSpecs = refs
	if err := r.r.Push(opts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}
