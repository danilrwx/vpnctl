package apply

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/plan"
	"github.com/danilrwx/vpnctl-go/render"
)

func TestTarballRoundTrip(t *testing.T) {
	files := render.HostFiles{
		"/etc/nftables.conf":          "table {}\n",
		"/etc/sing-box/config.json":   "{}\n",
		"/etc/sing-box/rules/ru.json": "{\"v\":3}\n",
	}
	b, err := Tarball(files)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Tarball(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, again) {
		t.Fatal("tarball is not reproducible")
	}
	got := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got["/"+h.Name] = string(body)
	}
	if len(got) != len(files) {
		t.Fatalf("got %d files, want %d", len(got), len(files))
	}
	for p, body := range files {
		if got[p] != body {
			t.Errorf("%s: %q", p, got[p])
		}
	}
}

func TestTarballModes(t *testing.T) {
	b, err := Tarball(render.HostFiles{"/etc/sing-box/config.json": "{}\n", "/etc/nftables.conf": "x\n"})
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		want := int64(0o644)
		if strings.HasPrefix("/"+h.Name, "/etc/sing-box/") {
			want = 0o600
		}
		if h.Typeflag == tar.TypeReg && h.Mode != want {
			t.Errorf("%s: mode %o, want %o", h.Name, h.Mode, want)
		}
	}
}

func TestInstallScriptOrder(t *testing.T) {
	hp := plan.HostPlan{Files: []plan.FileStatus{
		{Path: "/etc/systemd/system/mtg.service", Status: plan.Changed},
		{Path: "/etc/sing-box/config.json", Status: plan.Changed},
		{Path: "/etc/nftables.conf", Status: plan.Changed},
		{Path: "/etc/sysctl.d/90-vpnctl.conf", Status: plan.Changed},
		{Path: "/etc/caddy/Caddyfile", Status: plan.New},
		{Path: "/etc/sing-box/rules/old.json", Status: plan.Removed},
	}}
	s := InstallScript(hp, "/var/lib/vpnctl/backup/abc")
	order := []string{
		"systemctl daemon-reload",
		"sysctl --system",
		"nft -f /etc/nftables.conf",
		"reload caddy",
		"restart sing-box",
		"restart mtg",
	}
	last := -1
	for _, cmd := range order {
		i := strings.Index(s, cmd)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", cmd, s)
		}
		if i < last {
			t.Errorf("%q is out of order", cmd)
		}
		last = i
	}
	if !strings.Contains(s, "install -D -m 0644 '/tmp/.vpnctl/etc/caddy/Caddyfile' '/etc/caddy/Caddyfile'") {
		t.Errorf("new file is not installed:\n%s", s)
	}
	if !strings.Contains(s, "echo '/etc/caddy/Caddyfile' >> /var/lib/vpnctl/backup/abc/.new") {
		t.Errorf("new file is not tracked for rollback:\n%s", s)
	}
	if !strings.Contains(s, "rm -f '/etc/sing-box/rules/old.json'") {
		t.Errorf("removed file is not deleted:\n%s", s)
	}
	if !strings.HasPrefix(s, "set -e\n") || !strings.HasSuffix(s, "echo ok\n") {
		t.Errorf("script framing:\n%s", s)
	}
}

func TestServiceFor(t *testing.T) {
	cases := map[string]string{
		"/etc/sing-box/config.json":        "sing-box",
		"/etc/sing-box/rules/ru.json":      "sing-box",
		"/etc/xray/config.json":            "xray",
		"/etc/caddy/Caddyfile":             "caddy",
		"/etc/mtg/config.toml":             "mtg",
		"/etc/amnezia/amneziawg/awg0.conf": "awg-quick@awg0",
		"/etc/nftables.conf":               "nftables",
		"/etc/sysctl.d/90-vpnctl.conf":     "sysctl",
		"/etc/systemd/system/mtg.service":  "systemd",
	}
	for path, want := range cases {
		got, ok := ServiceFor(path)
		if !ok || got != want {
			t.Errorf("%s: %q %v", path, got, ok)
		}
	}
	if _, ok := ServiceFor("/var/www/sub/x/split"); ok {
		t.Error("a subscription file must not map to a service")
	}
}

func TestCheckCmd(t *testing.T) {
	if c, ok := CheckCmd("sing-box", "/etc/sing-box/config.json"); !ok || !strings.Contains(c, "sing-box check") {
		t.Fatalf("%q %v", c, ok)
	}
	if c, ok := CheckCmd("xray", "/etc/xray/config.json"); !ok || !strings.Contains(c, "xray run -test") {
		t.Fatalf("%q %v", c, ok)
	}
	if _, ok := CheckCmd("xray", "/etc/sing-box/config.json"); ok {
		t.Fatal("sing-box config must not be checked with xray")
	}
	if _, ok := CheckCmd("sing-box", "/var/www/sub/x/split"); ok {
		t.Fatal("no check for subscriptions")
	}
}
