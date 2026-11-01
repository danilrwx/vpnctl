package mobile

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/apply"
	"github.com/danilrwx/vpnctl-go/internal/hostscript"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"github.com/danilrwx/vpnctl-go/internal/render"
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

	// the same payload the command line signs, but the signature comes from the
	// Keystore: the host verifies it with the public key it was given
	req := &agent.Request{
		V: agent.Version, Op: "apply", Host: host, Commit: commit, Files: list,
		TS: time.Now().Unix(), Nonce: nonce(),
	}
	req.TreeHash = agent.TreeHash(list)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		return apply.HostResult{}, err
	}

	req.Sig = base64.StdEncoding.EncodeToString(sig)

	resp, err := agent.Call(c, req)
	if err != nil {
		return apply.HostResult{}, err
	}

	return apply.HostResult{Status: "applied", Probe: resp.Probe, Marker: commit}, nil
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
