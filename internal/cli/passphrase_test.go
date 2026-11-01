package cli

import "testing"

func TestPassphraseSources(t *testing.T) {
	t.Setenv("VPNCTL_BACKUP_PASS", "from the environment")

	if got, err := passphrase("", false); err != nil || got != "from the environment" {
		t.Fatalf("%q %v", got, err)
	}

	if got, err := passphrase("explicit flag", true); err != nil || got != "explicit flag" {
		t.Fatalf("%q %v", got, err)
	}

	t.Setenv("VPNCTL_BACKUP_PASS", "")
	// the test binary has no terminal, so this must refuse instead of reading
	// from a pipe that would silently take the wrong thing
	if _, err := passphrase("", true); err == nil {
		t.Fatal("a missing terminal was accepted")
	}
}
