package render

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/crypto/ssh"
)

// TestDeviceFiles: one device yields the agent registry PEM and a managed
// authorized_keys line with the forced command.
func TestDeviceFiles(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))

	st := &state.State{Devices: map[string]state.Device{
		"phone": {SSH: line, Age: "age1lepx07qs7qzfhzavhvyn75l9qwflxgszjtcgs2y75nkaphxzxu3se8ufp3"},
	}}

	f, err := deviceFiles(st)
	if err != nil {
		t.Fatal(err)
	}

	body := f["/etc/vpnctl/agents/phone.pub"]

	block, _ := pem.Decode([]byte(body))
	if block == nil {
		t.Fatal("the agent key is not PEM")
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	ec, ok := parsed.(*ecdsa.PublicKey)
	if !ok || !ec.Equal(&key.PublicKey) {
		t.Fatal("the agent key is a different key")
	}

	if !strings.Contains(f["/etc/vpnctl/authorized_keys"], `no-pty,command="/usr/local/bin/vpnctl-agent" `+line+` vpnctl-agent:phone`) {
		t.Fatalf("the block has no forced command for the device:\n%s", f["/etc/vpnctl/authorized_keys"])
	}
}

// TestDeviceFilesWithoutDevices: the block is still emitted, empty, so the
// install script removes a block that was dropped from the state.
func TestDeviceFilesWithoutDevices(t *testing.T) {
	f, err := deviceFiles(&state.State{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(f["/etc/vpnctl/authorized_keys"], "# BEGIN vpnctl devices") {
		t.Fatal("the empty block has no marker")
	}

	if strings.Contains(f["/etc/vpnctl/authorized_keys"], "vpnctl-agent:") {
		t.Fatal("the empty block has a line")
	}
}

// TestDeviceFilesRejectsANonP256Key: the agent verifies ECDSA P-256 only.
func TestDeviceFilesRejectsANonP256Key(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	st := &state.State{Devices: map[string]state.Device{
		"phone": {SSH: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))},
	}}

	if _, err := deviceFiles(st); err == nil {
		t.Fatal("a P-384 key was accepted")
	}
}
