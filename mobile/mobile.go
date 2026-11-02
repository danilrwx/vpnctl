// Package mobile is the gomobile-facing API: simple types only, no generics,
// no channels. The UI calls it through the generated bindings.
package mobile

import (
	"bytes"
	"encoding/base64"
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
	"github.com/danilrwx/vpnctl-go/internal/agent"
	"github.com/danilrwx/vpnctl-go/internal/apply"
	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/ops"
	"github.com/danilrwx/vpnctl-go/internal/plan"
	"github.com/danilrwx/vpnctl-go/internal/remote"
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

// Pull fetches origin and fast-forwards the state. The phone could only clone
// and push, so a state that moved on the laptop stayed stale here until the
// operator reconnected — which deletes the local copy.
func (a *App) Pull() (string, error) {
	if a.repo == nil {
		return "", fmt.Errorf("no git repository")
	}

	if err := a.repo.Fetch(); err != nil {
		return "", err
	}

	// uncommitted work would be buried by the hard reset the fast-forward uses
	if clean, err := a.repo.Clean(); err == nil && !clean {
		return "", fmt.Errorf("the state has uncommitted changes: push them before pulling")
	}

	head, err := a.repo.FastForward()
	if err != nil {
		return "", err
	}

	// the files under the app changed, and so did the secrets age decrypts
	st, err := state.Load(a.dir)
	if err != nil {
		return "", err
	}

	a.st = st

	return head, nil
}

// RepoState tells the Overview which branch it shows and whether the tree is
// clean: the same two facts the command line prints before an apply.
func (a *App) RepoState() string {
	out := struct {
		Branch string `json:"branch"`
		Clean  bool   `json:"clean"`
		Head   string `json:"head"`
	}{}

	if a.repo != nil {
		if branch, err := a.repo.Branch(); err == nil {
			out.Branch = branch
		}

		if clean, err := a.repo.Clean(); err == nil {
			out.Clean = clean
		}

		if head, err := a.repo.Head(); err == nil {
			out.Head = head
		}
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}

	return string(b)
}

// AddDevice adds a recipient that was generated on another device. The phone
// can hand out access without waiting for the laptop; the key itself still has
// to be produced by the device that will hold it.
func (a *App) AddDevice(name, recipient string) (string, error) {
	msg, err := ops.KeyAdd(a.st, name, recipient)
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

// NewBackup replaces the offline backup identity and returns the sealed blob,
// which the caller shows once and stores outside every device.
func (a *App) NewBackup(passphrase string) (string, error) {
	blob, msg, err := ops.KeyBackup(a.st, passphrase)
	if err != nil {
		return "", err
	}

	if a.repo != nil {
		if err := a.repo.Commit(msg); err != nil {
			return "", err
		}
	}

	return blob, nil
}

// AddHost creates a host with its inbound, keys and routing, the way `host add`
// does. The caller still has to bootstrap it: that needs the host to answer, and
// a phone can only reach hosts through the agent the bootstrap installs.
func (a *App) AddHost(name, role, ssh, ip, domain, via, engine string, awg bool) (string, error) {
	msg, err := ops.HostAdd(a.st, ops.HostOptions{
		Name: name, Role: role, SSH: ssh, IP: ip, Domain: domain, Via: via, Engine: engine, AWG: awg,
	})
	if err != nil {
		return "", err
	}

	if err := ops.HostFiles(a.st, name); err != nil {
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

// AddLink appends a link between two hosts, with the transports it may carry.
func (a *App) AddLink(from, to, protocols, active, policy string) (string, error) {
	msg, err := ops.LinkAdd(a.st, from, to, splitList(protocols), active, policy)
	if err != nil {
		return "", err
	}

	if err := state.SaveLinks(a.dir, a.st.Links); err != nil {
		return "", err
	}

	// the link brought its own key material
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

// Metrics returns the newest machine samples a host collected, as JSON. The
// agent reads its own log; the phone has no shell to cat it.
func (a *App) Metrics(host string) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: the device cannot ask the agent")
	}

	c, err := remote.Dial(a.st, host)
	if err != nil {
		return "", err
	}

	defer func() { _ = c.Close() }()

	req := &agent.Request{V: agent.Version, Op: "metrics", Host: host, TS: time.Now().Unix(), Nonce: nonce()}
	req.TreeHash = agent.TreeHash(nil)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		return "", err
	}

	req.Sig = base64.StdEncoding.EncodeToString(sig)

	resp, err := agent.Call(c, req)
	if err != nil {
		return "", err
	}

	return mustJSON(resp.Samples), nil
}

// Alerts returns the notification setup: the public topic URL and the entry
// that serves it. The token stays in the secrets file and is only reported as
// present or not.
func (a *App) Alerts() string {
	out := struct {
		URL   string `json:"url"`
		Serve string `json:"serve"`
		Token bool   `json:"token"`
	}{URL: a.st.Alerts.Ntfy, Serve: a.st.Alerts.Serve, Token: a.st.Secrets.Alerts.Token != ""}

	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}

	return string(b)
}

// SetAlerts points notifications at the entry's own ntfy server, or turns them
// off with an empty topic. The token is left alone: a phone cannot mint it, and
// an empty one still delivers to a topic that needs no auth.
func (a *App) SetAlerts(serve, topic string) (string, error) {
	var (
		msg string
		err error
	)

	if topic == "" {
		msg, err = ops.AlertsSet(a.st, "", "")
	} else {
		msg, err = ops.AlertsServe(a.st, serve, topic, a.st.Secrets.Alerts.Token)
	}

	if err != nil {
		return "", err
	}

	if err := state.SaveAlerts(a.dir, a.st.Alerts); err != nil {
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

// SetLink adds or removes transports on an existing link and can move the
// active one. The link secrets are untouched: a removed transport keeps its keys
// for the day it comes back.
func (a *App) SetLink(from, to, add, rm, active, policy string) (string, error) {
	msg, err := ops.LinkSet(a.st, from, to, splitList(add), splitList(rm), active, policy)
	if err != nil {
		return "", err
	}

	if err := state.SaveLinks(a.dir, a.st.Links); err != nil {
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

// EditHost changes the address, domain, via or engine of a host. Everything
// that rewrites links and keys — rename, role — stays a remove-and-add job.
func (a *App) EditHost(name, ssh, ip, domain, via, engine string) (string, error) {
	msg, err := ops.HostEdit(a.st, name, ops.HostOptions{SSH: ssh, IP: ip, Domain: domain, Via: via, Engine: engine})
	if err != nil {
		return "", err
	}

	if err := state.SaveHosts(a.dir, a.st.Hosts); err != nil {
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

// AddRule appends a routing rule to a chain. The end is deliberate: the list is
// top-down and first match wins.
func (a *App) AddRule(host, chain, sets, geoip, inbound, domains, ips, action, note string) (string, error) {
	msg, err := ops.RuleAdd(a.st, host, chain, ops.RuleInput{
		Sets: splitList(sets), GeoIP: splitList(geoip), Inbound: splitList(inbound),
		Domains: splitList(domains), IPs: splitList(ips), Action: action, Note: note,
	})
	if err != nil {
		return "", err
	}

	if err := a.saveRouting(host, msg); err != nil {
		return "", err
	}

	return msg, nil
}

// RemoveRule drops one rule by its position.
func (a *App) RemoveRule(host, chain string, index int) (string, error) {
	msg, err := ops.RuleRemove(a.st, host, chain, index)
	if err != nil {
		return "", err
	}

	if err := a.saveRouting(host, msg); err != nil {
		return "", err
	}

	return msg, nil
}

// MoveRule swaps a rule with its neighbour.
func (a *App) MoveRule(host, chain string, index int, up bool) (string, error) {
	msg, err := ops.RuleMove(a.st, host, chain, index, up)
	if err != nil {
		return "", err
	}

	if err := a.saveRouting(host, msg); err != nil {
		return "", err
	}

	return msg, nil
}

// ToggleRule turns a rule off without losing its place in the chain.
func (a *App) ToggleRule(host, chain string, index int, on bool) (string, error) {
	msg, err := ops.RuleToggle(a.st, host, chain, index, on)
	if err != nil {
		return "", err
	}

	if err := a.saveRouting(host, msg); err != nil {
		return "", err
	}

	return msg, nil
}

func (a *App) saveRouting(host, msg string) error {
	if err := state.SaveRouting(a.dir, host, a.st.Routing[host]); err != nil {
		return err
	}

	if errs := validate.State(a.st); len(errs) > 0 {
		return fmt.Errorf("invalid state: %s", errs[0])
	}

	if a.repo != nil {
		return a.repo.Commit(msg)
	}

	return nil
}

// Certs returns the certificates a host holds, as JSON: the name Caddy filed
// them under, the expiry and the covered names. A host without Caddy answers
// with an empty list.
func (a *App) Certs(host string) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("no signer: the device cannot ask the agent")
	}

	c, err := remote.Dial(a.st, host)
	if err != nil {
		return "", err
	}

	defer func() { _ = c.Close() }()

	req := &agent.Request{V: agent.Version, Op: "cert", Host: host, TS: time.Now().Unix(), Nonce: nonce()}
	req.TreeHash = agent.TreeHash(nil)

	sig, err := SignMessage([]byte(agent.Message(req)))
	if err != nil {
		return "", err
	}

	req.Sig = base64.StdEncoding.EncodeToString(sig)

	resp, err := agent.Call(c, req)
	if err != nil {
		return "", err
	}

	return mustJSON(resp.Certs), nil
}

// ToggleInbound turns one inbound of a host on or off. The change is written and
// validated now and reaches the host on the next apply.
func (a *App) ToggleInbound(host, name string, on bool) (string, error) {
	msg, err := ops.InboundToggle(a.st, host, name, on)
	if err != nil {
		return "", err
	}

	if err := state.SaveInbounds(a.dir, a.st.Inbounds); err != nil {
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

// InboundInfo is one inbound of a host as the screen shows it.
type InboundInfo struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Enabled bool   `json:"enabled"`
}

// Inbounds returns the inbounds of a host, sorted by name, as JSON.
func (a *App) Inbounds(host string) string {
	inb, ok := a.st.Inbounds[host]
	if !ok {
		return "[]"
	}

	out := make([]InboundInfo, 0, len(inb))

	for name, i := range inb {
		out = append(out, InboundInfo{Name: name, Port: i.Port, Enabled: i.IsEnabled()})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return mustJSON(out)
}
