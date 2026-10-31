package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Check validates the state statically.
func Check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	dir := fs.String("dir", ".", "state directory")
	stub := fs.Bool("stub", false, "deterministic secret stubs instead of the age key")
	warn := fs.Bool("warnings", false, "print warnings too")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := state.Load(*dir)
	if err != nil {
		return err
	}
	if *stub {
		st.FillStubSecrets()
	}
	if *warn {
		for _, w := range validate.Warnings(st) {
			fmt.Println("  !", w)
		}
	}
	errs := validate.State(st)
	if len(errs) == 0 {
		fmt.Printf("✓ state is valid: %d hosts, %d links, %d users\n",
			len(st.Hosts), len(st.Links), len(st.Users))
		return nil
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  ✗", e)
	}
	fmt.Fprintf(os.Stderr, "Error: %d errors in the state\n", len(errs))
	return ErrSilent
}
