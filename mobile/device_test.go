package mobile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// agedState builds a real state directory: an age-encrypted secrets file and a
// second device, so removal has something to lock out. It returns the state, the
// key file of this device and the key file of the other one.
func agedState(t *testing.T) (*state.State, string, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "state")

	laptop := filepath.Join(t.TempDir(), "laptop.txt")

	id, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if err := ageio.WriteIdentity(laptop, id); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VPNCTL_AGE_KEY", laptop)

	if _, err := ops.Init(dir, ops.InitOptions{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	phone := filepath.Join(t.TempDir(), "phone.txt")

	phoneID, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if err := ageio.WriteIdentity(phone, phoneID); err != nil {
		t.Fatal(err)
	}

	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ops.KeyAdd(st, "phone", ageio.RecipientOf(phoneID)); err != nil {
		t.Fatal(err)
	}

	return st, laptop, phone
}

// TestRemoveDeviceLocksTheKeyOut: a revoked phone must stop reading the secrets,
// and the device that revoked it must not lose access in the process.
func TestRemoveDeviceLocksTheKeyOut(t *testing.T) {
	st, laptop, phone := agedState(t)

	app, err := Open(st.Dir, laptop)
	if err != nil {
		t.Fatal(err)
	}

	secrets := filepath.Join(st.Dir, "secrets.age.yaml")

	if _, err := ageio.Decrypt(secrets, phone); err != nil {
		t.Fatalf("the phone could not read the state before the revocation: %v", err)
	}

	msg, err := app.RemoveDevice("phone")
	if err != nil {
		t.Fatal(err)
	}

	if msg != "keys: - phone" {
		t.Fatalf("unexpected message %q", msg)
	}

	if _, err := ageio.Decrypt(secrets, phone); err == nil {
		t.Fatal("the revoked phone still reads the secrets")
	}

	if _, err := ageio.Decrypt(secrets, laptop); err != nil {
		t.Fatalf("this device lost access: %v", err)
	}

	recs, err := ageio.LoadRecipients(st.Dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range recs.Recipients {
		if r.Name == "phone" {
			t.Fatal("the phone is still listed in age.yaml")
		}
	}
}

// TestRemoveDeviceRefusesItself: the phone cannot revoke the key it is using,
// because that leaves no device able to open the state.
func TestRemoveDeviceRefusesItself(t *testing.T) {
	st, laptop, _ := agedState(t)

	app, err := Open(st.Dir, laptop)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := app.RemoveDevice("laptop"); err == nil {
		t.Fatal("the device revoked itself")
	}

	if _, err := app.RemoveDevice("ghost"); err == nil {
		t.Fatal("revoking an unknown device was accepted")
	}

	if _, err := ageio.Decrypt(filepath.Join(st.Dir, "secrets.age.yaml"), laptop); err != nil {
		t.Fatalf("the refusal damaged the state: %v", err)
	}
}

// TestRemoveDeviceFromStateWithoutAgeYaml: a fixture dropped in by hand has no
// recipient list, and revoking against it would be a guess about who can read.
func TestRemoveDeviceFromStateWithoutAgeYaml(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "users.yaml"), []byte("users: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{st: &state.State{Dir: dir}, dir: dir}

	if _, err := app.RemoveDevice("phone"); err == nil {
		t.Fatal("a state without age.yaml accepted a revocation")
	}
}
