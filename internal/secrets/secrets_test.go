package secrets

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestValuesAreShapedForTheEngine: lengths and alphabets are not cosmetic — a
// short sub id or a padded password breaks a subscription the moment it ships.
func TestValuesAreShapedForTheEngine(t *testing.T) {
	if _, err := uuid.Parse(UUID()); err != nil {
		t.Fatalf("uuid: %v", err)
	}

	sub := SubID()
	if raw, err := base64.RawURLEncoding.DecodeString(sub); err != nil || len(raw) != 12 {
		t.Fatalf("sub id %q: %d bytes, %v", sub, len(raw), err)
	}

	pw := Password()
	if raw, err := base64.RawURLEncoding.DecodeString(pw); err != nil || len(raw) != 24 {
		t.Fatalf("password %q: %d bytes, %v", pw, len(raw), err)
	}

	for _, v := range []string{sub, pw} {
		if strings.ContainsAny(v, "+/=") {
			t.Errorf("%q is not url-safe without padding", v)
		}
	}
}

// TestValuesDiffer: a generator that repeats itself hands the same credentials
// to two people, and a leaked link then works for both.
func TestValuesDiffer(t *testing.T) {
	seen := map[string]bool{}

	for i := 0; i < 100; i++ {
		for _, v := range []string{UUID(), SubID(), Password()} {
			if seen[v] {
				t.Fatalf("a value repeated after %d draws: %q", i, v)
			}

			seen[v] = true
		}
	}
}

// TestMTG: "ee" + 16 random bytes + the cover domain in hex, which is what the
// mtg server parses out of a tg:// link.
func TestMTG(t *testing.T) {
	secret := MTG("example.com")
	if !strings.HasPrefix(secret, "ee") {
		t.Fatalf("%q does not start with ee", secret)
	}

	want := "ee" + strings.Repeat("00", 0) + hexOf("example.com")
	if !strings.HasSuffix(secret, hexOf("example.com")) {
		t.Fatalf("%q does not end with the domain in hex (%s)", secret, want)
	}

	// two calls differ in the random part but agree on the domain
	if MTG("example.com") == secret {
		t.Fatal("two mtg secrets are identical")
	}
}

// TestKeys: the pairs are X25519 and the encodings match what each consumer
// expects — reality reads url-safe without padding, awg standard base64.
func TestKeys(t *testing.T) {
	priv, pub, sid, err := RealityKeys()
	if err != nil {
		t.Fatal(err)
	}

	if len(sid) != 16 {
		t.Errorf("short id %q, want 8 bytes in hex", sid)
	}

	for _, k := range []string{priv, pub} {
		raw, err := base64.RawURLEncoding.DecodeString(k)
		if err != nil || len(raw) != 32 {
			t.Errorf("reality key %q: %d bytes, %v", k, len(raw), err)
		}
	}

	if priv == pub {
		t.Error("the private and public halves are the same string")
	}

	awgPriv, awgPub, err := AWGKeys()
	if err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{awgPriv, awgPub} {
		raw, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(raw) != 32 {
			t.Errorf("awg key %q: %d bytes, %v", k, len(raw), err)
		}
	}
}

func hexOf(s string) string {
	const digits = "0123456789abcdef"

	var b strings.Builder

	for _, c := range []byte(s) {
		b.WriteByte(digits[c>>4])
		b.WriteByte(digits[c&0x0f])
	}

	return b.String()
}
