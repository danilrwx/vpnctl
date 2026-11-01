package cli

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/agent"
	"github.com/danilrwx/vpnctl-go/remote"

	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Plan reports what differs between the state and the hosts.
func Plan(dir, host, diff, adopt string, viaAgent bool) error {
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
	var prober plan.Prober
	if viaAgent {
		prober = agentProbe
	}
	plans, err := plan.PlanWith(st, one, prober)
	if err != nil {
		return err
	}
	return emit(plan.JSONOf(plans), func() { plan.Print(plans) })
}

func splitTarget(s string) (string, string) {
	i := strings.Index(s, ":")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// agentProbe asks the agent for the marker and the hashes in one request.
func agentProbe(c *remote.Client, host string, paths []string) (plan.Probe, error) {
	key, err := agent.LoadKey(agent.KeyPath())
	if err != nil {
		return plan.Probe{}, err
	}
	req, err := agent.NewRequest(key, "plan", host, "", nil)
	if err != nil {
		return plan.Probe{}, err
	}
	req.Paths = paths
	resp, err := agent.Call(c, req)
	if err != nil {
		return plan.Probe{}, err
	}
	p := plan.Probe{Hashes: map[string]string{}, Missing: map[string]bool{}, Bootstrapped: resp.Bootstrapped}
	if resp.Marker != "" && resp.Marker != "-" {
		fields := strings.Fields(resp.Marker)
		p.Marker, p.HasMarker = fields[0], true
		p.Unpushed = len(fields) > 1 && fields[1] == "unpushed"
	}
	for path, sum := range resp.Hashes {
		if sum == "" {
			p.Missing[path] = true
			continue
		}
		p.Hashes[path] = sum
	}
	return p, nil
}
