package render

import (
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func messageState() *state.State {
	return &state.State{
		Hosts: map[string]state.Host{
			"entry": {Role: state.Entry, IP: "203.0.113.10", Domain: "vpn.example"},
		},
		Users: map[string]state.User{
			"olga": {Modes: []string{"split", "full"}, Protocols: []string{"reality", "ws"}},
		},
		Inbounds: map[string]map[string]state.Inbound{
			"entry": {
				"reality": {Port: 443},
				"ws":      {Port: 8444, Path: "/ws"},
				"mtg":     {Port: 2053},
			},
		},
		Secrets: state.Secrets{
			Users: map[string]state.UserSecrets{
				"olga": {UUID: "uuid-1", Hy2Pass: "hy2-1", SubID: "sub1"},
			},
			Hosts: map[string]state.HostSecrets{
				"entry": {
					Reality: &state.RealityKeys{Public: "PBK", ShortIDs: []string{"SID"}},
					MTG:     "SECRET",
				},
			},
		},
	}
}

func TestMessageUsesDomain(t *testing.T) {
	msg, err := Message(messageState(), "olga", MessageOptions{Lang: "ru"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"VPN · vpn.example",
		"https://vpn.example/s/sub1/split",
		"https://vpn.example/s/sub1/full",
		"@vpn.example:443",
		"sni=vpn.example",
		"tg://proxy?server=vpn.example",
		"reality",
		"ws",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "203.0.113.10") {
		t.Error("the IP leaked into the default message")
	}
}

func TestMessageIPModeKeepsNames(t *testing.T) {
	msg, err := Message(messageState(), "olga", MessageOptions{Lang: "ru", IP: true, Mode: "split"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "@203.0.113.10:443") {
		t.Errorf("the address is not the IP:\n%s", msg)
	}
	// a TLS certificate and the ws virtual host still need the name
	if !strings.Contains(msg, "sni=vpn.example") || !strings.Contains(msg, "host=vpn.example") {
		t.Errorf("sni or host lost the domain:\n%s", msg)
	}
	if strings.Contains(msg, "Подписка") {
		t.Errorf("the subscription needs DNS and must be omitted:\n%s", msg)
	}
	if !strings.Contains(msg, "server=203.0.113.10") {
		t.Errorf("the telegram proxy is not switched to the IP:\n%s", msg)
	}
}

func TestMessageMarkdown(t *testing.T) {
	msg, err := Message(messageState(), "olga", MessageOptions{Lang: "ru", Markdown: true, NoLinks: true, NoTG: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "**VPN · vpn.example**") {
		t.Errorf("no bold title:\n%s", msg)
	}
	if strings.Contains(msg, "reality") {
		t.Errorf("links were not omitted:\n%s", msg)
	}
}

func TestMessageUnknownMode(t *testing.T) {
	if _, err := Message(messageState(), "olga", MessageOptions{Lang: "ru", Mode: "full-only"}); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
}

func TestMessageUnknownUser(t *testing.T) {
	if _, err := Message(messageState(), "ghost", MessageOptions{Lang: "ru"}); err == nil {
		t.Fatal("an unknown user was accepted")
	}
}

func TestMessageLanguage(t *testing.T) {
	msg, err := Message(messageState(), "olga", MessageOptions{Lang: "en", NoLinks: true, NoTG: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1) Client", "2) Subscription", "Check: after enabling"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Приложение") {
		t.Errorf("russian text leaked into the english message:\n%s", msg)
	}
}

func TestNormalizeLang(t *testing.T) {
	cases := map[string]string{
		"ru_RU.UTF-8": "ru",
		"ru-RU":       "ru",
		"en_US.UTF-8": "en",
		"EN":          "en",
		"de_DE.UTF-8": "en",
		"":            "en",
		"  ru  ":      "ru",
	}
	for in, want := range cases {
		if got := NormalizeLang(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
