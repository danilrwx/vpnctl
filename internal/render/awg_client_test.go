package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

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
