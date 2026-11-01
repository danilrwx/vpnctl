package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixtureIsCanonical holds the tree we render and test against to the same
// rule as the live state: if the fixture drifts, every golden file is suspect.
func TestFixtureIsCanonical(t *testing.T) {
	st, err := Load(filepath.Join("..", "..", "testdata", "basic"))
	if err != nil {
		t.Fatal(err)
	}

	diff, err := Differences(st)
	if err != nil {
		t.Fatal(err)
	}

	if len(diff) > 0 {
		t.Fatalf("the fixture is not canonical: %v (run vpnctl state fmt)", diff)
	}
}

// TestFormatRewritesFourSpaces: the rule is enforced by rewriting, not by hope.
func TestFormatRewritesFourSpaces(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()

		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("users.yaml", "users:\n    anna:\n        modes:\n            - split\n        protocols:\n            - reality\n")
	write("hosts.yaml", "hosts: {}\n")
	write("links.yaml", "links: []\n")
	write("protocols.yaml", "inbounds: {}\n")
	write("versions.yaml", "{}\n")

	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	diff, err := Differences(st)
	if err != nil {
		t.Fatal(err)
	}

	if len(diff) != 1 || diff[0] != "users.yaml" {
		t.Fatalf("differences %v", diff)
	}

	changed, err := Format(st)
	if err != nil {
		t.Fatal(err)
	}

	if len(changed) != 1 {
		t.Fatalf("changed %v", changed)
	}

	body, err := os.ReadFile(filepath.Join(dir, "users.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "\n  anna:\n") {
		t.Fatalf("the file is still not two spaces:\n%s", body)
	}

	if diff, err := Differences(st); err != nil || len(diff) != 0 {
		t.Fatalf("after fmt: %v %v", diff, err)
	}
}
