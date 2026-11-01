package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/danilrwx/vpnctl-go/bootstrap"
	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// HostAdd creates a host with its inbound, keys and routing.
func HostAdd(dir string, o ops.HostOptions, nocommit bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	msg, err := ops.HostAdd(st, o)
	if err != nil {
		return err
	}
	if err := ops.HostFiles(st, o.Name); err != nil {
		return err
	}
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	if nocommit {
		fmt.Println("(not committed)")
	} else {
		repo, err := gitsync.Open(dir)
		if err != nil {
			return err
		}
		if err := repo.Commit(msg); err != nil {
			return err
		}
	}
	fmt.Printf("✓ %s\n", msg)
	return nil
}

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
