package mobile

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/apply"
	"github.com/danilrwx/vpnctl-go/internal/hostscript"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// agentApply signs the tree with the Keystore key and hands it to the agent as
// one request. This is the only way a phone can apply: it has no general SSH key
// on the hosts, and the agent's key is restricted to the forced command.
func agentApply(c *remote.Client, host, commit string, files render.HostFiles) (apply.HostResult, error) {
	if signer == nil {
		return apply.HostResult{}, fmt.Errorf("no signer: the device cannot authorise an apply")
	}

	list, err := agent.FilesFromTree(files, hostscript.ModeForInt)
	if err != nil {
		return apply.HostResult{}, err
	}

	req, err := signedRequest("apply", host, commit, list)
	if err != nil {
		return apply.HostResult{}, err
	}

	resp, err := agent.Call(c, req)
	if err != nil {
		return apply.HostResult{}, err
	}

	return apply.HostResult{Status: "applied", Probe: resp.Probe, Marker: commit}, nil
}

// agentMark records the commit and the manifest on a host that needs no other
// change. Without it a phone could never move the marker: it has no shell, and
// the shell is where the command line writes it.
func agentMark(c *remote.Client, host, commit string, files render.HostFiles) error {
	if signer == nil {
		return fmt.Errorf("no signer: the device cannot authorise a mark")
	}

	list, err := agent.FilesFromTree(files, hostscript.ModeForInt)
	if err != nil {
		return err
	}

	req, err := signedRequest("mark", host, commit, list)
	if err != nil {
		return err
	}

	_, err = agent.Call(c, req)

	return err
}

// signedRequest is the one place a request is built and signed with the
// Keystore key, so every operation hashes and signs the same payload.
func signedRequest(op, host, commit string, files []agent.File) (*agent.Request, error) {
	req := &agent.Request{
		V: agent.Version, Op: op, Host: host, Commit: commit, Files: files,
		TS: time.Now().Unix(), Nonce: nonce(),
	}
	req.TreeHash = agent.TreeHash(files)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		return nil, err
	}

	req.Sig = base64.StdEncoding.EncodeToString(sig)

	return req, nil
}

// nonce is 16 random bytes in hex, the same shape the command line uses: the
// host refuses a nonce it has seen.
func nonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	return hex.EncodeToString(b)
}

// agentProbe asks the agent for the marker and the hashes in one signed request.
// A phone has no shell on the hosts, so this is the only way it can plan.
func agentProbe(st *state.State, host string, paths []string) (plan.Probe, error) {
	if signer == nil {
		return plan.Probe{}, fmt.Errorf("no signer: the device cannot ask the agent")
	}

	c, err := remote.Dial(st, host)
	if err != nil {
		return plan.Probe{}, err
	}

	defer func() { _ = c.Close() }()

	req := &agent.Request{V: agent.Version, Op: "plan", Host: host, Paths: paths, TS: time.Now().Unix(), Nonce: nonce()}
	req.TreeHash = agent.TreeHash(nil)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		return plan.Probe{}, err
	}

	req.Sig = base64.StdEncoding.EncodeToString(sig)

	resp, err := agent.Call(c, req)
	if err != nil {
		return plan.Probe{}, err
	}

	p := plan.Probe{Hashes: resp.Hashes, Missing: map[string]bool{}, Bootstrapped: resp.Bootstrapped}

	if resp.Marker != "" && resp.Marker != "-" {
		fields := strings.Fields(resp.Marker)
		p.Marker, p.HasMarker = fields[0], true

		if len(fields) > 1 && fields[1] == "unpushed" {
			p.Unpushed = true
		}
	}

	for path, sum := range resp.Hashes {
		if sum == "" {
			p.Missing[path] = true
		}
	}

	return p, nil
}
