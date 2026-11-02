package mobile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// RouteRule is one rule as the screen shows it: what it matches, what it does.
type RouteRule struct {
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Action  string `json:"action"`
	Note    string `json:"note,omitempty"`
	Enabled bool   `json:"enabled"`
}

// RouteGroup is one of the three chains: all, split, full.
type RouteGroup struct {
	Name  string      `json:"name"`
	Label string      `json:"label"`
	Rules []RouteRule `json:"rules"`
}

// RouteHost is the routing of one host.
type RouteHost struct {
	Name   string       `json:"name"`
	Role   string       `json:"role"`
	Exit   string       `json:"exit"`
	DNS    string       `json:"dns"`
	Groups []RouteGroup `json:"groups"`
}

// Routing returns the rules of every host, as JSON. The screen shows the state;
// it does not evaluate addresses, because geosite and geoip live on the hosts.
func (a *App) Routing() string {
	names := make([]string, 0, len(a.st.Routing))
	for name := range a.st.Routing {
		names = append(names, name)
	}

	sort.Strings(names)

	out := make([]RouteHost, 0, len(names))

	for _, name := range names {
		r := a.st.Routing[name]

		out = append(out, RouteHost{
			Name: name,
			Role: string(a.st.Hosts[name].Role),
			Exit: exitText(r.Exit),
			DNS:  dnsText(r.DNS),
			Groups: []RouteGroup{
				{Name: "all", Label: "For all", Rules: routeRules(a.st.Sets, r.Groups.All)},
				{Name: "split", Label: "split", Rules: routeRules(a.st.Sets, r.Groups.Split)},
				{Name: "full", Label: "full", Rules: routeRules(a.st.Sets, r.Groups.Full)},
			},
		})
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}

	return string(b)
}

func exitText(e state.ExitPolicy) string {
	if e.Policy == "fixed" && e.Prefer != "" {
		return "fixed · prefer " + e.Prefer
	}

	if e.Policy == "" {
		return "auto"
	}

	return e.Policy
}

func dnsText(d state.DNS) string {
	direct := strings.Join(d.Direct, ", ")
	if d.Remote == "" {
		return direct
	}

	if direct == "" {
		return d.Remote
	}

	return direct + " → " + d.Remote
}

func routeRules(sets map[string]state.Set, rules []state.Rule) []RouteRule {
	out := make([]RouteRule, 0, len(rules))

	for _, r := range rules {
		title, detail := ruleMatch(sets, r.Match)

		out = append(out, RouteRule{
			Title:   title,
			Detail:  detail,
			Action:  actionText(r.Action),
			Note:    r.Note,
			Enabled: r.IsEnabled(),
		})
	}

	return out
}

// ruleMatch names a rule the way the mockup does: the first set it uses, with
// the geosite names and the operator's own entries counted underneath.
func ruleMatch(sets map[string]state.Set, m state.Match) (string, string) {
	switch {
	case len(m.Sets) > 0:
		title := "set " + m.Sets[0]
		if len(m.Sets) > 1 {
			title = fmt.Sprintf("%s +%d", title, len(m.Sets)-1)
		}

		var parts []string

		own := 0

		for _, name := range m.Sets {
			s, ok := sets[name]
			if !ok {
				parts = append(parts, name+" (missing)")
				continue
			}

			if list := append(append([]string{}, s.Geosite...), s.GeoIP...); len(list) > 0 {
				parts = append(parts, strings.Join(list, ", "))
			}

			own += len(s.Domains) + len(s.IPs)
		}

		if own > 0 {
			parts = append(parts, fmt.Sprintf("%d own", own))
		}

		return title, strings.Join(parts, " · ")

	case len(m.Geosite) > 0 || len(m.GeoIP) > 0:
		var parts []string
		if len(m.Geosite) > 0 {
			parts = append(parts, "geosite:"+strings.Join(m.Geosite, ","))
		}

		if len(m.GeoIP) > 0 {
			parts = append(parts, "geoip:"+strings.Join(m.GeoIP, ","))
		}

		return "addresses", strings.Join(parts, " · ")

	case len(m.Inbound) > 0:
		return "inbound " + strings.Join(m.Inbound, ", "), ""

	case len(m.Domains) > 0 || len(m.IP) > 0:
		return "addresses", fmt.Sprintf("%d domains, %d prefixes", len(m.Domains), len(m.IP))

	case len(m.Port) > 0:
		return "ports", fmt.Sprint(m.Port)
	}

	return "everything else", "catch-all, keep last"
}

func actionText(a state.Action) string {
	switch a.Kind {
	case state.ActionDirect:
		return "direct"
	case state.ActionBlock:
		return "block"
	case state.ActionLink:
		return "→ link " + a.Link
	}

	return string(a.Kind)
}
