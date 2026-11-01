package ops

import (
	"testing"

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
