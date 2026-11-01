package render

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

const (
	// TProxyPort is where the engine accepts intercepted L3 traffic (clients in awg).
	TProxyPort = 7893
	// CaddyPort is the local cover site, the ws endpoint and the subscriptions; Reality falls back to it.
	CaddyPort = 8443
	// ProbePort is the local socks inbound: chain probes and the mtg upstream.
	ProbePort = 1080
	// StatsAPI is the clash_api controller of the engine.
	StatsAPI = "127.0.0.1:9090"
	// ProbeURL is the latency probe target.
	ProbeURL = "https://www.gstatic.com/generate_204"

	engineSingBox = "sing-box"
	engineXray    = "xray"
)

// RealityTarget is the Reality SNI and where a non-Reality handshake goes: our own site
// behind Caddy (an entry with a domain) or the external site from the inbound `sni`.
func RealityTarget(st *state.State, host string, i state.Inbound) (sni, server string, port int) {
	if i.SNI != "" {
		if h, p, ok := splitHostPort(i.Handshake); ok {
			return i.SNI, h, p
		}
		return i.SNI, i.SNI, 443
	}
	if h, ok := st.Hosts[host]; ok && h.Domain != "" {
		return h.Domain, "127.0.0.1", CaddyPort
	}
	return "www.microsoft.com", "www.microsoft.com", 443
}

func splitHostPort(s string) (string, int, bool) {
	if s == "" {
		return "", 0, false
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 65535 {
		return "", 0, false
	}
	return s[:i], p, true
}

// EngineBin is the engine executable name; an empty engine means sing-box.
func EngineBin(engine string) string {
	if engine == engineXray {
		return engineXray
	}
	return engineSingBox
}

// EngineConfigPath is where the engine reads its config on the host.
func EngineConfigPath(engine string) string {
	return "/etc/" + EngineBin(engine) + "/config.json"
}

// OutLink is an outgoing link with the interface number awg<i> uses (awg0 is clients and incoming).
type OutLink struct {
	N int
	L *state.Link
}

// OutLinks returns the links leaving the host, numbered from 1.
func OutLinks(st *state.State, host string) []OutLink {
	var out []OutLink
	n := 0
	for i := range st.Links {
		if st.Links[i].From != host {
			continue
		}
		n++
		out = append(out, OutLink{N: n, L: &st.Links[i]})
	}
	return out
}

// InLinks returns the links arriving at the host, in state order.
func InLinks(st *state.State, host string) []*state.Link {
	var out []*state.Link
	for i := range st.Links {
		if st.Links[i].To == host {
			out = append(out, &st.Links[i])
		}
	}
	return out
}

// Account is a user of the entry with the name the state knows it by.
type Account struct {
	Name string
	U    state.User
}

// UsersOf returns enabled users served by the entry, sorted by name.
func UsersOf(st *state.State, entry string) []Account {
	var names []string
	for name := range st.Users {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []Account
	for _, name := range names {
		u := st.Users[name]
		if !u.IsEnabled() {
			continue
		}
		if e, ok := st.EntryOf(u); !ok || e != entry {
			continue
		}
		out = append(out, Account{Name: name, U: u})
	}
	return out
}

// CertPaths are the certificate and key the engine serves: Let's Encrypt on the entry, self-signed elsewhere.
func CertPaths(st *state.State, host string) (crt, key string) {
	h, ok := st.Hosts[host]
	if ok && h.Role == state.Entry && h.Domain != "" {
		base := "/var/lib/caddy/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/" +
			h.Domain + "/" + h.Domain
		return base + ".crt", base + ".key"
	}
	return "/etc/vpnctl/self.crt", "/etc/vpnctl/self.key"
}

// Hosts of a prefix, .1 and .2 first: the gateway and the tunnel sides.
func hostsOf(cidr string) []string {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	ones, bits := n.Mask.Size()
	total := 1 << (bits - ones)
	var out []string
	for i := 1; i < total-1; i++ {
		v := make(net.IP, len(ip))
		copy(v, ip)
		v[3] += byte(i)
		out = append(out, v.String())
	}
	return out
}

// Gateway is the first host address of the client subnet.
func Gateway(cidr string) string {
	if h := hostsOf(cidr); len(h) > 0 {
		return h[0]
	}
	if _, n, err := net.ParseCIDR(cidr); err == nil {
		return n.IP.String()
	}
	return cidr
}

// TunnelAddr is side 1 or 2 of the /30 tunnel between nodes.
func TunnelAddr(cidr string, side int) string {
	h := hostsOf(cidr)
	idx := side - 1
	if idx < 0 || idx >= len(h) {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			return n.IP.String() + "/30"
		}
		return cidr
	}
	return h[idx] + "/30"
}

// Sha256 is the hex digest of a string.
func Sha256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
