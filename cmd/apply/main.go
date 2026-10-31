package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/state"
)

// apply installs the state on the hosts: apply -dir <state> [-host name] [-branch main] [-force]
func main() {
	dir := flag.String("dir", ".", "state directory")
	host := flag.String("host", "", "only this host")
	branch := flag.String("branch", "", "apply from a branch other than main")
	force := flag.Bool("force", false, "do not stop on a dead link or a missing certificate")
	flag.Parse()

	st, err := state.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := apply.Apply(st, &apply.Opts{Host: *host, Branch: *branch, Force: *force}); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
