// vpnctl is the single entry point: the state is a git directory, the hosts are
// reached over SSH, and every subcommand is a pure function of the state.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/cli"
)

const usage = `vpnctl — VPN infrastructure as a git state

usage: vpnctl <command> [flags]

  check    validate the state statically
  render   write the config tree of the state
  plan     what will change on the hosts: diff, drift, unreachable
  apply    apply the state on the hosts, leaves first
  user     add, remove, list and share users

Common flags: -dir <state directory> (default: .)
Run "vpnctl <command> -h" for the flags of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "check", "validate":
		err = cli.Check(args)
	case "render":
		err = cli.Render(args)
	case "plan":
		err = cli.Plan(args)
	case "apply":
		err = cli.Apply(args)
	case "user":
		err = cli.User(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if !errors.Is(err, cli.ErrSilent) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}
