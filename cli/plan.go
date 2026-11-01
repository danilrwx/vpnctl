package cli

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Plan reports what differs between the state and the hosts.
func Plan(dir, host, diff, adopt string) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if errs := validate.State(st); len(errs) > 0 {
		for _, e := range errs {
			fmt.Println("  ✗", e)
		}
		return fmt.Errorf("%d errors in the state", len(errs))
	}
	rendered, err := render.Check(st)
	if err != nil {
		return err
	}
	if adopt != "" {
		h, p := splitTarget(adopt)
		msg, err := plan.Adopt(st, h, p)
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	}
	if diff != "" {
		h, p := splitTarget(diff)
		out, err := plan.Diff(st, rendered, h, p)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}
	one := render.Rendered{}
	for name, files := range rendered {
		if host == "" || host == name {
			one[name] = files
		}
	}
	plans, err := plan.Plan(st, one)
	if err != nil {
		return err
	}
	return emit(toPlanJSON(plans), func() { plan.Print(plans) })
}

// planJSON is the machine-readable plan: what an agent reads before applying.
type planJSON []hostPlanJSON

type hostPlanJSON struct {
	Name         string     `json:"name"`
	Marker       string     `json:"marker,omitempty"`
	Unpushed     bool       `json:"unpushed,omitempty"`
	Bootstrapped bool       `json:"bootstrapped"`
	Unreachable  string     `json:"unreachable,omitempty"`
	Changes      int        `json:"changes"`
	Files        []fileJSON `json:"files"`
	Notes        []string   `json:"notes,omitempty"`
}

type fileJSON struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

func toPlanJSON(plans []plan.HostPlan) planJSON {
	out := make(planJSON, 0, len(plans))
	for _, p := range plans {
		h := hostPlanJSON{
			Name: p.Name, Marker: p.Marker, Unpushed: p.Unpushed,
			Bootstrapped: p.Bootstrapped, Unreachable: p.Unreachable,
			Changes: p.Changes(), Notes: p.Notes,
		}
		for _, f := range p.Files {
			h.Files = append(h.Files, fileJSON{Path: f.Path, Status: f.Status.Mark()})
		}
		if h.Files == nil {
			h.Files = []fileJSON{}
		}
		out = append(out, h)
	}
	return out
}

func splitTarget(s string) (string, string) {
	i := strings.Index(s, ":")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}
