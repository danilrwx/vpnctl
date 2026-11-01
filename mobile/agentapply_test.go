package mobile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/agent"
)

// TestApplyNeedsASigner: without a Keystore key the app must refuse to build an
// agent request, because an unsigned apply is a request the host drops.
func TestApplyNeedsASigner(t *testing.T) {
	SetSigner(nil)

	_, err := agentApply(nil, "node", "abc1234", nil)
	if err == nil {
		t.Fatal("an apply without a signer was attempted")
	}
}

// TestNonceIsUnique: the host refuses a nonce it has seen, so two requests made
// in the same second must not share one.
func TestNonceIsUnique(t *testing.T) {
	seen := map[string]bool{}

	for i := 0; i < 100; i++ {
		n := nonce()
		if len(n) != 32 {
			t.Fatalf("nonce %q", n)
		}

		if seen[n] {
			t.Fatalf("a nonce repeated after %d draws", i)
		}

		seen[n] = true
	}
}

// TestSignedRequestIsAcceptedByTheAgent verifies the whole contract on the
// device: build, sign through the signer, and check with the agent's verifier.
func TestSignedRequestIsAcceptedByTheAgent(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	SetSigner(&keystoreLike{key: key})
	t.Cleanup(func() { SetSigner(nil) })

	req := &agent.Request{V: agent.Version, Op: "plan", Host: "node", TS: 1, Nonce: "abcdef"}
	req.TreeHash = agent.TreeHash(nil)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyWith(sig, []byte(agent.Message(req))); err != nil {
		t.Fatalf("the host would reject this signature: %v", err)
	}
}
