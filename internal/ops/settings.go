package ops

import (
	"fmt"
	"net"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// setNames are the inbounds the renderer knows how to write; a typo here would
// produce a file nobody reads.
var inboundNames = map[string]bool{"reality": true, "ws": true, "hy2": true, "awg": true, "mtg": true}

// SetInfo is one address set as the editor shows it.
type SetInfo struct {
	Name    string
	Geosite []string
	GeoIP   []string
	Domains []string
	IPs     []string
}

// Sets returns the address sets sorted by name.
func Sets(st *state.State) []SetInfo {
	out := make([]SetInfo, 0, len(st.Sets))

	for name, s := range st.Sets {
		out = append(out, SetInfo{Name: name, Geosite: s.Geosite, GeoIP: s.GeoIP, Domains: s.Domains, IPs: s.IPs})
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}

// SetEntries replaces the operator's own domains and addresses in a set. The
// geosite and geoip names stay: they come from the pinned geodata, not from the
// phone, and a set without its category is a different rule.
func SetEntries(st *state.State, name string, domains, ips []string) (string, error) {
	s, ok := st.Sets[name]
	if !ok {
		return "", fmt.Errorf("set %q is not described", name)
	}

	for _, d := range domains {
		if !domainOK(d) {
			return "", fmt.Errorf("domain %q is not a name", d)
		}
	}

	for _, ip := range ips {
		if net.ParseIP(strings.SplitN(ip, "/", 2)[0]) == nil {
			return "", fmt.Errorf("address %q is not an IP", ip)
		}
	}

	s.Domains = domains
	s.IPs = ips
	st.Sets[name] = s

	return fmt.Sprintf("sets: %s: %d domains, %d addresses", name, len(domains), len(ips)), nil
}

func domainOK(d string) bool {
	if d == "" || strings.ContainsAny(d, " \t/") {
		return false
	}

	for _, c := range d {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '-' && c != '*' {
			return false
		}
	}

	return strings.Contains(d, ".")
}

// InboundSet creates or edits one inbound of a host. Port is required for a new
// one; path and sni are only written when given, because reality has one and ws
// the other.
func InboundSet(st *state.State, host, name string, port int, path, sni string) (string, error) {
	if _, ok := st.Hosts[host]; !ok {
		return "", fmt.Errorf("host %q is not described", host)
	}

	if !inboundNames[name] {
		return "", fmt.Errorf("inbound %q: expected reality, ws, hy2, awg or mtg", name)
	}

	inb, ok := st.Inbounds[host]
	if !ok {
		inb = map[string]state.Inbound{}
	}

	i := inb[name]

	if port == 0 {
		if i.Port == 0 {
			return "", fmt.Errorf("inbound %s/%s: a port is required", host, name)
		}
	} else {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("port %d: expected 1-65535", port)
		}

		i.Port = port
	}

	if path != "" {
		if !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("path %q: expected a leading slash", path)
		}

		i.Path = path
	}

	if sni != "" {
		i.SNI = sni
	}

	inb[name] = i
	st.Inbounds[host] = inb

	return fmt.Sprintf("inbounds: %s/%s: port %d", host, name, i.Port), nil
}

// ExitPolicySet is how a host picks its exit: automatic, or fixed to one exit.
// The empty policy is the automatic one the renderer already understands.
func ExitPolicySet(st *state.State, host, policy, prefer string) (string, error) {
	r, ok := st.Routing[host]
	if !ok {
		return "", fmt.Errorf("host %q has no routing", host)
	}

	switch policy {
	case "", "auto":
		r.Exit.Policy = ""
		r.Exit.Prefer = ""
	case "fixed":
		if prefer == "" {
			return "", fmt.Errorf("a fixed exit needs a host")
		}

		h, ok := st.Hosts[prefer]
		if !ok {
			return "", fmt.Errorf("host %q is not described", prefer)
		}

		if h.Role != state.Exit {
			return "", fmt.Errorf("host %q is not an exit", prefer)
		}

		r.Exit.Policy = "fixed"
		r.Exit.Prefer = prefer
	default:
		return "", fmt.Errorf("policy %q: expected auto or fixed", policy)
	}

	st.Routing[host] = r

	if r.Exit.Policy == "fixed" {
		return fmt.Sprintf("routing: %s: exit fixed to %s", host, prefer), nil
	}

	return fmt.Sprintf("routing: %s: exit auto", host), nil
}
