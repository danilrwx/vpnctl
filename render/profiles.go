package render

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/danilrwx/vpnctl-go/state"
)

// Acc is one protocol account of a user on the entry: what a link or a subscription entry needs.
type Acc struct {
	Proto    string
	Email    string
	Domain   string
	Port     int
	SNI      string
	Path     string
	UUID     string
	Password string
	PBK      string
	SID      string
}

// SubAccounts are the accounts of a user in a mode: only the engine inbounds enabled on the entry.
func SubAccounts(st *state.State, entry, user string, u state.User, mode string) ([]Acc, error) {
	h, err := st.Host(entry)
	if err != nil {
		return nil, err
	}
	if h.Domain == "" {
		return nil, fmt.Errorf("the entry has no domain")
	}
	inb := st.Inbounds[entry]
	hs := st.Secrets.Hosts[entry]
	sec, ok := st.Secrets.Users[user]
	if !ok {
		return nil, fmt.Errorf("no secrets for user %s", user)
	}
	var v []Acc
	for _, p := range []string{"reality", "ws", "hy2"} {
		if !containsStr(u.Protocols, p) {
			continue
		}
		i, ok := inb[p]
		if !ok || !i.IsEnabled() {
			continue
		}
		a := Acc{
			Proto:    p,
			Email:    st.Email(user, mode, p),
			Domain:   h.Domain,
			Port:     i.Port,
			Path:     pathOr(i.Path),
			UUID:     sec.UUID,
			Password: sec.Hy2Pass,
		}
		// ws goes through the web server on 443, the others use their own port
		if p == "ws" {
			a.Port = 443
		}
		if p == "reality" {
			a.SNI, _, _ = RealityTarget(st, entry, i)
			if hs.Reality != nil {
				a.PBK, a.SID = hs.Reality.Public, first(hs.Reality.ShortIDs)
			}
		} else {
			a.SNI = h.Domain
		}
		v = append(v, a)
	}
	return v, nil
}

// enc escapes the ws path the way clients expect it in a link.
func enc(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "/", "%2F"), ":", "%3A")
}

// Base64List is the plain subscription body: one link per line, base64.
func Base64List(links []string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))) + "\n"
}

// SingboxJSON is the profile served to a client with a User-Agent of *sing-box*.
func SingboxJSON(st *state.State, entry, user string, u state.User, mode string) (string, error) {
	accs, err := SubAccounts(st, entry, user, u, mode)
	if err != nil {
		return "", err
	}
	outs := []any{}
	tags := []string{}
	for _, a := range accs {
		switch a.Proto {
		case "reality":
			outs = append(outs, obj("type", "vless", "tag", a.Email, "server", a.Domain, "server_port", a.Port,
				"uuid", a.UUID, "flow", "xtls-rprx-vision",
				"tls", obj("enabled", true, "server_name", a.SNI,
					"utls", obj("enabled", true, "fingerprint", "chrome"),
					"reality", obj("enabled", true, "public_key", a.PBK, "short_id", a.SID))))
		case "ws":
			outs = append(outs, obj("type", "vless", "tag", a.Email, "server", a.Domain, "server_port", a.Port,
				"uuid", a.UUID,
				"tls", obj("enabled", true, "server_name", a.Domain),
				"transport", obj("type", "ws", "path", a.Path, "headers", obj("Host", a.Domain))))
		case "hy2":
			outs = append(outs, obj("type", "hysteria2", "tag", a.Email, "server", a.Domain, "server_port", a.Port,
				"password", a.Password, "tls", obj("enabled", true, "server_name", a.Domain)))
		}
		tags = append(tags, a.Email)
	}
	group := user + "-" + mode
	outs = append([]any{obj("type", "urltest", "tag", group, "outbounds", tags, "url", ProbeURL, "interval", "60s")}, outs...)
	outs = append(outs, obj("type", "direct", "tag", "direct"))
	cfg := obj("outbounds", outs, "route", obj("final", group, "auto_detect_interface", true))
	return toJSON(cfg)
}

// ClashYAML is the profile served to a client with a User-Agent of *clash*.
func ClashYAML(st *state.State, entry, user string, u state.User, mode string) (string, error) {
	accs, err := SubAccounts(st, entry, user, u, mode)
	if err != nil {
		return "", err
	}
	proxies := []any{}
	names := []string{}
	for _, a := range accs {
		switch a.Proto {
		case "reality":
			proxies = append(proxies, obj("name", a.Email, "type", "vless", "server", a.Domain, "port", a.Port,
				"uuid", a.UUID, "network", "tcp", "tls", true, "udp", true, "flow", "xtls-rprx-vision",
				"servername", a.SNI, "client-fingerprint", "chrome",
				"reality-opts", obj("public-key", a.PBK, "short-id", a.SID)))
		case "ws":
			proxies = append(proxies, obj("name", a.Email, "type", "vless", "server", a.Domain, "port", a.Port,
				"uuid", a.UUID, "network", "ws", "tls", true, "udp", true,
				"servername", a.Domain, "ws-opts", obj("path", a.Path, "headers", obj("Host", a.Domain))))
		case "hy2":
			proxies = append(proxies, obj("name", a.Email, "type", "hysteria2", "server", a.Domain, "port", a.Port,
				"password", a.Password, "sni", a.Domain))
		}
		names = append(names, a.Email)
	}
	group := user + "-" + mode
	cfg := obj(
		"proxies", proxies,
		"proxy-groups", []any{obj("name", group, "type", "url-test", "proxies", names, "url", ProbeURL, "interval", 60)},
		"rules", []any{"MATCH," + group},
	)
	return yamlDoc(cfg), nil
}

// yamlDoc is serde_yaml's block style for the shapes the Clash profile uses: sorted keys,
// two-space indentation and a sequence at the indentation of its key.
func yamlDoc(m map[string]any) string {
	var b strings.Builder
	emitMap(&b, m, 0, false)
	return b.String()
}

func emitMap(b *strings.Builder, m map[string]any, indent int, inline bool) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 || !inline {
			b.WriteString(strings.Repeat(" ", indent))
		}
		b.WriteString(k + ":")
		switch v := m[k].(type) {
		case map[string]any:
			b.WriteString("\n")
			emitMap(b, v, indent+2, false)
		case []any:
			if len(v) == 0 {
				b.WriteString(" []\n")
				break
			}
			b.WriteString("\n")
			emitSeq(b, v, indent)
		case []string:
			if len(v) == 0 {
				b.WriteString(" []\n")
				break
			}
			b.WriteString("\n")
			emitSeq(b, anyList(v), indent)
		default:
			b.WriteString(" " + yamlScalar(v) + "\n")
		}
	}
}

func emitSeq(b *strings.Builder, s []any, indent int) {
	for _, it := range s {
		b.WriteString(strings.Repeat(" ", indent) + "- ")
		switch v := it.(type) {
		case map[string]any:
			emitMap(b, v, indent+2, true)
		case []any:
			b.WriteString("\n")
			emitSeq(b, v, indent+2)
		case []string:
			b.WriteString("\n")
			emitSeq(b, anyList(v), indent+2)
		default:
			b.WriteString(yamlScalar(v) + "\n")
		}
	}
}

// anyList widens a typed slice for the emitter.
func anyList(v []string) []any {
	out := make([]any, len(v))
	for i, x := range v {
		out[i] = x
	}
	return out
}

func yamlScalar(v any) string {
	switch x := v.(type) {
	case string:
		if needsQuote(x) {
			return "'" + strings.ReplaceAll(x, "'", "''") + "'"
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	}
	return fmt.Sprint(v)
}

// needsQuote reports strings YAML would not read back as the same plain scalar.
func needsQuote(s string) bool {
	if s == "" || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return true
	}
	if strings.ContainsAny(s, "\n\t") || strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	if strings.ContainsRune("!&*|>'\"%@`{}[],#?:-", rune(s[0])) {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return true
	}
	return false
}
