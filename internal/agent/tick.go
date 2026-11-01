package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/render"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// TickOptions is what a timer hands the agent: where the plan is, what to edit
// and how to check it. The paths are rendered into the unit, so a host with xray
// gets xray's config and nobody guesses.
type TickOptions struct {
	ExpiryPath   string
	EngineConfig string
	EngineBin    string
	SubRoot      string
	AWGConfig    string
	ExpiredDir   string
	AlertsEnv    string
	Now          time.Time
	DryRun       bool
	// Run executes one shell command and returns its output. Tests replace it.
	Run func(cmd string) (string, error)
}

// TickReport says what the timer did: which accounts expired, which were taken
// off the host now, and which subscriptions and peers went with them.
type TickReport struct {
	Expired []string `json:"expired"`
	Removed []string `json:"removed"`
	Subs    []string `json:"subs"`
	Peers   []string `json:"peers"`
	Changed bool     `json:"changed"`
}

func (o *TickOptions) defaults() {
	if o.ExpiryPath == "" {
		o.ExpiryPath = "/etc/vpnctl/expiry"
	}

	if o.EngineConfig == "" {
		o.EngineConfig = "/etc/sing-box/config.json"
	}

	if o.EngineBin == "" {
		o.EngineBin = "sing-box"
	}

	if o.SubRoot == "" {
		o.SubRoot = "/var/www/sub"
	}

	if o.ExpiredDir == "" {
		o.ExpiredDir = "/var/lib/vpnctl/expired"
	}

	if o.AlertsEnv == "" {
		o.AlertsEnv = "/etc/vpnctl/alerts.env"
	}

	if o.Now.IsZero() {
		o.Now = time.Now()
	}

	if o.Run == nil {
		o.Run = func(cmd string) (string, error) {
			out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
			return string(out), err
		}
	}
}

// Tick disables the accounts whose day has come. It runs from a systemd timer,
// so it must be boring and idempotent: read the plan apply wrote, compare dates,
// and when something expired, edit the engine config the way apply would have
// done that day — check first, then swap, restart and probe, and put everything
// back if the chain stops answering.
func Tick(opts TickOptions) (TickReport, error) {
	opts.defaults()

	rep := TickReport{Expired: []string{}, Removed: []string{}, Subs: []string{}, Peers: []string{}}

	body, err := os.ReadFile(opts.ExpiryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rep, nil
		}

		return rep, err
	}

	exp, err := state.ParseExpiry(body)
	if err != nil {
		return rep, err
	}

	var gone []string

	for name, e := range exp.Users {
		if state.Expired(e.Expires, opts.Now) {
			gone = append(gone, name)
		}
	}

	sort.Strings(gone)

	if len(gone) == 0 {
		return rep, nil
	}

	rep.Expired = gone

	emails := map[string]bool{}

	for _, name := range gone {
		for _, e := range exp.Users[name].Emails {
			emails[e] = true
		}
	}

	// the engine config: the same edit the renderer makes for an expired user
	cfg, err := os.ReadFile(opts.EngineConfig)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// no engine on this host; the rest of the plan still applies
	case err != nil:
		return rep, err
	default:
		out, changed, err := StripUsers(cfg, emails)
		if err != nil {
			return rep, err
		}

		if changed {
			rep.Changed = true

			rep.Removed = gone
			if opts.DryRun {
				return rep, nil
			}

			if err := swapChecked(opts, out); err != nil {
				notifyFailure(opts, "expiry: "+err.Error())
				return rep, err
			}
		}
	}

	if opts.DryRun {
		return rep, nil
	}

	// subscriptions: off the web root, kept under the expired directory so a
	// mistaken date can be undone without re-rendering
	subs, err := takeDownSubs(opts, exp, gone)
	if err != nil {
		return rep, err
	}

	rep.Subs = subs

	rep.Peers, err = dropPeers(opts, exp, gone)
	if err != nil {
		return rep, err
	}

	if rep.Changed || len(rep.Subs) > 0 || len(rep.Peers) > 0 {
		// the record is an audit trail, not part of the change: a host whose
		// disk refused it still had its accounts taken off, so say so and go on
		if record, err := json.Marshal(rep); err == nil {
			if err := os.MkdirAll(opts.ExpiredDir, 0o700); err != nil {
				fmt.Fprintf(os.Stderr, "tick: record: %v\n", err)
			} else if err := os.WriteFile(filepath.Join(opts.ExpiredDir, "last-tick.json"), record, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "tick: record: %v\n", err)
			}
		}
	}

	return rep, nil
}

// takeDownSubs moves every subscription of the expired accounts out of the web
// root. A directory that is already gone is not an error: the tick is idempotent.
func takeDownSubs(opts TickOptions, exp state.Expiry, gone []string) ([]string, error) {
	var subs []string

	for _, name := range gone {
		sub := exp.Users[name].SubID
		if sub == "" {
			continue
		}

		src := filepath.Join(opts.SubRoot, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}

		if err := os.MkdirAll(opts.ExpiredDir, 0o700); err != nil {
			return subs, err
		}

		if err := os.Rename(src, filepath.Join(opts.ExpiredDir, "sub-"+sub)); err != nil {
			return subs, err
		}

		subs = append(subs, sub)
	}

	return subs, nil
}

// dropPeers removes the awg peer of every expired account: a live tunnel
// outlives the engine identity, so the account would keep its path otherwise.
func dropPeers(opts TickOptions, exp state.Expiry, gone []string) ([]string, error) {
	var peers []string

	for _, name := range gone {
		ip := exp.Users[name].AWGIP
		if ip == "" || opts.AWGConfig == "" {
			continue
		}

		changed, err := DropPeer(opts.AWGConfig, ip)
		if err != nil {
			return peers, err
		}

		if !changed {
			continue
		}

		peers = append(peers, ip)
		if _, err := opts.Run("systemctl restart awg-quick@awg0"); err != nil {
			return peers, fmt.Errorf("restart awg-quick@awg0: %w", err)
		}
	}

	return peers, nil
}

// swapChecked writes the new config only after it passes the engine's own check,
// then restarts and probes; on any failure the old file goes back.
func swapChecked(opts TickOptions, body []byte) error {
	tmp := opts.EngineConfig + ".tick"
	back := opts.EngineConfig + ".tick.bak"

	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	check := fmt.Sprintf("sing-box check -c %s", tmp)
	if opts.EngineBin == "xray" {
		check = fmt.Sprintf("xray run -test -c %s", tmp)
	}

	if out, err := opts.Run(check); err != nil {
		return fmt.Errorf("the engine rejected the new config: %s", strings.TrimSpace(out))
	}

	if err := copyFile(opts.EngineConfig, back); err != nil {
		return err
	}

	if err := os.Rename(tmp, opts.EngineConfig); err != nil {
		return err
	}

	if _, err := opts.Run("systemctl restart " + opts.EngineBin); err != nil {
		restore(back, opts)
		return fmt.Errorf("the engine did not restart")
	}

	if out, err := opts.Run("/bin/sh /etc/vpnctl/probe.sh"); err != nil {
		restore(back, opts)
		return fmt.Errorf("the chain stopped answering after the restart: %s", strings.TrimSpace(out))
	}

	_ = os.Remove(back)

	return nil
}

// restore puts the old config back and restarts the engine with it.
func restore(back string, opts TickOptions) {
	if err := copyFile(back, opts.EngineConfig); err == nil {
		_, _ = opts.Run("systemctl restart " + opts.EngineBin)
	}

	_ = os.Remove(back)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, b, 0o600)
}

// StripUsers removes the named identities from the rendered engine config: the
// users of every inbound and the auth_user lists of the routing rules. A rule
// left with nothing to match goes away with them.
func StripUsers(body []byte, emails map[string]bool) ([]byte, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var cfg any
	if err := dec.Decode(&cfg); err != nil {
		return nil, false, fmt.Errorf("parse the engine config: %w", err)
	}

	out, changed := stripNode(cfg, emails, false)
	if !changed {
		return body, false, nil
	}

	b, err := render.JSON(out)
	if err != nil {
		return nil, false, err
	}

	return b, true, nil
}

// matchKeys are the fields that make a rule match something. A rule left with
// none of them matches everything, so it must be dropped, not kept.
var matchKeys = map[string]bool{
	"auth_user": true, "source_ip_cidr": true, "rule_set": true, "domain": true,
	"domain_suffix": true, "domain_keyword": true, "domain_regex": true,
	"ip_cidr": true, "ip_is_private": true, "port": true, "port_range": true,
	"network": true, "protocol": true, "clash_mode": true, "process_name": true,
	"inbound": true, "geoip": true, "geosite": true,
}

// stripNode walks the config. droppable says the value sits in a rules array,
// where a rule left without a match must go; an inbound or an outbound that
// looks empty from here is still content and stays.
func stripNode(v any, emails map[string]bool, droppable bool) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		changed := false

		for k, val := range t {
			switch k {
			case "users":
				list, ok := val.([]any)
				if !ok {
					continue
				}

				kept := make([]any, 0, len(list))
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						if name, ok := m["name"].(string); ok && emails[name] {
							changed = true
							continue
						}
					}

					kept = append(kept, item)
				}

				t[k] = kept
			case "auth_user":
				list, ok := val.([]any)
				if !ok {
					continue
				}

				kept := make([]any, 0, len(list))
				for _, item := range list {
					if s, ok := item.(string); ok && emails[s] {
						changed = true
						continue
					}

					kept = append(kept, item)
				}

				if len(kept) == 0 {
					delete(t, k)

					changed = true

					continue
				}

				t[k] = kept
			case "rules":
				list, _ := val.([]any)

				nv, c := stripNode(val, emails, true)
				if c {
					changed = true
					kept, _ := nv.([]any)
					// An AND that lost a whole condition no longer means the
					// same: keeping the rest would apply a group rule to
					// everybody. A condition that only shrank is still the
					// narrowing condition it was.
					mode, _ := t["mode"].(string)
					if mode == "and" && len(kept) < len(list) {
						return map[string]any{}, true
					}

					t[k] = nv
				}
			default:
				nv, c := stripNode(val, emails, false)
				if c {
					t[k] = nv
					changed = true
				}
			}
		}

		return t, changed
	case []any:
		kept := make([]any, 0, len(t))
		changed := false

		for _, item := range t {
			nv, c := stripNode(item, emails, false)
			if c {
				changed = true
			}
			// only a rule the strip emptied is dropped: an action-only rule
			// never had a match and must stay where it is
			if droppable && c {
				if m, ok := nv.(map[string]any); ok && !hasMatchKey(m) {
					changed = true
					continue
				}
			}

			kept = append(kept, nv)
		}

		return kept, changed
	}

	return v, false
}

// hasMatchKey reports whether a rule can still match something. Containers and
// output actions carry no match keys of their own and are never dropped here.
func hasMatchKey(m map[string]any) bool {
	for _, k := range []string{"type", "mode", "rules"} {
		if _, ok := m[k]; ok {
			return true
		}
	}

	for k := range m {
		if matchKeys[k] {
			return true
		}
	}

	return false
}

// DropPeer removes the [Peer] block whose AllowedIPs carries the address.
func DropPeer(path, ip string) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, err
	}

	lines := strings.Split(string(body), "\n")

	var out []string

	dropped := false

	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "[Peer]" {
			out = append(out, lines[i])
			continue
		}

		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "[") {
			end++
		}

		block := strings.Join(lines[i:end], "\n")
		if strings.Contains(block, ip+"/32") || strings.Contains(block, ip+"/") {
			dropped = true
			i = end - 1

			continue
		}

		out = append(out, lines[i:end]...)
		i = end - 1
	}

	if !dropped {
		return false, nil
	}

	return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600)
}

// notifyFailure sends one message when the timer could not do its job: a silent
// failed unit is how an account stays alive a week past its date.
func notifyFailure(opts TickOptions, text string) {
	env, err := os.ReadFile(opts.AlertsEnv)
	if err != nil {
		return
	}

	vars := map[string]string{}

	for _, line := range strings.Split(string(env), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}

		if s, err := strconv.Unquote(v); err == nil {
			vars[k] = s
		} else {
			vars[k] = v
		}
	}

	url := vars["VPNCTL_ALERTS_LOCAL"]
	if url == "" {
		url = vars["VPNCTL_ALERTS_URL"]
	}

	if url == "" {
		return
	}

	cmd := fmt.Sprintf("curl -sS --max-time 10 -o /dev/null -H 'Title: vpnctl expiry' -H 'Priority: high' -d %s %s",
		shellQuote(text), shellQuote(url))
	if t := vars["VPNCTL_ALERTS_TOKEN"]; t != "" {
		cmd = fmt.Sprintf("curl -sS --max-time 10 -o /dev/null -H 'Title: vpnctl expiry' -H 'Priority: high' -H %s -d %s %s",
			shellQuote("Authorization: Bearer "+t), shellQuote(text), shellQuote(url))
	}

	_, _ = opts.Run(cmd)
}

// shellQuote wraps a string in single quotes for the shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunTick runs tick and writes its report as JSON on stdout, the way the agent
// answers: a timer's journal is the only place the operator sees what happened.
func RunTick(opts TickOptions) error {
	rep, err := Tick(opts)
	if err != nil {
		return err
	}

	b, err := json.Marshal(rep)
	if err != nil {
		return err
	}

	fmt.Println(string(b))

	return nil
}
