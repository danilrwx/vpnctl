package remote

import (
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func chainState() *state.State {
	return &state.State{Hosts: map[string]state.Host{
		"entry": {Role: state.Entry, IP: "198.51.100.1", SSH: "root@198.51.100.1"},
		"exit":  {Role: state.Exit, IP: "198.51.100.2", SSH: "admin@198.51.100.2", Via: "entry"},
		"deep":  {Role: state.Exit, IP: "198.51.100.3", SSH: "root@198.51.100.3", Via: "exit"},
	}}
}

// TestJumpChain: the chain is built innermost first, so the dial order is the
// outside-in order a human would type after -J.
func TestJumpChain(t *testing.T) {
	st := chainState()
	got, err := JumpChain(st, "deep")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"entry", "exit"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("chain %v, want %v", got, want)
	}
	if c, err := JumpChain(st, "entry"); err != nil || len(c) != 0 {
		t.Fatalf("the entry needs no jump: %v %v", c, err)
	}
}

// TestJumpChainCycle: a via loop must be an error, not an endless walk.
func TestJumpChainCycle(t *testing.T) {
	st := chainState()
	h := st.Hosts["entry"]
	h.Via = "deep"
	st.Hosts["entry"] = h
	if _, err := JumpChain(st, "deep"); err == nil {
		t.Fatal("a cycle in the via graph was accepted")
	}
}

// TestAddressAndUser: the ssh address is the IP with the port, not the ssh
// string, and the user comes from ssh with root as the fallback.
func TestAddressAndUser(t *testing.T) {
	if got, err := address(state.Host{IP: "203.0.113.9"}); err != nil || got != "203.0.113.9:22" {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := address(state.Host{IP: "203.0.113.9", SSHPort: 2222}); err != nil || got != "203.0.113.9:2222" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := address(state.Host{SSH: "root@x"}); err == nil {
		t.Fatal("a host without an ip got an address")
	}
	if got := user(state.Host{SSH: "admin@203.0.113.9"}); got != "admin" {
		t.Fatalf("user %q", got)
	}
	if got := user(state.Host{SSH: "203.0.113.9"}); got != "root" {
		t.Fatalf("user %q", got)
	}
}

// TestHostKeyAlgorithms: the pinned key type narrows the handshake. Without a
// pin the entry offers ECDSA first, and an ed25519 pin alone is not enough.
func TestHostKeyAlgorithms(t *testing.T) {
	if got := hostKeyAlgorithms("ssh-ed25519 AAAA"); len(got) != 1 || got[0] != "ssh-ed25519" {
		t.Fatalf("%v", got)
	}
	if got := hostKeyAlgorithms(""); got != nil {
		t.Fatalf("%v", got)
	}
}
