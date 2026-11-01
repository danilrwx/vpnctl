package ops

import (
	"strings"
	"testing"
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
