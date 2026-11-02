package ops

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"golang.org/x/crypto/ssh"
)

// TestDeviceSetAndSync: a device carries its ssh key and age recipient; the
// sync turns the recipient into an age.yaml entry without a second paste.
func TestDeviceSetAndSync(t *testing.T) {
	useKey(t)

	dir := filepath.Join(t.TempDir(), "state")
	if _, err := Init(dir, InitOptions{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " vpnctl@android"

	id, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	ageKey := ageio.RecipientOf(id)

	if _, err := DeviceSet(st, "phone", line, ageKey); err != nil {
		t.Fatal(err)
	}

	added, err := DeviceSync(st)
	if err != nil {
		t.Fatal(err)
	}

	if len(added) != 1 || !strings.Contains(added[0], "phone") {
		t.Fatalf("added = %v", added)
	}

	recs, err := ageio.LoadRecipients(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, r := range recs.Recipients {
		if r.Name == "phone" && r.Key == ageKey {
			found = true
		}
	}

	if !found {
		t.Fatalf("phone is not a recipient: %+v", recs.Recipients)
	}

	again, err := DeviceSync(st)
	if err != nil {
		t.Fatal(err)
	}

	if len(again) != 0 {
		t.Fatalf("a second sync added %v", again)
	}
}

// TestDeviceSetRejectsBadInput: the name is a path component, the key must be
// P-256, and the age recipient must parse.
func TestDeviceSetRejectsBadInput(t *testing.T) {
	st := &state.State{Dir: t.TempDir()}

	if _, err := DeviceSet(st, "bad/name", "ecdsa-sha2-nistp256 AAAA", ""); err == nil {
		t.Fatal("a name with a slash was accepted")
	}

	if _, err := DeviceSet(st, "phone", "not a key", ""); err == nil {
		t.Fatal("a bogus ssh key was accepted")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))

	if _, err := DeviceSet(st, "phone", line, "not-an-age-recipient"); err == nil {
		t.Fatal("a bogus age recipient was accepted")
	}
}
