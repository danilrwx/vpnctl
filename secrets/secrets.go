// Package secrets generates the values the state keeps encrypted.
package secrets

import (
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
