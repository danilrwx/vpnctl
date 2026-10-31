package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/danilrwx/vpnctl-go/state"
)

// xrayDomains turns the xray-syntax domains back into xray entries.
func xrayDomains(d []string) []string {
	s, f, k, r := domainParts(d)
	var v []string
	for _, x := range s {
		v = append(v, "domain:"+x)
	}
	for _, x := range f {
		v = append(v, "full:"+x)
	}
	for _, x := range k {
		v = append(v, "keyword:"+x)
	}
	for _, x := range r {
		v = append(v, "regexp:"+x)
	}
	return v
}

func domainsOf(st *state.State, m state.Match) []string {
	var v []string
	for _, name := range m.Sets {
		if s, ok := st.Sets[name]; ok {
			v = append(v, xrayDomains(s.Domains)...)
			for _, g := range s.Geosite {
				v = append(v, "geosite:"+g)
			}
		}
	}
	v = append(v, xrayDomains(m.Domains)...)
	for _, g := range m.Geosite {
		v = append(v, "geosite:"+g)
	}
	return v
}

func ipsOf(st *state.State, m state.Match) []string {
	v := append([]string(nil), m.IP...)
	for _, name := range m.Sets {
		if s, ok := st.Sets[name]; ok {
			v = append(v, s.IPs...)
			for _, g := range s.GeoIP {
				v = append(v, "geoip:"+g)
			}
		}
	}
	for _, g := range m.GeoIP {
		v = append(v, "geoip:"+g)
	}
	return v
}

// XrayConfig renders /etc/xray/config.json for hosts with engine: xray.
func XrayConfig(st *state.State, host string) (string, error) {
	h, err := st.Host(host)
	if err != nil {
		return "", err
	}
	inb := st.Inbounds[host]
	hs := st.Secrets.Hosts[host]
	users := UsersOf(st, host)
	incoming := InLinks(st, host)

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

	inbounds := []any{
		obj("tag", "api", "listen", "127.0.0.1", "port", 10085, "protocol", "dokodemo-door",
			"settings", obj("address", "127.0.0.1")),
	}
	if i, ok := inb["reality"]; ok && i.IsEnabled() {
		if hs.Reality == nil {
			return "", fmt.Errorf("host %s: no reality keys", host)
		}
		keys := *hs.Reality
		accs, err := accounts("reality")
		if err != nil {
			return "", err
		}
		clients := []any{}
		for _, a := range accs {
			clients = append(clients, obj("email", a.Email, "id", a.Sec.UUID, "flow", "xtls-rprx-vision"))
		}
		sids := append([]string{}, keys.ShortIDs...)
		for _, l := range incoming {
			if r := st.Secrets.Links[l.Key()].Reality; r != nil {
				clients = append(clients, obj("email", "link-"+l.From, "id", r.UUID, "flow", "xtls-rprx-vision"))
				sids = append(sids, r.Keys.ShortIDs...)
			}
		}
		sni, hsServer, hsPort := RealityTarget(st, host, i)
		inbounds = append(inbounds, obj("tag", "reality", "listen", "0.0.0.0", "port", i.Port, "protocol", "vless",
			"settings", obj("clients", clients, "decryption", "none"),
			"streamSettings", obj("network", "tcp", "security", "reality",
				"realitySettings", obj("dest", fmt.Sprintf("%s:%d", hsServer, hsPort), "serverNames", []string{sni},
					"privateKey", keys.Private, "shortIds", sids)),
			"sniffing", obj("enabled", true, "destOverride", []string{"http", "tls", "quic"})))
	}
	if i, ok := inb["ws"]; ok && i.IsEnabled() {
		accs, err := accounts("ws")
		if err != nil {
			return "", err
		}
		clients := []any{}
		for _, a := range accs {
			clients = append(clients, obj("email", a.Email, "id", a.Sec.UUID))
		}
		inbounds = append(inbounds, obj("tag", "ws", "listen", "127.0.0.1", "port", i.Port, "protocol", "vless",
			"settings", obj("clients", clients, "decryption", "none"),
			"streamSettings", obj("network", "ws", "wsSettings", obj("path", pathOr(i.Path))),
			"sniffing", obj("enabled", true, "destOverride", []string{"http", "tls"})))
	}
	if h.Role == state.Entry {
		inbounds = append(inbounds, obj("tag", "probe", "listen", "127.0.0.1", "port", ProbePort, "protocol", "socks",
			"settings", obj("udp", true), "sniffing", obj("enabled", true, "destOverride", []string{"http", "tls", "quic"})))
	}

	// link outbounds and balancers
	outbounds := []any{
		obj("tag", "direct", "protocol", "freedom"),
		obj("tag", "block", "protocol", "blackhole"),
	}
	balancers := []any{}
	routing := st.Routing[host]
	var linkTags []string
	for _, ol := range OutLinks(st, host) {
		l := ol.L
		to, err := st.Host(l.To)
		if err != nil {
			return "", err
		}
		toInb := st.Inbounds[l.To]
		ls := st.Secrets.Links[l.Key()]
		tags := []string{}
		for _, p := range l.Protocols {
			tag := l.To + "-" + p
			switch p {
			case "awg":
				outbounds = append(outbounds, obj("tag", tag, "protocol", "freedom",
					"streamSettings", obj("sockopt", obj("interface", fmt.Sprintf("awg%d", ol.N)))))
			case "reality":
				if ls.Reality == nil {
					return "", fmt.Errorf("link %s: no reality secrets for the link", l.Key())
				}
				var pk string
				if kh, ok := st.Secrets.Hosts[l.To]; ok && kh.Reality != nil {
					pk = kh.Reality.Public
				}
				port := 443
				sni := ""
				if i, ok := toInb[p]; ok {
					if i.Port != 0 {
						port = i.Port
					}
					sni, _, _ = RealityTarget(st, l.To, i)
				}
				outbounds = append(outbounds, obj("tag", tag, "protocol", "vless",
					"settings", obj("vnext", []any{obj("address", to.IP, "port", port,
						"users", []any{obj("id", ls.Reality.UUID, "encryption", "none", "flow", "xtls-rprx-vision")})}),
					"streamSettings", obj("network", "tcp", "security", "reality",
						"realitySettings", obj("serverName", sni, "fingerprint", "chrome", "publicKey", pk,
							"shortId", first(ls.Reality.Keys.ShortIDs)))))
			default:
				continue // hy2 is unavailable on xray
			}
			tags = append(tags, tag)
		}
		gtag := "link-" + l.To
		selected := tags
		if l.Policy == "fixed" && l.Active != "" {
			selected = []string{l.To + "-" + l.Active}
		}
		balancers = append(balancers, obj("tag", gtag, "selector", selected, "strategy", obj("type", "leastPing")))
		linkTags = append(linkTags, gtag)
	}
	exitSel := []string{}
	if routing.Exit.Policy == "fixed" && routing.Exit.Prefer != "" {
		exitSel = []string{routing.Exit.Prefer + "-"}
	} else {
		for _, t := range linkTags {
			exitSel = append(exitSel, strings.TrimPrefix(t, "link-")+"-")
		}
	}
	if len(linkTags) > 0 {
		balancers = append(balancers, obj("tag", "exit-auto", "selector", exitSel, "strategy", obj("type", "leastPing")))
	}
	subjects := []string{}
	for _, ol := range OutLinks(st, host) {
		subjects = append(subjects, ol.L.To+"-")
	}

	rules := []any{obj("type", "field", "inboundTag", []string{"api"}, "outboundTag", "api")}
	target := func(a state.Action) map[string]any {
		switch {
		case a.Kind == state.ActionDirect:
			return obj("outboundTag", "direct")
		case a.Kind == state.ActionBlock:
			return obj("outboundTag", "block")
		case a.IsAuto():
			if len(linkTags) == 0 {
				return obj("outboundTag", "direct")
			}
			return obj("balancerTag", "exit-auto")
		}
		return obj("balancerTag", "link-"+a.Link)
	}
	groups := []struct {
		rules []state.Rule
		mode  string
	}{
		{routing.Groups.All, ""},
		{routing.Groups.Split, "split"},
		{routing.Groups.Full, "full"},
	}
	for _, g := range groups {
		var scopeUsers []string
		if g.mode != "" {
			for _, u := range users {
				if !containsStr(u.U.Modes, g.mode) {
					continue
				}
				for _, p := range u.U.Protocols {
					if isEngineUser(p) {
						scopeUsers = append(scopeUsers, st.Email(u.Name, g.mode, p))
					}
				}
			}
			if len(scopeUsers) == 0 {
				continue
			}
		}
		for _, r := range g.rules {
			if !r.IsEnabled() {
				continue
			}
			var kinds []any
			if d := domainsOf(st, r.Match); len(d) > 0 {
				kinds = append(kinds, obj("domain", d))
			}
			if ip := ipsOf(st, r.Match); len(ip) > 0 {
				kinds = append(kinds, obj("ip", ip))
			}
			if len(r.Match.Inbound) > 0 {
				kinds = append(kinds, obj("inboundTag", r.Match.Inbound))
			}
			if len(r.Match.Port) > 0 {
				ports := make([]string, len(r.Match.Port))
				for i, p := range r.Match.Port {
					ports[i] = strconv.Itoa(p)
				}
				kinds = append(kinds, obj("port", strings.Join(ports, ",")))
			}
			if len(kinds) == 0 {
				kinds = append(kinds, map[string]any{})
			}
			for _, k := range kinds {
				rule := obj("type", "field")
				rule = merge(rule, k.(map[string]any))
				if g.mode != "" {
					rule["user"] = scopeUsers
				}
				rules = append(rules, merge(rule, target(r.Action)))
			}
		}
	}

	cfg := obj(
		"log", obj("loglevel", "warning"),
		"api", obj("tag", "api", "services", []string{"StatsService"}),
		"stats", map[string]any{},
		"policy", obj("levels", obj("0", obj("statsUserUplink", true, "statsUserDownlink", true)),
			"system", obj("statsOutboundUplink", true, "statsOutboundDownlink", true)),
		"inbounds", inbounds,
		"outbounds", outbounds,
		"routing", obj("domainStrategy", "IPIfNonMatch", "rules", rules, "balancers", balancers),
		"observatory", obj("subjectSelector", subjects, "probeUrl", ProbeURL,
			"probeInterval", fmt.Sprintf("%ds", routing.Exit.URLTest.IntervalS)),
	)
	return toJSON(cfg)
}
