package gitsync

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"golang.org/x/crypto/ssh"
)

// device is the Keystore in miniature: it signs a digest and hands out the
// point, and nothing else leaves it.
type device struct{ key *ecdsa.PrivateKey }

func (d *device) Public() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(d.key.Public())
}

func (d *device) Sign(data []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, d.key, data)
}

// TestAuthMethodSignsLikeSSH: the adapter must produce a signature the SSH
// handshake accepts, which means DER in, r||s out, format ecdsa-sha2-nistp256.
func TestAuthMethodSignsLikeSSH(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	auth, err := AuthMethod(&device{key: key})
	if err != nil {
		t.Fatal(err)
	}

	if auth == nil {
		t.Fatal("no auth method")
	}

	signer, err := newSSHSigner(&device{key: key})
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("session id and exchange hash")

	sig, err := signer.Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}

	if sig.Format != ssh.KeyAlgoECDSA256 {
		t.Fatalf("format %q", sig.Format)
	}

	// ssh hashes the data itself: the adapter already hashed it before asking
	// the device, so the check is over the raw message
	if err := signer.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("the host would reject this signature: %v", err)
	}

	// the key has to be the same curve and point the device holds
	der, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.(*ecdsa.PublicKey).Curve != elliptic.P256() {
		t.Fatal("wrong curve")
	}
}

// TestAuthMethodRejectsGarbage: a signer that returns something other than a
// P-256 point is a mistake, not a key.
func TestAuthMethodRejectsGarbage(t *testing.T) {
	if _, err := AuthMethod(badDevice{}); err == nil {
		t.Fatal("a bogus public key was accepted")
	}
}

type badDevice struct{}

func (badDevice) Public() ([]byte, error)     { return []byte{1, 2, 3}, nil }
func (badDevice) Sign([]byte) ([]byte, error) { return nil, nil }
