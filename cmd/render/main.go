package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
)

// render writes the config tree of the state: render -dir <state> -out <dir> [-host name] [-stub]
func main() {
	dir := flag.String("dir", ".", "state directory")
	out := flag.String("out", "", "output directory")
	host := flag.String("host", "", "only this host")
	stub := flag.Bool("stub", false, "deterministic secret stubs instead of the age key")
	flag.Parse()
	st, err := state.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *stub {
		st.FillStubSecrets()
	}
	r, err := render.Render(st, *host)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *out != "" {
		if err := render.Write(r, *out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	for name, files := range r {
		fmt.Printf("%s: %d files → %s\n", name, len(files), filepath.Join(*out, name))
	}
}
