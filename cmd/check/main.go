package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// check prints the state errors the way the Rust CLI does: check -dir <state> [-stub]
func main() {
	dir := flag.String("dir", ".", "state directory")
	stub := flag.Bool("stub", false, "deterministic secret stubs instead of the age key")
	warn := flag.Bool("warnings", false, "print warnings too")
	flag.Parse()
	st, err := state.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *stub {
		st.FillStubSecrets()
	}
	errs := validate.State(st)
	if *warn {
		for _, w := range validate.Warnings(st) {
			fmt.Println("  !", w)
		}
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		fmt.Fprintf(os.Stderr, "Error: %d errors in the state\n", len(errs))
		os.Exit(1)
	}
	fmt.Printf("✓ state is valid: %d hosts, %d links, %d users\n",
		len(st.Hosts), len(st.Links), len(st.Users))
}
