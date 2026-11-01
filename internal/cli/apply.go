package cli

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/apply"
	"github.com/danilrwx/vpnctl-go/internal/hostscript"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
)

// Apply installs the state on the hosts. Under --json the progress goes nowhere
// and the result is printed as JSON instead.
func Apply(dir, host, branch string, force, unpushed, viaAgent bool, at time.Time) error {
	st, err := loadState(dir)
	if err != nil {
		return err
	}
	var out io.Writer = os.Stdout
	if jsonOut {
		out = io.Discard
	}
	opts := &apply.Opts{Host: host, Branch: branch, Force: force, Unpushed: unpushed, At: at}
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
	list, err := agent.FilesFromTree(files, hostscript.ModeForInt)
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
