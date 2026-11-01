package hostscript

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

var update = flag.Bool("update", false, "rewrite the golden scripts")

const goldenDir = "../../testdata/hostscript"

// fixture is an entry with one file of every status, which is what the install
// script has to distinguish.
func fixture() plan.HostPlan {
	return plan.HostPlan{Name: "node", Files: []plan.FileStatus{
		{Path: "/etc/sing-box/config.json", Status: plan.Changed},
		{Path: "/etc/nftables.conf", Status: plan.Changed},
		{Path: "/etc/sysctl.d/90-vpnctl.conf", Status: plan.Changed},
		{Path: "/etc/caddy/Caddyfile", Status: plan.New},
		{Path: "/etc/sing-box/rules/old.json", Status: plan.Removed},
		{Path: "/etc/systemd/system/mtg.service", Status: plan.Changed},
		{Path: "/etc/vpnctl/probe.sh", Status: plan.Unchanged},
	}}
}

func check(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run -update)", path, err)
	}
	if string(want) != body {
		t.Errorf("%s differs\n--- want ---\n%s\n--- got ---\n%s", name, want, body)
	}
}

func TestInstallScript(t *testing.T) {
	check(t, "install.sh", InstallScript(fixture(), "/var/lib/vpnctl/backup/abc1234"))
}

func TestRollbackScript(t *testing.T) {
	check(t, "rollback.sh", RollbackScript(fixture(), "/var/lib/vpnctl/backup/abc1234"))
}

func TestProbeScripts(t *testing.T) {
	check(t, "probe-entry.sh", ProbeScript(state.Entry))
	check(t, "probe-exit.sh", ProbeScript(state.Exit))
}

func TestManifestScript(t *testing.T) {
	body := ManifestScript(map[string]string{
		"/etc/b.conf": "b\n",
		"/etc/a.conf": "a\n",
	})
	check(t, "manifest.sh", body)
	if strings.Index(body, "/etc/a.conf") > strings.Index(body, "/etc/b.conf") {
		t.Error("the manifest is not sorted")
	}
}

func TestModeAndService(t *testing.T) {
	if ModeFor("/etc/sing-box/config.json") != 0o600 {
		t.Error("engine configs must be 0600")
	}
	if ModeFor("/etc/nftables.conf") != 0o644 {
		t.Error("network configs are 0644")
	}
	if s, ok := ServiceFor("/etc/systemd/system/mtg.service"); !ok || s != "systemd" {
		t.Errorf("%q %v", s, ok)
	}
}
