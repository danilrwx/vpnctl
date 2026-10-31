// Package validate checks the state statically and returns a list of errors.
package validate

import (
	"fmt"
	"sort"

	"github.com/danilrwx/vpnctl-go/state"
)

var linkProtos = map[string]bool{"reality": true, "hy2": true, "awg": true}

func enable(i state.Inbound) bool { return i.Enabled == nil || *i.Enabled }

// State returns the errors of the state; an empty slice means it is valid.
func State(st *state.State) []string {
	var errs []string
	err := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	// hosts
	domains := map[string][]string{}
	for _, name := range sortedHosts(st) {
		h := st.Hosts[name]
		if !nameOK(name) {
			err("host `%s`: name must match [a-z0-9-_]", name)
		}
		if h.Via != "" {
			if _, ok := st.Hosts[h.Via]; !ok {
				err("host `%s`: via `%s` is not described", name, h.Via)
			}
			if h.Via == name {
				err("host `%s`: via points to the host itself", name)
			}
		}
		if h.Role == state.Entry && h.Domain == "" {
			err("host `%s`: the entry requires a domain", name)
		}
		if h.Domain != "" {
			domains[h.Domain] = append(domains[h.Domain], name)
		}
		ports := map[int]string{}
		for proto, i := range st.Inbounds[name] {
			if !enable(i) {
				continue
			}
			if other, ok := ports[i.Port]; ok && other != proto {
				err("host `%s`: port %d is used by both %s and %s", name, i.Port, other, proto)
			}
			ports[i.Port] = proto
		}
	}
	if cycle := viaCycle(st); cycle != "" {
		err("cycle in the via graph: %s", cycle)
	}
	for d, hosts := range domains {
		if len(hosts) > 1 {
			err("domain %s on hosts %v: allowed only during a migration", d, hosts)
		}
	}

	// links
	seen := map[string]bool{}
	for _, l := range st.Links {
		key := l.From + "->" + l.To
		if seen[key] {
			err("link %s is described twice", key)
			continue
		}
		seen[key] = true
		if l.From == l.To {
			err("link %s: from and to are the same", key)
		}
		for _, side := range []string{l.From, l.To} {
			if _, ok := st.Hosts[side]; !ok {
				err("link %s: host `%s` is not described", key, side)
			}
		}
		if len(l.Protocols) == 0 {
			err("link %s: no protocols", key)
		}
		for _, p := range l.Protocols {
			if !linkProtos[p] {
				err("link %s: %s cannot carry a link", key, p)
				continue
			}
			if i, ok := st.Inbounds[l.To][p]; !ok || !enable(i) {
				err("link %s: receiver `%s` has no enabled inbound %s", key, l.To, p)
			}
		}
		if l.Active != "" && !contains(l.Protocols, l.Active) {
			err("link %s: active %s is not among the protocols", key, l.Active)
		}
	}

	// users
	entry, entryErr := st.Entry()
	for _, name := range sortedUsers(st) {
		u := st.Users[name]
		if !nameOK(name) {
			err("user `%s`: name must match [a-z0-9-_]", name)
		}
		if len(u.Modes) == 0 {
			err("user `%s`: no modes", name)
		}
		if len(u.Protocols) == 0 {
			err("user `%s`: no protocols", name)
		}
		if u.Entry != "" && st.Hosts[u.Entry].Role != state.Entry {
			err("user `%s`: entry `%s` is not an entry host", name, u.Entry)
		}
		target := u.Entry
		if target == "" {
			if entryErr != nil {
				err("user `%s`: entry is not set and there is not exactly one entry", name)
			} else {
				target = entry
			}
		}
		for _, p := range u.Protocols {
			if p == "mtg" || p == "awg" {
				continue
			}
			if i, ok := st.Inbounds[target][p]; !ok || !enable(i) {
				err("user `%s`: entry `%s` has no inbound %s", name, target, p)
			}
		}
		if _, ok := st.Secrets.Users[name]; !ok {
			err("user `%s`: no secrets (uuid, hy2_password, sub_id)", name)
		}
	}

	// routing
	for name, r := range st.Routing {
		if _, ok := st.Hosts[name]; !ok {
			err("routing/%s.yaml: host is not described", name)
			continue
		}
		if r.Exit.Prefer != "" && !seen[name+"->"+r.Exit.Prefer] {
			err("routing/%s.yaml: exit.prefer `%s` but there is no link %s->%s", name, r.Exit.Prefer, name, r.Exit.Prefer)
		}
	}
	return errs
}

func sortedHosts(st *state.State) []string {
	out := make([]string, 0, len(st.Hosts))
	for n := range st.Hosts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func sortedUsers(st *state.State) []string {
	out := make([]string, 0, len(st.Users))
	for n := range st.Users {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func nameOK(s string) bool {
	for _, ch := range s {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return s != ""
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func viaCycle(st *state.State) string {
	for start := range st.Hosts {
		seen := []string{start}
		cur := st.Hosts[start].Via
		for cur != "" {
			if contains(seen, cur) {
				return cur
			}
			seen = append(seen, cur)
			h, ok := st.Hosts[cur]
			if !ok {
				break
			}
			cur = h.Via
		}
	}
	return ""
}
