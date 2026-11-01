package state

import "testing"

// TestCheckNtfy pins the shape of a topic: the probe posts to it with curl, so
// a URL that it cannot use must be refused here and not on the host at 3 a.m.
func TestCheckNtfy(t *testing.T) {
	ok := map[string]string{
		"":                                "",
		"https://ntfy.sh/vpnctl-abc123":   "https://ntfy.sh/vpnctl-abc123",
		"https://ntfy.sh/vpnctl-abc123/":  "https://ntfy.sh/vpnctl-abc123",
		"  https://ntfy.example.com/a/b ": "https://ntfy.example.com/a/b",
	}
	for in, want := range ok {
		got, err := CheckNtfy(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}

		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}

	bad := []string{
		"ntfy.sh/vpnctl",
		"http://ntfy.sh/vpnctl",
		"https://",
		"https://user:pass@ntfy.sh/vpnctl",
		"https://ntfy.sh/vpnctl?x=1",
		"https://ntfy.sh/vpnctl#frag",
		"https://ntfy.sh/",
	}
	for _, in := range bad {
		if _, err := CheckNtfy(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}
