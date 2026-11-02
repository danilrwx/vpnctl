package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
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

	// the list crosses the gomobile boundary as JSON: a struct with a slice
	// field is not bindable, and gobind drops such a method without a word
	var users []UserInfo
	if err := json.Unmarshal([]byte(app.Users()), &users); err != nil {
		t.Fatalf("users are not JSON: %v", err)
	}

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

// TestRenewAndDisable: the two actions a phone is most likely to be opened for.
// Both go through save, which validates and writes before anything is shown.
func TestRenewAndDisable(t *testing.T) {
	app := openFixture(t)

	msg, err := app.Renew("olga", 30)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	if !strings.Contains(msg, "renew") {
		t.Fatalf("message %q", msg)
	}

	if app.st.Users["olga"].Expires == "" {
		t.Fatal("no expiry was written")
	}

	if _, err := app.SetEnabled("olga", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	if !app.st.Users["olga"].IsEnabled() {
		t.Fatal("the user is still off")
	}

	if _, err := app.SetEnabled("ghost", false); err == nil {
		t.Fatal("an unknown user was accepted")
	}

	if _, err := app.Renew("ghost", 30); err == nil {
		t.Fatal("renewing an unknown user was accepted")
	}
}

// TestHeadIsTheShortCommit: the user card's footer names the revision on screen,
// so the value has to be the short hash git shows. A state that is not a
// repository has nothing to name, and the footer falls back to a dash.
func TestHeadIsTheShortCommit(t *testing.T) {
	dir := t.TempDir()

	repo, err := gitsync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Commit writes nothing for a clean tree, so the repository needs a file
	if err := os.WriteFile(filepath.Join(dir, "users.yaml"), []byte("users: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state"); err != nil {
		t.Fatal(err)
	}

	app := &App{repo: repo}

	head := app.Head()
	if len(head) != 7 {
		t.Fatalf("head %q is not a short hash", head)
	}

	empty := &App{}
	if empty.Head() != "" {
		t.Fatalf("a state without a repository named a commit: %q", empty.Head())
	}
}
