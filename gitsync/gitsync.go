// Package gitsync keeps the state directory in git: commit after every
// change, pull before writing, push on demand.
package gitsync

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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
	r, err := git.PlainInit(dir, false)
	if err != nil {
		return nil, fmt.Errorf("init repo: %w", err)
	}
	return &Repo{r: r}, nil
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
