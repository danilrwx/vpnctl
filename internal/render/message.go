package render

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// MessageOptions shapes the user-facing message.
type MessageOptions struct {
	// Lang is the message language: ru or en. Empty and unknown fall back to en.
	Lang string
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

// messageText is the whole message in one language: the body is prose, not labels.
type messageText struct {
	Title        string // VPN · <domain>
	Client       string
	Android      string
	IOS          string
	Desktop      string
	Subscription string
	SubNote      string
	NoDNS        string
	Links        string
	LinksNoDNS   string
	Telegram     string
	Check        string
}

var messageTexts = map[string]messageText{
	"ru": {
		Title:        "VPN · %s",
		Client:       "1) Приложение",
		Android:      "Android        Karing, Hiddify, v2rayNG",
		IOS:          "iOS            Karing, Streisand",
		Desktop:      "Windows/macOS  Karing, Hiddify",
		Subscription: "2) Подписка",
		SubNote:      "добавляется один раз — ключи и узлы обновляются сами",
		NoDNS:        "Адреса указаны по IP: подписка недоступна, устройство без DNS.",
		Links:        "3) Отдельные ссылки — роутеры, Podkop, клиенты без подписок",
		LinksNoDNS:   "2) Отдельные ссылки — роутеры, Podkop, клиенты без подписок",
		Telegram:     "4) Telegram без VPN-клиента",
		Check:        "Проверка: после включения открой https://ifconfig.me — там должен быть не домашний адрес.",
	},
	"en": {
		Title:        "VPN · %s",
		Client:       "1) Client",
		Android:      "Android        Karing, Hiddify, v2rayNG",
		IOS:          "iOS            Karing, Streisand",
		Desktop:      "Windows/macOS  Karing, Hiddify",
		Subscription: "2) Subscription",
		SubNote:      "added once — keys and nodes refresh themselves",
		NoDNS:        "Addresses are given by IP: no subscription, the device has no DNS.",
		Links:        "3) Direct links — routers, Podkop, clients without subscriptions",
		LinksNoDNS:   "2) Direct links — routers, Podkop, clients without subscriptions",
		Telegram:     "4) Telegram without a VPN client",
		Check:        "Check: after enabling, open https://ifconfig.me — it must not be your home address.",
	},
}

// NormalizeLang is the language code of a locale string: ru_RU.UTF-8 → ru, en-US → en.
// Anything unknown becomes en: a message must never come out half-translated.
func NormalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "_-.@"); i >= 0 {
		s = s[:i]
	}
	if _, ok := messageTexts[s]; ok {
		return s
	}
	return "en"
}

// Message is the ready-to-forward text for a user: client, subscription, direct
// links and the Telegram proxy. The domain is used everywhere unless IP is set.
func Message(st *state.State, user string, opt MessageOptions) (string, error) {
	t := messageTexts[NormalizeLang(opt.Lang)]
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
	b.WriteString(bold(fmt.Sprintf(t.Title, domain)))
	b.WriteString("\n\n")

	b.WriteString(bold(t.Client) + "\n")
	b.WriteString("   " + t.Android + "\n")
	b.WriteString("   " + t.IOS + "\n")
	b.WriteString("   " + t.Desktop + "\n\n")

	if opt.IP {
		b.WriteString(t.NoDNS + "\n\n")
	} else {
		b.WriteString(bold(t.Subscription) + "\n")
		for _, m := range modes {
			sub, err := SubURL(st, entry, user, m)
			if err != nil {
				continue
			}
			fmt.Fprintf(&b, "   %-6s%s\n", m, code(sub))
		}
		b.WriteString("   " + t.SubNote + "\n\n")
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
			title := t.Links
			if opt.IP {
				title = t.LinksNoDNS
			}
			b.WriteString(bold(title) + "\n")
			b.WriteString(strings.Join(lines, "\n"))
			b.WriteString("\n")
		}
	}

	if !opt.NoTG {
		if tg, ok := TGLink(st, entry); ok {
			b.WriteString(bold(t.Telegram) + "\n")
			fmt.Fprintf(&b, "   %s\n", code(withHost(tg, domain, host)))
		}
	}

	if !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	b.WriteString(t.Check + "\n")
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
