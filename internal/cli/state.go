package cli

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
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
