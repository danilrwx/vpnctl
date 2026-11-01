package bootstrap

import (
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

func fixtureState(t *testing.T, role state.Role, engine string, awgLink bool) *state.State {
	t.Helper()
	links := []state.Link{}
	if awgLink {
		links = append(links, state.Link{From: "entry", To: "node", Protocols: []string{"awg"}, Tunnel: "10.9.1.0/30"})
	}
	st := &state.State{
		Hosts: map[string]state.Host{
			"node": {Role: role, Engine: engine, IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"},
		},
		Users: map[string]state.User{},
		Inbounds: map[string]map[string]state.Inbound{
			"node": {"reality": {Port: 443}},
		},
		Links: links,
		Routing: map[string]state.Routing{
			"node": {Groups: state.Groups{All: []state.Rule{{
				Match:  state.Match{Sets: []string{"ru"}},
				Action: state.Action{Kind: state.ActionDirect},
			}}}},
		},
		Sets: map[string]state.Set{
			"ru": {Geosite: []string{"category-ru"}},
		},
		Versions: map[string]state.Pin{
			"sing-box": {Version: "1.12.4"},
			"xray":     {Version: "25.9.11"},
			"geodata":  {Version: "latest"},
			"caddy":    {Version: "2.10.0"},
			"mtg":      {Version: "2.1.7"},
			"awg":      {Version: "1.0.20250706"},
		},
	}
	if role == state.Entry {
		st.Inbounds["node"]["ws"] = state.Inbound{Port: 8444, Path: "/ws"}
		st.Inbounds["node"]["mtg"] = state.Inbound{Port: 2053}
	}
	if awgLink {
		st.Hosts["entry"] = state.Host{Role: state.Entry, IP: "198.51.100.1", Domain: "e.example", SSH: "root@198.51.100.1"}
		awg := st.Inbounds["node"]["awg"]
		awg.Port = 51820
		st.Inbounds["node"]["awg"] = awg
	}
	st.FillStubSecrets()
	return st
}

func TestScriptEntryRunning(t *testing.T) {
	st := fixtureState(t, state.Entry, "", false)
	s, err := Script(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#!/bin/sh",
		"# vpnctl · bootstrap node (entry)",
		"sing-box-1.12.4-linux-amd64.tar.gz",
		"caddy_2.10.0_linux_amd64.tar.gz",
		"mtg-2.1.7-linux-amd64.tar.gz",
		"geosite-category-ru.srs",
		"table inet filter",
		"net.ipv4.ip_forward = 1",
		"systemctl enable -q caddy",
		"OnCalendar=*-*-01,03",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "dkms") {
		t.Error("dkms must not appear without AWG")
	}
}

func TestScriptExitAndAWG(t *testing.T) {
	st := fixtureState(t, state.Exit, "", true)
	s, err := Script(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "OnCalendar=*-*-02,04") {
		t.Error("an exit must reboot on even days")
	}
	if !strings.Contains(s, "git clone -q --depth 1 --branch \"v1.0.20250706\"") {
		t.Error("the AWG module is not built")
	}
	if !strings.Contains(s, "awg-quick@awg0") {
		t.Error("the awg interface is not enabled")
	}
	if strings.Contains(s, "caddy") {
		t.Error("an exit must not install caddy")
	}
}

func TestScriptXray(t *testing.T) {
	st := fixtureState(t, state.Exit, "xray", false)
	s, err := Script(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Xray-linux-64.zip", "xray version", "geosite.dat", "geoip.dat"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "sing-box-1.12.4") {
		t.Error("sing-box must not be installed for an xray host")
	}
}

func TestScriptMissingPin(t *testing.T) {
	st := fixtureState(t, state.Exit, "", true)
	delete(st.Versions, "sing-box")
	if _, err := Script(st, "node", testDay()); err == nil {
		t.Fatal("a missing pin was accepted")
	}
}

func TestRuleSetTags(t *testing.T) {
	cfg := `{"route":{"rule_set":[{"type":"local","tag":"set-a","format":"source"},{"type":"local","tag":"geoip-ru","format":"binary"},{"type":"local","tag":"geoip-ru","format":"binary"}]}}`
	tags := RuleSetTags(cfg)
	if len(tags) != 1 || tags[0] != "geoip-ru" {
		t.Fatalf("%v", tags)
	}
	if RuleSetTags("not json") != nil {
		t.Fatal("broken json must not panic")
	}
}

func TestRuleSetURL(t *testing.T) {
	if got := RuleSetURL("geoip-ru", "latest"); !strings.Contains(got, "sing-geoip/rule-set/geoip-ru.srs") {
		t.Fatal(got)
	}
	if got := RuleSetURL("geosite-ru", "1.12.0"); !strings.Contains(got, "sing-geosite/releases/download/1.12.0/geosite-ru.srs") {
		t.Fatal(got)
	}
}

func TestRebootDays(t *testing.T) {
	if got := RebootDays(state.Entry); !strings.HasPrefix(got, "01,03") || strings.Contains(got, "02") {
		t.Fatal(got)
	}
	if got := RebootDays(state.Exit); !strings.HasPrefix(got, "02,04") {
		t.Fatal(got)
	}
}

var _ = render.EngineConfigPath

// TestScriptNtfy: the notification server is installed only on the host that
// alerts.yaml names, and the pin comes from versions.yaml like every other
// component.
func TestScriptNtfy(t *testing.T) {
	st := fixtureState(t, state.Entry, "", false)
	st.Versions["ntfy"] = state.Pin{Version: "2.36.0", SHA256: strings.Repeat("a", 64)}
	st.Alerts = state.Alerts{Ntfy: "https://ntfy.vpn.example/topic", Serve: "node"}
	s, err := Script(st, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ntfy_2.36.0_linux_amd64.tar.gz",
		"install -m 0755 /var/lib/vpnctl/dl/ntfy_2.36.0_linux_amd64/ntfy /usr/local/bin/ntfy",
		"ntfy.service",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the bootstrap script lacks %q", want)
		}
	}
	plain := fixtureState(t, state.Entry, "", false)
	s, err = Script(plain, "node", testDay())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "ntfy_") {
		t.Error("a host without a topic installs the server anyway")
	}
}

// testDay keeps the bootstrap script independent of the wall clock.
func testDay() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}
