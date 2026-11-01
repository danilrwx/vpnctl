package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"github.com/danilrwx/vpnctl-go/internal/validate"
)

// Render writes the config tree of the state.
func Render(dir, out, host string, stub bool, at time.Time) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if stub {
		st.FillStubSecrets()
	} else if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Println("  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	r, err := render.Render(st, host, at)
	if err != nil {
		return err
	}
	if out != "" {
		if err := render.Write(r, out); err != nil {
			return err
		}
	}
	counts := map[string]int{}
	for name, files := range r {
		counts[name] = len(files)
	}
	return emit(struct {
		Out   string         `json:"out,omitempty"`
		Hosts map[string]int `json:"hosts"`
	}{Out: out, Hosts: counts}, func() {
		for name, files := range r {
			fmt.Printf("%s: %d files → %s\n", name, len(files), filepath.Join(out, name))
		}
	})
}
