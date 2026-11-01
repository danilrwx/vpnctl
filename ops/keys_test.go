package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danilrwx/vpnctl-go/ageio"
	"github.com/danilrwx/vpnctl-go/state"
)

// useKey points the age identity lookup at a temporary file.
func useKey(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key.txt")
	t.Setenv("VPNCTL_AGE_KEY", p)
	return p
}

func TestInit(t *testing.T) {
	useKey(t)
	dir := filepath.Join(t.TempDir(), "state")
	res, err := Init(dir, InitOptions{Name: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recipient == "" || res.BackupBlob != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	for _, f := range []string{"hosts.yaml", "links.yaml", "users.yaml", "protocols.yaml", "versions.yaml", "age.yaml", "secrets.age.yaml", "sets/ads.yaml", "sets/ru-direct.yaml", ".gitignore"} {
		if _, err := state.Load(dir); err != nil {
			t.Fatalf("load: %v", err)
		}
		if !fileExists(filepath.Join(dir, f)) {
			t.Errorf("missing %s", f)
		}
	}
	recs, err := ageio.LoadRecipients(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs.Recipients) != 1 || recs.Recipients[0].Name != "laptop" {
		t.Fatalf("recipients: %+v", recs)
	}
	if _, err := ageio.Decrypt(filepath.Join(dir, "secrets.age.yaml"), state.KeyPath()); err != nil {
		t.Fatalf("the created state is not decryptable: %v", err)
	}
	if _, err := Init(dir, InitOptions{Name: "again"}); err == nil {
		t.Fatal("a second init over the same directory was accepted")
	}
}

func TestInitWithBackup(t *testing.T) {
	useKey(t)
	dir := filepath.Join(t.TempDir(), "state")
	res, err := Init(dir, InitOptions{Name: "phone", BackupPassphrase: "correct horse battery"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupBlob == "" {
		t.Fatal("no backup blob")
	}
	id, err := ageio.OpenIdentity(res.BackupBlob, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	recs, err := ageio.LoadRecipients(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !recs.Has(ageio.RecipientOf(id)) {
		t.Fatal("the backup recipient is not in age.yaml")
	}
	if recs.Names()[0] != "backup" || len(recs.Recipients) != 2 {
		t.Fatalf("recipients: %+v", recs.Names())
	}
}

func TestKeyAddAndRemove(t *testing.T) {
	useKey(t)
	dir := filepath.Join(t.TempDir(), "state")
	if _, err := Init(dir, InitOptions{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	phone, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	phoneKey := filepath.Join(t.TempDir(), "phone.txt")
	if err := ageio.WriteIdentity(phoneKey, phone); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := KeyAdd(st, "phone", ageio.RecipientOf(phone)); err != nil {
		t.Fatal(err)
	}
	secrets := filepath.Join(dir, "secrets.age.yaml")
	if _, err := ageio.Decrypt(secrets, phoneKey); err != nil {
		t.Fatalf("the phone cannot read the re-encrypted secrets: %v", err)
	}
	keys, err := Keys(st)
	if err != nil {
		t.Fatal(err)
	}
	marked := 0
	for _, k := range keys {
		if k.This {
			marked++
			if k.Name != "laptop" {
				t.Fatalf("the wrong device is marked: %s", k.Name)
			}
		}
	}
	if marked != 1 {
		t.Fatalf("%d devices marked as this one", marked)
	}
	if _, err := KeyRemove(st, "phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := ageio.Decrypt(secrets, phoneKey); err == nil {
		t.Fatal("the removed device can still read the secrets")
	}
	if _, err := ageio.Decrypt(secrets, state.KeyPath()); err != nil {
		t.Fatalf("the remaining device lost access: %v", err)
	}
	if _, err := KeyRemove(st, "ghost"); err == nil {
		t.Fatal("removing a missing recipient was accepted")
	}
}

func TestKeyInitOnExistingState(t *testing.T) {
	useKey(t)
	dir := filepath.Join(t.TempDir(), "state")
	if _, err := Init(dir, InitOptions{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := KeyInit(st, "laptop"); err == nil {
		t.Fatal("key init over an existing age.yaml was accepted")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
