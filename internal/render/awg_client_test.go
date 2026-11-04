package render

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// Every mask has the same three forbidden addresses and at least one usable
// address, except a /31 and /32 where a client cannot be placed at all.
func TestUsableAWGAddr(t *testing.T) {
	cases := []struct {
		subnet string
		addr   string
		usable bool
	}{
		{"10.8.1.0/24", "10.8.1.0", false},
		{"10.8.1.0/24", "10.8.1.1", false},
		{"10.8.1.0/24", "10.8.1.2", true},
		{"10.8.1.0/24", "10.8.1.254", true},
		{"10.8.1.0/24", "10.8.1.255", false},
		{"10.8.0.0/23", "10.8.1.255", false},
		{"10.8.0.0/23", "10.8.1.2", true},
		{"10.8.1.0/25", "10.8.1.127", false},
		{"10.8.1.0/25", "10.8.1.126", true},
		{"10.8.1.0/30", "10.8.1.2", true},
		{"10.8.1.0/30", "10.8.1.3", false},
		{"10.8.1.0/31", "10.8.1.0", false},
		{"10.8.1.0/31", "10.8.1.1", false},
		{"10.8.1.2/32", "10.8.1.2", false},
	}

	for _, tc := range cases {
		t.Run(tc.subnet+"/"+tc.addr, func(t *testing.T) {
			subnet := netip.MustParsePrefix(tc.subnet)
			if got := usableAWGAddr(netip.MustParseAddr(tc.addr), subnet); got != tc.usable {
				t.Fatalf("usable = %v, want %v", got, tc.usable)
			}
		})
	}
}

func TestAWGClientMatchesHost(t *testing.T) {
	st := fixture(t)

	body, err := AWGClient(st, "ru-msk-1", "brat")
	if err != nil {
		t.Fatal(err)
	}

	files, err := RenderHost(st, "ru-msk-1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if files["/etc/vpnctl/profiles/brat/awg.conf"] != body {
		t.Fatal("host and API differ")
	}

	if !strings.Contains(body, "DNS = 10.8.1.1") {
		t.Fatal(body)
	}

	st.Dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(st.Dir, "templates"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(st.Dir, "templates", "awg-client.conf.j2"), []byte("custom {{ user }}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if body, err := AWGClient(st, "ru-msk-1", "brat"); err != nil || body != "custom brat\n" {
		t.Fatalf("template override: %q %v", body, err)
	}

	path := filepath.Join(st.Dir, "overrides/ru-msk-1/etc/vpnctl/profiles/brat")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(path, "awg.conf"), []byte("adopted"), 0o600); err != nil {
		t.Fatal(err)
	}

	if body, err := AWGClient(st, "ru-msk-1", "brat"); err != nil || body != "adopted" {
		t.Fatalf("file override: %q %v", body, err)
	}
}

func TestAWGClientGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*state.State)
	}{
		{"disabled user", func(st *state.State) { u := st.Users["brat"]; off := false; u.Enabled = &off; st.Users["brat"] = u }},
		{"disabled inbound", func(st *state.State) {
			in := st.Inbounds["ru-msk-1"]["awg"]
			off := false
			in.Enabled = &off
			st.Inbounds["ru-msk-1"]["awg"] = in
		}},
		{"missing subnets", func(st *state.State) {
			in := st.Inbounds["ru-msk-1"]["awg"]
			in.Subnets = nil
			st.Inbounds["ru-msk-1"]["awg"] = in
		}},
		{"missing user keys", func(st *state.State) { sec := st.Secrets.Users["brat"]; sec.AWG = nil; st.Secrets.Users["brat"] = sec }},
		{"missing server keys", func(st *state.State) {
			sec := st.Secrets.Hosts["ru-msk-1"]
			sec.AWG = nil
			st.Secrets.Hosts["ru-msk-1"] = sec
		}},
		{"wrong subnet", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.99.0.2"; st.Users["brat"] = u }},
		{"gateway address", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.8.1.1"; st.Users["brat"] = u }},
		{"broadcast address", func(st *state.State) { u := st.Users["brat"]; u.AWG.IP = "10.8.1.255"; st.Users["brat"] = u }},
		{"port out of range", func(st *state.State) {
			in := st.Inbounds["ru-msk-1"]["awg"]
			in.Port = 70000
			st.Inbounds["ru-msk-1"]["awg"] = in
		}},
		{"subnet with host bits", func(st *state.State) {
			st.Inbounds["ru-msk-1"]["awg"].Subnets.Split = "10.8.1.3/24"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := fixture(t)
			tc.mutate(st)

			if body, err := AWGClient(st, "ru-msk-1", "brat"); err == nil || body != "" {
				t.Fatalf("unsafe profile: %q %v", body, err)
			}
		})
	}
}
