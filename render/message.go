package render

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/state"
)

// MessageOptions shapes the user-facing message.
type MessageOptions struct {
	// Markdown wraps headings in bold and links in backticks.
	Markdown bool
	// IP replaces the domain with the entry address: routers and clients without DNS.
	IP bool
	// NoLinks omits the direct links block.
	NoLinks bool
	// NoTG omits the Telegram proxy block.
	NoTG bool
	// Mode limits the message to one mode; empty means every mode of the user.
	Mode string
}

// Message is the ready-to-forward text for a user: client, subscription, direct
// links and the Telegram proxy. The domain is used everywhere unless IP is set.
func Message(st *state.State, user string, opt MessageOptions) (string, error) {
	u, ok := st.Users[user]
	if !ok {
		return "", fmt.Errorf("user %q is not described", user)
	}
	entry, err := st.Entry()
	if err != nil {
		return "", err
	}
	h := st.Hosts[entry]
	domain := h.Domain
	if domain == "" {
		domain = h.IP
	}
	host := domain
	if opt.IP {
		host = h.IP
	}
	modes := u.Modes
	if opt.Mode != "" {
		if !containsStr(u.Modes, opt.Mode) {
			return "", fmt.Errorf("user %q has no mode %q", user, opt.Mode)
		}
		modes = []string{opt.Mode}
	}
	if len(modes) == 0 {
		modes = []string{"split"}
	}
	bold := func(s string) string {
		if opt.Markdown {
			return "**" + s + "**"
		}
		return s
	}
	code := func(s string) string {
		if opt.Markdown {
			return "`" + s + "`"
		}
		return s
	}

	var b strings.Builder
	b.WriteString(bold("VPN · " + domain))
	b.WriteString("\n\n")

	b.WriteString(bold("1) Приложение") + "\n")
	b.WriteString("   Android        Karing, Hiddify, v2rayNG\n")
	b.WriteString("   iOS            Karing, Streisand\n")
	b.WriteString("   Windows/macOS  Karing, Hiddify\n\n")

	if opt.IP {
		b.WriteString("Адреса указаны по IP: подписка недоступна, устройство без DNS.\n\n")
	} else {
		b.WriteString(bold("2) Подписка") + "\n")
		for _, m := range modes {
			sub, err := SubURL(st, entry, user, m)
			if err != nil {
				continue
			}
			fmt.Fprintf(&b, "   %-6s%s\n", m, code(sub))
		}
		b.WriteString("   добавляется один раз — ключи и узлы обновляются сами\n\n")
	}

	if !opt.NoLinks {
		var lines []string
		for _, m := range modes {
			links, err := Links(st, entry, user, m)
			if err != nil {
				continue
			}
			for _, l := range links {
				label := protoOf(l)
				if len(modes) > 1 {
					label = m + " " + label
				}
				lines = append(lines, fmt.Sprintf("   %s\n   %s\n", label, code(withHost(l, domain, host))))
			}
		}
		if len(lines) > 0 {
			title := "3) Отдельные ссылки — роутеры, Podkop, клиенты без подписок"
			if opt.IP {
				title = "2) Отдельные ссылки — роутеры, Podkop, клиенты без подписок"
			}
			b.WriteString(bold(title) + "\n")
			b.WriteString(strings.Join(lines, "\n"))
			b.WriteString("\n")
		}
	}

	if !opt.NoTG {
		if tg, ok := TGLink(st, entry); ok {
			tg = withHost(tg, domain, host)
			b.WriteString(bold("4) Telegram без VPN-клиента") + "\n")
			fmt.Fprintf(&b, "   %s\n", code(tg))
		}
	}

	if !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	b.WriteString("Проверка: после включения открой https://ifconfig.me — там должен быть не домашний адрес.\n")
	return b.String(), nil
}

// withHost swaps the domain in a link for another host, keeping the port and path.
func withHost(link, domain, host string) string {
	if domain == host || domain == "" {
		return link
	}
	// only the address changes: a TLS certificate and the ws virtual host still need the name
	link = strings.ReplaceAll(link, "@"+domain+":", "@"+host+":")
	link = strings.ReplaceAll(link, "server="+domain, "server="+host)
	return link
}

// protoOf reads the protocol from the fragment: <user>-<mode>-<proto>.
func protoOf(link string) string {
	i := strings.IndexByte(link, '#')
	if i < 0 {
		return "link"
	}
	parts := strings.Split(link[i+1:], "-")
	if len(parts) < 3 {
		return "link"
	}
	return parts[len(parts)-1]
}
