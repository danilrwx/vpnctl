package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTick(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "expiry")
	body := "# vpnctl\nanna-split-reality 2026-10-01\nboris-split-ws 2030-01-01\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	expired, err := Tick(path, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0] != "anna-split-reality" {
		t.Fatalf("%v", expired)
	}
	// a missing file is not an error: hosts that never had users have none
	if got, err := Tick(filepath.Join(dir, "nope"), "", now); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}
