// Package mobile is the gomobile-facing API: simple types only, no generics,
// no channels. The UI calls it through the generated bindings.
package mobile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/ageio"
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
	// The render of an older commit needs scratch space. On Android the system
	// temp (/data/local/tmp) is not writable by the app, and a plan that cannot
	// render the applied commit gives up on drift, so the scratch goes next to
	// the state instead of into the process environment.
	if tmp := scratchDir(dir); tmp != "" {
		plan.TempBase = tmp
	}

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

// scratchDir creates the app-owned temp directory the render needs.
func scratchDir(dir string) string {
	tmp := filepath.Join(filepath.Dir(dir), "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return ""
	}

	return tmp
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
func (a *App) AddUser(name, modes, protocols string, tg bool, expires string) (string, error) {
	// the date is checked before anything is created: a user that cannot be
	// fixed from the phone is worse than an error on the form
	day, err := expiryDate(expires)
	if err != nil {
		return "", err
	}

	msg, err := ops.UserAdd(a.st, name, splitList(modes), splitList(protocols), tg)
	if err != nil {
		return "", err
	}

	if day != "" {
		u := a.st.Users[name]
		u.Expires = day
		a.st.Users[name] = u
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

// HostInfo is one row of the hosts screen: what the state says, not what the
// host answers. The marker and reachability come from a plan, which probes.
type HostInfo struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Engine   string `json:"engine"`
	Domain   string `json:"domain,omitempty"`
	IP       string `json:"ip,omitempty"`
	Via      string `json:"via,omitempty"`
	Retiring bool   `json:"retiring,omitempty"`
}

// Device is one recipient of secrets.age.yaml: a phone, a laptop or the backup.
type Device struct {
	Name      string `json:"name"`
	Recipient string `json:"recipient"`
	Added     string `json:"added,omitempty"`
}

// Devices returns who can read the secrets, as JSON. It is the state file, not
// a live query: the phone cannot know which of those devices still answer.
func (a *App) Devices() (string, error) {
	recs, err := ageio.LoadRecipients(a.dir)
	if err != nil {
		return "", err
	}

	out := make([]Device, 0, 8)

	if recs != nil {
		for _, r := range recs.Recipients {
			out = append(out, Device{Name: r.Name, Recipient: r.Key, Added: r.Added})
		}
	}

	return mustJSON(out), nil
}

// HostLink is one node-to-node link, as the state describes it.
type HostLink struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Protocols []string `json:"protocols,omitempty"`
	Active    string   `json:"active,omitempty"`
	Policy    string   `json:"policy,omitempty"`
}

// HostLinks returns the links between hosts, sorted, as JSON.
func (a *App) HostLinks() string {
	out := make([]HostLink, 0, len(a.st.Links))

	for _, l := range a.st.Links {
		out = append(out, HostLink{
			From: l.From, To: l.To, Protocols: l.Protocols, Active: l.Active, Policy: l.Policy,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}

		return out[i].To < out[j].To
	})

	return mustJSON(out)
}

// Hosts returns the estate as JSON, sorted by name.
func (a *App) Hosts() string {
	names := make([]string, 0, len(a.st.Hosts))
	for n := range a.st.Hosts {
		names = append(names, n)
	}

	sort.Strings(names)

	out := make([]HostInfo, 0, len(names))

	for _, n := range names {
		h := a.st.Hosts[n]
		out = append(out, HostInfo{
			Name: n, Role: string(h.Role), Engine: h.Engine,
			Domain: h.Domain, IP: h.IP, Via: h.Via, Retiring: h.Retiring,
		})
	}

	return mustJSON(out)
}

// Protocols returns the protocols the entry actually serves, as JSON. The screen
// offers only these: choosing one the entry has no inbound for passes nothing
// until save, and the refusal looks like the app's mistake, not the state's.
func (a *App) Protocols() string {
	entry, err := a.st.Entry()
	if err != nil {
		return "[]"
	}

	names := make([]string, 0, len(a.st.Inbounds[entry]))
	for name, in := range a.st.Inbounds[entry] {
		if in.IsEnabled() {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	return mustJSON(names)
}

// EditUser replaces what a user gets: the modes, the protocols, the Telegram
// flag and the day the term ends. The screen shows the current values, so it
// sends the whole form back instead of the one field that changed; an empty
// date means no deadline at all, which the tick respects.
func (a *App) EditUser(name, modes, protocols string, tg bool, expires string) (string, error) {
	if len(splitList(modes)) == 0 || len(splitList(protocols)) == 0 {
		return "", fmt.Errorf("user %q: a mode and a protocol are required", name)
	}

	date, err := expiryDate(expires)
	if err != nil {
		return "", err
	}

	msg, err := ops.UserEdit(a.st, name, ops.UserPatch{
		Modes: splitList(modes), Protocols: splitList(protocols), TG: &tg, Expires: &date,
	})
	if err != nil {
		return "", err
	}

	if err := a.save(msg); err != nil {
		return "", err
	}

	return msg, nil
}

// RmUser removes a user and its secrets. The hosts keep the account until the
// next apply, so the answer is a commit, not a disconnection.
func (a *App) RmUser(name string) (string, error) {
	msg, err := ops.UserRm(a.st, name)
	if err != nil {
		return "", err
	}

	if err := a.save(msg); err != nil {
		return "", err
	}

	return msg, nil
}

// expiryDate is the one rule the form has for the date: empty means no deadline
// at all, which the tick respects, and anything else has to be a day.
func expiryDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("%q is not a date: use YYYY-MM-DD", s)
	}

	return s, nil
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

// Progress receives the lines apply prints while it works. A series over three
// hosts takes long enough that a single spinner is indistinguishable from a
// hang, so the phone shows the same lines the command line does.
type Progress interface {
	Update(line string)
}

// progressWriter cuts the apply's output into lines for the UI.
type progressWriter struct {
	p   Progress
	buf []byte
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)

	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}

		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]

		if line != "" {
			w.p.Update(line)
		}
	}

	return len(b), nil
}

// Apply installs the state on the hosts and returns the result as JSON.
func (a *App) Apply(host string) (string, error) { return a.apply(host, nil) }

// ApplyProgress is Apply with the progress lines going to the caller.
func (a *App) ApplyProgress(host string, p Progress) (string, error) { return a.apply(host, p) }

func (a *App) apply(host string, p Progress) (string, error) {
	out := io.Discard
	if p != nil {
		out = &progressWriter{p: p}
	}

	opts := &apply.Opts{Host: host}

	if signer != nil {
		// the guards fetch before they touch a host, and that fetch needs the
		// same credential the clone used
		if sshSigner, err := gitsync.SSHSigner(vpnctlSigner{signer}); err == nil {
			opts.FetchAuth = gitsync.GitAuth("git", sshSigner)
		}

		opts.Prober = func(host string, paths []string) (plan.Probe, error) {
			return agentProbe(a.st, host, paths)
		}

		// a phone has no ssh key on the hosts, only the agent's
		opts.AgentApply = agentApply
		opts.AgentMark = agentMark
	}

	res, err := apply.Apply(a.st, opts, out)
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

// Head is the commit the app is looking at. The user card's footer names it, so
// a screen can say which revision of the state it shows after every change.
func (a *App) Head() string {
	if a.repo == nil {
		return ""
	}

	head, err := a.repo.Head()
	if err != nil {
		return ""
	}

	return head
}

// Diff is one file of drift: what the host holds against what the render wants.
// It is the same unified diff the command line prints.
func (a *App) Diff(host, path string) (string, error) {
	rendered, err := render.Check(a.st, time.Now())
	if err != nil {
		return "", err
	}

	return plan.DiffWith(a.st, rendered, host, path, func(h, p string) (string, error) {
		return agentRead(a.st, h, p)
	})
}

// Adopt takes the host's copy of a drifted file into overrides/<host>/<path>, so
// the next apply keeps it instead of overwriting it. It commits like any other
// change the phone makes.
func (a *App) Adopt(host, path string) (string, error) {
	msg, err := plan.AdoptWith(a.st, host, path, func(h, p string) (string, error) {
		return agentRead(a.st, h, p)
	})
	if err != nil {
		return "", err
	}

	if a.repo != nil {
		if err := a.repo.Commit(msg); err != nil {
			return "", err
		}
	}

	return msg, nil
}
