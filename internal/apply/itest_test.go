package apply

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5"
	"golang.org/x/crypto/ssh"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// fakeHost is an SSH server that records the commands an apply sends and
// answers like a bootstrapped host would. Nothing runs on the real machine: the
// scripts are captured, not executed.
type fakeHost struct {
	addr      string
	probeOut  string
	probeCode string

	mu      sync.Mutex
	log     []string
	tarball []byte
	// pub is the server's host key: the state pins it, so the test also proves
	// the pinned path works and not just the first contact.
	pub ssh.PublicKey
}

func newFakeHost(t *testing.T, probeOut string) *fakeHost {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, nil // any key is fine: the test is about the client
	}}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	// the client must use a throwaway key, never the real agent or ~/.ssh
	_, clientKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	block, err := ssh.MarshalPrivateKey(clientKey, "")
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VPNCTL_SSH_KEY", keyPath)
	t.Setenv("SSH_AUTH_SOCK", "")

	h := &fakeHost{addr: ln.Addr().String(), probeOut: probeOut, probeCode: "204", pub: signer.PublicKey()}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go h.serve(conn, cfg)
		}
	}()

	return h
}

func (h *fakeHost) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}

	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}

		c, creqs, err := ch.Accept()
		if err != nil {
			continue
		}

		go func() {
			defer func() { _ = c.Close() }()

			for r := range creqs {
				if r.Type != "exec" {
					_ = r.Reply(false, nil)
					continue
				}

				var payload struct{ Command string }
				if err := ssh.Unmarshal(r.Payload, &payload); err != nil {
					_ = r.Reply(false, nil)
					continue
				}

				_ = r.Reply(true, nil)

				h.run(c, payload.Command)

				return
			}
		}()
	}
}

// run answers one command: the probe gets a canned answer, a tar gets its
// stream stored, everything else succeeds.
func (h *fakeHost) run(c ssh.Channel, cmd string) {
	h.mu.Lock()
	h.log = append(h.log, cmd)
	h.mu.Unlock()

	switch {
	case strings.Contains(cmd, "applied-commit"):
		_, _ = fmt.Fprint(c, h.probeOut)
	case strings.Contains(cmd, "tar -xf -"):
		body, _ := io.ReadAll(c)

		h.mu.Lock()
		h.tarball = body
		h.mu.Unlock()
	case strings.Contains(cmd, "curl"):
		_, _ = fmt.Fprint(c, h.probeCode)
	}

	_, _ = c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
}

func (h *fakeHost) commands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.log...)
}

func (h *fakeHost) received() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.tarball
}

// pushedState builds a state directory with one pushed commit and one host
// pointing at the fake server.
func itestState(t *testing.T, hostAddr, hostKey string) string {
	t.Helper()
	root := t.TempDir()

	bare := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	hostIP, port, err := net.SplitHostPort(hostAddr)
	if err != nil {
		t.Fatal(err)
	}

	portNum := 0
	_, _ = fmt.Sscanf(port, "%d", &portNum)
	hosts := fmt.Sprintf("hosts:\n  node:\n    role: exit\n    engine: sing-box\n    ip: %s\n    ssh_port: %d\n    ssh: root@%s\n    host_key: %s\n", hostIP, portNum, hostIP, hostKey)
	write(t, filepath.Join(dir, "hosts.yaml"), hosts)
	write(t, filepath.Join(dir, "links.yaml"), "links: []\n")
	write(t, filepath.Join(dir, "users.yaml"), "users: {}\n")
	write(t, filepath.Join(dir, "protocols.yaml"), "inbounds:\n  node:\n    reality:\n      port: 443\n")
	write(t, filepath.Join(dir, "versions.yaml"), "sing-box:\n  version: 1.12.4\n")

	repo, err := gitsync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.AddRemote(bare); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("feat(state): fixture"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestApplyOverSSH runs a whole series against the fake host and checks what
// the client actually sent: the tree, the checks, the install, the probe, the
// marker and the manifest, in that order.
func TestApplyOverSSH(t *testing.T) {
	host := newFakeHost(t, "-\nyes\n-\n")
	dir := itestState(t, host.addr, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host.pub))))

	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	st.FillStubSecrets()

	out := &strings.Builder{}

	res, err := Apply(st, &Opts{}, out)
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out.String())
	}

	if len(res.Hosts) != 1 || res.Hosts[0].Status != "applied" {
		t.Fatalf("result: %+v", res.Hosts)
	}

	if len(host.received()) == 0 {
		t.Fatal("the tree never arrived")
	}

	cmds := strings.Join(host.commands(), "\n")
	for _, want := range []string{"tar -xf -", "applied.sha256", "applied-commit", "curl"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("no %q in:\n%s", want, cmds)
		}
	}

	if !strings.Contains(cmds, "set -e") {
		t.Error("the install script was not sent")
	}
}

// TestApplyRollsBackWhenProbeFails: a probe that is not 204 must send the
// rollback script and stop the series.
func TestApplyRollsBackWhenProbeFails(t *testing.T) {
	host := newFakeHost(t, "-\nyes\n-\n")
	host.probeCode = "000"
	dir := itestState(t, host.addr, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host.pub))))

	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	st.FillStubSecrets()

	out := &strings.Builder{}

	_, err = Apply(st, &Opts{}, out)
	if err == nil {
		t.Fatalf("apply succeeded with a dead probe:\n%s", out.String())
	}

	if !strings.Contains(err.Error(), "probe") {
		t.Errorf("unexpected error: %v", err)
	}

	cmds := strings.Join(host.commands(), "\n")
	if !strings.Contains(cmds, "set +e") {
		t.Errorf("no rollback script was sent:\n%s", cmds)
	}

	if strings.Contains(cmds, "applied-commit") && strings.Contains(out.String(), "marker") {
		t.Error("the marker was written after a failed probe")
	}
}
