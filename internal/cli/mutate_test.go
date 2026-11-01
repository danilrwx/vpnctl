package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
)

// withIdentity gives the test its own age key, so the commands that seal the
// secrets file can run without touching the operator's key.
func withIdentity(t *testing.T) {
	t.Helper()

	id, err := ageio.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "keys.txt")
	if err := ageio.WriteIdentity(path, id); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VPNCTL_AGE_KEY", path)
}

// TestUserDisableAndEnable: the switch is committed to users.yaml and the
// secrets are re-sealed on the way, which is the path every user command takes.
func TestUserDisableAndEnable(t *testing.T) {
	withIdentity(t)
	dir := openFixtureDir(t)

	if err := UserSetEnabled(dir, "brat", false, true); err != nil {
		t.Fatalf("disable: %v", err)
	}

	body := readFile(t, filepath.Join(dir, "users.yaml"))
	if !strings.Contains(body, "enabled: false") {
		t.Fatalf("users.yaml after disable:\n%s", body)
	}

	if _, err := os.Stat(filepath.Join(dir, "secrets.age.yaml")); err != nil {
		t.Fatalf("the secrets were not re-sealed: %v", err)
	}

	if err := UserSetEnabled(dir, "brat", true, true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	if !strings.Contains(readFile(t, filepath.Join(dir, "users.yaml")), "enabled: true") {
		t.Fatal("enable did not reach users.yaml")
	}

	if err := UserSetEnabled(dir, "ghost", false, true); err == nil {
		t.Fatal("an unknown user was accepted")
	}
}

// TestUserRenewExpired: renewing a lapsed account says out loud that only apply
// puts it back, because the tick has already taken it off the host.
func TestUserRenewExpired(t *testing.T) {
	withIdentity(t)
	dir := openFixtureDir(t)

	if err := UserEdit(dir, "olga", nil, nil, nil, strPtr("2020-01-01"), nil, true); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if err := UserRenew(dir, "olga", 30, "", true); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if !strings.Contains(readFile(t, filepath.Join(dir, "users.yaml")), "expires: \"20") {
		t.Fatal("renew did not reach users.yaml")
	}
}

// TestAlertsOffClearsTheFile: off means the file is gone, not empty.
func TestAlertsOffClearsTheFile(t *testing.T) {
	withIdentity(t)
	dir := openFixtureDir(t)

	if err := AlertsSet(dir, "https://ntfy.example/topic", "", "", "", true); err != nil {
		t.Fatalf("alerts set: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "alerts.yaml")); err != nil {
		t.Fatalf("alerts.yaml was not written: %v", err)
	}

	if err := AlertsOff(dir, true); err != nil {
		t.Fatalf("alerts off: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "alerts.yaml")); !os.IsNotExist(err) {
		t.Fatalf("alerts.yaml survived off: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func strPtr(s string) *string { return &s }
