// vpnctl is the single entry point; the command tree lives in internal/cli.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/internal/cli"
)

func main() {
	if err := cli.App().Run(context.Background(), os.Args); err != nil {
		cli.ReportError(err)
		if !errors.Is(err, cli.ErrSilent) && !cli.JSON() {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
