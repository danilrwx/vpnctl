package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// openFixture copies the shared fixture and fills its secrets with stubs, the
// way the CLI does without the age key. The app is the interface the phone will
// use, so it is tested as plain Go even though it is built with gomobile.
func openFixture(t *testing.T) *App {
	t.Helper()

	src := filepath.Join("..", "testdata", "basic")
	dir := t.TempDir()

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, sub := range []string{"routing", "sets", "templates"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}

		if ents, err := os.ReadDir(filepath.Join(src, sub)); err == nil {
			for _, e := range ents {
				b, err := os.ReadFile(filepath.Join(src, sub, e.Name()))
				if err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(filepath.Join(dir, sub, e.Name()), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// the fixture ships plain-text secrets under the sops name: load them the
	// way the CLI's --stub does, without touching the encrypted file
	b, err := os.ReadFile(filepath.Join(dir, "secrets.sops.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var sec state.Secrets
	if err := yaml.Unmarshal(b, &sec); err != nil {
		t.Fatal(err)
	}

	app, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	app.st.Secrets = sec

	return app
}

// TestUsersAndShare is the surface the phone calls first: a list of users and
// the one message a person forwards.
func TestUsersAndShare(t *testing.T) {
	app := openFixture(t)

	users := app.Users()
	if len(users) == 0 {
		t.Fatal("no users")
	}

	var brat bool

	for _, u := range users {
		if u.Name == "brat" {
			brat = u.Enabled
		}
	}

	if !brat {
		t.Fatal("brat is enabled in the fixture but not in the list")
	}

	share, err := app.Share("brat")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(share, "vless://") {
		t.Fatalf("the shared text has no link:\n%s", share)
	}

	if _, err := app.Share("ghost"); err == nil {
		t.Fatal("sharing for an unknown user was accepted")
	}
}

// TestMessageLanguages: the message is user-facing text and must exist in both
// languages the app offers.
func TestMessageLanguages(t *testing.T) {
	app := openFixture(t)
	for _, lang := range []string{"ru", "en"} {
		msg, err := app.Message("brat", lang, false)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}

		if strings.TrimSpace(msg) == "" {
			t.Fatalf("%s: an empty message", lang)
		}
	}
}

// TestPlanRejectsBrokenState: the app is not allowed to plan a state the CLI
// would refuse.
func TestPlanRejectsBrokenState(t *testing.T) {
	app := openFixture(t)

	app.st.Users["brat"] = state.User{Modes: []string{"weird"}, Protocols: []string{"reality"}}
	if _, err := app.Plan(""); err == nil {
		t.Fatal("plan accepted an invalid state")
	}
}
