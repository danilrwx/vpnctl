package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// TestHostKeyRefusedWhenUnpinned: an empty host_key is not consent. Accepting
// silently is a man in the middle with extra steps.
func TestHostKeyRefusedWhenUnpinned(t *testing.T) {
	key := testKey(t)
	cb := hostKeyCallback("", false)

	err := cb("203.0.113.1:22", nil, key)
	if err == nil {
		t.Fatal("an unpinned host was accepted")
	}

	if !strings.Contains(err.Error(), "SHA256:") || !strings.Contains(err.Error(), "--accept-host-key") {
		t.Errorf("the refusal does not let a human decide: %v", err)
	}

	if err := hostKeyCallback("", true)("203.0.113.1:22", nil, key); err != nil {
		t.Errorf("the explicit first contact was refused: %v", err)
	}
}

// TestHostKeyMismatch: a recorded key is the only one that may answer.
func TestHostKeyMismatch(t *testing.T) {
	pinned := "ssh-ed25519 " + "AAAA" // deliberately wrong

	cb := hostKeyCallback(pinned, true)
	if err := cb("203.0.113.1:22", nil, testKey(t)); err == nil {
		t.Fatal("a mismatched key was accepted")
	}

	key := testKey(t)

	good := key.Type() + " " + base64Std(key.Marshal())
	if err := hostKeyCallback(good, false)("203.0.113.1:22", nil, key); err != nil {
		t.Fatalf("the pinned key was refused: %v", err)
	}
}
