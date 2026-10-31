// Package mobile is the gomobile-facing API: simple types only, no generics,
// no channels. The UI calls it through the generated bindings.
package mobile

import (
	"fmt"
	"os"
	"sort"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

// Link is one copyable entry of a user card.
type Link struct {
	Label string
	URL   string
}

// UserInfo is what the list screen needs.
type UserInfo struct {
	Name      string
	Modes     []string
	Protocols []string
	TG        bool
	Enabled   bool
}

// App holds the opened state; the UI keeps one instance.
type App struct {
	st   *state.State
	repo *gitsync.Repo
	dir  string
}

// Open loads the state directory. keyPath may be empty to use the default age key.
func Open(dir, keyPath string) (*App, error) {
	if keyPath != "" {
		if err := setKey(keyPath); err != nil {
			return nil, err
		}
	}
	st, err := state.Load(dir)
	if err != nil {
		return nil, err
	}
	repo, err := gitsync.Open(dir)
	if err != nil {
		repo = nil // read-only mode is fine
	}
	return &App{st: st, repo: repo, dir: dir}, nil
}

func setKey(path string) error {
	return os.Setenv("VPNCTL_AGE_KEY", path)
}

// Users returns the list, sorted by name.
func (a *App) Users() []UserInfo {
	names := make([]string, 0, len(a.st.Users))
	for n := range a.st.Users {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]UserInfo, 0, len(names))
	for _, n := range names {
		u := a.st.Users[n]
		out = append(out, UserInfo{
			Name:      n,
			Modes:     u.Modes,
			Protocols: u.Protocols,
			TG:        u.TG,
			Enabled:   u.Enabled == nil || *u.Enabled,
		})
	}
	return out
}

// Links returns the subscription and the direct links of a user.
func (a *App) Links(user string) ([]Link, error) {
	u, ok := a.st.Users[user]
	if !ok {
		return nil, fmt.Errorf("user %q is not described", user)
	}
	entry, err := a.st.Entry()
	if err != nil {
		return nil, err
	}
	mode := "split"
	if len(u.Modes) > 0 {
		mode = u.Modes[0]
	}
	var out []Link
	if sub, err := render.SubURL(a.st, entry, user, mode); err == nil {
		out = append(out, Link{Label: "subscription " + mode, URL: sub})
	}
	links, err := render.Links(a.st, entry, user, mode)
	if err != nil {
		return nil, err
	}
	for i, l := range links {
		out = append(out, Link{Label: fmt.Sprintf("direct %d", i+1), URL: l})
	}
	if tg, ok := render.TGLink(a.st, entry); ok {
		out = append(out, Link{Label: "telegram", URL: tg})
	}
	return out, nil
}

// Share returns the ready-to-forward message of a user, in the system language.
func (a *App) Share(user string) (string, error) { return render.Share(a.st, user) }

// Message returns the message for a user in lang (ru or en): text or markdown.
func (a *App) Message(user, lang string, markdown bool) (string, error) {
	return render.Message(a.st, user, render.MessageOptions{Lang: lang, Markdown: markdown})
}

// AddUser creates a user, writes the state and commits it.
func (a *App) AddUser(name string, modes, protocols []string, tg bool) (string, error) {
	msg, err := ops.UserAdd(a.st, name, modes, protocols, tg)
	if err != nil {
		return "", err
	}
	if err := state.SaveUsers(a.dir, a.st.Users); err != nil {
		return "", err
	}
	if err := state.SaveSecrets(a.dir, a.st.Secrets); err != nil {
		return "", err
	}
	if errs := validate.State(a.st); len(errs) > 0 {
		return "", fmt.Errorf("invalid state: %s", errs[0])
	}
	if a.repo != nil {
		if err := a.repo.Commit(msg); err != nil {
			return "", err
		}
	}
	return msg, nil
}

// Sync pushes the state to origin.
func (a *App) Sync() error {
	if a.repo == nil {
		return fmt.Errorf("no git repository")
	}
	return a.repo.Push()
}
