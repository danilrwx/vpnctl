package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// fakeSSH is a real SSH server on loopback: these tests exercise the client over
// the wire, not a mock of it.
type fakeSSH struct {
	addr string
	pub  ssh.PublicKey
}

func newFakeSSH(t *testing.T) *fakeSSH {
	t.Helper()

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go serveSSH(conn, cfg)
		}
	}()

	return &fakeSSH{addr: ln.Addr().String(), pub: signer.PublicKey()}
}

// serveSSH answers an exec request with "ran:<command>" and a status, which is
// all the client needs to prove it reached the far side.
func serveSSH(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}

	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "sessions only")

			continue
		}

		c, creqs, err := ch.Accept()
		if err != nil {
			continue
		}

		go func() {
			for req := range creqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)

					continue
				}

				_ = req.Reply(true, nil)

				cmd := string(req.Payload[4:])
				status := struct{ Status uint32 }{0}

				if strings.Contains(cmd, "fail") {
					status.Status = 1
					_, _ = c.Write([]byte("it broke\n"))
				} else {
					_, _ = c.Write([]byte("ran:" + cmd + "\n"))
				}

				_, _ = c.SendRequest("exit-status", false, ssh.Marshal(status))
				_ = c.Close()
			}
		}()
	}
}

// clientIdentity writes a throwaway key and points the tool at it.
func clientIdentity(t *testing.T) {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}

	path := t.TempDir() + "/id_test"
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VPNCTL_SSH_KEY", path)
	t.Setenv("SSH_AUTH_SOCK", "")
}

// dialState describes the fake server as a host of the state.
func dialState(t *testing.T, f *fakeSSH, pinned, accept bool) *state.State {
	t.Helper()

	host, port, err := net.SplitHostPort(f.addr)
	if err != nil {
		t.Fatal(err)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	h := state.Host{Role: state.Entry, IP: host, SSH: "root@" + host, SSHPort: portNum}
	if pinned {
		h.HostKey = f.pub.Type() + " " + base64.StdEncoding.EncodeToString(f.pub.Marshal())
	}

	return &state.State{Hosts: map[string]state.Host{"node": h}, AcceptNewHostKey: accept}
}

// TestDialAndRun: the client reaches the host, pins its key and runs a script,
// which is the whole contract every guard depends on.
func TestDialAndRun(t *testing.T) {
	clientIdentity(t)
	f := newFakeSSH(t)

	c, err := Dial(dialState(t, f, true, false), "node")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Reachable(); err != nil {
		t.Fatalf("reachable: %v", err)
	}

	out, err := c.SH("echo hello")
	if err != nil {
		t.Fatalf("sh: %v", err)
	}

	if !strings.Contains(out, "ran:echo hello") {
		t.Fatalf("output %q", out)
	}

	res, err := c.Run("echo hello", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !res.OK() || !strings.Contains(string(res.Stdout), "ran:echo hello") {
		t.Fatalf("run: %+v", res)
	}

	failed, err := c.Run("fail", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if failed.OK() {
		t.Fatalf("a non-zero status was reported as success: %+v", failed)
	}
}

// TestDialRefusesAnotherHost: the pinned key is the only one that may answer.
func TestDialRefusesAnotherHost(t *testing.T) {
	clientIdentity(t)
	f := newFakeSSH(t)
	other := newFakeSSH(t)

	st := dialState(t, f, false, false)
	h := st.Hosts["node"]
	h.HostKey = other.pub.Type() + " " + base64.StdEncoding.EncodeToString(other.pub.Marshal())
	st.Hosts["node"] = h

	if _, err := Dial(st, "node"); err == nil {
		t.Fatal("a host with another key was accepted")
	}
}

// TestDialFirstContact: without a pin the client refuses unless the caller said
// so out loud.
func TestDialFirstContact(t *testing.T) {
	clientIdentity(t)
	f := newFakeSSH(t)

	if _, err := Dial(dialState(t, f, false, false), "node"); err == nil {
		t.Fatal("the first contact was accepted silently")
	}

	c, err := Dial(dialState(t, f, false, true), "node")
	if err != nil {
		t.Fatalf("an explicit first contact was refused: %v", err)
	}

	_ = c.Close()
}
