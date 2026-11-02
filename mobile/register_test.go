package mobile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// TestRegisterDevice: the phone writes both of its public halves to the state,
// so the laptop can put the ssh key on the hosts and the age key in age.yaml.
func TestRegisterDevice(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	SetSigner(&keystoreLike{key: key})
	t.Cleanup(func() { SetSigner(nil) })

	app := openFixture(t)

	id, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	ageKey := ageio.RecipientOf(id)

	msg, err := app.RegisterDevice("phone", ageKey)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "phone") {
		t.Fatalf("message does not name the device: %q", msg)
	}

	st, err := state.Load(app.dir)
	if err != nil {
		t.Fatal(err)
	}

	d, ok := st.Devices["phone"]
	if !ok {
		t.Fatal("devices.yaml has no phone")
	}

	if !strings.HasPrefix(d.SSH, "ecdsa-sha2-nistp256 ") {
		t.Fatalf("the stored key is not an OpenSSH line: %q", d.SSH)
	}

	if d.Age != ageKey {
		t.Fatalf("age recipient = %q, want %q", d.Age, ageKey)
	}
}

// TestRegisterDeviceRejectsABadName: the name is a file name and an
// authorized_keys comment, so a slash or a space must fail before the save.
func TestRegisterDeviceRejectsABadName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	SetSigner(&keystoreLike{key: key})
	t.Cleanup(func() { SetSigner(nil) })

	app := openFixture(t)

	if _, err := app.RegisterDevice("bad/name", ""); err == nil {
		t.Fatal("a name with a slash was accepted")
	}

	if _, err := app.RegisterDevice("bad name", ""); err == nil {
		t.Fatal("a name with a space was accepted")
	}
}
