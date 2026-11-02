package ops

import (
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// TestSetEntriesKeepsTheGeodata: the editor owns the domains and addresses the
// operator typed, not the geosite names that come from the pinned data.
func TestSetEntriesKeepsTheGeodata(t *testing.T) {
	st := fixture(t)
	st.Sets = map[string]state.Set{"ru-direct": {Geosite: []string{"category-ru"}, Domains: []string{"old.ru"}}}

	msg, err := SetEntries(st, "ru-direct", []string{"gosuslugi.ru", "mos.ru"}, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "2 domains") {
		t.Fatalf("message %q", msg)
	}

	if got := st.Sets["ru-direct"]; len(got.Domains) != 2 || len(got.IPs) != 1 || len(got.Geosite) != 1 {
		t.Fatalf("set after edit: %+v", got)
	}

	if _, err := SetEntries(st, "ru-direct", []string{"not a domain"}, nil); err == nil {
		t.Fatal("a domain with a space was accepted")
	}

	if _, err := SetEntries(st, "ru-direct", nil, []string{"300.1.1.1"}); err == nil {
		t.Fatal("a bogus address was accepted")
	}

	if _, err := SetEntries(st, "ghost", nil, nil); err == nil {
		t.Fatal("an unknown set was edited")
	}
}

// TestInboundSet: a new inbound needs a port, an edit may move it, and only the
// names the renderer writes are accepted.
func TestInboundSet(t *testing.T) {
	st := fixture(t)

	msg, err := InboundSet(st, "a", "ws", 8444, "/ws", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "8444") {
		t.Fatalf("message %q", msg)
	}

	if i := st.Inbounds["a"]["ws"]; i.Port != 8444 || i.Path != "/ws" {
		t.Fatalf("inbound %+v", i)
	}

	if _, err := InboundSet(st, "a", "ws", 0, "", ""); err != nil {
		t.Fatalf("an edit without a port lost it: %v", err)
	}

	if _, err := InboundSet(st, "a", "quic", 443, "", ""); err == nil {
		t.Fatal("an unknown inbound name was accepted")
	}

	if _, err := InboundSet(st, "a", "hy2", 70000, "", ""); err == nil {
		t.Fatal("an out-of-range port was accepted")
	}

	if _, err := InboundSet(st, "a", "ws", 8444, "ws", ""); err == nil {
		t.Fatal("a path without a leading slash was accepted")
	}

	if _, err := InboundSet(st, "ghost", "ws", 8444, "", ""); err == nil {
		t.Fatal("an inbound on an unknown host was accepted")
	}
}

// TestExitPolicy: auto clears both fields, fixed needs an exit that exists.
func TestExitPolicy(t *testing.T) {
	st := fixture(t)

	msg, err := ExitPolicySet(st, "a", "fixed", "b")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "fixed to b") {
		t.Fatalf("message %q", msg)
	}

	if r := st.Routing["a"].Exit; r.Policy != "fixed" || r.Prefer != "b" {
		t.Fatalf("exit %+v", r)
	}

	if _, err := ExitPolicySet(st, "a", "fixed", "a"); err == nil {
		t.Fatal("an entry was accepted as its own exit")
	}

	if _, err := ExitPolicySet(st, "a", "fixed", ""); err == nil {
		t.Fatal("a fixed exit without a host was accepted")
	}

	if msg, err := ExitPolicySet(st, "a", "auto", ""); err != nil || !strings.Contains(msg, "auto") {
		t.Fatalf("auto: %q %v", msg, err)
	}

	if r := st.Routing["a"].Exit; r.Policy != "" || r.Prefer != "" {
		t.Fatalf("auto left fields behind: %+v", r)
	}

	if _, err := ExitPolicySet(st, "a", "sometimes", ""); err == nil {
		t.Fatal("an unknown policy was accepted")
	}
}
