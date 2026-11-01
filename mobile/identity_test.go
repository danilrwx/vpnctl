package mobile

import (
	"strings"
	"testing"
)

// TestNewIdentityRoundTrips: the pair the phone will store has to decrypt what
// it names, because the operator adds that recipient to the real age.yaml.
func TestNewIdentityRoundTrips(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(id.Recipient, "age1") {
		t.Fatalf("recipient %q", id.Recipient)
	}

	if !strings.HasPrefix(id.Secret, "AGE-SECRET-KEY-1") {
		t.Fatalf("secret %q", id.Secret)
	}

	got, err := RoundTrip(id.Recipient, id.Secret)
	if err != nil {
		t.Fatal(err)
	}

	if got != "identity works" {
		t.Fatalf("%q", got)
	}
}

// TestRoundTripRejectsAMismatch: a secret filed under somebody else's recipient
// would silently make the state unreadable, so it is refused.
func TestRoundTripRejectsAMismatch(t *testing.T) {
	a, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	b, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := RoundTrip(a.Recipient, b.Secret); err == nil {
		t.Fatal("a foreign secret was accepted")
	}

	if _, err := RoundTrip(a.Recipient, "not a key"); err == nil {
		t.Fatal("garbage was accepted")
	}
}
