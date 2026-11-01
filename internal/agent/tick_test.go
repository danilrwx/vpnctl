package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

const tickConfig = `{
  "inbounds": [
    {
      "type": "vless",
      "listen_port": 443,
      "users": [
        {"name": "anna-split-reality", "uuid": "u1", "flow": "xtls-rprx-vision"},
        {"name": "boris-split-reality", "uuid": "u2", "flow": "xtls-rprx-vision"}
      ]
    }
  ],
  "route": {
    "rules": [
      {"auth_user": ["anna-split-reality", "boris-split-reality"], "outbound": "link-hy2"},
      {"rule_set": ["geoip-ru"], "outbound": "direct"}
    ]
  }
}
`

func tickOptions(t *testing.T, dir string, now time.Time) TickOptions {
	t.Helper()

	expiry := filepath.Join(dir, "expiry")

	body := `# vpnctl · account expiry
users:
  anna:
    expires: "2026-10-01"
    sub_id: sub-anna
    emails:
      - anna-split-reality
  boris:
    expires: "2030-01-01"
    sub_id: sub-boris
    emails:
      - boris-split-reality
`
	if err := os.WriteFile(expiry, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(tickConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	subs := filepath.Join(dir, "sub")
	if err := os.MkdirAll(filepath.Join(subs, "sub-anna"), 0o755); err != nil {
		t.Fatal(err)
	}

	return TickOptions{
		ExpiryPath:   expiry,
		EngineConfig: cfg,
		SubRoot:      subs,
		ExpiredDir:   filepath.Join(dir, "expired"),
		Now:          now,
		Run: func(cmd string) (string, error) {
			return "", nil
		},
	}
}

// TestTickRemovesTheExpired: the identity leaves the engine, the auth_user list
// shrinks, the subscription leaves the web root, and the other user is untouched.
func TestTickRemovesTheExpired(t *testing.T) {
	opts := tickOptions(t, t.TempDir(), time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC))

	rep, err := Tick(opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Expired) != 1 || rep.Expired[0] != "anna" {
		t.Fatalf("expired %v", rep.Expired)
	}

	if !rep.Changed || len(rep.Subs) != 1 || rep.Subs[0] != "sub-anna" {
		t.Fatalf("report %+v", rep)
	}

	cfg, err := os.ReadFile(opts.EngineConfig)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(cfg), "anna-split-reality") {
		t.Errorf("the expired identity is still in the engine:\n%s", cfg)
	}

	if !strings.Contains(string(cfg), "boris-split-reality") {
		t.Errorf("the other user was removed too:\n%s", cfg)
	}

	if !strings.Contains(string(cfg), "geoip-ru") {
		t.Errorf("an unrelated rule was dropped:\n%s", cfg)
	}

	if _, err := os.Stat(filepath.Join(opts.SubRoot, "sub-anna")); !os.IsNotExist(err) {
		t.Error("the subscription is still served")
	}

	if _, err := os.Stat(filepath.Join(opts.ExpiredDir, "sub-sub-anna")); err != nil {
		t.Errorf("the subscription was not kept: %v", err)
	}
}

// TestTickIsIdempotent: a second run has nothing to do and does not restart the
// engine, because a restart drops every live session.
func TestTickIsIdempotent(t *testing.T) {
	opts := tickOptions(t, t.TempDir(), time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC))
	if _, err := Tick(opts); err != nil {
		t.Fatal(err)
	}

	restarts := 0
	opts.Run = func(cmd string) (string, error) {
		if strings.Contains(cmd, "systemctl restart") {
			restarts++
		}

		return "", nil
	}

	rep, err := Tick(opts)
	if err != nil {
		t.Fatal(err)
	}

	if rep.Changed || len(rep.Subs) > 0 {
		t.Fatalf("the second run changed something: %+v", rep)
	}

	if restarts != 0 {
		t.Errorf("the second run restarted the engine %d times", restarts)
	}
}

// TestTickRollsBack: a config the engine rejects, or a chain that stops
// answering, must leave the host as it was.
func TestTickRollsBack(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(cmd string) (string, error)
	}{
		{"the engine rejects the config", func(cmd string) (string, error) {
			if strings.Contains(cmd, " check ") {
				return "FATAL bad config", os.ErrInvalid
			}

			return "", nil
		}},
		{"the chain stops answering", func(cmd string) (string, error) {
			if strings.Contains(cmd, "probe.sh") {
				return "chain probe: 000 instead of 204", os.ErrDeadlineExceeded
			}

			return "", nil
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := tickOptions(t, t.TempDir(), time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC))

			opts.Run = c.run
			if _, err := Tick(opts); err == nil {
				t.Fatal("the failure was swallowed")
			}

			cfg, err := os.ReadFile(opts.EngineConfig)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(cfg), "anna-split-reality") {
				t.Errorf("the config was not restored:\n%s", cfg)
			}

			if _, err := os.Stat(filepath.Join(opts.SubRoot, "sub-anna")); err != nil {
				t.Error("the subscription was taken down before the probe failed")
			}
		})
	}
}

// TestTickNothingExpired: no edits, no restarts, no subscription moves.
func TestTickNothingExpired(t *testing.T) {
	opts := tickOptions(t, t.TempDir(), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))

	rep, err := Tick(opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Expired) != 0 || rep.Changed {
		t.Fatalf("report %+v", rep)
	}
}

// TestStripUsersMatchesTheRenderer is the promise the whole scheme rests on: the
// edit the agent makes is byte for byte the file apply would have written that
// day. If they ever drift, every host with an expired user shows false drift.
func TestStripUsersMatchesTheRenderer(t *testing.T) {
	// a state rendered a day before and a day after the expiry
	before := `{
  "inbounds": [
    {
      "listen_port": 443,
      "users": [
        {"name": "goner-split-reality", "uuid": "u1"},
        {"name": "stays-split-reality", "uuid": "u2"}
      ]
    }
  ],
  "route": {"rules": [{"auth_user": ["goner-split-reality", "stays-split-reality"], "outbound": "link"}]}
}
`
	after := `{
  "inbounds": [
    {
      "listen_port": 443,
      "users": [
        {
          "name": "stays-split-reality",
          "uuid": "u2"
        }
      ]
    }
  ],
  "route": {
    "rules": [
      {
        "auth_user": [
          "stays-split-reality"
        ],
        "outbound": "link"
      }
    ]
  }
}
`

	got, changed, err := StripUsers([]byte(before), map[string]bool{"goner-split-reality": true})
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("nothing changed")
	}

	if string(got) != after {
		t.Fatalf("the agent's edit differs from the render\n--- got ---\n%s\n--- want ---\n%s", got, after)
	}
}

// TestDropPeer: the peer block goes, its neighbours stay.
func TestDropPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awg0.conf")

	body := `[Interface]
PrivateKey = x

[Peer]
PublicKey = a
AllowedIPs = 10.8.1.2/32

[Peer]
PublicKey = b
AllowedIPs = 10.8.1.3/32
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := DropPeer(path, "10.8.1.2")
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("nothing changed")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(got), "10.8.1.2") {
		t.Errorf("the peer is still there:\n%s", got)
	}

	if !strings.Contains(string(got), "10.8.1.3") {
		t.Errorf("the other peer was removed:\n%s", got)
	}

	if again, err := DropPeer(path, "10.8.1.2"); err != nil || again {
		t.Errorf("a second run changed the file again: %v %v", again, err)
	}
}

// TestParseExpiry: the file the agent reads is the file the renderer writes.
func TestParseExpiry(t *testing.T) {
	exp, err := state.ParseExpiry([]byte("users:\n  a:\n    expires: \"2026-01-01\"\n    emails:\n      - a-split-reality\n"))
	if err != nil {
		t.Fatal(err)
	}

	if e := exp.Users["a"]; e.Expires != "2026-01-01" || len(e.Emails) != 1 {
		t.Fatalf("entry %+v", e)
	}

	if !state.Expired("2026-01-01", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("the expiry date itself is not expired")
	}

	if state.Expired("2026-01-02", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("a future date is expired")
	}
}

// TestTickEditEqualsTheRender is the property the whole scheme rests on, taken
// end to end: render the state a day before the expiry, strip the expired user
// the way the timer does, and compare with what the renderer writes on the day
// itself. Any difference becomes false drift on every host.
func TestTickEditEqualsTheRender(t *testing.T) {
	st := &state.State{
		Hosts: map[string]state.Host{
			"node": {Role: state.Entry, Engine: "sing-box", IP: "203.0.113.10", Domain: "vpn.example", SSH: "root@203.0.113.10"},
		},
		Users: map[string]state.User{
			"goner": {Modes: []string{"split", "full"}, Protocols: []string{"reality", "ws"}, Expires: "2026-06-01", TG: true},
			"stays": {Modes: []string{"split"}, Protocols: []string{"reality", "ws"}},
		},
		Inbounds: map[string]map[string]state.Inbound{
			"node": {"reality": {Port: 443}, "ws": {Port: 8444, Path: "/ws"}},
		},
		Routing: map[string]state.Routing{"node": {Groups: state.Groups{
			Split: []state.Rule{{Match: state.Match{Sets: []string{"ru"}}, Action: state.Action{Kind: state.ActionDirect}}},
			Full:  []state.Rule{{Match: state.Match{Sets: []string{"ru"}}, Action: state.Action{Kind: state.ActionDirect}}},
		}}},
		Sets:     map[string]state.Set{"ru": {Geosite: []string{"category-ru"}}},
		Versions: map[string]state.Pin{},
	}
	st.FillStubSecrets()

	before, err := render.RenderHost(st, "node", time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	after, err := render.RenderHost(st, "node", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	const path = "/etc/sing-box/config.json"

	emails := map[string]bool{
		"goner-split-reality": true, "goner-split-ws": true,
		"goner-full-reality": true, "goner-full-ws": true,
	}

	got, changed, err := StripUsers([]byte(before[path]), emails)
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("the strip found nothing to remove")
	}

	if string(got) != after[path] {
		t.Fatalf("the timer's edit differs from the render\n--- timer ---\n%s\n--- render ---\n%s", got, after[path])
	}
}
