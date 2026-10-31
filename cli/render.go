package cli

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Render writes the config tree of the state.
func Render(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	dir := fs.String("dir", ".", "state directory")
	out := fs.String("out", "", "output directory")
	host := fs.String("host", "", "only this host")
	stub := fs.Bool("stub", false, "deterministic secret stubs instead of the age key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := state.Load(*dir)
	if err != nil {
		return err
	}
	if *stub {
		st.FillStubSecrets()
	}
	if !*stub {
		if errs := validate.State(st); len(errs) > 0 {
			for _, e := range errs {
				fmt.Println("  ✗", e)
			}
			return fmt.Errorf("%d errors in the state", len(errs))
		}
	}
	r, err := render.Render(st, *host)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := render.Write(r, *out); err != nil {
			return err
		}
	}
	for name, files := range r {
		fmt.Printf("%s: %d files → %s\n", name, len(files), filepath.Join(*out, name))
	}
	return nil
}
