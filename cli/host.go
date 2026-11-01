package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/danilrwx/vpnctl-go/bootstrap"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// HostBootstrap renders the setup script of a host and runs it over SSH.
// With printOnly the script goes to stdout and the host is not touched.
func HostBootstrap(dir, name, ssh string, printOnly bool, out io.Writer) error {
	if name == "" {
		return fmt.Errorf("the host name is required")
	}
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	if printOnly {
		script, err := bootstrap.Script(st, name)
		if err != nil {
			return err
		}
		fmt.Fprint(out, script)
		return nil
	}
	return bootstrap.Run(st, name, ssh, out)
}
