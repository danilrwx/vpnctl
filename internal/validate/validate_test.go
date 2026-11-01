package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// loadFixture reads the shared state with the plain-text fixture secrets.
func loadFixture(t *testing.T) *state.State {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "basic")
	st, err := state.Load(dir)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "secrets.sops.yaml"))
	if err != nil {
		t.Fatalf("read fixture secrets: %v", err)
	}
	if err := yaml.Unmarshal(b, &st.Secrets); err != nil {
		t.Fatalf("parse fixture secrets: %v", err)
	}
	return st
}

// TestCases is the table the Rust core never had: a state mutation and the
// errors it must produce.
func TestCases(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(st *state.State)
		want   string
		absent bool
	}{
		{
			name:   "the fixture itself",
			mutate: func(st *state.State) {},
			absent: true,
		},
		{
			name: "a host name outside the alphabet",
			mutate: func(st *state.State) {
				h := st.Hosts["fi-hel-1"]
				st.Hosts["Fi_Hel"] = h
			},
			want: "name must match",
		},
		{
			name: "an entry without a domain",
			mutate: func(st *state.State) {
				h := st.Hosts["ru-msk-1"]
				h.Domain = ""
				st.Hosts["ru-msk-1"] = h
			},
			want: "the entry requires a domain",
		},
		{
			name: "xray with hysteria2",
			mutate: func(st *state.State) {
				h := st.Hosts["ru-msk-1"]
				h.Engine = "xray"
				st.Hosts["ru-msk-1"] = h
			},
			want: "engine xray does not support hysteria2",
		},
		{
			name: "xray with awg on the entry",
			mutate: func(st *state.State) {
				h := st.Hosts["ru-msk-1"]
				h.Engine = "xray"
				st.Hosts["ru-msk-1"] = h
				inb := st.Inbounds["ru-msk-1"]
				delete(inb, "hy2")
			},
			want: "awg on the entry requires sing-box",
		},
		{
			name: "two inbounds on one port",
			mutate: func(st *state.State) {
				inb := st.Inbounds["ru-msk-1"]
				ws := inb["ws"]
				ws.Port = 443
				inb["ws"] = ws
			},
			want: "is used by both",
		},
		{
			name: "a link described twice",
			mutate: func(st *state.State) {
				st.Links = append(st.Links, st.Links[0])
			},
			want: "is described twice",
		},
		{
			name: "a link to an unknown host",
			mutate: func(st *state.State) {
				st.Links[0].To = "ghost"
			},
			want: "host «ghost» is not described",
		},
		{
			name: "a user protocol without an inbound",
			mutate: func(st *state.State) {
				u := st.Users["olga"]
				u.Protocols = append(u.Protocols, "trojan")
				st.Users["olga"] = u
			},
			want: "has no inbound trojan",
		},
		{
			name: "an awg peer without the protocol",
			mutate: func(st *state.State) {
				u := st.Users["brat"]
				var kept []string
				for _, p := range u.Protocols {
					if p != "awg" {
						kept = append(kept, p)
					}
				}
				u.Protocols = kept
				st.Users["brat"] = u
			},
			want: "an awg peer is described but the awg protocol is not set",
		},
		{
			name: "a mode nobody renders",
			mutate: func(st *state.State) {
				u := st.Users["olga"]
				u.Modes = append(u.Modes, "weird")
				st.Users["olga"] = u
			},
			want: "mode «weird» is neither split nor full",
		},
		{
			name: "a user without secrets",
			mutate: func(st *state.State) {
				delete(st.Secrets.Users, "olga")
			},
			want: "no secrets (uuid, hy2_password, sub_id)",
		},
		{
			name: "a routing set that does not exist",
			mutate: func(st *state.State) {
				r := st.Routing["ru-msk-1"]
				for i := range r.Groups.All {
					r.Groups.All[i].Match.Sets = []string{"nope"}
				}
				st.Routing["ru-msk-1"] = r
			},
			want: "set «nope» is not described",
		},
		{
			name: "a rule without conditions above another",
			mutate: func(st *state.State) {
				r := st.Routing["ru-msk-1"]
				r.Groups.Split = append([]state.Rule{{Action: state.Action{Kind: state.ActionDirect}}}, r.Groups.Split...)
				st.Routing["ru-msk-1"] = r
			},
			want: "rules below are unreachable",
		},
		{
			name: "a preferred exit without a link",
			mutate: func(st *state.State) {
				r := st.Routing["ru-msk-1"]
				r.Exit.Prefer = "ghost"
				st.Routing["ru-msk-1"] = r
			},
			want: "there is no link ru-msk-1->ghost",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := loadFixture(t)
			c.mutate(st)
			errs := State(st)
			joined := strings.Join(errs, "\n")
			if c.absent {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, c.want) {
				t.Fatalf("expected %q, got:\n%s", c.want, joined)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	st := loadFixture(t)
	st.Hosts["lonely"] = state.Host{Role: state.Entry, IP: "203.0.113.99", Domain: "lonely.example", SSH: "root@203.0.113.99"}
	w := strings.Join(Warnings(st), "\n")
	if !strings.Contains(w, "the entry has no link to an exit") {
		t.Fatalf("no warning about a lone entry:\n%s", w)
	}
	if !strings.Contains(w, "host key is not recorded") {
		t.Fatalf("no warning about the host key:\n%s", w)
	}
}
