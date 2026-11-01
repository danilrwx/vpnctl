package render

import (
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func probeFixture(role state.Role, ntfy string) *state.State {
	st := &state.State{
		Hosts: map[string]state.Host{
			"node": {Role: role, Engine: "sing-box", IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"},
		},
		Inbounds: map[string]map[string]state.Inbound{
			"node": {"reality": {Port: 443}},
		},
		Routing:  map[string]state.Routing{},
		Sets:     map[string]state.Set{},
		Versions: map[string]state.Pin{},
	}
	if ntfy != "" {
		st.Alerts = state.Alerts{Ntfy: ntfy}
		st.Secrets.Alerts = state.AlertsSecret{Token: "tok-123"}
	}
	st.FillStubSecrets()
	return st
}

func TestProbeRenderedOnEntryOnly(t *testing.T) {
	st := probeFixture(state.Entry, "https://ntfy.sh/vpn")
	files, err := RenderHost(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/vpnctl/probe.sh", "/etc/systemd/system/vpnctl-probe.service", "/etc/systemd/system/vpnctl-probe.timer", "/etc/vpnctl/alerts.env"} {
		if _, ok := files[p]; !ok {
			t.Errorf("entry is missing %s", p)
		}
	}
	if !strings.Contains(files["/etc/vpnctl/probe.sh"], "--socks5-hostname 127.0.0.1:1080") {
		t.Error("the probe does not go through the engine socks inbound")
	}
	probe := files["/etc/vpnctl/probe.sh"]
	if !strings.Contains(probe, "VPNCTL_ALERTS_URL") || !strings.Contains(probe, "notify()") {
		t.Error("the probe does not notify")
	}
	if !strings.Contains(probe, "REMIND=1800") {
		t.Error("the probe reminds on every failing minute instead of every half hour")
	}
	if !strings.Contains(probe, "/var/lib/vpnctl/probe.state") {
		t.Error("the probe keeps no state, so it cannot tell a transition from a repeat")
	}
	if !strings.Contains(probe, `if [ "$was" = "fail" ]; then notify default "chain probe: recovered"`) {
		t.Error("a recovery is silent")
	}
	if !strings.Contains(files["/etc/vpnctl/alerts.env"], `VPNCTL_ALERTS_URL="https://ntfy.sh/vpn"`) {
		t.Errorf("alerts env: %s", files["/etc/vpnctl/alerts.env"])
	}
	if !strings.Contains(files["/etc/vpnctl/alerts.env"], `VPNCTL_ALERTS_TOKEN="tok-123"`) {
		t.Error("the token is not in the env file")
	}
	if !strings.Contains(files["/etc/systemd/system/vpnctl-probe.timer"], "OnUnitActiveSec=1min") {
		t.Error("the timer does not run every minute")
	}

	exit, err := RenderHost(probeFixture(state.Exit, ""), "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exit["/etc/vpnctl/probe.sh"]; ok {
		t.Error("an exit must not run the chain probe")
	}
}

func TestProbeWithoutAlerts(t *testing.T) {
	files, err := RenderHost(probeFixture(state.Entry, ""), "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	probe := files["/etc/vpnctl/probe.sh"]
	if strings.Contains(probe, "VPNCTL_ALERTS_URL") {
		t.Error("the script notifies without a topic")
	}
	if !strings.Contains(probe, "notify() { return 0; }") {
		t.Error("the script calls notify without defining it, so a recovery would print command-not-found")
	}
}
