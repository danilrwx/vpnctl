package cli

import (
	"errors"
	"io"
	"os"

	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/state"
)

// Apply installs the state on the hosts. Under --json the progress goes nowhere
// and the result is printed as JSON instead.
func Apply(dir, host, branch string, force, unpushed bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	var out io.Writer = os.Stdout
	if jsonOut {
		out = io.Discard
	}
	res, err := apply.Apply(st, &apply.Opts{Host: host, Branch: branch, Force: force, Unpushed: unpushed}, out)
	if err != nil {
		var ge *apply.GuardError
		if errors.As(err, &ge) {
			return Guard(err)
		}
		return err
	}
	if jsonOut {
		return emit(res, func() {})
	}
	return nil
}
