package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

const usage = `usage: user <add|rm|list|link|share> -dir <state> [flags]

  add   -name <name> [-modes split,full] [-protos reality,ws,hy2] [-tg] [-entry <host>] [-nocommit]
  rm    -name <name> [-nocommit]
  list
  link  -name <name>
  share -name <name>
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	op := os.Args[1]
	fs := flag.NewFlagSet(op, flag.ExitOnError)
	dir := fs.String("dir", ".", "state directory")
	name := fs.String("name", "", "user name")
	modes := fs.String("modes", "split", "comma-separated modes: split, full")
	protos := fs.String("protos", "reality,ws", "comma-separated protocols: reality, ws, hy2, awg")
	tg := fs.Bool("tg", false, "the user gets the Telegram proxy")
	entry := fs.String("entry", "", "entry host, when the state has several")
	nocommit := fs.Bool("nocommit", false, "do not commit")
	_ = fs.Parse(os.Args[2:])

	st, err := state.Load(*dir)
	if err != nil {
		die(err)
	}

	switch op {
	case "add":
		if *name == "" {
			die(fmt.Errorf("-name is required"))
		}
		if _, ok := st.Users[*name]; ok {
			die(fmt.Errorf("user %q already exists", *name))
		}
		u := state.User{
			Modes:     splitList(*modes),
			Protocols: splitList(*protos),
			TG:        *tg,
			Entry:     *entry,
		}
		msg, err := ops.UserAdd(st, *name, u.Modes, u.Protocols, u.TG)
		if err != nil {
			die(err)
		}
		if *entry != "" {
			nu := st.Users[*name]
			nu.Entry = *entry
			st.Users[*name] = nu
		}
		write(st, *dir, msg, *nocommit)
		fmt.Printf("✓ %s: %s (%s)\n", msg, strings.Join(u.Modes, ","), strings.Join(u.Protocols, ","))
	case "rm":
		if *name == "" {
			die(fmt.Errorf("-name is required"))
		}
		msg, err := ops.UserRm(st, *name)
		if err != nil {
			die(err)
		}
		write(st, *dir, msg, *nocommit)
		fmt.Printf("✓ %s\n", msg)
	case "list":
		for _, n := range ops.SortedUsers(st) {
			u := st.Users[n]
			status := "on"
			if !u.IsEnabled() {
				status = "off"
			}
			fmt.Printf("%-14s %-6s modes=%-12s protos=%-22s tg=%v awg=%v\n",
				n, status, strings.Join(u.Modes, ","), strings.Join(u.Protocols, ","), u.TG, u.AWG != nil)
		}
	case "link":
		entryName, err := st.Entry()
		if err != nil {
			die(err)
		}
		u := st.Users[*name]
		mode := "split"
		if len(u.Modes) > 0 {
			mode = u.Modes[0]
		}
		sub, err := render.SubURL(st, entryName, *name, mode)
		if err != nil {
			die(err)
		}
		fmt.Printf("sub: %s\n", sub)
		links, err := render.Links(st, entryName, *name, mode)
		if err != nil {
			die(err)
		}
		for _, l := range links {
			fmt.Println(l)
		}
		if tg, ok := render.TGLink(st, entryName); ok {
			fmt.Printf("tg:  %s\n", tg)
		}
	case "share":
		if *name == "" {
			die(fmt.Errorf("-name is required"))
		}
		msg, err := render.Share(st, *name)
		if err != nil {
			die(err)
		}
		fmt.Print(msg)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func write(st *state.State, dir, msg string, nocommit bool) {
	if err := state.SaveUsers(dir, st.Users); err != nil {
		die(err)
	}
	if err := state.SaveSecrets(dir, st.Secrets); err != nil {
		die(err)
	}
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		die(fmt.Errorf("%d errors in the state", len(errs)))
	}
	if nocommit {
		fmt.Println("(not committed)")
		return
	}
	repo, err := gitsync.Open(dir)
	if err != nil {
		die(err)
	}
	if err := repo.Commit(msg); err != nil {
		die(err)
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

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
