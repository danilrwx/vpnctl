// Package mobile is the gomobile-facing API: simple types only, no generics,
// no channels. The UI calls it through the generated bindings.
package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/apply"
	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
	"github.com/danilrwx/vpnctl-go/internal/validate"
)

// Link is one copyable entry of a user card. gomobile cannot bind a struct with
// a slice field and drops such a method without a word, so every list crosses
// the boundary as JSON: one shape, parseable by any client.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// UserInfo is what the list screen needs, as JSON.
type UserInfo struct {
	Name      string   `json:"name"`
	Modes     []string `json:"modes"`
	Protocols []string `json:"protocols"`
	TG        bool     `json:"tg"`
	Enabled   bool     `json:"enabled"`
	Expires   string   `json:"expires,omitempty"`
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
func (a *App) Users() string {
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
			Expires:   u.Expires,
		})
	}

	return mustJSON(out)
}

// mustJSON marshals what the UI gets back; a failure here is a programming
// error, and an empty list is better than a crash on the list screen.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}

	return string(b)
}

// Links returns the subscription and the direct links of a user, as JSON.
func (a *App) Links(user string) (string, error) {
	u, ok := a.st.Users[user]
	if !ok {
		return "", fmt.Errorf("user %q is not described", user)
	}

	entry, err := a.st.Entry()
	if err != nil {
		return "", err
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
		return "", err
	}

	for i, l := range links {
		out = append(out, Link{Label: fmt.Sprintf("direct %d", i+1), URL: l})
	}

	if tg, ok := render.TGLink(a.st, entry); ok {
		out = append(out, Link{Label: "telegram", URL: tg})
	}

	return mustJSON(out), nil
}

// splitList turns "split,full" into the list the ops layer wants.
func splitList(s string) []string {
	var out []string

	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}

	return out
}

// Share returns the ready-to-forward message of a user, in the system language.
func (a *App) Share(user string) (string, error) { return render.Share(a.st, user) }

// Message returns the message for a user in lang (ru or en): text or markdown.
func (a *App) Message(user, lang string, markdown bool) (string, error) {
	return render.Message(a.st, user, render.MessageOptions{Lang: lang, Markdown: markdown})
}

// AddUser creates a user, writes the state and commits it.
func (a *App) AddUser(name, modes, protocols string, tg bool) (string, error) {
	msg, err := ops.UserAdd(a.st, name, splitList(modes), splitList(protocols), tg)
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

// save is the one path a phone takes after changing the state: validate, write,
// commit. Push is a separate step because it needs the forge to be reachable.
func (a *App) save(msg string) error {
	if errs := validate.State(a.st); len(errs) > 0 {
		return fmt.Errorf("invalid state: %s", errs[0])
	}

	if err := state.SaveUsers(a.dir, a.st.Users); err != nil {
		return err
	}

	if err := state.SaveSecrets(a.dir, a.st.Secrets); err != nil {
		return err
	}

	if a.repo != nil {
		return a.repo.Commit(msg)
	}

	return nil
}

// SetEnabled turns a user on or off without touching its credentials.
func (a *App) SetEnabled(name string, on bool) (string, error) {
	u, ok := a.st.Users[name]
	if !ok {
		return "", fmt.Errorf("user %q is not described", name)
	}

	u.Enabled = &on
	a.st.Users[name] = u

	msg := "users: " + name + ": disable"
	if on {
		msg = "users: " + name + ": enable"
	}

	if err := a.save(msg); err != nil {
		return "", err
	}

	return msg, nil
}

// Renew extends a term from the later of today and the current date.
func (a *App) Renew(name string, days int) (string, error) {
	msg, err := ops.UserRenew(a.st, name, days, "", time.Now().UTC())
	if err != nil {
		return "", err
	}

	if err := a.save(msg); err != nil {
		return "", err
	}

	return msg, nil
}

// Push sends what the phone committed to the repository. It uses the same
// Keystore key the clone used: one identity, two jobs.
func (a *App) Push() (string, error) {
	if a.repo == nil {
		return "", fmt.Errorf("no repository: this state was not cloned")
	}

	if signer == nil {
		return "", fmt.Errorf("no signer: the device cannot authenticate to git")
	}

	sshSigner, err := gitsync.SSHSigner(vpnctlSigner{signer})
	if err != nil {
		return "", err
	}

	remotes, err := a.repo.Remotes()
	if err != nil {
		return "", err
	}

	if len(remotes) == 0 {
		return "", fmt.Errorf("no remote to push to")
	}

	url := remotes[0].Config().URLs[0]
	if err := a.repo.PushAuth(gitsync.GitAuth(gitUser(url), sshSigner)); err != nil {
		return "", err
	}

	return a.repo.Head()
}

// Plan returns the plan as JSON: the same shape the CLI prints with --json.
// An empty host plans every host of the state.
func (a *App) Plan(host string) (string, error) {
	if errs := validate.State(a.st); len(errs) > 0 {
		return "", fmt.Errorf("invalid state: %s", errs[0])
	}

	rendered, err := render.Check(a.st, time.Now().UTC())
	if err != nil {
		return "", err
	}

	one := render.Rendered{}

	for name, files := range rendered {
		if host == "" || host == name {
			one[name] = files
		}
	}

	var prober plan.Prober
	if signer != nil {
		// the phone's key is restricted to the agent, so the shell probe cannot work
		prober = func(host string, paths []string) (plan.Probe, error) {
			return agentProbe(a.st, host, paths)
		}
	}

	plans, err := plan.PlanWith(a.st, one, time.Now().UTC(), prober)
	if err != nil {
		return "", err
	}

	b, err := json.MarshalIndent(plan.JSONOf(plans), "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// Apply installs the state on the hosts and returns the result as JSON.
func (a *App) Apply(host string) (string, error) {
	opts := &apply.Opts{Host: host}
	if signer != nil {
		// a phone has no ssh key on the hosts, only the agent's
		opts.AgentApply = agentApply
	}

	res, err := apply.Apply(a.st, opts, io.Discard)
	if err != nil {
		var ge *apply.GuardError
		if errors.As(err, &ge) {
			return "", fmt.Errorf("guard: %w", err)
		}

		return "", err
	}

	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// Sync pushes the state to origin.
func (a *App) Sync() error {
	if a.repo == nil {
		return fmt.Errorf("no git repository")
	}

	return a.repo.Push()
}
