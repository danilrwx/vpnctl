package mobile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
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

// TestSealIdentityRoundTrip: the backup blob is the only way back into a wiped
// phone, so it has to reopen to the very key that sealed it.
func TestSealIdentityRoundTrip(t *testing.T) {
	st, laptop, _ := agedState(t)

	blob, err := SealIdentity("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}

	restored, err := UnsealIdentity(blob, "correct horse battery", st.Dir)
	if err != nil {
		t.Fatal(err)
	}

	want, err := ageio.OwnRecipient(laptop)
	if err != nil {
		t.Fatal(err)
	}

	if restored.Recipient != want {
		t.Fatalf("restored recipient %q, want %q", restored.Recipient, want)
	}

	if _, err := UnsealIdentity(blob, "wrong passphrase", st.Dir); err == nil {
		t.Fatal("a wrong passphrase was accepted")
	}

	if _, err := SealIdentity("short"); err == nil {
		t.Fatal("a passphrase under eight characters was accepted")
	}

	if _, err := UnsealIdentity(blob, "correct horse battery", st.Dir); err != nil {
		t.Fatalf("a second unseal failed: %v", err)
	}
}

// TestUnsealRefusesAForeignIdentity: a backup of some other state would replace
// the phone's key with one that cannot read anything here.
func TestUnsealRefusesAForeignIdentity(t *testing.T) {
	st, _, _ := agedState(t)

	stranger, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	blob, err := ageio.SealIdentity(stranger, "another passphrase")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := UnsealIdentity(blob, "another passphrase", st.Dir); err == nil {
		t.Fatal("an identity that does not decrypt the secrets was accepted")
	}

	// before the first clone there is no secrets file, and the phone has nothing
	// to check a restored key against
	if _, err := UnsealIdentity(blob, "another passphrase", t.TempDir()); err != nil {
		t.Fatalf("a restore before the first clone failed: %v", err)
	}
}

// TestSealIdentityWithoutAKey: the app must say the device has no identity
// rather than seal an empty file.
func TestSealIdentityWithoutAKey(t *testing.T) {
	t.Setenv("VPNCTL_AGE_KEY", filepath.Join(t.TempDir(), "missing.txt"))

	if _, err := SealIdentity("correct horse battery"); err == nil {
		t.Fatal("sealing without an identity was accepted")
	}

	if _, err := UnsealIdentity("not a blob", "correct horse battery", t.TempDir()); err == nil {
		t.Fatal("garbage was accepted as a backup")
	}
}
