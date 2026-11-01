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
	files, err := RenderHost(ntfyFixture("node"), "node", testDay())
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
		files, err := RenderHost(ntfyFixture("node"), host, testDay())
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

	files, err := RenderHost(st, "node", testDay())
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

// TestProbePlanIsRenderedOnTheEntry: the matrix is measured where the links
// start, and the plan carries no secrets.
func TestProbePlanIsRenderedOnTheEntry(t *testing.T) {
	st := ntfyFixture("")
	st.Hosts["node"] = state.Host{Role: state.Entry, Engine: "sing-box", IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"}
	st.Hosts["exit"] = state.Host{Role: state.Exit, Engine: "sing-box", IP: "203.0.113.12", SSH: "root@203.0.113.12"}
	st.Links = []state.Link{{From: "node", To: "exit", Protocols: []string{"reality", "hy2", "awg"}, Tunnel: "10.9.9.0/30"}}
	inb := st.Inbounds["exit"]
	inb["reality"] = state.Inbound{Port: 443}
	inb["hy2"] = state.Inbound{Port: 8443}
	inb["awg"] = state.Inbound{Port: 51820}
	st.Inbounds["exit"] = inb
	st.FillStubSecrets() // the link needs its own secrets once it exists

	files, err := RenderHost(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}

	plan, ok := files["/etc/vpnctl/probe.json"]
	if !ok {
		t.Fatal("the entry has no probe plan")
	}

	for _, want := range []string{`"to": "exit"`, `"addr": "203.0.113.12"`, `"protocol": "reality"`, `"protocol": "hy2"`, `"protocol": "awg"`, `"port": 51820`} {
		if !strings.Contains(plan, want) {
			t.Errorf("the plan lacks %q:\n%s", want, plan)
		}
	}

	for _, secret := range []string{"STUB-uuid", "STUB-hy2", "password", "uuid"} {
		if strings.Contains(plan, secret) {
			t.Errorf("the plan carries %q", secret)
		}
	}

	if _, ok := mustRender(t, st, "exit")["/etc/vpnctl/probe.json"]; ok {
		t.Error("an exit renders a probe plan it can never measure")
	}
}

func mustRender(t *testing.T, st *state.State, host string) map[string]string {
	t.Helper()

	files, err := RenderHost(st, host, testDay())
	if err != nil {
		t.Fatal(err)
	}

	return files
}
