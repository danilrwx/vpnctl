package mobile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/hostscript"
	"github.com/danilrwx/vpnctl-go/internal/render"
)

// deviceSigner stands in for the Keystore: the same contract, a key the test
// owns, so the request can be checked by the host's own verifier.
type deviceSigner struct{ key *ecdsa.PrivateKey }

func (d deviceSigner) PublicKeyPEM() (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&d.key.PublicKey)
	if err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func (d deviceSigner) PublicKeyDER() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(&d.key.PublicKey)
}

func (d deviceSigner) Sign(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, d.key, digest)
}

// TestTheDeviceMarkPassesTheHostVerifier is the contract the phone and the host
// share: the same payload, hashed once, signed over the digest, checked by the
// agent before it writes anything. A mark that the host refuses would leave the
// marker behind and make the next plan see a stale commit.
func TestTheDeviceMarkPassesTheHostVerifier(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	old := signer
	signer = deviceSigner{key: key}

	t.Cleanup(func() { signer = old })

	files := render.HostFiles{
		"/etc/vpnctl/applied-commit": "abc1234\n",
		"/etc/vpnctl/applied.sha256": "deadbeef /etc/sing-box/config.json\n",
	}

	list, err := agent.FilesFromTree(files, hostscript.ModeForInt)
	if err != nil {
		t.Fatal(err)
	}

	req, err := signedRequest("mark", "ru-spb-1", "abc1234", list)
	if err != nil {
		t.Fatal(err)
	}

	if err := agent.Verify(req, "ru-spb-1", &key.PublicKey, time.Now()); err != nil {
		t.Fatalf("the host would refuse the device's mark: %v", err)
	}
	// the host name is inside the signature: a request replayed at another host
	// has to be refused, or one device could move two markers with one signature
	if err := agent.Verify(req, "nl-exit", &key.PublicKey, time.Now()); err == nil {
		t.Fatal("a mark signed for one host was accepted by another")
	}
}
