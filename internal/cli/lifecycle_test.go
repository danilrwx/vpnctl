package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
	"github.com/danilrwx/vpnctl-go/internal/ops"
)

// TestInitHostAndRender walks the command line the way a first day does: create
// the state, add a device, add an entry, check it and render it. Every step is
// nocommit: a test has no repository and must not want one.
func TestInitHostAndRender(t *testing.T) {
	withIdentity(t)

	dir := filepath.Join(t.TempDir(), "state")

	if err := Init(dir, "laptop", "", "", "", false, true); err != nil {
		t.Fatalf("init: %v", err)
	}

	for _, name := range []string{"hosts.yaml", "age.yaml", "versions.yaml", "secrets.age.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s after init: %v", name, err)
		}
	}

	phone, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if err := KeyAdd(dir, "phone", ageio.RecipientOf(phone), true); err != nil {
		t.Fatalf("key add: %v", err)
	}

	if err := KeyList(dir); err != nil {
		t.Fatalf("key list: %v", err)
	}

	if err := HostAdd(dir, ops.HostOptions{
		Name: "ru-msk-1", Role: "entry", SSH: "root@203.0.113.10", Domain: "gw.example.ru",
	}, true); err != nil {
		t.Fatalf("host add: %v", err)
	}

	if err := Check(dir, false, false); err != nil {
		t.Fatalf("check after host add: %v", err)
	}

	out := filepath.Join(t.TempDir(), "render")

	if err := Render(dir, out, "", true, time.Now().UTC()); err != nil {
		t.Fatalf("render: %v", err)
	}

	cfg := filepath.Join(out, "ru-msk-1", "etc", "sing-box", "config.json")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("rendered config: %v", err)
	}

	if err := HostRemove(dir, "ru-msk-1", true); err != nil {
		t.Fatalf("host remove: %v", err)
	}

	if err := KeyRemove(dir, "phone", true); err != nil {
		t.Fatalf("key remove: %v", err)
	}
}

// TestAlertsFromTheCommandLine: set, show and off are the same path the phone
// uses through ops; the topic lands in alerts.yaml and disappears again.
func TestAlertsFromTheCommandLine(t *testing.T) {
	withIdentity(t)

	dir := filepath.Join(t.TempDir(), "state")

	if err := Init(dir, "laptop", "", "", "", false, true); err != nil {
		t.Fatal(err)
	}

	// an empty state fails its own check, so the notification commands need a
	// host the way a real state has one
	if err := HostAdd(dir, ops.HostOptions{
		Name: "ru-msk-1", Role: "entry", SSH: "root@203.0.113.10", Domain: "gw.example.ru",
	}, true); err != nil {
		t.Fatal(err)
	}

	if err := AlertsSet(dir, "https://ntfy.sh/vpnctl-test-42", "", "", "", true); err != nil {
		t.Fatalf("alerts set: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "alerts.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "vpnctl-test-42") {
		t.Fatalf("alerts.yaml: %s", body)
	}

	if err := AlertsShow(dir); err != nil {
		t.Fatalf("alerts show: %v", err)
	}

	if err := AlertsOff(dir, true); err != nil {
		t.Fatalf("alerts off: %v", err)
	}

	// off removes the file: an empty alerts.yaml is noise in the history
	if _, err := os.Stat(filepath.Join(dir, "alerts.yaml")); !os.IsNotExist(err) {
		t.Fatalf("alerts.yaml survived alerts off: %v", err)
	}
}
