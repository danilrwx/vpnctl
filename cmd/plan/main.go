package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// plan reports what differs between the state and the hosts: plan -dir <state> [-host name]
func main() {
	dir := flag.String("dir", ".", "state directory")
	host := flag.String("host", "", "only this host")
	diff := flag.String("diff", "", "show a file diff: -diff host:/path")
	adopt := flag.String("adopt", "", "take a file from a host into overrides/: -adopt host:/path")
	flag.Parse()

	st, err := state.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ✗", e)
		}
		fmt.Fprintf(os.Stderr, "Error: %d errors in the state\n", len(errs))
		os.Exit(1)
	}
	rendered, err := render.Check(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *adopt != "" {
		h, p := splitTarget(*adopt)
		msg, err := plan.Adopt(st, h, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(msg)
		return
	}
	if *diff != "" {
		h, p := splitTarget(*diff)
		out, err := plan.Diff(st, rendered, h, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(out)
		return
	}
	one := render.Rendered{}
	for name, files := range rendered {
		if *host == "" || *host == name {
			one[name] = files
		}
	}
	plans, err := plan.Plan(st, one)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	plan.Print(plans)
}

func splitTarget(s string) (string, string) {
	i := strings.Index(s, ":")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}
