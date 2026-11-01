// Package cli is the command surface of vpnctl: the tree, the flags and the
// completion, next to the functions each command runs.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	ucli "github.com/urfave/cli/v3"

	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

func dirFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "dir", Aliases: []string{"C"}, Value: ".", Usage: "state directory"}
}

func jsonFlag() ucli.Flag {
	return &ucli.BoolFlag{Name: "json", Usage: "machine-readable output"}
}

func App() *ucli.Command {
	app := &ucli.Command{
		Name:                  "vpnctl",
		Usage:                 "VPN infrastructure as a git state",
		EnableShellCompletion: true,
		Description: "Hosts, links, users and routing live in git. `plan` compares the render with\n" +
			"the hosts, `apply` installs it leaves first, `user config` hands out client profiles.",
		Commands: []*ucli.Command{
			{
				Name:  "init",
				Usage: "create a state: skeleton, age.yaml and the first commit",
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.StringFlag{Name: "name", Usage: "device name in age.yaml, default: hostname"},
					&ucli.BoolFlag{Name: "backup", Usage: "create the offline backup identity; the passphrase is asked for"},
					&ucli.StringFlag{Name: "backup-pass", Usage: "passphrase for --backup, for scripts"},
					&ucli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write the backup blob here instead of printing it"},
					&ucli.StringFlag{Name: "remote", Usage: "push the new state here"},
					&ucli.BoolFlag{Name: "fresh", Usage: "overwrite a remote that already has history, keeping its tip as a backup branch"},
					&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					pass := c.String("backup-pass")
					if pass == "" && c.Bool("backup") {
						var err error
						if pass, err = passphrase("", true); err != nil {
							return err
						}
					}
					return Init(c.String("dir"), c.String("name"), pass,
						c.String("remote"), c.String("out"), c.Bool("fresh"), c.Bool("nocommit"))
				},
			},
			{
				Name:  "key",
				Usage: "recipients of secrets.age.yaml",
				Commands: []*ucli.Command{
					{
						Name:      "init",
						Usage:     "write age.yaml from the local identity",
						ArgsUsage: "<name>",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the device name is required")
							}
							return KeyInit(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:  "backup",
						Usage: "issue a new offline backup identity, shown once",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "pass", Usage: "passphrase that seals the blob"},
							&ucli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write the blob here instead of printing it"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return KeyBackup(c.String("dir"), c.String("pass"), c.String("out"), c.Bool("nocommit"))
						},
					},
					{
						Name:      "restore",
						Usage:     "restore this device's identity from a sealed backup file",
						ArgsUsage: "<file>",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "key", Usage: "where to write the identity, default: the usual lookup"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return KeyRestore(c.String("dir"), c.Args().First(), c.String("key"))
						},
					},
					{
						Name:      "add",
						Usage:     "add a device: re-encrypts the secrets for everyone",
						ArgsUsage: "<name> <age1…>",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().Get(0)
							key := c.Args().Get(1)
							if name == "" || key == "" {
								return errors.New("usage: key add <name> <age1…>")
							}
							return KeyAdd(c.String("dir"), name, key, c.Bool("nocommit"))
						},
					},
					{
						Name:      "rm",
						Usage:     "drop a device: re-encrypts the secrets for the rest",
						ArgsUsage: "<name>",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the device name is required")
							}
							return KeyRemove(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:  "list",
						Usage: "list recipients, * marks this device",
						Flags: []ucli.Flag{dirFlag(), jsonFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return KeyList(c.String("dir"))
						},
					},
				},
			},
			{
				Name:    "check",
				Aliases: []string{"validate"},
				Usage:   "validate the state statically",
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.BoolFlag{Name: "stub", Usage: "secret stubs instead of the age key"},
					&ucli.BoolFlag{Name: "warnings", Usage: "print warnings too"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					return Check(c.String("dir"), c.Bool("stub"), c.Bool("warnings"))
				},
			},
			{
				Name:  "render",
				Usage: "write the config tree of the state",
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.StringFlag{Name: "out", Usage: "output directory"},
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.BoolFlag{Name: "stub", Usage: "secret stubs instead of the age key"},
					atFlag(),
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					at, err := dayArg(c)
					if err != nil {
						return err
					}
					return Render(c.String("dir"), c.String("out"), c.String("host"), c.Bool("stub"), at)
				},
			},
			{
				Name:          "plan",
				Usage:         "what will change on the hosts: diff, drift, unreachable",
				ShellComplete: completeHosts,
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.StringFlag{Name: "diff", Usage: "show a file diff: host:/path"},
					&ucli.StringFlag{Name: "adopt", Usage: "take a file from a host into overrides/: host:/path"},
					&ucli.BoolFlag{Name: "agent", Usage: "ask the host agent instead of running a probe script"},
					atFlag(),
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					at, err := dayArg(c)
					if err != nil {
						return err
					}
					return Plan(c.String("dir"), c.String("host"), c.String("diff"), c.String("adopt"), c.Bool("agent"), at)
				},
			},
			{
				Name:          "apply",
				Usage:         "apply the state on the hosts, leaves first",
				ShellComplete: completeHosts,
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.StringFlag{Name: "host", Usage: "only this host"},
					&ucli.StringFlag{Name: "branch", Usage: "apply from a branch other than main"},
					&ucli.BoolFlag{Name: "force", Usage: "do not stop on a dead link or a missing certificate"},
					&ucli.BoolFlag{Name: "unpushed", Usage: "apply a commit that is not on origin/main: only when the remote is unavailable"},
					&ucli.BoolFlag{Name: "agent", Usage: "one signed request to the host agent instead of shell steps"},
					atFlag(),
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					at, err := dayArg(c)
					if err != nil {
						return err
					}
					return Apply(c.String("dir"), c.String("host"), c.String("branch"),
						c.Bool("force"), c.Bool("unpushed"), c.Bool("agent"), at)
				},
			},
			{
				Name:  "host",
				Usage: "hosts: setup and maintenance",
				Commands: []*ucli.Command{
					{
						Name:          "add",
						Usage:         "create a host with its inbound, keys and routing",
						ArgsUsage:     "<name>",
						ShellComplete: completeNewHost,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "role", Value: "exit", Usage: "entry or exit"},
							&ucli.StringFlag{Name: "ssh", Usage: "root@ip"},
							&ucli.StringFlag{Name: "ip", Usage: "address, inferred from --ssh when omitted"},
							&ucli.StringFlag{Name: "domain", Usage: "the entry domain, required for an entry"},
							&ucli.StringFlag{Name: "via", Usage: "jump host name"},
							&ucli.StringFlag{Name: "engine", Usage: "sing-box (default) or xray"},
							&ucli.BoolFlag{Name: "awg", Usage: "add an AmneziaWG inbound"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return HostAdd(c.String("dir"), ops.HostOptions{
								Name:   c.Args().First(),
								Role:   c.String("role"),
								SSH:    c.String("ssh"),
								IP:     c.String("ip"),
								Domain: c.String("domain"),
								Via:    c.String("via"),
								Engine: c.String("engine"),
								AWG:    c.Bool("awg"),
							}, c.Bool("nocommit"))
						},
					},
					{
						Name:          "agent-install",
						Usage:         "put the agent on a host and restrict the key to it",
						ArgsUsage:     "<name>",
						ShellComplete: completeHosts,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "binary", Usage: "path to vpnctl-agent, default: next to this binary"},
							&ucli.StringFlag{Name: "operator", Usage: "name of the key in authorized_keys and /etc/vpnctl/agents"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return HostAgentInstall(c.String("dir"), c.Args().First(), c.String("binary"), c.String("operator"))
						},
					},
					{
						Name:          "rm",
						Usage:         "remove a host with its inbound and routing",
						ArgsUsage:     "<name>",
						ShellComplete: completeHosts,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the host name is required")
							}
							return HostRemove(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:          "bootstrap",
						Usage:         "install packages, engines and units on a host, idempotent",
						ArgsUsage:     "<name>",
						ShellComplete: completeHosts,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "ssh", Usage: "user@host to record when the state has none"},
							&ucli.BoolFlag{Name: "print", Usage: "print the script instead of running it"},
							atFlag(),
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							at, err := dayArg(c)
							if err != nil {
								return err
							}
							return HostBootstrap(c.String("dir"), c.Args().First(), c.String("ssh"), c.Bool("print"), os.Stdout, at)
						},
					},
				},
			},
			{
				Name:          "metrics",
				Usage:         "host metrics collected by the timer on the entry",
				ArgsUsage:     "<host>",
				ShellComplete: completeHosts,
				Flags: []ucli.Flag{
					dirFlag(), jsonFlag(),
					&ucli.IntFlag{Name: "last", Value: 24, Usage: "how many samples to show"},
				},
				Action: func(_ context.Context, c *ucli.Command) error {
					SetJSON(c.Bool("json"))
					name := c.Args().First()
					if name == "" {
						return errors.New("the host name is required")
					}
					return Metrics(c.String("dir"), name, c.Int("last"))
				},
			},
			{
				Name:  "alerts",
				Usage: "where the entry sends notifications when the chain breaks",
				Commands: []*ucli.Command{
					{
						Name:  "set",
						Usage: "point the probe at an ntfy topic, ours or someone else's",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "ntfy", Usage: "https://ntfy.sh/vpnctl-xxxx, a topic that already exists"},
							&ucli.StringFlag{Name: "serve", Usage: "entry that runs the notification server itself"},
							&ucli.StringFlag{Name: "topic", Usage: "topic name for --serve, e.g. vpnctl-659b3bf0"},
							&ucli.StringFlag{Name: "token-file", Usage: "file with the bearer token; a public topic needs none"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							if c.String("ntfy") == "" && c.String("serve") == "" {
								return errors.New("either --ntfy <url> or --serve <host> --topic <name> is required")
							}
							return AlertsSet(c.String("dir"), c.String("ntfy"), c.String("serve"),
								c.String("topic"), c.String("token-file"), c.Bool("nocommit"))
						},
					},
					{
						Name:  "off",
						Usage: "clear the topic and the token",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return AlertsOff(c.String("dir"), c.Bool("nocommit"))
						},
					},
					{
						Name:  "show",
						Usage: "print the topic, whether a token is set and which hosts alert",
						Flags: []ucli.Flag{dirFlag(), jsonFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return AlertsShow(c.String("dir"))
						},
					},
					{
						Name:  "test",
						Usage: "send one message now and print the answer",
						Flags: []ucli.Flag{dirFlag(), jsonFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return AlertsTest(c.String("dir"))
						},
					},
				},
			},
			{
				Name:  "versions",
				Usage: "pinned component versions",
				Commands: []*ucli.Command{
					{
						Name:  "check",
						Usage: "compare the pins with the newest upstream releases",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "breaking", Usage: "exit 1 when a minor or a major is out"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return VersionsCheck(c.String("dir"), c.Bool("breaking"))
						},
					},
					{
						Name:  "auto",
						Usage: "pull in patch updates: bump the pin, record sha256, commit, push",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "apply", Usage: "also plan and apply on the hosts"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return VersionsAuto(c.String("dir"), c.Bool("apply"), c.Bool("nocommit"))
						},
					},
					{
						Name:      "pin",
						Usage:     "download the pinned releases and record their sha256",
						ArgsUsage: "[component]",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return VersionsPin(c.String("dir"), c.Args().First(), c.Bool("nocommit"))
						},
					},
				},
			},
			{
				Name:  "state",
				Usage: "keep the files of the state canonical: two spaces everywhere",
				Commands: []*ucli.Command{
					{
						Name:  "fmt",
						Usage: "rewrite every state file in its canonical form",
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "check", Usage: "report instead of writing, exit 1 when something differs"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return StateFmt(c.String("dir"), c.Bool("check"), c.Bool("nocommit"))
						},
					},
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
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "modes", Value: "split", Usage: "split,full"},
							&ucli.StringFlag{Name: "protos", Value: "reality,ws", Usage: "reality,ws,hy2,awg"},
							&ucli.BoolFlag{Name: "tg", Usage: "the user gets the Telegram proxy"},
							&ucli.StringFlag{Name: "entry", Usage: "entry host when the state has several"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserAdd(c.String("dir"), name,
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
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserRm(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:          "rotate",
						Usage:         "new uuid, passwords and sub id: a leaked link stops working",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserRotate(c.String("dir"), name, c.Bool("nocommit"))
						},
					},
					{
						Name:          "disable",
						Usage:         "turn a user off without touching the credentials",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserSetEnabled(c.String("dir"), name, false, c.Bool("nocommit"))
						},
					},
					{
						Name:          "enable",
						Usage:         "turn a user back on",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserSetEnabled(c.String("dir"), name, true, c.Bool("nocommit"))
						},
					},
					{
						Name:          "edit",
						Usage:         "change modes, protocols, expiry or a note",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "modes", Usage: "split,full"},
							&ucli.StringFlag{Name: "protos", Usage: "reality,ws,hy2"},
							&ucli.StringFlag{Name: "expires", Usage: "YYYY-MM-DD, empty clears it"},
							&ucli.StringFlag{Name: "note", Usage: "free text"},
							&ucli.BoolFlag{Name: "tg", Usage: "the user gets the Telegram proxy"},
							&ucli.BoolFlag{Name: "no-tg", Usage: "the user does not get it"},
							&ucli.BoolFlag{Name: "nocommit", Usage: "do not commit"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							var modes, protos []string
							if c.IsSet("modes") {
								modes = splitList(c.String("modes"))
							}
							if c.IsSet("protos") {
								protos = splitList(c.String("protos"))
							}
							var tg *bool
							switch {
							case c.Bool("tg") && c.Bool("no-tg"):
								return errors.New("either --tg or --no-tg, not both")
							case c.Bool("tg"):
								v := true
								tg = &v
							case c.Bool("no-tg"):
								v := false
								tg = &v
							}
							var expires, note *string
							if c.IsSet("expires") {
								v := c.String("expires")
								expires = &v
							}
							if c.IsSet("note") {
								v := c.String("note")
								note = &v
							}
							return UserEdit(c.String("dir"), name, modes, protos, tg, expires, note, c.Bool("nocommit"))
						},
					},
					{
						Name:  "list",
						Usage: "list users",
						Flags: []ucli.Flag{dirFlag(), jsonFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							return UserList(c.String("dir"))
						},
					},
					{
						Name:          "link",
						Usage:         "subscription URL, direct links and the Telegram proxy",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags:         []ucli.Flag{dirFlag(), jsonFlag()},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserLink(c.String("dir"), name)
						},
					},
					{
						Name:          "message",
						Aliases:       []string{"share"},
						Usage:         "a ready-to-forward message for the user",
						ArgsUsage:     "<name>",
						ShellComplete: completeUsers,
						Flags: []ucli.Flag{
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "format", Value: "text", Usage: "text or markdown"},
							&ucli.StringFlag{Name: "mode", Usage: "split or full, default: every mode"},
							&ucli.StringFlag{Name: "lang", Usage: "ru or en, default: the system locale"},
							&ucli.BoolFlag{Name: "ip", Usage: "use the entry IP instead of the domain"},
							&ucli.BoolFlag{Name: "no-links", Usage: "omit the direct links block"},
							&ucli.BoolFlag{Name: "no-tg", Usage: "omit the Telegram proxy block"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserMessage(MessageOpts{
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
							dirFlag(), jsonFlag(),
							&ucli.StringFlag{Name: "mode", Usage: "split or full, default: all user modes"},
							&ucli.StringFlag{Name: "format", Value: "links", Usage: "links, sub, singbox or clash"},
							&ucli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to a file or a directory instead of stdout"},
						},
						Action: func(_ context.Context, c *ucli.Command) error {
							SetJSON(c.Bool("json"))
							name := c.Args().First()
							if name == "" {
								return errors.New("the user name is required")
							}
							return UserConfig(c.String("dir"), name, c.String("mode"), c.String("format"), c.String("out"))
						},
					},
				},
			},
		},
	}
	return app
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

// completeNewHost has nothing to suggest: a new host has no name yet.
func completeNewHost(_ context.Context, _ *ucli.Command) {}

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

// ShowAppHelp prints the top-level help, for the caller that has to reject an
// unknown command before urfave turns it into its own exit code.
func ShowAppHelp(app *ucli.Command) error {
	return ucli.ShowAppHelp(app)
}
