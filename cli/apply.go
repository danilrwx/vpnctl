package cli

import (
	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/state"
)

// Apply installs the state on the hosts.
func Apply(dir, host, branch string, force bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	return apply.Apply(st, &apply.Opts{Host: host, Branch: branch, Force: force})
}
