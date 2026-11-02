package ops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func fixture(t *testing.T) *state.State {
	t.Helper()

	st := &state.State{
		Hosts:    map[string]state.Host{"a": {Role: state.Entry, IP: "203.0.113.1", Domain: "e.example", SSH: "root@203.0.113.1"}, "b": {Role: state.Exit, IP: "203.0.113.2", SSH: "root@203.0.113.2"}},
		Inbounds: map[string]map[string]state.Inbound{"a": {"reality": {Port: 443}}, "b": {"reality": {Port: 443}}},
		Routing:  map[string]state.Routing{"a": {}, "b": {}},
		Users:    map[string]state.User{"u": {Modes: []string{"split"}, Protocols: []string{"reality"}}},
		Links:    []state.Link{{From: "a", To: "b", Protocols: []string{"reality"}}},
		Versions: map[string]state.Pin{},
	}
	st.FillStubSecrets()

	return st
}

func TestUserRotate(t *testing.T) {
	st := fixture(t)

	before := st.Secrets.Users["u"]
	if _, err := UserRotate(st, "u"); err != nil {
		t.Fatal(err)
	}

	after := st.Secrets.Users["u"]
	if after.UUID == before.UUID || after.Hy2Pass == before.Hy2Pass || after.SubID == before.SubID {
		t.Fatal("the credentials did not change")
	}

	if len(after.UUID) != 36 || len(after.SubID) == 0 {
		t.Fatalf("unexpected shapes: %+v", after)
	}

	if _, err := UserRotate(st, "ghost"); err == nil {
		t.Fatal("rotating an unknown user was accepted")
	}
}

func TestHostRemove(t *testing.T) {
	st := fixture(t)
	if _, err := HostRemove(st, "a"); err == nil {
		t.Fatal("removing a host used by a link was accepted")
	}

	st.Links = nil
	if _, err := HostRemove(st, "a"); err != nil {
		t.Fatal(err)
	}

	if _, ok := st.Hosts["a"]; ok {
		t.Fatal("the host is still there")
	}

	if _, ok := st.Inbounds["a"]; ok {
		t.Fatal("the inbound is still there")
	}

	if _, ok := st.Secrets.Hosts["a"]; ok {
		t.Fatal("the secrets are still there")
	}

	if _, err := HostRemove(st, "a"); err == nil {
		t.Fatal("removing a missing host was accepted")
	}
}

func TestKeyBackupRotates(t *testing.T) {
	dir := t.TempDir()
	useKey(t)

	dir = filepath.Join(dir, "state")

	res, err := Init(dir, InitOptions{Name: "laptop", BackupPassphrase: "first passphrase"})
	if err != nil {
		t.Fatal(err)
	}

	before, err := ageio.OpenIdentity(res.BackupBlob, "first passphrase")
	if err != nil {
		t.Fatal(err)
	}

	st, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	blob, _, err := KeyBackup(st, "second passphrase")
	if err != nil {
		t.Fatal(err)
	}

	after, err := ageio.OpenIdentity(blob, "second passphrase")
	if err != nil {
		t.Fatal(err)
	}

	if before.String() == after.String() {
		t.Fatal("the backup identity did not change")
	}

	recs, err := ageio.LoadRecipients(dir)
	if err != nil {
		t.Fatal(err)
	}

	if recs.Has(ageio.RecipientOf(before)) {
		t.Fatal("the old backup is still a recipient")
	}

	if !recs.Has(ageio.RecipientOf(after)) {
		t.Fatal("the new backup is not a recipient")
	}
	// the new backup can read the secrets, the old one cannot
	secrets := filepath.Join(dir, "secrets.age.yaml")

	keyFile := filepath.Join(t.TempDir(), "backup.key")
	if err := ageio.WriteIdentity(keyFile, after); err != nil {
		t.Fatal(err)
	}

	if _, err := ageio.Decrypt(secrets, keyFile); err != nil {
		t.Fatalf("the new backup cannot read the secrets: %v", err)
	}

	old := filepath.Join(t.TempDir(), "old.key")
	if err := ageio.WriteIdentity(old, before); err != nil {
		t.Fatal(err)
	}

	if _, err := ageio.Decrypt(secrets, old); err == nil {
		t.Fatal("the old backup can still read the secrets")
	}

	if _, _, err := KeyBackup(st, ""); err == nil {
		t.Fatal("an empty passphrase was accepted")
	}
}

// TestLinkAddAndSet: a new link names two existing hosts; adding a protocol to
// it and then removing all of them is refused, because a link with no transport
// is not a link.
func TestLinkAddAndSet(t *testing.T) {
	st := fixture(t)

	st.Hosts["c"] = state.Host{Role: state.Exit, IP: "203.0.113.3", SSH: "root@203.0.113.3"}

	if _, err := LinkAdd(st, "a", "ghost", []string{"awg"}, "", ""); err == nil {
		t.Fatal("a link to an unknown host was accepted")
	}

	if _, err := LinkAdd(st, "a", "a", []string{"awg"}, "", ""); err == nil {
		t.Fatal("a link to itself was accepted")
	}

	msg, err := LinkAdd(st, "a", "c", []string{"awg"}, "awg", "auto")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "a->c") {
		t.Fatalf("message %q", msg)
	}

	if _, err := LinkAdd(st, "a", "c", []string{"hy2"}, "", ""); err == nil {
		t.Fatal("a duplicate link was accepted")
	}

	if _, err := LinkAdd(st, "a", "c", []string{"quic"}, "", ""); err == nil {
		t.Fatal("an unknown protocol was accepted")
	}

	// the fixture already carries a->b: extend it and then empty it
	if _, err := LinkSet(st, "a", "b", []string{"hy2"}, nil, "hy2", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := LinkSet(st, "a", "b", nil, []string{"reality", "hy2"}, "", ""); err == nil {
		t.Fatal("a link with no protocols left was accepted")
	}
}
