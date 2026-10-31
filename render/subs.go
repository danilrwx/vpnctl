// Package render turns the state into user-facing links and subscriptions.
package render

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/state"
)

// Email is the engine account name: <user>-<mode>-<protocol>.
func Email(user, mode, proto string) string {
	return user + "-" + mode + "-" + proto
}

// SubURL is the public subscription link served by Caddy on the entry.
func SubURL(st *state.State, entry, user, mode string) (string, error) {
	h, ok := st.Hosts[entry]
	if !ok {
		return "", fmt.Errorf("host %q is not described", entry)
	}
	if h.Domain == "" {
		return "", fmt.Errorf("the entry has no domain")
	}
	sec, ok := st.Secrets.Users[user]
	if !ok {
		return "", fmt.Errorf("no secrets for user %s", user)
	}
	return fmt.Sprintf("https://%s/s/%s/%s", h.Domain, sec.SubID, mode), nil
}

// Links returns one link per protocol enabled for the user on the entry.
func Links(st *state.State, entry, user, mode string) ([]string, error) {
	u, ok := st.Users[user]
	if !ok {
		return nil, fmt.Errorf("user %q is not described", user)
	}
	accs, err := SubAccounts(st, entry, user, u, mode)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range accs {
		switch a.Proto {
		case "reality":
			out = append(out, fmt.Sprintf(
				"vless://%s@%s:%d?encryption=none&flow=xtls-rprx-vision&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp#%s",
				a.UUID, a.Domain, a.Port, a.SNI, a.PBK, a.SID, a.Email))
		case "ws":
			out = append(out, fmt.Sprintf(
				"vless://%s@%s:%d?encryption=none&security=tls&sni=%s&type=ws&host=%s&path=%s#%s",
				a.UUID, a.Domain, a.Port, a.Domain, a.Domain, enc(a.Path), a.Email))
		case "hy2":
			out = append(out, fmt.Sprintf(
				"hysteria2://%s@%s:%d?sni=%s#%s", a.Password, a.Domain, a.Port, a.SNI, a.Email))
		}
	}
	return out, nil
}

// TGLink is the MTProto proxy link of the entry, when the inbound exists.
func TGLink(st *state.State, entry string) (string, bool) {
	h, ok := st.Hosts[entry]
	if !ok || h.Domain == "" {
		return "", false
	}
	i, ok := st.Inbounds[entry]["mtg"]
	if !ok || !i.IsEnabled() {
		return "", false
	}
	sec := st.Secrets.Hosts[entry].MTG
	if sec == "" {
		return "", false
	}
	return fmt.Sprintf("tg://proxy?server=%s&port=%d&secret=%s", h.Domain, i.Port, sec), true
}

// Share is a ready-to-forward message: clients, subscription, direct links,
// the exit check and the Telegram proxy.
func Share(st *state.State, user string) (string, error) {
	u, ok := st.Users[user]
	if !ok {
		return "", fmt.Errorf("user %q is not described", user)
	}
	entry, err := st.Entry()
	if err != nil {
		return "", err
	}
	h := st.Hosts[entry]
	ip := h.IP
	domain := h.Domain
	if domain == "" {
		domain = ip
	}
	mode := "split"
	if len(u.Modes) > 0 {
		mode = u.Modes[0]
	}
	var b strings.Builder
	b.WriteString("VPN\n\n")
	b.WriteString("1) Install a client:\n")
	b.WriteString("   Android       — Karing, Hiddify or v2rayNG\n")
	b.WriteString("   iOS           — Karing or Streisand\n")
	b.WriteString("   Windows/macOS — Karing or Hiddify\n\n")
	if sub, err := SubURL(st, entry, user, mode); err == nil {
		fmt.Fprintf(&b, "2) Add the subscription:\n   %s\n\n", sub)
	}
	if links, err := Links(st, entry, user, mode); err == nil && len(links) > 0 {
		b.WriteString("3) Direct links (Podkop, routers, clients without subscriptions):\n\n")
		for _, l := range links {
			l = strings.ReplaceAll(l, "@"+domain+":", "@"+ip+":")
			if i := strings.IndexByte(l, '#'); i >= 0 {
				l = l[:i]
			}
			b.WriteString(l + "\n\n")
		}
	}
	fmt.Fprintf(&b, "Check: open https://ifconfig.me after enabling.\n")
	if tg, ok := TGLink(st, entry); ok {
		tg = strings.Replace(tg, "server="+domain, "server="+ip, 1)
		b.WriteString("\n---\nTelegram without a VPN client:\n")
		b.WriteString(tg + "\n")
	}
	return b.String(), nil
}
