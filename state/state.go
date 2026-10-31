// Package state describes the vpnctl state directory: YAML files plus
// decrypted secrets. No processes, no OS dependencies.
package state

import (
	"fmt"
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

type ExitPolicy struct {
	Policy  string  `yaml:"policy,omitempty"`
	Prefer  string  `yaml:"prefer,omitempty"`
	URLTest URLTest `yaml:"urltest,omitempty"`
}

type Groups struct {
	All   []any `yaml:"all,omitempty"`
	Split []any `yaml:"split,omitempty"`
	Full  []any `yaml:"full,omitempty"`
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
}

// Secrets mirror the encrypted file; only the parts the core needs.
type HostSecrets struct {
	Reality *RealityKeys `yaml:"reality,omitempty"`
	MTG     string       `yaml:"mtg_secret,omitempty"`
}

type RealityKeys struct {
	Private  string   `yaml:"private"`
	Public   string   `yaml:"public"`
	ShortIDs []string `yaml:"short_ids"`
}

type UserSecrets struct {
	UUID       string `yaml:"uuid"`
	Hy2Pass    string `yaml:"hy2_password"`
	SubID      string `yaml:"sub_id"`
}

type Secrets struct {
	Hosts map[string]HostSecrets `yaml:"hosts"`
	Users map[string]UserSecrets `yaml:"users"`
}

type State struct {
	Dir      string
	Hosts    map[string]Host
	Links    []Link
	Users    map[string]User
	Inbounds map[string]map[string]Inbound
	Routing  map[string]Routing
	Sets     map[string]Set
	Secrets  Secrets
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
		if err != nil && !os.IsNotExist(err) {
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

	return st, nil
}

func sortedKeys(m map[string]Host) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

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
