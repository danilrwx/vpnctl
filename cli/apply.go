package cli

import (
	"errors"
	"io"
	"os"

	"github.com/danilrwx/vpnctl-go/agent"
	"github.com/danilrwx/vpnctl-go/apply"
	"github.com/danilrwx/vpnctl-go/remote"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
)

// Apply installs the state on the hosts. Under --json the progress goes nowhere
// and the result is printed as JSON instead.
func Apply(dir, host, branch string, force, unpushed, viaAgent bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	var out io.Writer = os.Stdout
	if jsonOut {
		out = io.Discard
	}
	opts := &apply.Opts{Host: host, Branch: branch, Force: force, Unpushed: unpushed}
	if viaAgent {
		opts.AgentApply = agentApply
	}
	res, err := apply.Apply(st, opts, out)
	if err != nil {
		var ge *apply.GuardError
		if errors.As(err, &ge) {
			return Guard(err)
		}
		return err
	}
	if jsonOut {
		return emit(res, func() {})
	}
	return nil
}

// agentApply signs the tree and hands it to the agent as one request.
func agentApply(c *remote.Client, host, commit string, files render.HostFiles) (apply.HostResult, error) {
	key, err := agent.LoadKey(agent.KeyPath())
	if err != nil {
		return apply.HostResult{}, err
	}
	list, err := agent.FilesFromTree(files, apply.ModeForInt)
	if err != nil {
		return apply.HostResult{}, err
	}
	req, err := agent.NewRequest(key, "apply", host, commit, list)
	if err != nil {
		return apply.HostResult{}, err
	}
	resp, err := agent.Call(c, req)
	if err != nil {
		return apply.HostResult{}, err
	}
	return apply.HostResult{Status: "applied", Probe: resp.Probe, Marker: commit}, nil
}
