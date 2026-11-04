package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUserHandoffRoundTrip(t *testing.T) {
	a := openFixture(t)
	if _, err := a.AddUser("handoff", "split,full", "hy2,awg", false, ""); err != nil {
		t.Fatal(err)
	}

	links, err := a.Links("handoff")
	if err != nil {
		t.Fatal(err)
	}

	var got []Link
	if err := json.Unmarshal([]byte(links), &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 {
		t.Fatalf("links: %s", links)
	}

	for i, label := range []string{"subscription split", "hy2 split", "subscription full", "hy2 full"} {
		if got[i].Label != label {
			t.Fatalf("link %d: %+v", i, got[i])
		}
	}

	profile, err := a.UserProfiles("handoff")
	if err != nil {
		t.Fatal(err)
	}

	var profiles []UserProfile
	if err := json.Unmarshal([]byte(profile), &profiles); err != nil {
		t.Fatal(err)
	}

	if len(profiles) != 1 || profiles[0].Protocol != "awg" || !strings.Contains(profiles[0].Content, "PrivateKey = "+a.st.Secrets.Users["handoff"].AWG.Private) {
		t.Fatalf("profiles: %s", profile)
	}

	if _, err := a.EditUser("handoff", "full", "hy2,awg", false, ""); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(a.dir, "")
	if err != nil {
		t.Fatal(err)
	}

	profile, err = reopened.UserProfiles("handoff")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(profile, `"mode":"full"`) {
		t.Fatalf("edited profile: %s", profile)
	}

	if _, err := reopened.EditUser("handoff", "full", "awg", false, ""); err != nil {
		t.Fatal(err)
	}

	if links, err := reopened.Links("handoff"); err != nil || links != "[]" {
		t.Fatalf("AWG-only links: %s %v", links, err)
	}

	if _, err := reopened.SetEnabled("handoff", false); err != nil {
		t.Fatal(err)
	}

	if profiles, err := reopened.UserProfiles("handoff"); err != nil || profiles != "[]" {
		t.Fatalf("disabled: %s %v", profiles, err)
	}
}

func diskFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			out[path] = string(b)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestInvalidMutationsDoNotWrite(t *testing.T) {
	a := openFixture(t)
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"create", func() error { _, err := a.AddUser("invalid", "invalid", "hy2", false, ""); return err }},
		{"edit", func() error { _, err := a.EditUser("brat", "invalid", "hy2", false, ""); return err }},
		{"inbound", func() error { _, err := a.SaveInbound("ru-msk-1", "hy2", -1, "", ""); return err }},
	} {
		t.Run(action.name, func(t *testing.T) {
			before, err := json.Marshal(a.st)
			if err != nil {
				t.Fatal(err)
			}

			disk := diskFiles(t, a.dir)
			if err := action.run(); err == nil {
				t.Fatal("invalid mutation accepted")
			}

			after, err := json.Marshal(a.st)
			if err != nil {
				t.Fatal(err)
			}

			if string(before) != string(after) {
				t.Fatal("state changed")
			}

			if !reflect.DeepEqual(disk, diskFiles(t, a.dir)) {
				t.Fatal("disk changed")
			}
		})
	}
}

func TestLinksUseUsersOwnEntry(t *testing.T) {
	a := openFixture(t)
	host := a.st.Hosts["ru-msk-1"]
	host.Domain = "other.example"
	a.st.Hosts["other"] = host
	a.st.Inbounds["other"] = a.st.Inbounds["ru-msk-1"]
	a.st.Secrets.Hosts["other"] = a.st.Secrets.Hosts["ru-msk-1"]
	u := a.st.Users["brat"]
	u.Entry, u.TG = "other", false
	a.st.Users["brat"] = u

	links, err := a.Links("brat")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(links, "other.example") || strings.Contains(links, "telegram") {
		t.Fatal(links)
	}

	profiles, err := a.UserProfiles("brat")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(profiles, "other.example") {
		t.Fatal(profiles)
	}
}
