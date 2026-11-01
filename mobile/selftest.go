package mobile

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"

	"github.com/danilrwx/vpnctl-go/internal/agent"
)

// SelfTest proves the device can do what a host will ask of it: build a request,
// sign it with the Keystore key, and have the host's own verifier accept it.
// The screen shows the answer — a key that cannot sign is worse than no key,
// and this is the cheapest place to find out.
func SelfTest() string {
	if signer == nil {
		return "no signer"
	}

	// the public key is the operator's key to the host, so the request is
	// signed with it exactly as a real one would be
	body, err := signer.PublicKeyPEM()
	if err != nil {
		return "key: " + err.Error()
	}

	pub, err := agent.ParsePublicKey([]byte(body))
	if err != nil {
		return "key is not one the host would accept: " + err.Error()
	}

	req := &agent.Request{V: agent.Version, Op: "plan", Host: "selftest", TS: 1, Nonce: "selftest"}
	req.TreeHash = agent.TreeHash(nil)

	msg := []byte(agent.Message(req))

	sig, err := SignMessage(msg)
	if err != nil {
		return "sign: " + err.Error()
	}

	sum := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(pub, sum[:], sig) {
		return "verify: the host would reject this signature"
	}

	digest := sha256.Sum256(sig)

	return "signed and verified: " + hex.EncodeToString(digest[:4])
}
