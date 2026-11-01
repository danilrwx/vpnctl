package mobile

import (
	"testing"
)

// TestExpiryDate: the form's date rule. An empty field is not "unset for now" —
// it removes the deadline, so a typo has to be refused instead of saving as one.
func TestExpiryDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"2026-12-01", "2026-12-01", false},
		{" 2026-12-31 ", "2026-12-31", false},
		{"01.12.2026", "", true},
		{"2026-13-01", "", true},
		{"soon", "", true},
	}

	for _, c := range cases {
		got, err := expiryDate(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q was accepted as %q", c.in, got)
			}

			continue
		}

		if err != nil || got != c.want {
			t.Errorf("%q: %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
