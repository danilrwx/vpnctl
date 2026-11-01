// vpnctl is the single entry point; the command tree lives in internal/cli.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	ucli "github.com/urfave/cli/v3"

	"github.com/danilrwx/vpnctl-go/internal/cli"
	"github.com/danilrwx/vpnctl-go/internal/version"
)

func main() {
	// --version answers before the command tree: a wrapper script asks this,
	// and the answer must not depend on a valid state directory
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("vpnctl", version.String())
		return
	}
	app := cli.App()
	// urfave exits 3 for an unknown command, which is our guard code: a typo
	// must not look like "pull the repository"
	if len(os.Args) > 1 && !known(app, os.Args[1]) {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		cli.PrintUsage(app, os.Stderr)
		os.Exit(cli.ExitUsage)
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		cli.ReportError(err)
		if !errors.Is(err, cli.ErrSilent) && !cli.JSON() {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}

// known reports whether the argument names a command or a flag. help,
// completion and version are built in by urfave or by us, not by the tree, and
// rejecting them would break `vpnctl completion zsh > completions/zsh`.
func known(app *ucli.Command, arg string) bool {
	if len(arg) > 0 && arg[0] == '-' {
		return true
	}
	switch arg {
	case "help", "completion", "version":
		return true
	}
	for _, c := range app.Commands {
		if c.HasName(arg) {
			return true
		}
	}
	return false
}
