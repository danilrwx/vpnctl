package render

import (
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func expiryFixture() *state.State {
	st := &state.State{
		Hosts: map[string]state.Host{
			"node": {Role: state.Entry, Engine: "sing-box", IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"},
		},
		Users: map[string]state.User{
			"goner": {Modes: []string{"split", "full"}, Protocols: []string{"reality", "ws"}, Expires: "2026-06-01", TG: true},
			"stays": {Modes: []string{"split"}, Protocols: []string{"reality"}},
		},
		Inbounds: map[string]map[string]state.Inbound{
			"node": {"reality": {Port: 443}, "ws": {Port: 8444, Path: "/ws"}},
		},
		Routing:  map[string]state.Routing{},
		Sets:     map[string]state.Set{},
		Versions: map[string]state.Pin{},
	}
	st.FillStubSecrets()

	return st
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

// TestExpiredUserIsGone: an account works through the day before its date and
// disappears from the engine, the subscriptions and the expiry file on the date
// itself — the same rule the agent's tick applies on the host.
func TestExpiredUserIsGone(t *testing.T) {
	st := expiryFixture()

	before, err := RenderHost(st, "node", day(2026, 5, 31))
	if err != nil {
		t.Fatal(err)
	}

	cfg := before["/etc/sing-box/config.json"]
	for _, email := range []string{"goner-split-reality", "goner-full-ws", "stays-split-reality"} {
		if !strings.Contains(cfg, email) {
			t.Errorf("the day before expiry %s is missing from the engine", email)
		}
	}

	sub := "/var/www/sub/STUB-sub-goner/split"
	if _, ok := before[sub]; !ok {
		t.Errorf("the day before expiry %s is missing", sub)
	}

	exp := before["/etc/vpnctl/expiry"]
	for _, want := range []string{"goner:", `expires: "2026-06-01"`, "sub_id: STUB-sub-goner", "- goner-split-reality"} {
		if !strings.Contains(exp, want) {
			t.Errorf("the expiry file lacks %q:\n%s", want, exp)
		}
	}

	after, err := RenderHost(st, "node", day(2026, 6, 1))
	if err != nil {
		t.Fatal(err)
	}

	cfg = after["/etc/sing-box/config.json"]
	for _, email := range []string{"goner-split-reality", "goner-full-ws"} {
		if strings.Contains(cfg, email) {
			t.Errorf("on the expiry date %s is still in the engine", email)
		}
	}

	if !strings.Contains(cfg, "stays-split-reality") {
		t.Error("the other user vanished too")
	}

	if _, ok := after[sub]; ok {
		t.Errorf("on the expiry date %s is still served", sub)
	}

	if strings.Contains(after["/etc/vpnctl/expiry"], "goner") {
		t.Error("an expired user is still in the expiry file")
	}
}

// TestExpiryFileCarriesThePeer: the agent cannot read the state, so the peer
// address has to travel with the plan.
func TestExpiryFileCarriesThePeer(t *testing.T) {
	st := expiryFixture()
	u := st.Users["goner"]
	u.AWG = &state.Peer{Mode: "split", IP: "10.8.1.2"}
	u.Protocols = append(u.Protocols, "awg")
	st.Users["goner"] = u
	inb := st.Inbounds["node"]
	inb["awg"] = state.Inbound{Port: 51820, Subnets: &state.Subnets{Split: "10.8.1.0/24", Full: "10.8.2.0/24"}}
	st.Inbounds["node"] = inb

	files, err := RenderHost(st, "node", day(2026, 5, 31))
	if err != nil {
		t.Fatal(err)
	}

	exp := files["/etc/vpnctl/expiry"]
	if !strings.Contains(exp, "awg_ip: 10.8.1.2") {
		t.Errorf("the peer address is not in the plan:\n%s", exp)
	}
	// awg is not an engine identity: the peer address is what the agent needs,
	// and no awg email may appear in the engine list
	if strings.Contains(exp, "-goner-split-awg") {
		t.Errorf("awg is not an engine user but is listed as one:\n%s", exp)
	}
}
