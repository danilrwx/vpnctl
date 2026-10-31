// Package state describes the vpnctl state directory: YAML files plus
// decrypted secrets. No processes, no OS dependencies.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/ageio"
	"gopkg.in/yaml.v3"
)

type Role string

const (
	Entry Role = "entry"
	Exit  Role = "exit"
)

type Host struct {
	Role     Role   `yaml:"role"`
	Engine   string `yaml:"engine"`
	IP       string `yaml:"ip"`
	Domain   string `yaml:"domain,omitempty"`
	OS       string `yaml:"os,omitempty"`
	SSH      string `yaml:"ssh"`
	HostKey  string `yaml:"host_key,omitempty"`
	Via      string `yaml:"via,omitempty"`
	Retiring bool   `yaml:"retiring,omitempty"`
}

type Peer struct {
	Mode string `yaml:"mode"`
	IP   string `yaml:"ip"`
}

type User struct {
	Modes     []string `yaml:"modes"`
	Protocols []string `yaml:"protocols"`
	AWG       *Peer    `yaml:"awg,omitempty"`
	TG        bool     `yaml:"tg,omitempty"`
	Enabled   *bool    `yaml:"enabled,omitempty"`
	Expires   string   `yaml:"expires,omitempty"`
	Note      string   `yaml:"note,omitempty"`
	Entry     string   `yaml:"entry,omitempty"`
}

// IsEnabled reports the user state; an absent flag means enabled.
func (u User) IsEnabled() bool { return u.Enabled == nil || *u.Enabled }

type URLTest struct {
	IntervalS   int `yaml:"interval_s,omitempty"`
	ToleranceMS int `yaml:"tolerance_ms,omitempty"`
	Failures    int `yaml:"failures,omitempty"`
}

type Link struct {
	From      string   `yaml:"from"`
	To        string   `yaml:"to"`
	Protocols []string `yaml:"protocols"`
	Policy    string   `yaml:"policy,omitempty"`
	Active    string   `yaml:"active,omitempty"`
	Tunnel    string   `yaml:"tunnel,omitempty"`
	URLTest   URLTest  `yaml:"urltest,omitempty"`
}

type Subnets struct {
	Split string `yaml:"split"`
	Full  string `yaml:"full"`
}

type Inbound struct {
	Port      int      `yaml:"port"`
	Enabled   *bool    `yaml:"enabled,omitempty"`
	SNI       string   `yaml:"sni,omitempty"`
	Handshake string   `yaml:"handshake,omitempty"`
	Path      string   `yaml:"path,omitempty"`
	Subnets   *Subnets `yaml:"subnets,omitempty"`
}

// IsEnabled reports the inbound state; an absent flag means enabled.
func (i Inbound) IsEnabled() bool { return i.Enabled == nil || *i.Enabled }

// ForMode is the client subnet of the mode.
func (s Subnets) ForMode(mode string) string {
	if mode == "full" {
		return s.Full
	}
	return s.Split
}

type ExitPolicy struct {
	Policy  string  `yaml:"policy,omitempty"`
	Prefer  string  `yaml:"prefer,omitempty"`
	URLTest URLTest `yaml:"urltest,omitempty"`
}

// Match is the condition of a routing rule: xray syntax for domains and prefixes.
type Match struct {
	Sets    []string `yaml:"sets,omitempty"`
	Geosite []string `yaml:"geosite,omitempty"`
	GeoIP   []string `yaml:"geoip,omitempty"`
	Domains []string `yaml:"domains,omitempty"`
	IP      []string `yaml:"ip,omitempty"`
	Inbound []string `yaml:"inbound,omitempty"`
	Port    []int    `yaml:"port,omitempty"`
}

// ActionKind is what a rule does with a matching connection.
type ActionKind string

const (
	ActionDirect ActionKind = "direct"
	ActionBlock  ActionKind = "block"
	ActionLink   ActionKind = "link"
)

// Action is `direct`, `block`, `link:<host>` or `link` (the automatic exit).
type Action struct {
	Kind ActionKind
	// Link is the target host for ActionLink; "auto" keeps the exit policy.
	Link string
}

// ParseAction reads the short form used by the CLI: direct, block, link, link:<host>.
func ParseAction(s string) (Action, error) {
	switch {
	case s == "direct":
		return Action{Kind: ActionDirect}, nil
	case s == "block":
		return Action{Kind: ActionBlock}, nil
	case s == "link":
		return Action{Kind: ActionLink, Link: "auto"}, nil
	case strings.HasPrefix(s, "link:"):
		return Action{Kind: ActionLink, Link: strings.TrimPrefix(s, "link:")}, nil
	}
	return Action{}, fmt.Errorf("action %q: expected direct, block, link:auto or link:<host>", s)
}

func (a Action) String() string {
	switch a.Kind {
	case ActionLink:
		return "link:" + a.Link
	}
	return string(a.Kind)
}

// IsAuto reports the action `link` without a named host.
func (a Action) IsAuto() bool { return a.Kind == ActionLink && a.Link == "auto" }

// UnmarshalYAML accepts both the keyword form and the {link: <host>} mapping.
func (a *Action) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		v, err := ParseAction(n.Value)
		if err != nil {
			return err
		}
		*a = v
		return nil
	}
	var m struct {
		Link string `yaml:"link"`
	}
	if err := n.Decode(&m); err != nil {
		return err
	}
	if m.Link == "" {
		return fmt.Errorf("action: expected a keyword or {link: <host>}")
	}
	*a = Action{Kind: ActionLink, Link: m.Link}
	return nil
}

func (a Action) MarshalYAML() (any, error) {
	if a.Kind == ActionLink {
		return map[string]string{"link": a.Link}, nil
	}
	return string(a.Kind), nil
}

// Rule is one routing rule: match, action, enabled.
type Rule struct {
	Match   Match  `yaml:"match,omitempty"`
	Action  Action `yaml:"action"`
	Enabled *bool  `yaml:"enabled,omitempty"`
	Note    string `yaml:"note,omitempty"`
}

// IsEnabled reports the rule state; an absent flag means enabled.
func (r Rule) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

type Groups struct {
	All   []Rule `yaml:"all,omitempty"`
	Split []Rule `yaml:"split,omitempty"`
	Full  []Rule `yaml:"full,omitempty"`
}

type DNS struct {
	Direct []string `yaml:"direct,omitempty"`
	Remote string   `yaml:"remote,omitempty"`
}

type Routing struct {
	Exit   ExitPolicy `yaml:"exit"`
	Groups Groups     `yaml:"groups"`
	DNS    DNS        `yaml:"dns"`
}

type Set struct {
	Geosite []string `yaml:"geosite,omitempty"`
	GeoIP   []string `yaml:"geoip,omitempty"`
	Domains []string `yaml:"domains,omitempty"`
	IPs     []string `yaml:"ips,omitempty"`
}

// Empty reports a set with no local entries: only geosite/geoip remain.
func (s Set) Empty() bool { return len(s.Domains) == 0 && len(s.IPs) == 0 }

// Secrets mirror the encrypted file; only the parts the core needs.
type HostSecrets struct {
	Reality *RealityKeys `yaml:"reality,omitempty"`
	AWG     *KeyPair     `yaml:"awg,omitempty"`
	MTG     string       `yaml:"mtg_secret,omitempty"`
}

type KeyPair struct {
	Private string `yaml:"private"`
	Public  string `yaml:"public"`
}

type RealityKeys struct {
	Private  string   `yaml:"private"`
	Public   string   `yaml:"public"`
	ShortIDs []string `yaml:"short_ids"`
}

type UserSecrets struct {
	UUID         string   `yaml:"uuid"`
	Hy2Pass      string   `yaml:"hy2_password"`
	SubID        string   `yaml:"sub_id"`
	AWG          *KeyPair `yaml:"awg,omitempty"`
	AWGPreshared string   `yaml:"awg_preshared,omitempty"`
}

// LinkAWG is the key material of an AmneziaWG link: from and to sides plus the PSK.
type LinkAWG struct {
	From      KeyPair `yaml:"from"`
	To        KeyPair `yaml:"to"`
	Preshared string  `yaml:"preshared"`
}

// LinkReality is the account and keys a link uses on the exit's REALITY inbound.
type LinkReality struct {
	UUID string      `yaml:"uuid"`
	Keys RealityKeys `yaml:"keys"`
}

type LinkSecrets struct {
	AWG     *LinkAWG     `yaml:"awg,omitempty"`
	Reality *LinkReality `yaml:"reality,omitempty"`
	Hy2Pass string       `yaml:"hy2_password,omitempty"`
}

type Secrets struct {
	Hosts map[string]HostSecrets `yaml:"hosts"`
	Users map[string]UserSecrets `yaml:"users"`
	Links map[string]LinkSecrets `yaml:"links"`
}

type State struct {
	Dir       string
	Hosts     map[string]Host
	Links     []Link
	Users     map[string]User
	Inbounds  map[string]map[string]Inbound
	Routing   map[string]Routing
	Sets      map[string]Set
	Secrets   Secrets
	Encrypted bool
}

func read(dir, name string, v any) error {
	p := filepath.Join(dir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}

// Load reads the state directory. Secrets are decrypted through sops when the
// file is encrypted and the age key is available.
func Load(dir string) (*State, error) {
	st := &State{
		Dir:      dir,
		Hosts:    map[string]Host{},
		Users:    map[string]User{},
		Inbounds: map[string]map[string]Inbound{},
		Routing:  map[string]Routing{},
		Sets:     map[string]Set{},
	}
	var hosts struct {
		Hosts map[string]Host `yaml:"hosts"`
	}
	if err := read(dir, "hosts.yaml", &hosts); err != nil {
		return nil, err
	}
	st.Hosts = hosts.Hosts

	var links struct {
		Links []Link `yaml:"links"`
	}
	if err := read(dir, "links.yaml", &links); err != nil {
		return nil, err
	}
	st.Links = links.Links

	var users struct {
		Users map[string]User `yaml:"users"`
	}
	if err := read(dir, "users.yaml", &users); err != nil {
		return nil, err
	}
	st.Users = users.Users

	var proto struct {
		Inbounds map[string]map[string]Inbound `yaml:"inbounds"`
	}
	if err := read(dir, "protocols.yaml", &proto); err != nil {
		return nil, err
	}
	st.Inbounds = proto.Inbounds

	for _, name := range sortedKeys(st.Hosts) {
		var r Routing
		err := read(dir, filepath.Join("routing", name+".yaml"), &r)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		st.Routing[name] = r
	}

	entries, err := os.ReadDir(filepath.Join(dir, "sets"))
	if err == nil {
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".yaml")
			if name == e.Name() {
				continue
			}
			var s Set
			if err := read(dir, filepath.Join("sets", e.Name()), &s); err != nil {
				return nil, err
			}
			st.Sets[name] = s
		}
	}

	if b, err := ageio.Decrypt(filepath.Join(dir, "secrets.age.yaml"), KeyPath()); err == nil {
		if err := yaml.Unmarshal(b, &st.Secrets); err != nil {
			return nil, fmt.Errorf("parse secrets: %w", err)
		}
		st.Encrypted = true
	}
	st.normalize()

	return st, nil
}

// normalize fills the defaults serde applies on deserialization: urltest timings and
// the DNS resolvers of a routing file without a `dns` section.
func (s *State) normalize() {
	fillTest := func(t *URLTest) {
		if t.IntervalS == 0 {
			t.IntervalS = 60
		}
		if t.ToleranceMS == 0 {
			t.ToleranceMS = 300
		}
		if t.Failures == 0 {
			t.Failures = 2
		}
	}
	for i := range s.Links {
		fillTest(&s.Links[i].URLTest)
	}
	for name, r := range s.Routing {
		fillTest(&r.Exit.URLTest)
		if len(r.DNS.Direct) == 0 {
			r.DNS.Direct = []string{"77.88.8.8", "1.1.1.1"}
		}
		if r.DNS.Remote == "" {
			r.DNS.Remote = "https://1.1.1.1/dns-query"
		}
		s.Routing[name] = r
	}
	if s.Secrets.Hosts == nil {
		s.Secrets.Hosts = map[string]HostSecrets{}
	}
	if s.Secrets.Users == nil {
		s.Secrets.Users = map[string]UserSecrets{}
	}
	if s.Secrets.Links == nil {
		s.Secrets.Links = map[string]LinkSecrets{}
	}
}

func sortedKeys(m map[string]Host) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Host returns a described host or an error naming it.
func (s *State) Host(name string) (Host, error) {
	h, ok := s.Hosts[name]
	if !ok {
		return Host{}, fmt.Errorf("host %q is not described", name)
	}
	return h, nil
}

// Email is the engine account name: <user>-<mode>-<protocol>.
func (s *State) Email(user, mode, proto string) string {
	return user + "-" + mode + "-" + proto
}

// EntryOf is the user's entry: explicit or the only one in the state.
func (s *State) EntryOf(u User) (string, bool) {
	if u.Entry != "" {
		return u.Entry, true
	}
	var only string
	n := 0
	for name, h := range s.Hosts {
		if h.Role == Entry {
			only = name
			n++
		}
	}
	if n == 1 {
		return only, true
	}
	return "", false
}

// Link returns the link from one host to another.
func (s *State) Link(from, to string) (*Link, bool) {
	for i := range s.Links {
		if s.Links[i].From == from && s.Links[i].To == to {
			return &s.Links[i], true
		}
	}
	return nil, false
}

// LinksFrom counts the links leaving the host.
func (s *State) LinksFrom(from string) int {
	n := 0
	for _, l := range s.Links {
		if l.From == from {
			n++
		}
	}
	return n
}

// Key is the secrets key of a link.
func (l Link) Key() string { return l.From + "->" + l.To }

// Entry returns the only entry host, if there is exactly one.
func (s *State) Entry() (string, error) {
	var names []string
	for n, h := range s.Hosts {
		if h.Role == Entry {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return "", fmt.Errorf("the state has no entry")
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("the state has %d entries, specify one", len(names))
	}
}

// KeyPath is the age identity used for the secrets file.
func KeyPath() string {
	if p := os.Getenv("VPNCTL_AGE_KEY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/sops/age/keys.txt")
}

// FillStubSecrets replaces the secrets with deterministic stubs: render and validate
// in CI without the age key. Mirrors `vpnctl --stub-secrets`.
func (s *State) FillStubSecrets() {
	stub := func(what string) string { return "STUB-" + what }
	for n, u := range s.Users {
		sec := s.Secrets.Users[n]
		sec.UUID = stub("uuid-" + n)
		sec.Hy2Pass = stub("hy2-" + n)
		sec.SubID = stub("sub-" + n)
		if u.AWG != nil {
			sec.AWG = &KeyPair{Private: stub("awg-priv-" + n), Public: stub("awg-pub-" + n)}
			sec.AWGPreshared = stub("awg-psk-" + n)
		}
		s.Secrets.Users[n] = sec
	}
	for n := range s.Hosts {
		sec := s.Secrets.Hosts[n]
		sec.Reality = &RealityKeys{
			Private:  stub("reality-priv-" + n),
			Public:   stub("reality-pub-" + n),
			ShortIDs: []string{"0000000000000000"},
		}
		sec.AWG = &KeyPair{Private: stub("awg-priv-" + n), Public: stub("awg-pub-" + n)}
		sec.MTG = "ee00000000000000000000000000000000"
		s.Secrets.Hosts[n] = sec
	}
	for _, l := range s.Links {
		k := l.Key()
		s.Secrets.Links[k] = LinkSecrets{
			AWG: &LinkAWG{
				From:      KeyPair{Private: stub(k + "-from-priv"), Public: stub(k + "-from-pub")},
				To:        KeyPair{Private: stub(k + "-to-priv"), Public: stub(k + "-to-pub")},
				Preshared: stub(k + "-psk"),
			},
			Reality: &LinkReality{
				UUID: stub(k + "-uuid"),
				Keys: RealityKeys{
					Private:  stub(k + "-priv"),
					Public:   stub(k + "-pub"),
					ShortIDs: []string{"1111111111111111"},
				},
			},
			Hy2Pass: stub(k + "-hy2"),
		}
	}
}
