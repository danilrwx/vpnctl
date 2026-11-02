package ops

import (
	"fmt"
	"net"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/secrets"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// HostOptions describes a new host.
type HostOptions struct {
	Name   string
	Role   string // entry or exit
	SSH    string
	IP     string
	Domain string
	Via    string
	Engine string
	AWG    bool
}

// HostAdd creates a host with its inbound, keys and, for an entry, the routing
// every entry starts with. The caller writes the files and commits.
func HostAdd(st *state.State, o HostOptions) (string, error) {
	if o.Name == "" {
		return "", fmt.Errorf("the host name is required")
	}

	if _, ok := st.Hosts[o.Name]; ok {
		return "", fmt.Errorf("host %q already exists", o.Name)
	}

	role := state.Role(o.Role)
	if role != state.Entry && role != state.Exit {
		return "", fmt.Errorf("role %q: expected entry or exit", o.Role)
	}

	ssh := o.SSH
	if ssh == "" {
		return "", fmt.Errorf("--ssh is required")
	}

	ip := o.IP
	if ip == "" {
		if i := strings.LastIndex(ssh, "@"); i >= 0 {
			ip = ssh[i+1:]
		} else {
			ip = ssh
		}
	}

	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("cannot infer the IP from %q, pass --ip", ssh)
	}

	engine := o.Engine
	if engine == "" {
		engine = "sing-box"
	}

	if engine != "sing-box" && engine != "xray" {
		return "", fmt.Errorf("engine %q: expected sing-box or xray", o.Engine)
	}

	st.Hosts[o.Name] = state.Host{
		Role: role, Engine: engine, IP: ip, Domain: o.Domain, OS: "debian-13",
		SSH: ssh, Via: o.Via,
	}

	inb := map[string]state.Inbound{}

	priv, pub, sid, err := secrets.RealityKeys()
	if err != nil {
		return "", err
	}

	hs := state.HostSecrets{Reality: &state.RealityKeys{Private: priv, Public: pub, ShortIDs: []string{sid}}}
	// an entry covers Reality with its own site behind Caddy, an exit needs an external site
	sni := ""
	if role == state.Exit || o.Domain == "" {
		sni = "www.microsoft.com"
	}

	inb["reality"] = state.Inbound{Port: 443, SNI: sni}

	if o.AWG {
		aPriv, aPub, err := secrets.AWGKeys()
		if err != nil {
			return "", err
		}

		hs.AWG = &state.KeyPair{Private: aPriv, Public: aPub}
		inb["awg"] = state.Inbound{Port: 51820}
	}

	if role == state.Entry {
		inb["ws"] = state.Inbound{Port: 8444, Path: "/ws"}
		inb["hy2"] = state.Inbound{Port: 443}

		inb["mtg"] = state.Inbound{Port: 2053}
		if a, ok := inb["awg"]; ok {
			a.Subnets = &state.Subnets{Split: "10.8.1.0/24", Full: "10.8.2.0/24"}
			inb["awg"] = a
		}

		domain := o.Domain
		if domain == "" {
			domain = "www.microsoft.com"
		}

		hs.MTG = secrets.MTG(domain)
	}

	st.Inbounds[o.Name] = inb
	st.Secrets.Hosts[o.Name] = hs

	if role == state.Entry {
		st.Routing[o.Name] = entryRouting(st)
	} else {
		st.Routing[o.Name] = state.Routing{}
	}

	kind := "exit"
	if role == state.Entry {
		kind = "entry"
	}

	return fmt.Sprintf("hosts: + %s (%s)", o.Name, kind), nil
}

// entryRouting is the routing a new entry starts with: probe and ads for
// everyone, local services direct in split, everything else through the exit.
func entryRouting(st *state.State) state.Routing {
	auto := state.Action{Kind: state.ActionLink, Link: "auto"}
	rule := func(m state.Match, a state.Action, note string) state.Rule {
		return state.Rule{Match: m, Action: a, Note: note}
	}

	all := []state.Rule{
		rule(state.Match{Inbound: []string{"probe"}}, auto, "chain probe and mtg upstream"),
	}
	if _, ok := st.Sets["ads"]; ok {
		all = append(all, rule(state.Match{Sets: []string{"ads"}},
			state.Action{Kind: state.ActionBlock}, "ads"))
	}

	var split []state.Rule
	if _, ok := st.Sets["ru-direct"]; ok {
		split = append(split, rule(state.Match{Sets: []string{"ru-direct"}},
			state.Action{Kind: state.ActionDirect}, "local services direct"))
	}

	split = append(split,
		rule(state.Match{GeoIP: []string{"ru", "private"}},
			state.Action{Kind: state.ActionDirect}, "local addresses direct"),
		rule(state.Match{}, auto, "everything else via the exit"))
	full := []state.Rule{rule(state.Match{}, auto, "everything via the exit")}

	return state.Routing{
		Groups: state.Groups{All: all, Split: split, Full: full},
		DNS:    state.DNS{Direct: []string{"77.88.8.8", "1.1.1.1"}, Remote: "https://1.1.1.1/dns-query"},
	}
}

// HostFiles writes what a host change touches: hosts, inbounds, routing and secrets.
func HostFiles(st *state.State, name string) error {
	if err := state.SaveHosts(st.Dir, st.Hosts); err != nil {
		return err
	}

	if err := state.SaveInbounds(st.Dir, st.Inbounds); err != nil {
		return err
	}

	r, ok := st.Routing[name]
	if !ok {
		r = state.Routing{}
	}

	if err := state.SaveRouting(st.Dir, name, r); err != nil {
		return err
	}

	return state.SaveSecrets(st.Dir, st.Secrets)
}

// HostRemove drops a host, its inbound, routing and secrets. Refuses while a
// link still mentions it: removing the far side of a link is a state the
// validator rejects, and a silent half-removal is worse than an error.
func HostRemove(st *state.State, name string) (string, error) {
	if _, ok := st.Hosts[name]; !ok {
		return "", fmt.Errorf("host %q is not described", name)
	}

	for _, l := range st.Links {
		if l.From == name || l.To == name {
			return "", fmt.Errorf("host %q is used by the link %s: remove the link first", name, l.Key())
		}
	}

	delete(st.Hosts, name)
	delete(st.Inbounds, name)
	delete(st.Routing, name)
	delete(st.Secrets.Hosts, name)

	return fmt.Sprintf("hosts: - %s", name), nil
}

// HostEdit changes what the operator can change without touching the host's
// inbounds or keys: the address it is reached at, its domain and its engine.
// Renaming and role changes rewrite every link and secret, so they stay a
// deliberate `host rm` + `host add` job.
func HostEdit(st *state.State, name string, o HostOptions) (string, error) {
	h, ok := st.Hosts[name]
	if !ok {
		return "", fmt.Errorf("host %q is not described", name)
	}

	if o.SSH != "" {
		h.SSH = o.SSH
	}

	if o.IP != "" {
		if net.ParseIP(o.IP) == nil {
			return "", fmt.Errorf("ip %q is not an address", o.IP)
		}

		h.IP = o.IP
	}

	if o.Domain != "" && h.Role == state.Exit {
		return "", fmt.Errorf("host %q is an exit: a domain belongs to an entry", name)
	}

	// an empty domain is a request to clear it, which an entry cannot survive
	if o.Domain == "" && h.Role == state.Entry && h.Domain != "" {
		return "", fmt.Errorf("host %q is an entry: it needs a domain", name)
	}

	h.Domain = o.Domain

	if o.Engine != "" {
		if o.Engine != "sing-box" && o.Engine != "xray" {
			return "", fmt.Errorf("engine %q: expected sing-box or xray", o.Engine)
		}

		h.Engine = o.Engine
	}

	if o.Via != "" {
		if o.Via == name {
			return "", fmt.Errorf("host %q cannot be reached through itself", name)
		}

		if _, ok := st.Hosts[o.Via]; !ok {
			return "", fmt.Errorf("host %q is not described", o.Via)
		}

		h.Via = o.Via
	}

	st.Hosts[name] = h

	return fmt.Sprintf("hosts: %s: edited", name), nil
}
