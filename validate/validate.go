// Package validate checks the state statically and returns a list of errors.
package validate

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/state"
)

// protoOrder is the declaration order of the protocol enum: errors follow it, as the
// Rust core walks a BTreeMap<Proto, Inbound>.
var protoOrder = []string{"reality", "ws", "hy2", "awg", "mtg"}

var linkProtos = map[string]bool{"reality": true, "hy2": true, "awg": true}

func identOK(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func dateOK(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func sortedHosts(st *state.State) []string {
	out := make([]string, 0, len(st.Hosts))
	for k := range st.Hosts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedUsers(st *state.State) []string {
	out := make([]string, 0, len(st.Users))
	for k := range st.Users {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// canonCIDR is the canonical form of a prefix: masking the address, as ipnet displays it.
func canonCIDR(s string) string {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return s
	}
	ones, _ := n.Mask.Size()
	return fmt.Sprintf("%s/%d", n.IP.String(), ones)
}

func parseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return nil
	}
	return n
}

func cidrContains(outer, inner string) bool {
	o, i := parseCIDR(outer), parseCIDR(inner)
	if o == nil || i == nil {
		return false
	}
	return o.Contains(i.IP)
}

func sameIP(a, b net.IP) bool { return a.Equal(b) }

// gatewayOf is the first host of a prefix, the same address render uses.
func gatewayOf(cidr string) net.IP {
	n := parseCIDR(cidr)
	if n == nil {
		return nil
	}
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	ones, bits := n.Mask.Size()
	if 1<<(bits-ones) <= 2 {
		return n.IP
	}
	v := make(net.IP, len(ip))
	copy(v, ip)
	v[3]++
	return v
}

// State returns the errors of the state; an empty slice means it is valid.
func State(st *state.State) []string {
	var errs []string
	err := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	// hosts
	domains := map[string][]string{}
	for _, name := range sortedHosts(st) {
		h := st.Hosts[name]
		if !identOK(name) {
			err("host «%s»: name must match [a-z0-9-_]", name)
		}
		if h.Via != "" {
			if _, ok := st.Hosts[h.Via]; !ok {
				err("host «%s»: via «%s» is not described", name, h.Via)
			}
			if h.Via == name {
				err("host «%s»: via points to the host itself", name)
			}
		}
		if h.Domain != "" {
			domains[h.Domain] = append(domains[h.Domain], name)
		}
		inb := st.Inbounds[name]
		if h.Engine == "xray" {
			if _, ok := inb["hy2"]; ok {
				err("host «%s»: engine xray does not support hysteria2 — drop the hy2 inbound or switch the engine", name)
			}
			if _, ok := inb["awg"]; ok && h.Role == state.Entry {
				err("host «%s»: engine xray cannot serve DNS for L3 clients — awg on the entry requires sing-box", name)
			}
		}
		tcp := map[int]string{}
		udp := map[int]string{}
		for _, p := range protoOrder {
			i, ok := inb[p]
			if !ok {
				continue
			}
			table := tcp
			if p == "hy2" || p == "awg" {
				table = udp
			}
			if other, ok := table[i.Port]; ok {
				err("host «%s»: port %d is used by both %s and %s", name, i.Port, other, p)
			}
			table[i.Port] = p
			if p == "awg" && h.Role == state.Entry {
				switch {
				case i.Subnets == nil:
					err("host «%s»: awg on the entry requires subnets (split/full)", name)
				case cidrContains(i.Subnets.Split, i.Subnets.Full) || cidrContains(i.Subnets.Full, i.Subnets.Split):
					err("host «%s»: awg subnets split %s and full %s overlap",
						name, canonCIDR(i.Subnets.Split), canonCIDR(i.Subnets.Full))
				}
			}
		}
		hs, hasHS := st.Secrets.Hosts[name]
		if _, ok := inb["reality"]; ok && (!hasHS || hs.Reality == nil) {
			err("host «%s»: no reality keys in secrets", name)
		}
		if _, ok := inb["awg"]; ok && (!hasHS || hs.AWG == nil) {
			err("host «%s»: no awg keys in secrets", name)
		}
		if _, ok := inb["mtg"]; ok && (!hasHS || hs.MTG == "") {
			err("host «%s»: no mtg secret in secrets", name)
		}
		if h.Role == state.Entry && h.Domain == "" {
			err("host «%s»: the entry requires a domain", name)
		}
	}
	for _, d := range sortedKeys(domains) {
		hs := domains[d]
		if len(hs) < 2 {
			continue
		}
		retiring := false
		for _, n := range hs {
			if st.Hosts[n].Retiring {
				retiring = true
			}
		}
		if !retiring {
			err("domain %s on hosts %s: allowed only during a migration, mark the old host retiring: true",
				d, strings.Join(hs, ", "))
		}
	}
	// cycle in the via graph
	for _, start := range sortedHosts(st) {
		seen := []string{start}
		cur := st.Hosts[start].Via
		for cur != "" {
			if contains(seen, cur) {
				seen = append(seen, cur)
				err("cycle in the via graph: %s", strings.Join(seen, " → "))
				break
			}
			seen = append(seen, cur)
			cur = st.Hosts[cur].Via
		}
	}

	// links
	seen := map[string]bool{}
	for i := range st.Links {
		l := &st.Links[i]
		k := l.Key()
		if seen[k] {
			err("link %s is described twice", k)
		}
		seen[k] = true
		if l.From == l.To {
			err("link %s: from and to are the same", k)
		}
		for _, side := range []string{l.From, l.To} {
			if _, ok := st.Hosts[side]; !ok {
				err("link %s: host «%s» is not described", k, side)
			}
		}
		if len(l.Protocols) == 0 {
			err("link %s: no protocols", k)
		}
		toInb := st.Inbounds[l.To]
		for _, p := range l.Protocols {
			if !linkProtos[p] {
				err("link %s: %s cannot carry a link", k, p)
			}
			if i, ok := toInb[p]; !ok || !i.IsEnabled() {
				err("link %s: receiver «%s» has no enabled inbound %s", k, l.To, p)
			}
		}
		if l.Active != "" {
			if !contains(l.Protocols, l.Active) {
				err("link %s: active %s is not among the protocols", k, l.Active)
			}
		} else if l.Policy == "fixed" {
			err("link %s: policy fixed requires active", k)
		}
		if contains(l.Protocols, "awg") && l.Tunnel == "" {
			err("link %s: awg requires a tunnel (/30 subnet)", k)
		}
		ls, hasLS := st.Secrets.Links[k]
		if contains(l.Protocols, "awg") && (!hasLS || ls.AWG == nil) {
			err("link %s: no awg keys for both sides in secrets", k)
		}
		if contains(l.Protocols, "reality") && (!hasLS || ls.Reality == nil) {
			err("link %s: no reality keys in secrets", k)
		}
		if contains(l.Protocols, "hy2") && (!hasLS || ls.Hy2Pass == "") {
			err("link %s: no hy2 password in secrets", k)
		}
	}
	for i := 0; i < len(st.Links); i++ {
		for j := i + 1; j < len(st.Links); j++ {
			a, b := &st.Links[i], &st.Links[j]
			if a.Tunnel == "" || b.Tunnel == "" {
				continue
			}
			if cidrContains(a.Tunnel, b.Tunnel) || cidrContains(b.Tunnel, a.Tunnel) {
				err("links %s and %s: tunnel subnets %s and %s overlap",
					a.Key(), b.Key(), canonCIDR(a.Tunnel), canonCIDR(b.Tunnel))
			}
		}
	}

	// users
	peerIPs := map[string]string{}
	for _, name := range sortedUsers(st) {
		u := st.Users[name]
		if !identOK(name) {
			err("user «%s»: name must match [a-z0-9-_]", name)
		}
		if len(u.Modes) == 0 {
			err("user «%s»: no modes", name)
		}
		if len(u.Protocols) == 0 {
			err("user «%s»: no protocols", name)
		}
		if u.Expires != "" && !dateOK(u.Expires) {
			err("user «%s»: expires «%s» is not YYYY-MM-DD", name, u.Expires)
		}
		entry, ok := st.EntryOf(u)
		if !ok {
			err("user «%s»: entry is not set and there is not exactly one entry", name)
			continue
		}
		if h, ok := st.Hosts[entry]; !ok || h.Role != state.Entry {
			err("user «%s»: entry «%s» is not an entry host", name, entry)
			continue
		}
		inb := st.Inbounds[entry]
		for _, p := range u.Protocols {
			if _, ok := inb[p]; !ok {
				err("user «%s»: entry «%s» has no inbound %s", name, entry, p)
			}
		}
		hasAWG := contains(u.Protocols, "awg")
		switch {
		case u.AWG != nil && hasAWG:
			peer := u.AWG
			if s := inb["awg"].Subnets; s != nil {
				netCIDR := s.Split
				if peer.Mode == "full" {
					netCIDR = s.Full
				}
				ip := net.ParseIP(peer.IP)
				n := parseCIDR(netCIDR)
				if n != nil && ip != nil && !n.Contains(ip) {
					err("user «%s»: awg address %s is outside the %s mode subnet (%s)",
						name, peer.IP, peer.Mode, canonCIDR(netCIDR))
				}
				gw := gatewayOf(netCIDR)
				if ip != nil && gw != nil && (sameIP(ip, n.IP) || sameIP(ip, gw)) {
					err("user «%s»: awg address %s is reserved for the gateway", name, peer.IP)
				}
			}
			if !contains(u.Modes, peer.Mode) {
				err("user «%s»: awg peer mode %s is not among the user's modes", name, peer.Mode)
			}
			if other, ok := peerIPs[peer.IP]; ok {
				err("awg address %s is used by both «%s» and «%s»", peer.IP, other, name)
			}
			peerIPs[peer.IP] = name
			if sec, ok := st.Secrets.Users[name]; !ok || sec.AWG == nil {
				err("user «%s»: no awg keys in secrets", name)
			}
		case u.AWG == nil && hasAWG:
			err("user «%s»: awg protocol without a peer description (awg: {mode, ip})", name)
		case u.AWG != nil:
			err("user «%s»: an awg peer is described but the awg protocol is not set", name)
		}
		if _, ok := st.Secrets.Users[name]; !ok {
			err("user «%s»: no secrets (uuid, hy2_password, sub_id)", name)
		}
	}

	// routing and sets
	for _, name := range sortedKeys(st.Sets) {
		s := st.Sets[name]
		if !identOK(name) {
			err("set «%s»: name must match [a-z0-9-_]", name)
		}
		for _, d := range s.Domains {
			body := d
			if i := strings.Index(d, ":"); i >= 0 {
				if p := d[:i]; p == "full" || p == "keyword" || p == "regexp" || p == "domain" {
					body = d[i+1:]
				}
			}
			if body == "" || strings.ContainsFunc(body, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
				err("set «%s»: domain «%s» does not look valid", name, d)
			}
		}
	}
	for _, host := range sortedKeys(st.Routing) {
		r := st.Routing[host]
		h, ok := st.Hosts[host]
		if !ok {
			err("routing/%s.yaml: host is not described", host)
			continue
		}
		if p := r.Exit.Prefer; p != "" {
			if _, ok := st.Link(host, p); !ok {
				err("routing/%s.yaml: exit.prefer «%s» but there is no link %s->%s", host, p, host, p)
			}
		}
		if r.Exit.Policy == "fixed" && r.Exit.Prefer == "" {
			err("routing/%s.yaml: exit.policy fixed requires prefer", host)
		}
		inboundNames := map[string]bool{"probe": true}
		for p := range st.Inbounds[host] {
			inboundNames[p] = true
		}
		groups := []struct {
			name  string
			rules []state.Rule
		}{
			{"all", r.Groups.All},
			{"split", r.Groups.Split},
			{"full", r.Groups.Full},
		}
		for _, g := range groups {
			if h.Role != state.Entry && len(g.rules) > 0 {
				err("routing/%s.yaml: routing rules exist only on entries", host)
			}
			for i, rule := range g.rules {
				at := fmt.Sprintf("routing/%s.yaml: %s #%d", host, g.name, i+1)
				for _, s := range rule.Match.Sets {
					if _, ok := st.Sets[s]; !ok {
						err("%s: set «%s» is not described", at, s)
					}
				}
				for _, ib := range rule.Match.Inbound {
					if !inboundNames[ib] {
						err("%s: inbound «%s» is not described on the host", at, ib)
					}
				}
				if rule.Action.Kind == state.ActionLink && rule.Action.Link != "auto" {
					if _, ok := st.Link(host, rule.Action.Link); !ok {
						err("%s: there is no link %s->%s", at, host, rule.Action.Link)
					}
				}
				if len(rule.Match.Sets) == 0 && len(rule.Match.Geosite) == 0 && len(rule.Match.GeoIP) == 0 &&
					len(rule.Match.Domains) == 0 && len(rule.Match.IP) == 0 && len(rule.Match.Inbound) == 0 &&
					len(rule.Match.Port) == 0 && i+1 != len(g.rules) {
					err("%s: a rule without conditions matches everything; rules below are unreachable", at)
				}
			}
		}
	}
	return errs
}

// Warnings: the state is applicable, but something deserves attention.
func Warnings(st *state.State) []string {
	var w []string
	for _, name := range sortedHosts(st) {
		h := st.Hosts[name]
		if h.Role == state.Entry && !h.Retiring && st.LinksFrom(name) == 0 {
			w = append(w, fmt.Sprintf("host «%s»: the entry has no link to an exit — all traffic goes direct", name))
		}
		if h.HostKey == "" {
			w = append(w, fmt.Sprintf("host «%s»: host key is not recorded yet (appears after bootstrap)", name))
		}
	}
	if !st.Encrypted && len(st.Secrets.Users) > 0 {
		w = append(w, "secrets are stored as plaintext: no .sops.yaml (init --age <recipient>)")
	}
	return w
}
