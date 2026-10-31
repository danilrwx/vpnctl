package plan

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name           string
		old, new, host string
		hasOld, hasNew bool
		hasHost        bool
		want           Status
	}{
		{"same content", "a", "a", "a", true, true, true, Unchanged},
		{"will change", "a", "b", "a", true, true, true, Changed},
		{"host edited", "a", "b", "c", true, true, true, Drift},
		{"new file", "", "b", "", false, true, false, New},
		{"removed file", "a", "", "a", true, false, true, Removed},
		{"first apply", "", "b", "c", false, true, true, Changed},
		{"never existed", "", "", "", false, false, false, Unchanged},
		{"missing on host", "", "b", "", false, true, false, New},
	}
	for _, c := range cases {
		got := Classify(c.old, c.new, c.host, c.hasOld, c.hasNew, c.hasHost)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got.Mark(), c.want.Mark())
		}
	}
}

func TestParseProbe(t *testing.T) {
	out := "abc1234\nyes\n" +
		"deadbeef /etc/nftables.conf\n" +
		"missing /etc/caddy/Caddyfile\n"
	p := ParseProbe(out)
	if !p.HasMarker || p.Marker != "abc1234" {
		t.Fatalf("marker: %+v", p)
	}
	if !p.Bootstrapped {
		t.Fatal("bootstrap flag lost")
	}
	if p.Hashes["/etc/nftables.conf"] != "deadbeef" {
		t.Fatalf("hash: %+v", p.Hashes)
	}
	if !p.Missing["/etc/caddy/Caddyfile"] {
		t.Fatalf("missing: %+v", p.Missing)
	}
}

func TestParseProbeNoMarker(t *testing.T) {
	p := ParseProbe("-\nno\nmissing /etc/nftables.conf\n")
	if p.HasMarker {
		t.Fatal("a bare dash means no marker")
	}
	if p.Bootstrapped {
		t.Fatal("bootstrap must be false")
	}
}

func TestProbeScriptQuotesPaths(t *testing.T) {
	s := ProbeScript([]string{"/etc/nftables.conf", "/etc/sing-box/rules/ru.json"})
	if !strings.Contains(s, "'/etc/nftables.conf' '/etc/sing-box/rules/ru.json'") {
		t.Fatalf("paths are not quoted: %s", s)
	}
	if !strings.HasPrefix(s, "cat /etc/vpnctl/applied-commit") {
		t.Fatalf("marker is not first: %s", s)
	}
}

func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\n"
	next := "a\nB\nc\n"
	got := unifiedDiff("host:/etc/x", "render", old, next)
	for _, want := range []string{"--- host:/etc/x", "+++ render", "@@ -1,3 +1,3 @@", "-b", "+B", " a", " c"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUnifiedDiffEqual(t *testing.T) {
	if got := unifiedDiff("a", "b", "x\ny\n", "x\ny\n"); strings.Contains(got, "@@") {
		t.Fatalf("no hunks expected:\n%s", got)
	}
}
