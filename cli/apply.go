package cli

import (
	"flag"

	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/state"
)

// Apply installs the state on the hosts.
func Apply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	dir := fs.String("dir", ".", "state directory")
	host := fs.String("host", "", "only this host")
	branch := fs.String("branch", "", "apply from a branch other than main")
	force := fs.Bool("force", false, "do not stop on a dead link or a missing certificate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := state.Load(*dir)
	if err != nil {
		return err
	}
	return apply.Apply(st, &apply.Opts{Host: *host, Branch: *branch, Force: *force})
}
