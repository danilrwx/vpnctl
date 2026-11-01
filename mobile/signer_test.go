package mobile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/agent"
)

// keystoreLike is the Kotlin side in miniature: it holds the key, signs the
// digest, and hands out only the public half.
type keystoreLike struct {
	key *ecdsa.PrivateKey
}

func (k *keystoreLike) PublicKeyPEM() (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&k.key.PublicKey)
	if err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func (k *keystoreLike) Sign(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, k.key, digest)
}

// TestSignatureMatchesTheAgent: the payload the phone signs is the exact string
// the host verifies, and a wrong hash on either side must fail here.
func TestSignatureMatchesTheAgent(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	SetSigner(&keystoreLike{key: key})
	t.Cleanup(func() { SetSigner(nil) })

	pemPub, err := SignedBy()
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := agent.ParsePublicKey([]byte(pemPub))
	if err != nil {
		t.Fatalf("the host would not accept this key: %v", err)
	}

	// compare encodings: the raw coordinates are deprecated in Go 1.26
	wantPEM, err := (&keystoreLike{key: key}).PublicKeyPEM()
	if err != nil {
		t.Fatal(err)
	}

	gotPEM, err := x509.MarshalPKIXPublicKey(parsed)
	if err != nil {
		t.Fatal(err)
	}

	wantDER, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		t.Fatal(err)
	}

	if string(gotPEM) != string(wantDER) || pemPub != wantPEM {
		t.Fatal("a different key came back")
	}

	req, err := agent.NewRequest(key, "apply", "node", "abc1234", nil)
	if err != nil {
		t.Fatal(err)
	}

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyWith(sig, []byte(agent.Message(req))); err != nil {
		t.Fatalf("the signature does not match the request: %v", err)
	}
}

// TestNoSigner: without a key the app can read the state and must say plainly
// that it cannot sign, instead of producing an empty signature.
func TestNoSigner(t *testing.T) {
	SetSigner(nil)

	if _, err := SignedBy(); err == nil {
		t.Fatal("an empty key string was returned")
	}

	if _, err := SignMessage([]byte("x")); err == nil {
		t.Fatal("an unsigned message was accepted")
	}
}

// TestSelfTest: the check the screen runs says yes with a working key and says
// exactly what is wrong without one.
func TestSelfTest(t *testing.T) {
	SetSigner(nil)
	if got := SelfTest(); got != "no signer" {
		t.Fatalf("%q", got)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	SetSigner(&keystoreLike{key: key})
	t.Cleanup(func() { SetSigner(nil) })

	if got := SelfTest(); !strings.HasPrefix(got, "signed and verified:") {
		t.Fatalf("%q", got)
	}
}
