package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// VersionsPin downloads the pinned releases and records their sha256.
func VersionsPin(dir, only string, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	log, err := ops.VersionsPin(st, only)
	for _, l := range log {
		fmt.Println(" ", l)
	}

	if err != nil {
		return err
	}

	if err := state.SaveVersions(dir, st.Versions); err != nil {
		return err
	}

	if nocommit {
		return nil
	}

	repo, err := gitsync.Open(dir)
	if err != nil {
		return err
	}

	msg := "versions: pin sha256"
	if only != "" {
		msg = "versions: pin sha256 for " + only
	}

	return repo.Commit(msg)
}

// VersionsCheck compares the pins with the newest upstream releases. Patches
// are pulled in automatically; a minor or a major needs a human, so with
// --breaking it makes the command fail and a scheduled job notices.
func VersionsCheck(dir string, breaking bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	updates, cerr := ops.Check(st)

	type row struct {
		Updates  []ops.Update `json:"updates"`
		Breaking []string     `json:"breaking"`
	}

	r := row{Updates: updates, Breaking: []string{}}
	if r.Updates == nil {
		r.Updates = []ops.Update{}
	}

	for _, u := range updates {
		if u.Kind != "patch" {
			r.Breaking = append(r.Breaking, u.Name)
		}
	}

	err = emit(r, func() {
		if len(updates) == 0 {
			fmt.Println("✓ every pin is the newest release")
			return
		}

		for _, u := range updates {
			note := "pull it: vpnctl versions auto"
			if u.Kind != "patch" {
				note = "needs a human"
			}

			fmt.Printf("  %s %s → %s (%s, %s)\n", u.Name, u.Pinned, u.Latest, u.Kind, note)
		}
	})
	if err != nil {
		return err
	}

	if cerr != nil {
		return cerr
	}

	if breaking && len(r.Breaking) > 0 {
		return ErrSilent
	}

	return nil
}

// VersionsAuto pulls in the patch updates: bump the pin, fetch and record the
// sha256, commit and push. With apply it also configures the hosts; without it
// the operator runs plan and apply, which is what a laptop that is being
// watched for a breaking change usually wants.
func VersionsAuto(dir string, apply, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}

	updates, err := ops.Check(st)
	if err != nil {
		return err
	}

	patches := ops.Patches(updates)
	for _, u := range updates {
		if u.Kind != "patch" {
			fmt.Printf("  %s %s → %s (%s): needs a human\n", u.Name, u.Pinned, u.Latest, u.Kind)
		}
	}

	if len(patches) == 0 {
		fmt.Println("✓ no patch updates: nothing to pull")
		return nil
	}

	var done []string

	for _, u := range patches {
		p := st.Versions[u.Name]
		p.Version = u.Latest
		st.Versions[u.Name] = p

		log, err := ops.VersionsPin(st, u.Name)
		for _, l := range log {
			fmt.Println(" ", l)
		}

		if err != nil {
			return err
		}

		done = append(done, u.Name+" "+u.Pinned+" → "+u.Latest)
	}

	if err := state.SaveVersions(dir, st.Versions); err != nil {
		return err
	}

	if nocommit {
		fmt.Println("(not committed)")
		return nil
	}

	repo, err := gitsync.Open(dir)
	if err != nil {
		return err
	}

	msg := "versions: " + strings.Join(done, ", ")
	if err := repo.Commit(msg); err != nil {
		return err
	}

	fmt.Printf("✓ %s\n", msg)

	if err := repo.Push(); err != nil {
		fmt.Printf("  ! push failed, apply is skipped: %v\n", err)
		return nil
	}

	if !apply {
		fmt.Println("  run vpnctl plan and vpnctl apply to put it on the hosts")
		return nil
	}

	return Apply(dir, "", "", false, false, false, time.Now().UTC())
}
