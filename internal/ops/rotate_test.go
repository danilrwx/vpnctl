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

// TestRoutingRules: the chain is the decision and its order is part of it, so a
// new rule goes to the end, a move is one step, and a toggle keeps the rule.
func TestRoutingRules(t *testing.T) {
	st := fixture(t)
	st.Sets = map[string]state.Set{"ru-direct": {Geosite: []string{"category-ru"}}}

	if _, err := RuleAdd(st, "a", "split", RuleInput{Sets: []string{"ru-direct"}, Action: "direct"}); err != nil {
		t.Fatal(err)
	}

	if _, err := RuleAdd(st, "a", "split", RuleInput{Sets: []string{"ghost"}, Action: "direct"}); err == nil {
		t.Fatal("a rule over an unknown set was accepted")
	}

	if _, err := RuleAdd(st, "a", "split", RuleInput{Action: "direct"}); err == nil {
		t.Fatal("an empty catch-all was accepted")
	}

	if _, err := RuleAdd(st, "a", "split", RuleInput{Sets: []string{"ru-direct"}, Action: "haproxy"}); err == nil {
		t.Fatal("an unknown action was accepted")
	}

	if _, err := RuleAdd(st, "a", "nope", RuleInput{Sets: []string{"ru-direct"}, Action: "direct"}); err == nil {
		t.Fatal("an unknown chain was accepted")
	}

	if len(st.Routing["a"].Groups.Split) != 1 {
		t.Fatalf("split rules: %+v", st.Routing["a"].Groups.Split)
	}

	if _, err := RuleToggle(st, "a", "split", 0, false); err != nil {
		t.Fatal(err)
	}

	if st.Routing["a"].Groups.Split[0].IsEnabled() {
		t.Fatal("the rule is still enabled")
	}

	if _, err := RuleMove(st, "a", "split", 0, true); err == nil {
		t.Fatal("the first rule moved up")
	}

	if _, err := RuleRemove(st, "a", "split", 5); err == nil {
		t.Fatal("a rule that does not exist was removed")
	}

	if _, err := RuleRemove(st, "a", "split", 0); err != nil {
		t.Fatal(err)
	}

	if len(st.Routing["a"].Groups.Split) != 0 {
		t.Fatal("the rule survived removal")
	}
}

// TestInboundToggle: an inbound flips in the state, and the switch is the same
// one validate and the render read.
func TestInboundToggle(t *testing.T) {
	st := fixture(t)

	// reality on the entry carries the user and the link: the switch refuses
	if _, err := InboundToggle(st, "a", "reality", false); err == nil {
		t.Fatal("an inbound in use was turned off")
	}

	// ws exists but nobody depends on it yet
	st.Inbounds["a"]["ws"] = state.Inbound{Port: 8444}

	if _, err := InboundToggle(st, "a", "ws", false); err != nil {
		t.Fatal(err)
	}

	if st.Inbounds["a"]["ws"].IsEnabled() {
		t.Fatal("the inbound is still enabled")
	}

	if _, err := InboundToggle(st, "a", "ws", true); err != nil {
		t.Fatal(err)
	}

	if !st.Inbounds["a"]["ws"].IsEnabled() {
		t.Fatal("the inbound did not come back")
	}

	if _, err := InboundToggle(st, "a", "quic", true); err == nil {
		t.Fatal("an unknown inbound was toggled")
	}

	if _, err := InboundToggle(st, "ghost", "reality", true); err == nil {
		t.Fatal("an unknown host was toggled")
	}
}
