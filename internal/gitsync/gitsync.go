// Package gitsync keeps the state directory in git: commit after every
// change, pull before writing, push on demand.
package gitsync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	r   *git.Repository
	dir string
}

// Open opens an existing repository in dir.
func Open(dir string) (*Repo, error) {
	r, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	return &Repo{r: r, dir: dir}, nil
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

	return &Repo{r: r, dir: dir}, nil
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
	if err := r.r.Push(&git.PushOptions{}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
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

	if err := w.Pull(&git.PullOptions{RemoteName: "origin"}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
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

// Fetch updates the remote-tracking refs from origin. The system git is the
// fallback: go-git cannot always authenticate, for example against GitHub with
// an RSA key held by the agent, while git itself can.
// deviceAuth is what a phone supplies for git: no key file, no ssh agent.
var deviceAuth transport.AuthMethod

// SetDeviceAuth installs the auth used by Fetch, which the apply guards call.
func SetDeviceAuth(auth transport.AuthMethod) { deviceAuth = auth }

func (r *Repo) Fetch() error {
	opts := &git.FetchOptions{RemoteName: "origin"}
	if deviceAuth != nil {
		opts.Auth = deviceAuth
	}

	err := r.r.Fetch(opts)
	if err == nil || errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}

	if r.dir != "" {
		if cmd := exec.Command("git", "-C", r.dir, "fetch", "-q", "origin"); cmd.Run() == nil {
			return nil
		}
	}

	return fmt.Errorf("fetch: %w", err)
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
	if err := r.r.Fetch(&git.FetchOptions{RemoteName: "origin", Auth: auth}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("fetch: %w", err)
	}

	return nil
}

// PushRefs pushes the given refspecs, optionally forcing.
func (r *Repo) PushRefs(auth transport.AuthMethod, force bool, refs ...config.RefSpec) error {
	opts := &git.PushOptions{RemoteName: "origin", Auth: auth, Force: force}

	opts.RefSpecs = refs
	if err := r.r.Push(opts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push: %w", err)
	}

	return nil
}

// Clone brings the state down to a device that has nothing yet. auth may be nil
// for a local or already-trusted remote; a phone passes the Keystore signer's
// method, so no key file ever exists on disk.
func Clone(url, dir string, auth transport.AuthMethod) (*Repo, error) {
	opts := &git.CloneOptions{URL: url}
	if auth != nil {
		opts.Auth = auth
	}

	r, err := git.PlainClone(dir, false, opts)
	if err != nil {
		return nil, fmt.Errorf("clone %s: %w", url, err)
	}

	return &Repo{r: r, dir: dir}, nil
}

// Remotes lists what the repository knows, which is how a phone finds the URL
// it cloned from without a settings screen.
func (r *Repo) Remotes() ([]*git.Remote, error) {
	return r.r.Remotes()
}

// PushAuth pushes the current branch with an explicit auth method: a device
// has no key file, only a signer.
func (r *Repo) PushAuth(auth transport.AuthMethod) error {
	err := r.r.Push(&git.PushOptions{Auth: auth})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push: %w", err)
	}

	return nil
}
