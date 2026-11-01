package cli

import (
	"fmt"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// StateFmt rewrites every state file in its canonical form. With check it only
// reports: the rule is two spaces everywhere, and a file that breaks it must not
// sail through review as a formatting accident.
func StateFmt(dir string, check, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if check {
		diff, err := state.Differences(st)
		if err != nil {
			return err
		}
		type row struct {
			OK    bool     `json:"ok"`
			Files []string `json:"files"`
		}
		r := row{OK: len(diff) == 0, Files: diff}
		if r.Files == nil {
			r.Files = []string{}
		}
		if err := emit(r, func() {
			if len(diff) == 0 {
				fmt.Println("✓ every state file is canonical")
				return
			}
			for _, f := range diff {
				fmt.Printf("  ✗ %s\n", f)
			}
			fmt.Println("run vpnctl state fmt to fix")
		}); err != nil {
			return err
		}
		if len(diff) > 0 {
			return ErrSilent
		}
		return nil
	}
	changed, err := state.Format(st)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		if !jsonOut {
			fmt.Println("✓ every state file is canonical")
		}
		return emit(struct {
			Changed []string `json:"changed"`
		}{Changed: []string{}}, func() {})
	}
	msg := "state: two spaces everywhere"
	if !nocommit {
		repo, err := gitsync.Open(dir)
		if err != nil {
			return err
		}
		if err := repo.Commit(msg); err != nil {
			return err
		}
	}
	for _, f := range changed {
		fmt.Printf("  · %s\n", f)
	}
	fmt.Printf("✓ %s (%d files)\n", msg, len(changed))
	return nil
}

// stateProblems is what check adds to its list when a file breaks the rule.
func stateProblems(st *state.State) []string {
	diff, err := state.Differences(st)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, f := range diff {
		out = append(out, fmt.Sprintf("%s: not canonical — run vpnctl state fmt", f))
	}
	return out
}

// StateStatus is the one-screen answer to "where are we": the local head, what
// origin has, and which marker every host carries. It reads the hosts, so it
// needs the same access plan does.
func StateStatus(dir string, at time.Time) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if err := checkState(st); err != nil {
		return err
	}
	repo, err := gitsync.Open(dir)
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	branch, _ := repo.Branch()
	clean, _ := repo.Clean()
	origin, _ := repo.OriginMain()
	rendered, err := render.Check(st, at)
	if err != nil {
		return err
	}
	plans, err := plan.PlanWith(st, rendered, at, nil)
	if err != nil {
		return err
	}
	type hostRow struct {
		Host        string   `json:"host"`
		Marker      string   `json:"marker,omitempty"`
		Unreachable string   `json:"unreachable,omitempty"`
		Unpushed    bool     `json:"unpushed,omitempty"`
		Notes       []string `json:"notes,omitempty"`
	}
	type row struct {
		Branch string    `json:"branch"`
		Head   string    `json:"head"`
		Origin string    `json:"origin"`
		Dirty  bool      `json:"dirty"`
		Hosts  []hostRow `json:"hosts"`
	}
	r := row{Branch: branch, Head: head, Origin: origin, Dirty: !clean, Hosts: []hostRow{}}
	for _, p := range plans {
		r.Hosts = append(r.Hosts, hostRow{Host: p.Name, Marker: p.Marker, Unreachable: p.Unreachable, Unpushed: p.Unpushed, Notes: p.Notes})
	}
	return emit(r, func() {
		dirty := ""
		if r.Dirty {
			dirty = " (dirty worktree)"
		}
		fmt.Printf("branch %s · head %s · origin %s%s\n", r.Branch, r.Head, r.Origin, dirty)
		for _, h := range r.Hosts {
			switch {
			case h.Unreachable != "":
				fmt.Printf("  %-12s unreachable: %s\n", h.Host, h.Unreachable)
			case h.Marker == "":
				fmt.Printf("  %-12s never applied\n", h.Host)
			case h.Marker == r.Head:
				fmt.Printf("  %-12s %s · up to date\n", h.Host, h.Marker)
			default:
				fmt.Printf("  %-12s %s · behind head\n", h.Host, h.Marker)
			}
			if h.Unpushed {
				fmt.Printf("  %-12s applied from an unpushed commit\n", "")
			}
		}
	})
}
