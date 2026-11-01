package cli

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/state"
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
