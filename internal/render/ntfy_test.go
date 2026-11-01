package render

import (
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func ntfyFixture(serve string) *state.State {
	st := &state.State{
		Hosts: map[string]state.Host{
			"node":  {Role: state.Entry, Engine: "sing-box", IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"},
			"other": {Role: state.Entry, Engine: "sing-box", IP: "203.0.113.11", Domain: "other.example", SSH: "root@203.0.113.11"},
			"exit":  {Role: state.Exit, Engine: "sing-box", IP: "203.0.113.12", SSH: "root@203.0.113.12"},
		},
		Inbounds: map[string]map[string]state.Inbound{
			"node":  {"reality": {Port: 443}},
			"other": {"reality": {Port: 443}},
			"exit":  {"reality": {Port: 443}},
		},
		Routing:  map[string]state.Routing{},
		Sets:     map[string]state.Set{},
		Versions: map[string]state.Pin{},
	}
	st.Alerts = state.Alerts{Ntfy: "https://ntfy.vpn.example/vpnctl-abc", Serve: serve}
	st.FillStubSecrets()
	return st
}

func TestNtfyServedByTheEntry(t *testing.T) {
	files, err := RenderHost(ntfyFixture("node"), "node")
	if err != nil {
		t.Fatal(err)
	}
	server, ok := files["/etc/ntfy/server.yml"]
	if !ok {
		t.Fatal("the entry that serves ntfy has no server config")
	}
	for _, want := range []string{
		`base-url: "https://ntfy.vpn.example"`,
		`listen-http: "127.0.0.1:2586"`,
		`cache-file: "/var/lib/ntfy/cache.db"`,
		`web-root: "disable"`,
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server.yml lacks %q:\n%s", want, server)
		}
	}
	if _, ok := files["/etc/systemd/system/ntfy.service"]; !ok {
		t.Error("no unit for the notification server")
	}
	caddy := files["/etc/caddy/Caddyfile"]
	if !strings.Contains(caddy, "https://ntfy.vpn.example:8443") {
		t.Errorf("caddy does not serve the ntfy name:\n%s", caddy)
	}
	if !strings.Contains(caddy, "reverse_proxy 127.0.0.1:2586") {
		t.Errorf("caddy does not forward to ntfy:\n%s", caddy)
	}
	env := files["/etc/vpnctl/alerts.env"]
	if !strings.Contains(env, `VPNCTL_ALERTS_LOCAL="http://127.0.0.1:2586/vpnctl-abc"`) {
		t.Errorf("alerts.env has no loopback topic:\n%s", env)
	}
	if !strings.Contains(files["/etc/vpnctl/probe.sh"], "VPNCTL_ALERTS_LOCAL") {
		t.Error("the probe ignores the loopback topic")
	}
}

// TestNtfyOnlyOnItsHost: every entry renders a Caddyfile, but only the one named
// in alerts.yaml installs a notification server.
func TestNtfyOnlyOnItsHost(t *testing.T) {
	for _, host := range []string{"other", "exit"} {
		files, err := RenderHost(ntfyFixture("node"), host)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := files["/etc/ntfy/server.yml"]; ok {
			t.Errorf("%s runs a notification server it was not asked to run", host)
		}
		if strings.Contains(files["/etc/caddy/Caddyfile"], "ntfy.") {
			t.Errorf("%s has an ntfy site", host)
		}
	}
}

func TestNtfyOff(t *testing.T) {
	st := ntfyFixture("")
	st.Alerts = state.Alerts{}
	files, err := RenderHost(st, "node")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["/etc/ntfy/server.yml"]; ok {
		t.Error("a server was rendered without a topic")
	}
	if !strings.Contains(files["/etc/vpnctl/alerts.env"], `VPNCTL_ALERTS_LOCAL=""`) {
		t.Error("alerts.env has a loopback topic without a server")
	}
}

// TestNtfyLocal: the probe must reach the server without the public ingress.
func TestNtfyLocal(t *testing.T) {
	cases := map[string]string{
		"https://ntfy.vpn.example/vpnctl-abc": "http://127.0.0.1:2586/vpnctl-abc",
		"https://ntfy.sh/vpnctl-abc":          "http://127.0.0.1:2586/vpnctl-abc",
		"not a url":                           "",
		"https://ntfy.vpn.example":            "",
	}
	for in, want := range cases {
		if got := NtfyLocal(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
