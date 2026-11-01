package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// toJSON is serde_json::to_string_pretty: 2 spaces, sorted keys, no HTML escaping, trailing newline.
func toJSON(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return b.String(), nil
}

// obj is a JSON object; keys are sorted on marshalling.
func obj(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// domainParts splits xray-syntax domains into suffix, full, keyword and regexp lists.
func domainParts(domains []string) (suffix, full, keyword, regexp []string) {
	for _, d := range domains {
		i := strings.Index(d, ":")
		if i < 0 {
			suffix = append(suffix, d)
			continue
		}
		kind, rest := d[:i], d[i+1:]
		switch kind {
		case "full":
			full = append(full, rest)
		case "keyword":
			keyword = append(keyword, rest)
		case "regexp":
			regexp = append(regexp, rest)
		case "domain":
			suffix = append(suffix, rest)
		default:
			suffix = append(suffix, d)
		}
	}
	return
}

// headless is a sing-box rule without an action, usable as a condition or a rule set entry.
func headless(domains, ips []string) map[string]any {
	s, f, k, r := domainParts(domains)
	o := map[string]any{}
	if len(s) > 0 {
		o["domain_suffix"] = s
	}
	if len(f) > 0 {
		o["domain"] = f
	}
	if len(k) > 0 {
		o["domain_keyword"] = k
	}
	if len(r) > 0 {
		o["domain_regex"] = r
	}
	if len(ips) > 0 {
		o["ip_cidr"] = ips
	}
	return o
}

// RuleSet is a local rule set file: /etc/sing-box/rules/<tag>.json.
type RuleSet struct {
	Tag  string
	Body string
}

// RuleSets renders the set entries used by the host's routing as rule set files.
func RuleSets(st *state.State, host string) ([]RuleSet, error) {
	used := usedSets(st, host)
	var names []string
	for name := range st.Sets {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []RuleSet
	for _, name := range names {
		s := st.Sets[name]
		if !used[name] || s.Empty() {
			continue
		}
		body, err := toJSON(obj("version", 3, "rules", []any{headless(s.Domains, s.IPs)}))
		if err != nil {
			return nil, err
		}
		out = append(out, RuleSet{Tag: name, Body: body})
	}
	return out, nil
}

// usedSets is the set of rule set names any group of the host matches on.
func usedSets(st *state.State, host string) map[string]bool {
	out := map[string]bool{}
	r, ok := st.Routing[host]
	if !ok {
		return out
	}
	for _, g := range [][]state.Rule{r.Groups.All, r.Groups.Split, r.Groups.Full} {
		for _, rule := range g {
			for _, s := range rule.Match.Sets {
				out[s] = true
			}
		}
	}
	return out
}

// setTags are the rule set tags of a set: our entries, geosite and geoip.
func setTags(st *state.State, set string) []string {
	s, ok := st.Sets[set]
	if !ok {
		return nil
	}
	var t []string
	if !s.Empty() {
		t = append(t, "set-"+set)
	}
	for _, g := range s.Geosite {
		t = append(t, "geosite-"+g)
	}
	for _, g := range s.GeoIP {
		t = append(t, "geoip-"+g)
	}
	return t
}

// ruleSetSet collects the declared rule sets in tag order.
type ruleSetSet struct{ m map[string]map[string]any }

func (rs *ruleSetSet) add(tag string) {
	if _, ok := rs.m[tag]; ok {
		return
	}
	if set, ok := strings.CutPrefix(tag, "set-"); ok {
		rs.m[tag] = obj("type", "local", "tag", tag, "format", "source", "path", "rules/"+set+".json")
		return
	}
	rs.m[tag] = obj("type", "local", "tag", tag, "format", "binary", "path", "rules/"+tag+".srs")
}

// tags is the collected rule sets as a sorted list for the route section.
func (rs *ruleSetSet) tags() []any {
	names := make([]string, 0, len(rs.m))
	for tag := range rs.m {
		names = append(names, tag)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, rs.m[n])
	}
	return out
}

// condition renders a match as one sing-box condition, or a logical/or of several.
func condition(st *state.State, m state.Match, rs *ruleSetSet) (map[string]any, bool) {
	var parts []any
	var tags []string
	for _, s := range m.Sets {
		tags = append(tags, setTags(st, s)...)
	}
	for _, g := range m.Geosite {
		tags = append(tags, "geosite-"+g)
	}
	for _, g := range m.GeoIP {
		if g != "private" {
			tags = append(tags, "geoip-"+g)
		}
	}
	tags = dedupSorted(tags)
	if len(tags) > 0 {
		for _, t := range tags {
			rs.add(t)
		}
		parts = append(parts, obj("rule_set", tags))
	}
	if containsStr(m.GeoIP, "private") {
		parts = append(parts, obj("ip_is_private", true))
	}
	if len(m.Domains) > 0 || len(m.IP) > 0 {
		parts = append(parts, headless(m.Domains, m.IP))
	}
	if len(m.Inbound) > 0 {
		parts = append(parts, obj("inbound", m.Inbound))
	}
	if len(m.Port) > 0 {
		parts = append(parts, obj("port", m.Port))
	}
	switch len(parts) {
	case 0:
		return nil, false
	case 1:
		return parts[0].(map[string]any), true
	}
	return obj("type", "logical", "mode", "or", "rules", parts), true
}

// merge is Map.extend: the right side wins.
func merge(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// actionOf is the action half of a sing-box rule.
func actionOf(a state.Action, exitTag string) map[string]any {
	switch {
	case a.Kind == state.ActionDirect:
		return obj("outbound", "direct")
	case a.Kind == state.ActionBlock:
		return obj("action", "reject")
	case a.IsAuto():
		return obj("outbound", exitTag)
	}
	return obj("outbound", "link-"+a.Link)
}

// scope is the group scope: the accounts of the mode or the L3 peer subnet of the mode.
func scope(st *state.State, host, mode string, users []Account) (map[string]any, bool) {
	var emails []string
	for _, acc := range users {
		if !containsStr(acc.U.Modes, mode) {
			continue
		}
		for _, p := range acc.U.Protocols {
			if isEngineUser(p) {
				emails = append(emails, st.Email(acc.Name, mode, p))
			}
		}
	}
	var subnet string
	if i, ok := st.Inbounds[host]["awg"]; ok && i.IsEnabled() && i.Subnets != nil {
		subnet = i.Subnets.ForMode(mode)
	}
	var parts []any
	if len(emails) > 0 {
		parts = append(parts, obj("auth_user", emails))
	}
	if subnet != "" {
		parts = append(parts, obj("source_ip_cidr", []string{subnet}))
	}
	switch len(parts) {
	case 0:
		return nil, false
	case 1:
		return parts[0].(map[string]any), true
	}
	return obj("type", "logical", "mode", "or", "rules", parts), true
}

func isEngineUser(p string) bool {
	return p == "reality" || p == "ws" || p == "hy2"
}

// linkOutbounds is the outbounds of the outgoing links and the exit tag routing falls back to.
func linkOutbounds(st *state.State, host string) ([]any, string, error) {
	outs := []any{}
	type group struct {
		tag string
		val map[string]any
	}
	var groups []group
	routing := st.Routing[host]
	for _, ol := range OutLinks(st, host) {
		l := ol.L
		to, err := st.Host(l.To)
		if err != nil {
			return nil, "", err
		}
		toInb := st.Inbounds[l.To]
		ls := st.Secrets.Links[l.Key()]
		tags := []string{}
		protos := append([]string(nil), l.Protocols...)
		if l.Active != "" {
			protos = append([]string{l.Active}, withoutStr(protos, l.Active)...)
		}
		for _, p := range protos {
			tag := l.To + "-" + p
			port := 443
			if i, ok := toInb[p]; ok && i.Port != 0 {
				port = i.Port
			}
			var v map[string]any
			switch p {
			case "awg":
				v = obj("type", "direct", "tag", tag, "bind_interface", fmt.Sprintf("awg%d", ol.N))
			case "reality":
				if ls.Reality == nil {
					return nil, "", fmt.Errorf("link %s: no reality secrets for the link", l.Key())
				}
				var pk string
				if kh, ok := st.Secrets.Hosts[l.To]; ok && kh.Reality != nil {
					pk = kh.Reality.Public
				}
				sni := ""
				if i, ok := toInb[p]; ok {
					sni, _, _ = RealityTarget(st, l.To, i)
				}
				v = obj("type", "vless", "tag", tag, "server", to.IP, "server_port", port,
					"uuid", ls.Reality.UUID, "flow", "xtls-rprx-vision",
					"tls", obj("enabled", true, "server_name", sni,
						"utls", obj("enabled", true, "fingerprint", "chrome"),
						"reality", obj("enabled", true, "public_key", pk, "short_id", first(ls.Reality.Keys.ShortIDs))))
			case "hy2":
				v = obj("type", "hysteria2", "tag", tag, "server", to.IP, "server_port", port,
					"password", ls.Hy2Pass, "tls", obj("enabled", true, "server_name", l.To, "insecure", true))
			default:
				continue
			}
			outs = append(outs, v)
			tags = append(tags, tag)
		}
		gtag := "link-" + l.To
		var g map[string]any
		switch l.Policy {
		case "fixed":
			if len(tags) == 0 {
				return nil, "", fmt.Errorf("link %s: no protocol usable by sing-box", l.Key())
			}
			g = obj("type", "selector", "tag", gtag, "outbounds", tags, "default", tags[0], "interrupt_exist_connections", true)
		default:
			// rr: sing-box has no round-robin, behave like auto
			g = obj("type", "urltest", "tag", gtag, "outbounds", tags, "url", ProbeURL,
				"interval", fmt.Sprintf("%ds", l.URLTest.IntervalS), "tolerance", l.URLTest.ToleranceMS)
		}
		groups = append(groups, group{tag: gtag, val: g})
	}
	exitTag := "direct"
	if len(groups) > 0 {
		tags := make([]string, len(groups))
		for i, g := range groups {
			tags[i] = g.tag
		}
		if p := routing.Exit.Prefer; p != "" {
			pt := "link-" + p
			tags = append([]string{pt}, withoutStr(tags, pt)...)
		}
		var g map[string]any
		if routing.Exit.Policy == "fixed" {
			g = obj("type", "selector", "tag", "exit-auto", "outbounds", tags, "default", tags[0], "interrupt_exist_connections", true)
		} else {
			g = obj("type", "urltest", "tag", "exit-auto", "outbounds", tags, "url", ProbeURL,
				"interval", fmt.Sprintf("%ds", routing.Exit.URLTest.IntervalS),
				"tolerance", routing.Exit.URLTest.ToleranceMS)
		}
		groups = append(groups, group{tag: "exit-auto", val: g})
		exitTag = "exit-auto"
	}
	for _, g := range groups {
		outs = append(outs, g.val)
	}
	return outs, exitTag, nil
}

// SingBoxConfig renders /etc/sing-box/config.json.
func SingBoxConfig(st *state.State, host string) (string, error) {
	h, err := st.Host(host)
	if err != nil {
		return "", err
	}
	inb := st.Inbounds[host]
	hs := st.Secrets.Hosts[host]
	users := UsersOf(st, host)
	cert, key := CertPaths(st, host)
	inbounds := []any{}

	accounts := func(p string) ([]acc, error) {
		var out []acc
		for _, u := range users {
			if !containsStr(u.U.Protocols, p) {
				continue
			}
			s, ok := st.Secrets.Users[u.Name]
			if !ok {
				return nil, fmt.Errorf("no secrets for user %s", u.Name)
			}
			for _, m := range u.U.Modes {
				out = append(out, acc{Email: st.Email(u.Name, m, p), Sec: s})
			}
		}
		return out, nil
	}
	incoming := InLinks(st, host)

	if i, ok := inb["reality"]; ok && i.IsEnabled() {
		if hs.Reality == nil {
			return "", fmt.Errorf("host %s: no reality keys", host)
		}
		keys := *hs.Reality
		accs, err := accounts("reality")
		if err != nil {
			return "", err
		}
		us := []any{}
		for _, a := range accs {
			us = append(us, obj("name", a.Email, "uuid", a.Sec.UUID, "flow", "xtls-rprx-vision"))
		}
		sids := append([]string{}, keys.ShortIDs...)
		for _, l := range incoming {
			if r := st.Secrets.Links[l.Key()].Reality; r != nil {
				us = append(us, obj("name", "link-"+l.From, "uuid", r.UUID, "flow", "xtls-rprx-vision"))
				sids = append(sids, r.Keys.ShortIDs...)
			}
		}
		sni, hsServer, hsPort := RealityTarget(st, host, i)
		inbounds = append(inbounds, obj("type", "vless", "tag", "reality", "listen", "::", "listen_port", i.Port, "users", us,
			"tls", obj("enabled", true, "server_name", sni,
				"reality", obj("enabled", true,
					"handshake", obj("server", hsServer, "server_port", hsPort),
					"private_key", keys.Private, "short_id", sids))))
	}
	if i, ok := inb["ws"]; ok && i.IsEnabled() {
		accs, err := accounts("ws")
		if err != nil {
			return "", err
		}
		us := []any{}
		for _, a := range accs {
			us = append(us, obj("name", a.Email, "uuid", a.Sec.UUID))
		}
		inbounds = append(inbounds, obj("type", "vless", "tag", "ws", "listen", "127.0.0.1", "listen_port", i.Port, "users", us,
			"transport", obj("type", "ws", "path", pathOr(i.Path))))
	}
	if i, ok := inb["hy2"]; ok && i.IsEnabled() {
		accs, err := accounts("hy2")
		if err != nil {
			return "", err
		}
		us := []any{}
		for _, a := range accs {
			us = append(us, obj("name", a.Email, "password", a.Sec.Hy2Pass))
		}
		for _, l := range incoming {
			if p := st.Secrets.Links[l.Key()].Hy2Pass; p != "" {
				us = append(us, obj("name", "link-"+l.From, "password", p))
			}
		}
		inbounds = append(inbounds, obj("type", "hysteria2", "tag", "hy2", "listen", "::", "listen_port", i.Port, "users", us,
			"tls", obj("enabled", true, "certificate_path", cert, "key_path", key)))
	}
	if h.Role == state.Entry {
		if i, ok := inb["awg"]; ok && i.IsEnabled() {
			inbounds = append(inbounds, obj("type", "tproxy", "tag", "tproxy", "listen", "127.0.0.1", "listen_port", TProxyPort))
		}
		inbounds = append(inbounds, obj("type", "socks", "tag", "probe", "listen", "127.0.0.1", "listen_port", ProbePort))
	}

	outbounds, exitTag, err := linkOutbounds(st, host)
	if err != nil {
		return "", err
	}
	outbounds = append([]any{obj("type", "direct", "tag", "direct")}, outbounds...)

	rs := &ruleSetSet{m: map[string]map[string]any{}}
	rules := []any{
		obj("action", "sniff"),
		obj("protocol", "dns", "action", "hijack-dns"),
	}
	var directTags []string
	routing := st.Routing[host]
	groups := []struct {
		rules []state.Rule
		mode  string
	}{
		{routing.Groups.All, ""},
		{routing.Groups.Split, "split"},
		{routing.Groups.Full, "full"},
	}
	for _, g := range groups {
		var sc map[string]any
		if g.mode != "" {
			var ok bool
			if sc, ok = scope(st, host, g.mode, users); !ok {
				continue // nobody in this mode, no rules needed
			}
		}
		for _, r := range g.rules {
			if !r.IsEnabled() {
				continue
			}
			cond, ok := condition(st, r.Match, rs)
			if r.Action.Kind == state.ActionDirect && ok {
				if t, isArr := cond["rule_set"].([]string); isArr {
					directTags = append(directTags, t...)
				}
			}
			var m map[string]any
			switch {
			case sc == nil && !ok:
				m = map[string]any{}
			case sc != nil && !ok:
				m = sc
			case sc == nil:
				m = cond
			default:
				m = obj("type", "logical", "mode", "and", "rules", []any{sc, cond})
			}
			rules = append(rules, merge(m, actionOf(r.Action, exitTag)))
		}
	}

	var dns map[string]any
	if h.Role == state.Entry {
		var servers []any
		for i, s := range routing.DNS.Direct {
			servers = append(servers, obj("type", "udp", "tag", fmt.Sprintf("dns-direct-%d", i+1), "server", s))
		}
		proto, server, _ := strings.Cut(routing.DNS.Remote, "://")
		if proto == routing.DNS.Remote {
			proto, server = "udp", routing.DNS.Remote
		}
		hostPart, path := server, "/dns-query"
		if i := strings.Index(server, "/"); i >= 0 {
			hostPart, path = server[:i], server[i:]
		}
		remote := obj("type", proto, "tag", "dns-remote", "server", hostPart)
		// An unconfigured "direct" is an empty outbound: sing-box 1.12 rejects a detour to it
		if exitTag != "direct" {
			remote["detour"] = exitTag
		}
		if proto == "https" {
			remote["path"] = path
		}
		servers = append(servers, remote)
		drules := []any{}
		var dt []string
		for _, t := range directTags {
			if strings.HasPrefix(t, "geosite-") || strings.HasPrefix(t, "set-") {
				dt = append(dt, t)
			}
		}
		dt = dedupSorted(dt)
		if len(dt) > 0 && len(routing.DNS.Direct) > 0 {
			drules = append(drules, obj("rule_set", dt, "server", "dns-direct-1"))
		}
		dns = obj("servers", servers, "rules", drules, "final", "dns-remote", "strategy", "prefer_ipv4")
	} else {
		// An exit does not need a public resolver: UDP/53 is often blocked by the provider, while REALITY
		// must resolve its handshake domain. The system resolver works via DHCP settings.
		dns = obj("servers", []any{obj("type", "local", "tag", "dns-direct-1")}, "final", "dns-direct-1")
	}

	cfg := obj(
		"log", obj("level", "info", "timestamp", true),
		"dns", dns,
		"inbounds", inbounds,
		"outbounds", outbounds,
		"route", obj("rules", rules, "rule_set", rs.tags(), "final", "direct", "auto_detect_interface", true),
		// the official sing-box build has no v2ray_api; clash_api is available
		"experimental", obj("clash_api", obj("external_controller", StatsAPI)),
	)
	return toJSON(cfg)
}

type acc struct {
	Email string
	Sec   state.UserSecrets
}

func pathOr(p string) string {
	if p == "" {
		return "/ws"
	}
	return p
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func withoutStr(list []string, v string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func dedupSorted(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	var res []string
	for i, x := range out {
		if i == 0 || x != out[i-1] {
			res = append(res, x)
		}
	}
	return res
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}
