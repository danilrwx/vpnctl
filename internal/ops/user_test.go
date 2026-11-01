package ops

import (
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// TestUserEdit: every flag changes its field and only its field, and the commit
// message names what changed so the history reads as a list of decisions.
func TestUserEdit(t *testing.T) {
	st := fixture(t)
	msg, err := UserEdit(st, "u", UserPatch{
		Modes:     []string{"split", "full"},
		Protocols: []string{"reality", "ws"},
		TG:        ptr(true),
		Expires:   ptr("2026-12-01"),
		Note:      ptr("a note"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "users: u: modes, protocols, tg, expiry, note"; msg != want {
		t.Fatalf("message %q, want %q", msg, want)
	}
	u := st.Users["u"]
	if strings.Join(u.Modes, ",") != "split,full" || strings.Join(u.Protocols, ",") != "reality,ws" {
		t.Fatalf("unexpected lists: %+v", u)
	}
	if !u.TG || u.Expires != "2026-12-01" || u.Note != "a note" {
		t.Fatalf("unexpected flags: %+v", u)
	}
}

// TestUserEditClearsExpiry: an empty --expires lifts the deadline instead of
// writing an empty string that validate would reject.
func TestUserEditClearsExpiry(t *testing.T) {
	st := fixture(t)
	if _, err := UserEdit(st, "u", UserPatch{Expires: ptr("2026-12-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := UserEdit(st, "u", UserPatch{Expires: ptr("")}); err != nil {
		t.Fatal(err)
	}
	if got := st.Users["u"].Expires; got != "" {
		t.Fatalf("expiry is %q, want it cleared", got)
	}
}

// TestUserEditRejects: a patch that changes nothing or empties a required list
// is a mistake, not a commit.
func TestUserEditRejects(t *testing.T) {
	cases := []struct {
		name  string
		user  string
		patch UserPatch
		want  string
	}{
		{"an unknown user", "ghost", UserPatch{Note: ptr("x")}, "is not described"},
		{"an empty patch", "u", UserPatch{}, "nothing to change"},
		{"no modes", "u", UserPatch{Modes: []string{}}, "no modes"},
		{"no protocols", "u", UserPatch{Protocols: []string{}}, "no protocols"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := fixture(t)
			if _, err := UserEdit(st, c.user, c.patch); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
			if c.user == "u" && len(st.Users["u"].Modes) == 0 {
				t.Fatal("a rejected patch changed the state")
			}
		})
	}
}

// TestUserRenew: the term extends from the later of today and the current
// date, so renewing early never shortens an account and an expired one starts
// again from today.
func TestUserRenew(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expires string
		days    int
		until   string
		want    string
	}{
		{"no date yet", "", 30, "", "2026-12-01"},
		{"renewing early", "2026-12-20", 30, "", "2027-01-19"},
		{"already expired", "2026-10-01", 30, "", "2026-12-01"},
		{"a fixed date", "2026-10-01", 0, "2027-03-01", "2027-03-01"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := fixture(t)
			u := st.Users["u"]
			u.Expires = c.expires
			st.Users["u"] = u
			msg, err := UserRenew(st, "u", c.days, c.until, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := st.Users["u"].Expires; got != c.want {
				t.Fatalf("expiry %s, want %s", got, c.want)
			}
			if !strings.Contains(msg, c.want) {
				t.Errorf("message %q does not name the new date", msg)
			}
		})
	}
}

func TestUserRenewRejects(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		user  string
		days  int
		until string
		want  string
	}{
		{"an unknown user", "ghost", 30, "", "is not described"},
		{"no argument at all", "u", 0, "", "required"},
		{"both", "u", 30, "2027-01-01", "not both"},
		{"a date in the past", "u", 0, "2026-01-01", "not after today"},
		{"a date that is not a date", "u", 0, "soon", "not YYYY-MM-DD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := fixture(t)
			_, err := UserRenew(st, c.user, c.days, c.until, now)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}
