package gitsync

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

// KeystoreSigner is what a device can offer: the private key never leaves the
// Android Keystore, so the signature is produced outside Go and arrives here as
// two numbers.
type KeystoreSigner interface {
	// Public returns the key as PKIX DER, which is what AndroidKeyStore's
	// getEncoded() gives: a point would have to be assembled by hand.
	Public() ([]byte, error)
	// Sign returns the DER signature over the SHA-256 of data.
	Sign(data []byte) ([]byte, error)
}

// AuthMethod turns a Keystore signer into what go-git and ssh want, so a phone
// can push to the state repository without a key file on disk.
func AuthMethod(s KeystoreSigner) (ssh.AuthMethod, error) {
	signer, err := SSHSigner(s)
	if err != nil {
		return nil, err
	}

	return ssh.PublicKeys(signer), nil
}

// SSHSigner is the same adapter as AuthMethod, exposed as the signer go-git's
// transport wants to hold directly.
func SSHSigner(s KeystoreSigner) (ssh.Signer, error) {
	return newSSHSigner(s)
}

// GitAuth is what go-git needs: its own auth type around an ssh signer, with a
// user name taken from the remote URL (git@github.com:…).
func GitAuth(user string, s ssh.Signer) transport.AuthMethod {
	return &gogitssh.PublicKeys{
		User:   user,
		Signer: s,
		HostKeyCallbackHelper: gogitssh.HostKeyCallbackHelper{
			// the remote is a known git host; pinning it would need a known_hosts
			// file the phone does not have
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	}
}

// sshSigner is the adapter: ssh wants a Signer, the device gives two calls.
type sshSigner struct {
	s   KeystoreSigner
	pub ssh.PublicKey
}

func newSSHSigner(s KeystoreSigner) (*sshSigner, error) {
	raw, err := s.Public()
	if err != nil {
		return nil, err
	}

	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("public key is not PKIX DER: %w", err)
	}

	ec, ok := parsed.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, fmt.Errorf("public key is not P-256")
	}

	pub, err := ssh.NewPublicKey(ec)
	if err != nil {
		return nil, err
	}

	return &sshSigner{s: s, pub: pub}, nil
}

// PublicKey is what goes into authorized_keys and into the request signature.
func (s *sshSigner) PublicKey() ssh.PublicKey { return s.pub }

// Sign hashes, asks the device, and wraps the DER signature in the format ssh
// expects. Turning DER into r||s is the only thing that has to happen here.
func (s *sshSigner) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	sum := sha256.Sum256(data)

	der, err := s.s.Sign(sum[:])
	if err != nil {
		return nil, err
	}

	r, ss, err := parseSig(der)
	if err != nil {
		return nil, err
	}

	blob := ssh.Marshal(struct{ R, S *big.Int }{r, ss})

	return &ssh.Signature{Format: ssh.KeyAlgoECDSA256, Blob: blob}, nil
}

// parseSig reads a DER ECDSA signature into its two numbers.
func parseSig(der []byte) (*big.Int, *big.Int, error) {
	var sig struct {
		R, S *big.Int
	}

	if _, err := asn1.Unmarshal(der, &sig); err != nil || sig.R == nil || sig.S == nil {
		return nil, nil, fmt.Errorf("signature is not DER ECDSA")
	}

	return sig.R, sig.S, nil
}
