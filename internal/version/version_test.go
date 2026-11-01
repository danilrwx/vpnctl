package version

import "testing"

func TestString(t *testing.T) {
	v := String()
	if v == "" {
		t.Fatal("an empty version compares equal to everything")
	}
}

func TestSame(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc-dirty", "abc", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := Same(c.a, c.b); got != c.want {
			t.Errorf("Same(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
