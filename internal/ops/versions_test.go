package ops

import "testing"

func TestVersionKind(t *testing.T) {
	cases := map[[2]string]string{
		{"1.12.4", "1.12.9"}:   "patch",
		{"1.12.4", "1.13.0"}:   "minor",
		{"1.12.4", "2.0.0"}:    "major",
		{"25.9.11", "25.9.12"}: "patch",
		{"v2.10.0", "2.10.1"}:  "patch",
		{"latest", "1.0.0"}:    "major",
	}
	for in, want := range cases {
		if got := VersionKind(in[0], in[1]); got != want {
			t.Errorf("%s → %s: %s, want %s", in[0], in[1], got, want)
		}
	}
}

func TestPatches(t *testing.T) {
	up := []Update{
		{Name: "a", Kind: "patch"},
		{Name: "b", Kind: "minor"},
		{Name: "c", Kind: "major"},
	}

	got := Patches(up)
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("%v", got)
	}
}
