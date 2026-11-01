package ageio

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

func newID(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEncryptToSeveralRecipients(t *testing.T) {
	dir := t.TempDir()
	a, b := newID(t), newID(t)
	path := filepath.Join(dir, "secrets.age.yaml")
	plain := []byte("users: {}\n")
	if err := EncryptTo(path, []age.Recipient{a.Recipient(), b.Recipient()}, plain); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]*age.X25519Identity{"a": a, "b": b} {
		key := filepath.Join(dir, name+".key")
		if err := WriteIdentity(key, id); err != nil {
			t.Fatal(err)
		}
		got, err := Decrypt(path, key)
		if err != nil {
			t.Fatalf("%s cannot decrypt: %v", name, err)
		}
		if string(got) != string(plain) {
			t.Fatalf("%s: %q", name, got)
		}
	}
	// a third identity must not read it
	c := newID(t)
	key := filepath.Join(dir, "c.key")
	if err := WriteIdentity(key, c); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(path, key); err == nil {
		t.Fatal("an unlisted identity decrypted the file")
	}
}

func TestEncryptToRefusesEmpty(t *testing.T) {
	err := EncryptTo(filepath.Join(t.TempDir(), "x"), nil, []byte("x"))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestRecipientsFile(t *testing.T) {
	dir := t.TempDir()
	a, b := newID(t), newID(t)
	r := &Recipients{}
	if err := r.Add("phone", RecipientOf(a)); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("laptop", RecipientOf(b)); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRecipients(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := got.Names()
	if len(names) != 2 || names[0] != "laptop" || names[1] != "phone" {
		t.Fatalf("names: %v", names)
	}
	if !got.Has(RecipientOf(a)) || got.Has("age1nope") {
		t.Fatal("Has is wrong")
	}
}

func TestRecipientsAddRules(t *testing.T) {
	a := newID(t)
	r := &Recipients{}
	if err := r.Add("Phone", RecipientOf(a)); err == nil {
		t.Fatal("an invalid name was accepted")
	}
	if err := r.Add("phone", "age1nope"); err == nil {
		t.Fatal("an invalid key was accepted")
	}
	if err := r.Add("phone", RecipientOf(a)); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("phone", RecipientOf(a)); err != nil {
		t.Fatalf("the same key under the same name must be a no-op: %v", err)
	}
	if err := r.Add("phone", RecipientOf(newID(t))); err == nil {
		t.Fatal("a second key under the same name was accepted")
	}
}

func TestRecipientsRemove(t *testing.T) {
	a, b := newID(t), newID(t)
	r := &Recipients{}
	_ = r.Add("phone", RecipientOf(a))
	if _, err := r.Remove("phone"); err == nil {
		t.Fatal("the last recipient was removed")
	}
	_ = r.Add("laptop", RecipientOf(b))
	ok, err := r.Remove("phone")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if r.Has(RecipientOf(a)) || len(r.Recipients) != 1 {
		t.Fatalf("%+v", r)
	}
	if ok, _ := r.Remove("ghost"); ok {
		t.Fatal("removed a missing recipient")
	}
}

func TestRecipientsValidate(t *testing.T) {
	a := newID(t)
	r := &Recipients{Recipients: []Recipient{
		{Name: "a", Key: RecipientOf(a)},
		{Name: "a", Key: RecipientOf(a)},
	}}
	if err := r.Validate(); err == nil {
		t.Fatal("a duplicate name passed validation")
	}
}

func TestSealOpenIdentity(t *testing.T) {
	id := newID(t)
	blob, err := SealIdentity(id, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	back, err := OpenIdentity(blob, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if back.String() != id.String() {
		t.Fatal("identity changed on the round trip")
	}
	if _, err := OpenIdentity(blob, "wrong passphrase"); err == nil {
		t.Fatal("a wrong passphrase was accepted")
	}
	if _, err := SealIdentity(id, "short"); err == nil {
		t.Fatal("a short passphrase was accepted")
	}
}

func TestWriteIdentityPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	if err := WriteIdentity(path, newID(t)); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if _, err := Identity(path); err != nil {
		t.Fatalf("the written key is unreadable: %v", err)
	}
}

func TestLoadRecipientsMissing(t *testing.T) {
	r, err := LoadRecipients(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("a missing age.yaml must return nil")
	}
}
