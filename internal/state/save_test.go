package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRoundTrip writes every plain file and reads the state back: a saver that
// loses a field is worse than no saver, because the next apply ships the loss.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(filepath.Join("..", "..", "testdata", "basic"))
	if err != nil {
		t.Fatal(err)
	}
	st.Dir = dir
	if err := SaveUsers(dir, st.Users); err != nil {
		t.Fatal(err)
	}
	if err := SaveHosts(dir, st.Hosts); err != nil {
		t.Fatal(err)
	}
	if err := SaveLinks(dir, st.Links); err != nil {
		t.Fatal(err)
	}
	if err := SaveInbounds(dir, st.Inbounds); err != nil {
		t.Fatal(err)
	}
	if err := SaveVersions(dir, st.Versions); err != nil {
		t.Fatal(err)
	}
	for host, r := range st.Routing {
		if err := SaveRouting(dir, host, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveAlerts(dir, Alerts{Ntfy: "https://ntfy.example/x", Serve: "node"}); err != nil {
		t.Fatal(err)
	}

	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Users) != len(st.Users) || len(back.Hosts) != len(st.Hosts) || len(back.Links) != len(st.Links) {
		t.Fatalf("counts changed: %d/%d users, %d/%d hosts", len(back.Users), len(st.Users), len(back.Hosts), len(st.Hosts))
	}
	if back.Users["olga"].Note != st.Users["olga"].Note {
		t.Error("a note was lost")
	}
	if back.Alerts.Ntfy != "https://ntfy.example/x" || back.Alerts.Serve != "node" {
		t.Fatalf("alerts %+v", back.Alerts)
	}
	if back.NtfyName("node") != "ntfy."+back.Hosts["node"].Domain && back.Hosts["node"].Domain != "" {
		t.Errorf("NtfyName %q", back.NtfyName("node"))
	}
}

// TestSaveAlertsOff: "off" removes the file, so the history shows the decision
// and not an empty stub.
func TestSaveAlertsOff(t *testing.T) {
	dir := t.TempDir()
	if err := SaveAlerts(dir, Alerts{Ntfy: "https://ntfy.example/x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alerts.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := SaveAlerts(dir, Alerts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alerts.yaml")); !os.IsNotExist(err) {
		t.Fatalf("alerts.yaml survived off: %v", err)
	}
	// removing a file that is already gone is not an error
	if err := SaveAlerts(dir, Alerts{}); err != nil {
		t.Fatal(err)
	}
}

// TestParseFiles: the three rendered plans the agent reads back.
func TestParseFiles(t *testing.T) {
	exp, err := ParseExpiry([]byte("users:\n  a:\n    expires: \"2026-01-01\"\n    emails:\n      - a-split-reality\n"))
	if err != nil || exp.Users["a"].Emails[0] != "a-split-reality" {
		t.Fatalf("%+v %v", exp, err)
	}
	if !Expired("2026-01-01", time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)) {
		t.Error("the date itself is not expired")
	}
	if Expired("2026-01-02", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("tomorrow is expired")
	}
	plan, err := ParseProbePlan([]byte(`{"host":"e","links":[{"from":"e","to":"x","addr":"1.2.3.4","probes":[{"protocol":"hy2","port":443}]}]}`))
	if err != nil || plan.Links[0].Probes[0].Protocol != "hy2" {
		t.Fatalf("%+v %v", plan, err)
	}
	if _, err := ParseExpiry([]byte("users: [")); err == nil {
		t.Error("broken YAML was accepted")
	}
}
