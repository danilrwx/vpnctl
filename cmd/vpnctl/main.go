// vpnctl is the single entry point: the state is a git directory, the hosts are
// reached over SSH, and every subcommand is a pure function of the state.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	ucli "github.com/urfave/cli/v3"

	"github.com/danilrwx/vpnctl-go/cli"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/state"
)

func dirFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "dir", Aliases: []string{"C"}, Value: ".", Usage: "state directory"}
}

func main() {
	app := &ucli.Command{
		Name:                  "vpnctl",
		Usage:                 "VPN infrastructure as a git state",
		EnableShellCompletion: true,
		Description: "Hosts, links, users and routing live in git. `plan` compares the render with\n" +
			"the hosts, `apply` installs it leaves first, `user config` hands out client profiles.",
		Commands: []*ucli.Command{
			{
				Name:    "check",
				Aliases: []string{"validate"},
				Usage:   "validate the state statically",
				Flags: []ucli.Flag{
					dirFlag(),
					&ucli.BoolFlag{Name: "stub", Usage: "secret stubs instead of the age key"},
					&ucli.BoolFlag{Name: "warnings", Usage: "print warnings too"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					return cli.Check(c.String("dir"), c.Bool("stub"), c.Bool("warnings"))
				},
			},
			{
				Name:  "render",
				Usage: "write the config tree of the state",
				Flags: []ucli.Flag{
					dirFlag(),
					&ucli.StringFlag{Name: "out", Usage: "output directory"},
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.BoolFlag{Name: "stub", Usage: "secret stubs instead of the age key"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					return cli.Render(c.String("dir"), c.String("out"), c.String("host"), c.Bool("stub"))
				},
			},
			{
				Name:          "plan",
				Usage:         "what will change on the hosts: diff, drift, unreachable",
				ShellComplete: completeHosts,
				Flags: []ucli.Flag{
					dirFlag(),
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.StringFlag{Name: "diff", Usage: "show a file diff: host:/path"},
					&ucli.StringFlag{Name: "adopt", Usage: "take a file from a host into overrides/: host:/path"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					return cli.Plan(c.String("dir"), c.String("host"), c.String("diff"), c.String("adopt"))
				},
			},
			{
				Name:          "apply",
				Usage:         "apply the state on the hosts, leaves first",
				ShellComplete: completeHosts,
				Flags: []ucli.Flag{
					dirFlag(),
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.StringFlag{Name: "branch", Usage: "apply from a branch other than main"},
					&ucli.BoolFlag{Name: "force", Usage: "do not stop on a dead link or a missing certificate"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					return cli.Apply(c.String("dir"), c.String("host"), c.String("branch"), c.Bool("force"))
				},
			},
			{
				Name:  "user",
				Usage: "create, remove, list and hand out users",
				Commands: []*ucli.Command{
					{
						Name:      "add",
						Usage:     "create a user with fresh secrets",
						ArgsUsage: "<name>",
						Flags: []ucli.Flag{
							dirFlag(),
							&ucli.StringFlag{Name: "modes", Value: "split", Usage: "split,full"},
							&ucli.StringFlag{Name: "protos", Value: "reality,ws", Usage: "reality,ws,hy2,awg"},
							&ucli.BoolFlag{Name: "tg", Usage: "the user gets the Telegram proxy"},
							&ucli.StringFlag{Name: "entry", Usage: "entry host when the state has several"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return cli.UserAdd(c.String("dir"), name,
								splitList(c.String("modes")), splitList(c.String("protos")),
								c.Bool("tg"), c.String("entry"), c.Bool("nocommit"))
						},
					},
					{
						Name:          "rm",
						Usage:         "remove a user and its secrets",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return cli.UserRm(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:  "list",
						Usage: "list users",
						Flags: []ucli.Flag{dirFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							return cli.UserList(c.String("dir"))
						},
					},
					{
						Name:          "link",
						Usage:         "subscription URL, direct links and the Telegram proxy",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags:         []ucli.Flag{dirFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return cli.UserLink(c.String("dir"), name)
						},
					},
					{
						Name:          "message",
						Aliases:       []string{"share"},
						Usage:         "a ready-to-forward message for the user",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(),
							&ucli.StringFlag{Name: "format", Value: "text", Usage: "text or markdown"},
							&ucli.StringFlag{Name: "mode", Usage: "split or full, default: every mode"},
							&ucli.StringFlag{Name: "lang", Usage: "ru or en, default: the system locale"},
							&ucli.BoolFlag{Name: "ip", Usage: "use the entry IP instead of the domain"},
							&ucli.BoolFlag{Name: "no-links", Usage: "omit the direct links block"},
							&ucli.BoolFlag{Name: "no-tg", Usage: "omit the Telegram proxy block"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return cli.UserMessage(cli.MessageOpts{
								Dir:     c.String("dir"),
								Name:    name,
								Format:  c.String("format"),
								Mode:    c.String("mode"),
								Lang:    c.String("lang"),
								IP:      c.Bool("ip"),
								NoLinks: c.Bool("no-links"),
								NoTG:    c.Bool("no-tg"),
							})
						},
					},
					{
						Name:          "config",
						Usage:         "client profile: the same bytes the subscription endpoint serves",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(),
							&ucli.StringFlag{Name: "mode", Usage: "split or full, default: all user modes"},
							&ucli.StringFlag{Name: "format", Value: "links", Usage: "links, sub, singbox or clash"},
							&ucli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to a file or a directory instead of stdout"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return cli.UserConfig(c.String("dir"), name, c.String("mode"), c.String("format"), c.String("out"))
						},
					},
				},
			},
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		if !errors.Is(err, cli.ErrSilent) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

// stateDir reads -dir for the shell completion, where flags may still be unparsed.
func stateDir(c *ucli.Command) string {
	if d := c.String("dir"); d != "" {
		return d
	}
	args := c.Args().Slice()
	for i, a := range args {
		if (a == "-dir" || a == "--dir" || a == "-C" || a == "--C") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return "."
}

// completeUsers suggests the user names of the state.
func completeUsers(_ context.Context, c *ucli.Command) {
	st, err := state.Load(stateDir(c))
	if err != nil {
		return
	}
	for _, n := range ops.SortedUsers(st) {
		fmt.Fprintln(c.Root().Writer, n)
	}
}

// completeHosts suggests the host names of the state.
func completeHosts(_ context.Context, c *ucli.Command) {
	st, err := state.Load(stateDir(c))
	if err != nil {
		return
	}
	for name := range st.Hosts {
		fmt.Fprintln(c.Root().Writer, name)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
