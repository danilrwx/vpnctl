package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/state"
)

// update rewrites the golden files: go test ./render/ -update
var update = flag.Bool("update", false, "rewrite the golden files")

const (
	fixtureDir  = "../testdata/basic"
	snapshotDir = "../testdata/snapshots"
)

// fixture loads the shared state. The secrets live in plain text there, the way
// the Rust core keeps its own fixtures; the Go core only reads age files.
func fixture(t *testing.T) *state.State {
	t.Helper()
	st, err := state.Load(fixtureDir)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(fixtureDir, "secrets.sops.yaml"))
	if err != nil {
		t.Fatalf("read fixture secrets: %v", err)
	}
	if err := yaml.Unmarshal(b, &st.Secrets); err != nil {
		t.Fatalf("parse fixture secrets: %v", err)
	}
	return st
}

// snapshotBody strips the insta header: the body is everything after the second ---.
func snapshotBody(b []byte) string {
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if i := strings.Index(s[4:], "\n---\n"); i >= 0 {
		return s[4+i+5:]
	}
	return s
}

func checkSnapshot(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.Join(snapshotDir, name+".snap")
	if *update {
		out := "---\nsource: tests/render.rs\nexpression: body\n---\n" + body
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create it)", path, err)
	}
	want := snapshotBody(raw)
	if body != want {
		t.Errorf("%s differs from the golden file\n--- want ---\n%s\n--- got ---\n%s", name, want, body)
	}
}

func TestGoldenSingbox(t *testing.T) {
	st := fixture(t)
	r, err := Render(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r) == 0 {
		t.Fatal("nothing rendered")
	}
	for host, files := range r {
		for path, body := range files {
			checkSnapshot(t, "render__"+host+strings.ReplaceAll(path, "/", "_"), body)
		}
	}
}

func TestGoldenXray(t *testing.T) {
	st := fixture(t)
	// xray on the entry: no hysteria2 and no L3 clients
	h := st.Hosts["ru-msk-1"]
	h.Engine = "xray"
	st.Hosts["ru-msk-1"] = h
	inb := st.Inbounds["ru-msk-1"]
	delete(inb, "hy2")
	delete(inb, "awg")
	for name, u := range st.Users {
		u.AWG = nil
		var kept []string
		for _, p := range u.Protocols {
			if p != "awg" && p != "hy2" {
				kept = append(kept, p)
			}
		}
		u.Protocols = kept
		st.Users[name] = u
	}
	var protos []string
	for _, p := range st.Links[0].Protocols {
		if p != "hy2" {
			protos = append(protos, p)
		}
	}
	st.Links[0].Protocols = protos

	files, err := RenderHost(st, "ru-msk-1")
	if err != nil {
		t.Fatal(err)
	}
	checkSnapshot(t, "render__xray_config", files["/etc/xray/config.json"])
	if !strings.Contains(files["/etc/systemd/system/xray.service"], "/usr/local/bin/xray run") {
		t.Error("the xray unit does not start xray")
	}
	if _, ok := files["/etc/amnezia/amneziawg/awg0.conf"]; ok {
		t.Error("an xray host must not get an AWG interface")
	}
}

func TestStubSecretsRender(t *testing.T) {
	st := fixture(t)
	st.FillStubSecrets()
	r, err := Render(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r["ru-msk-1"]["/etc/sing-box/config.json"], "STUB-uuid-brat") {
		t.Error("the stub secrets are not used")
	}
}
