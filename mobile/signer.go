package mobile

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// Signer is what the device provides and Go cannot: the private key lives in the
// Android Keystore, which never hands it out, so signing happens on the Kotlin
// side and only the signature crosses back. gomobile supports an exported Go
// interface implemented in Kotlin, which is the whole reason this shape exists.
type Signer interface {
	// PublicKeyPEM is PKIX PEM, the same shape /etc/vpnctl/agents/<name>.pub has.
	PublicKeyPEM() (string, error)
	// Sign returns ASN.1 DER over the SHA-256 of message, the way ECDSA signs.
	Sign(message []byte) ([]byte, error)
}

// signer is set once by the UI. A nil signer means the app can read the state
// but cannot push or apply: both need a signature the host can verify.
var signer Signer

// SetSigner wires the device's Keystore-backed key.
func SetSigner(s Signer) { signer = s }

// SignedBy is the key an agent request would be signed with, in the PEM form the
// host keeps in /etc/vpnctl/agents/<name>.pub. An empty answer means no signer.
func SignedBy() (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: this device cannot sign requests yet")
	}

	return signer.PublicKeyPEM()
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
