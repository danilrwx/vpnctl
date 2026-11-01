// Package secrets generates the values the state keeps encrypted.
package secrets

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"github.com/google/uuid"
)

func bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// UUID is the engine account id.
func UUID() string { return uuid.NewString() }

// SubID is the public subscription id: 12 random bytes, base64url without padding.
func SubID() string { return base64.RawURLEncoding.EncodeToString(bytes(12)) }

// Password is the hysteria2 user password: 24 bytes, base64url without padding.
func Password() string { return base64.RawURLEncoding.EncodeToString(bytes(24)) }

// MTG is the fake-TLS MTProto secret: "ee" + 16 bytes + hex of the cover domain.
func MTG(domain string) string {
	return "ee" + hex.EncodeToString(bytes(16)) + hex.EncodeToString([]byte(domain))
}

// x25519Keys generates a keypair in the given base64 flavour.
func x25519Keys(enc *base64.Encoding) (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes()), nil
}

// RealityKeys are the X25519 pair Reality uses (URL-safe, no padding) and a short id.
func RealityKeys() (priv, pub, sid string, err error) {
	priv, pub, err = x25519Keys(base64.RawURLEncoding)
	if err != nil {
		return "", "", "", err
	}
	return priv, pub, hex.EncodeToString(bytes(8)), nil
}

// AWGKeys are the X25519 pair of a WireGuard-style interface (standard base64).
func AWGKeys() (priv, pub string, err error) {
	return x25519Keys(base64.StdEncoding)
}

// Preshared is a 32-byte WireGuard-style pre-shared key in standard base64.
func Preshared() string { return base64.StdEncoding.EncodeToString(bytes(32)) }
