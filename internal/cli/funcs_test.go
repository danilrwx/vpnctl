package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ucli "github.com/urfave/cli/v3"
)

// TestSplitList: flags take "a,b, c" and must not produce empty entries.
func TestSplitList(t *testing.T) {
	got := splitList("split, full ,,")
	if len(got) != 2 || got[0] != "split" || got[1] != "full" {
		t.Fatalf("%q", got)
	}
	if got := splitList(""); got != nil {
		t.Fatalf("%q", got)
	}
}

// TestDayArg: the day comes from --at or from the UTC clock, never from local
// time — the same state must render the same way on two laptops.
func TestDayArg(t *testing.T) {
	cmd := &ucli.Command{Flags: []ucli.Flag{atFlag()}}
	if err := cmd.Run(t.Context(), []string{"x", "--at", "2026-06-01"}); err != nil {
		t.Fatal(err)
	}
	d, err := dayArg(cmd)
	if err != nil || d.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("%v %v", d, err)
	}
	now := time.Now()
	if d, err := dayArg(&ucli.Command{Flags: []ucli.Flag{atFlag()}}); err != nil || d.Location() != time.UTC {
		t.Fatalf("the default day is not UTC: %v %v (%v)", d, err, now)
	}
	bad := &ucli.Command{Flags: []ucli.Flag{atFlag()}}
	if err := bad.Run(t.Context(), []string{"x", "--at", "soon"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dayArg(bad); err == nil || ExitCode(err) != ExitUsage {
		t.Fatalf("a bad --at is not a usage error: %v", err)
	}
}

// TestShellQuote and TestAlertTitle are small, but they are what keeps a token
// and a title from breaking the shell and the phone.
func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("%q", got)
	}
}

// TestCheckAndRender drive the two read-only commands end to end on the fixture.
func TestCheckAndRender(t *testing.T) {
	dir := openFixtureDir(t)
	if err := Check(dir, false, true); err != nil {
		t.Fatalf("check: %v", err)
	}
	out := filepath.Join(t.TempDir(), "render")
	if err := Render(dir, out, "ru-msk-1", true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "ru-msk-1", "etc", "sing-box", "config.json")); err != nil {
		t.Fatalf("the render is missing the engine config: %v", err)
	}
}

// TestStateFmtCheck: --check reports and fails, fmt fixes and says what it did.
func TestStateFmtCheck(t *testing.T) {
	dir := openFixtureDir(t)
	if err := StateFmt(dir, false, true); err != nil {
		t.Fatalf("the fixture must already be canonical: %v", err)
	}
	if err := StateFmt(dir, true, true); err != nil {
		t.Fatalf("check on a canonical tree: %v", err)
	}
	users := filepath.Join(dir, "users.yaml")
	body, err := os.ReadFile(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(users, []byte(strings.ReplaceAll(string(body), "\n  ", "\n    ")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StateFmt(dir, true, true); err == nil {
		t.Fatal("check passed on a tree with four spaces")
	}
	if err := StateFmt(dir, false, true); err != nil {
		t.Fatalf("fmt: %v", err)
	}
	if err := StateFmt(dir, true, true); err != nil {
		t.Fatalf("check after fmt: %v", err)
	}
}
