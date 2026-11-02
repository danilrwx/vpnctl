package mobile

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/remote"
	"golang.org/x/crypto/ssh"
)

// Signer is what the device provides and Go cannot: the private key lives in the
// Android Keystore, which never hands it out, so signing happens on the Kotlin
// side and only the signature crosses back. gomobile supports an exported Go
// interface implemented in Kotlin, which is the whole reason this shape exists.
type Signer interface {
	// PublicKeyPEM is PKIX PEM, the same shape /etc/vpnctl/agents/<name>.pub has.
	PublicKeyPEM() (string, error)
	// PublicKeyDER is the same key without the PEM envelope, which is what the
	// SSH adapter wants.
	PublicKeyDER() ([]byte, error)
	// Sign returns ASN.1 DER over the SHA-256 of message, the way ECDSA signs.
	Sign(message []byte) ([]byte, error)
}

// signer is set once by the UI. A nil signer means the app can read the state
// but cannot push or apply: both need a signature the host can verify.
var signer Signer

// SetSigner wires the device's Keystore-backed key. The same key is what the
// SSH client uses: a phone has no key file and no agent.
func SetSigner(s Signer) {
	signer = s

	if s == nil {
		remote.SetDeviceAuth(nil)
		gitsync.SetDeviceAuth(nil)

		return
	}

	if sshSigner, err := gitsync.SSHSigner(vpnctlSigner{s}); err == nil {
		remote.SetDeviceAuth(ssh.PublicKeys(sshSigner))
		// the apply guards fetch before they touch a host
		gitsync.SetDeviceAuth(gitsync.GitAuth("git", sshSigner))
	}
}

// SignedBy is the key an agent request would be signed with, in the PEM form the
// host keeps in /etc/vpnctl/agents/<name>.pub. An empty answer means no signer.
func SignedBy() (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: this device cannot sign requests yet")
	}

	return signer.PublicKeyPEM()
}

// AuthorizedKey is the device key as an authorized_keys line: what sshd reads,
// and what GitHub's deploy key field accepts. The PEM SignedBy returns is the
// agent registry's format; neither of those two reads it.
func AuthorizedKey() (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: this device cannot sign requests yet")
	}

	raw, err := signer.PublicKeyDER()
	if err != nil {
		return "", err
	}

	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return "", fmt.Errorf("public key is not PKIX DER: %w", err)
	}

	ec, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("public key is not ECDSA")
	}

	pub, err := ssh.NewPublicKey(ec)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " vpnctl@android", nil
}

// SignMessage is the one place that turns a request payload into a signature, so
// the SHA-256 is applied exactly once and the Kotlin side only does the raw
// ECDSA operation.
func SignMessage(message []byte) ([]byte, error) {
	if signer == nil {
		return nil, fmt.Errorf("no signer: this device cannot sign requests yet")
	}

	sum := sha256.Sum256(message)

	return signer.Sign(sum[:])
}

// verifyWith checks a signature against the signer's own public key. It exists
// so a wrong contract between Kotlin and Go fails here, not on a host at night.
func verifyWith(signature, message []byte) error {
	if signer == nil {
		return fmt.Errorf("no signer")
	}

	body, err := signer.PublicKeyPEM()
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(body))
	if block == nil {
		return fmt.Errorf("public key is not PEM")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}

	pub, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("public key is not ECDSA")
	}

	sum := sha256.Sum256(message)
	if !ecdsa.VerifyASN1(pub, sum[:], signature) {
		return fmt.Errorf("signature does not verify")
	}

	return nil
}

// SyncState clones the state repository with the device's own key: git talks
// SSH, the signature comes from the Keystore, and no key file is ever written.
// The public key must already be a deploy key on the repository.
func SyncState(url, dir string) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: the device cannot authenticate to git")
	}

	sshSigner, err := gitsync.SSHSigner(vpnctlSigner{signer})
	if err != nil {
		return "", err
	}

	repo, err := gitsync.Clone(url, dir, gitsync.GitAuth(gitUser(url), sshSigner))
	if err != nil {
		return "", err
	}

	head, err := repo.Head()
	if err != nil {
		return "", err
	}

	return head, nil
}

// gitUser is the account part of git@host:path, "git" for GitHub and GitLab.
func gitUser(url string) string {
	if at := strings.IndexByte(url, '@'); at > 0 {
		return url[:at]
	}

	return "git"
}

// vpnctlSigner adapts the gomobile Signer to what gitsync expects: the same two
// calls, PKIX DER out.
type vpnctlSigner struct{ s Signer }

func (v vpnctlSigner) Public() ([]byte, error) { return v.s.PublicKeyDER() }

func (v vpnctlSigner) Sign(digest []byte) ([]byte, error) { return v.s.Sign(digest) }
