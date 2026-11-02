package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/internal/gitsync"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// openFixture copies the shared fixture and fills its secrets with stubs, the
// way the CLI does without the age key. The app is the interface the phone will
// use, so it is tested as plain Go even though it is built with gomobile.
func openFixture(t *testing.T) *App {
	t.Helper()

	src := filepath.Join("..", "testdata", "basic")
	dir := t.TempDir()

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, sub := range []string{"routing", "sets", "templates"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}

		if ents, err := os.ReadDir(filepath.Join(src, sub)); err == nil {
			for _, e := range ents {
				b, err := os.ReadFile(filepath.Join(src, sub, e.Name()))
				if err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(filepath.Join(dir, sub, e.Name()), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// the fixture ships plain-text secrets under the sops name: load them the
	// way the CLI's --stub does, without touching the encrypted file
	b, err := os.ReadFile(filepath.Join(dir, "secrets.sops.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var sec state.Secrets
	if err := yaml.Unmarshal(b, &sec); err != nil {
		t.Fatal(err)
	}

	app, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	app.st.Secrets = sec

	return app
}

// TestUsersAndShare is the surface the phone calls first: a list of users and
// the one message a person forwards.
func TestUsersAndShare(t *testing.T) {
	app := openFixture(t)

	// the list crosses the gomobile boundary as JSON: a struct with a slice
	// field is not bindable, and gobind drops such a method without a word
	var users []UserInfo
	if err := json.Unmarshal([]byte(app.Users()), &users); err != nil {
		t.Fatalf("users are not JSON: %v", err)
	}

	if len(users) == 0 {
		t.Fatal("no users")
	}

	var brat bool

	for _, u := range users {
		if u.Name == "brat" {
			brat = u.Enabled
		}
	}

	if !brat {
		t.Fatal("brat is enabled in the fixture but not in the list")
	}

	share, err := app.Share("brat")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(share, "vless://") {
		t.Fatalf("the shared text has no link:\n%s", share)
	}

	if _, err := app.Share("ghost"); err == nil {
		t.Fatal("sharing for an unknown user was accepted")
	}
}

// TestMessageLanguages: the message is user-facing text and must exist in both
// languages the app offers.
func TestMessageLanguages(t *testing.T) {
	app := openFixture(t)
	for _, lang := range []string{"ru", "en"} {
		msg, err := app.Message("brat", lang, false)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}

		if strings.TrimSpace(msg) == "" {
			t.Fatalf("%s: an empty message", lang)
		}
	}
}

// TestPlanRejectsBrokenState: the app is not allowed to plan a state the CLI
// would refuse.
func TestPlanRejectsBrokenState(t *testing.T) {
	app := openFixture(t)

	app.st.Users["brat"] = state.User{Modes: []string{"weird"}, Protocols: []string{"reality"}}
	if _, err := app.Plan(""); err == nil {
		t.Fatal("plan accepted an invalid state")
	}
}

// TestRenewAndDisable: the two actions a phone is most likely to be opened for.
// Both go through save, which validates and writes before anything is shown.
func TestRenewAndDisable(t *testing.T) {
	app := openFixture(t)

	msg, err := app.Renew("olga", 30)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	if !strings.Contains(msg, "renew") {
		t.Fatalf("message %q", msg)
	}

	if app.st.Users["olga"].Expires == "" {
		t.Fatal("no expiry was written")
	}

	if _, err := app.SetEnabled("olga", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	if !app.st.Users["olga"].IsEnabled() {
		t.Fatal("the user is still off")
	}

	if _, err := app.SetEnabled("ghost", false); err == nil {
		t.Fatal("an unknown user was accepted")
	}

	if _, err := app.Renew("ghost", 30); err == nil {
		t.Fatal("renewing an unknown user was accepted")
	}
}

// TestHeadIsTheShortCommit: the user card's footer names the revision on screen,
// so the value has to be the short hash git shows. A state that is not a
// repository has nothing to name, and the footer falls back to a dash.
func TestHeadIsTheShortCommit(t *testing.T) {
	dir := t.TempDir()

	repo, err := gitsync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Commit writes nothing for a clean tree, so the repository needs a file
	if err := os.WriteFile(filepath.Join(dir, "users.yaml"), []byte("users: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("state"); err != nil {
		t.Fatal(err)
	}

	app := &App{repo: repo}

	head := app.Head()
	if len(head) != 7 {
		t.Fatalf("head %q is not a short hash", head)
	}

	empty := &App{}
	if empty.Head() != "" {
		t.Fatalf("a state without a repository named a commit: %q", empty.Head())
	}
}

// TestAddUserExpiry: the form offers none, 30 days, 90 days or a date. The date
// lands in users.yaml with the user, and a malformed one creates nothing.
func TestAddUserExpiry(t *testing.T) {
	app := openFixture(t)

	msg, err := app.AddUser("newbie", "split", "reality", false, "2026-12-01")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(msg, "users: + newbie") {
		t.Fatalf("unexpected message %q", msg)
	}

	if got := app.st.Users["newbie"].Expires; got != "2026-12-01" {
		t.Fatalf("expiry %q", got)
	}

	if _, err := app.AddUser("ghost", "split", "reality", false, "01.12.2026"); err == nil {
		t.Fatal("a date in the wrong format was accepted")
	}

	if _, ok := app.st.Users["ghost"]; ok {
		t.Fatal("a rejected date still created the user")
	}
}

// TestRoutingDescribesRules: the screen names what a rule matches without
// inventing an evaluator; the set expansion is the state's own data.
func TestRoutingDescribesRules(t *testing.T) {
	app := openFixture(t)

	app.st.Routing = map[string]state.Routing{
		"a": {
			Exit: state.ExitPolicy{Policy: "fixed", Prefer: "b"},
			DNS:  state.DNS{Direct: []string{"1.1.1.1"}, Remote: "https://dns"},
			Groups: state.Groups{
				All: []state.Rule{{Match: state.Match{Sets: []string{"ads"}}, Action: state.Action{Kind: state.ActionBlock}, Note: "ads"}},
				Split: []state.Rule{
					{Match: state.Match{Sets: []string{"ru"}}, Action: state.Action{Kind: state.ActionDirect}},
					{Action: state.Action{Kind: state.ActionLink, Link: "auto"}, Note: "the rest"},
				},
			},
		},
	}
	app.st.Sets["ru"] = state.Set{Geosite: []string{"category-ru"}, Domains: []string{"gosuslugi.ru"}}

	var hosts []RouteHost
	if err := json.Unmarshal([]byte(app.Routing()), &hosts); err != nil {
		t.Fatal(err)
	}

	if len(hosts) != 1 || hosts[0].Name != "a" {
		t.Fatalf("hosts: %+v", hosts)
	}

	if hosts[0].Exit != "fixed · prefer b" {
		t.Fatalf("exit %q", hosts[0].Exit)
	}

	split := hosts[0].Groups[1].Rules
	if len(split) != 2 || split[0].Title != "set ru" || split[0].Action != "direct" {
		t.Fatalf("split rules: %+v", split)
	}

	if !strings.Contains(split[0].Detail, "category-ru") || !strings.Contains(split[0].Detail, "1 own") {
		t.Fatalf("detail %q", split[0].Detail)
	}

	if split[1].Title != "everything else" || split[1].Action != "→ link auto" {
		t.Fatalf("catch-all: %+v", split[1])
	}

	if hosts[0].Groups[0].Rules[0].Action != "block" {
		t.Fatalf("all rules: %+v", hosts[0].Groups[0].Rules)
	}
}

// TestAddHostAndLink: the phone creates a host the way the command line does —
// files, keys, routing — and then a link to it. Bootstrap is deliberately not
// part of this: it needs a host that answers.
func TestAddHostAndLink(t *testing.T) {
	app := openFixture(t)

	// an exit that carries a link needs the transports that link names
	msg, err := app.AddHost("nl-ams-1", "exit", "root@203.0.113.9", "203.0.113.9", "", "ru-msk-1", "sing-box", true)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "nl-ams-1") {
		t.Fatalf("message %q", msg)
	}

	if _, ok := app.st.Hosts["nl-ams-1"]; !ok {
		t.Fatal("the host is not in the state")
	}

	if _, ok := app.st.Secrets.Hosts["nl-ams-1"]; !ok {
		t.Fatal("the host has no secrets")
	}

	if _, err := app.AddHost("nl-ams-1", "exit", "root@203.0.113.9", "", "", "", "", false); err == nil {
		t.Fatal("a duplicate host was accepted")
	}

	if _, err := app.AddHost("bad", "router", "root@203.0.113.10", "", "", "", "", false); err == nil {
		t.Fatal("an unknown role was accepted")
	}

	linkMsg, err := app.AddLink("ru-msk-1", "nl-ams-1", "reality", "reality", "auto")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(linkMsg, "nl-ams-1") {
		t.Fatalf("link message %q", linkMsg)
	}

	if _, err := app.AddLink("ru-msk-1", "nl-ams-1", "awg", "", ""); err == nil {
		t.Fatal("a duplicate link was accepted")
	}

	if _, err := app.AddLink("ru-msk-1", "ghost", "awg", "", ""); err == nil {
		t.Fatal("a link to an unknown host was accepted")
	}
}

// TestAlertsSetup: notifications can be pointed at the entry's own ntfy from the
// phone, and turned off again. The topic lives in alerts.yaml, the token in the
// secrets file.
func TestAlertsSetup(t *testing.T) {
	app := openFixture(t)

	app.st.Versions["ntfy"] = state.Pin{Version: "2.11.0"}

	msg, err := app.SetAlerts("ru-msk-1", "vpnctl-test")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "vpnctl-test") {
		t.Fatalf("message %q", msg)
	}

	if !strings.Contains(app.st.Alerts.Ntfy, "vpnctl-test") {
		t.Fatalf("alerts url %q", app.st.Alerts.Ntfy)
	}

	if app.st.Alerts.Serve != "ru-msk-1" {
		t.Fatalf("serve %q", app.st.Alerts.Serve)
	}

	if _, err := app.SetAlerts("fi-hel-1", "vpnctl-test"); err == nil {
		t.Fatal("a non-entry host was accepted as the notification server")
	}

	if msg, err := app.SetAlerts("", ""); err != nil || !strings.Contains(msg, "off") {
		t.Fatalf("turning alerts off: %q %v", msg, err)
	}

	if app.st.Alerts.Ntfy != "" || app.st.Alerts.Serve != "" {
		t.Fatalf("alerts stayed: %+v", app.st.Alerts)
	}
}

// TestSetLinkEditsTransports: the link edit dialog sends the whole protocol set;
// the wrapper turns it into add/remove operations.
func TestSetLinkEditsTransports(t *testing.T) {
	app := openFixture(t)

	msg, err := app.SetLink("ru-msk-1", "fi-hel-1", "hy2", "reality", "hy2", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "-reality") || !strings.Contains(msg, "active hy2") {
		t.Fatalf("message %q", msg)
	}

	if _, err := app.SetLink("ru-msk-1", "fi-hel-1", "", "reality", "", ""); err == nil {
		t.Fatal("removing a protocol that is gone was reported as a change")
	}
}

// TestEditHost: the fields an operator can change without touching the host's
// keys and links. A role or a rename is not among them, and an exit cannot grow
// a domain.
func TestEditHost(t *testing.T) {
	app := openFixture(t)

	msg, err := app.EditHost("ru-msk-1", "", "", "new.example.ru", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(msg, "ru-msk-1") {
		t.Fatalf("message %q", msg)
	}

	if app.st.Hosts["ru-msk-1"].Domain != "new.example.ru" {
		t.Fatalf("domain %q", app.st.Hosts["ru-msk-1"].Domain)
	}

	if _, err := app.EditHost("ghost", "", "", "", "", ""); err == nil {
		t.Fatal("an unknown host was edited")
	}

	if _, err := app.EditHost("ru-msk-1", "", "not-an-ip", "", "", ""); err == nil {
		t.Fatal("a bogus address was accepted")
	}

	if _, err := app.EditHost("ru-msk-1", "", "", "", "ghost", ""); err == nil {
		t.Fatal("a via to an unknown host was accepted")
	}

	if _, err := app.EditHost("fi-hel-1", "", "", "example.ru", "", ""); err == nil {
		t.Fatal("an exit was given a domain")
	}

	if _, err := app.EditHost("ru-msk-1", "", "", "", "", "haproxy"); err == nil {
		t.Fatal("an unknown engine was accepted")
	}
}

// TestRouteTestStopsAtWhatItCannotKnow: the phone answers for explicit domains
// and refuses to guess when a rule needs geosite or an address.
func TestRouteTestStopsAtWhatItCannotKnow(t *testing.T) {
	app := openFixture(t)

	app.st.Sets["own"] = state.Set{Domains: []string{"example.ru"}}
	app.st.Sets["geo"] = state.Set{Geosite: []string{"category-ru"}}
	app.st.Routing["ru-msk-1"] = state.Routing{Groups: state.Groups{Split: []state.Rule{
		{Match: state.Match{Sets: []string{"own"}}, Action: state.Action{Kind: state.ActionDirect}},
		{Match: state.Match{GeoIP: []string{"ru"}}, Action: state.Action{Kind: state.ActionDirect}},
		{Action: state.Action{Kind: state.ActionLink, Link: "auto"}},
	}}}

	var answer RouteAnswer
	if err := json.Unmarshal([]byte(app.RouteTest("ru-msk-1", "split", "www.example.ru")), &answer); err != nil {
		t.Fatal(err)
	}

	if answer.Result != "direct" {
		t.Fatalf("result %q, trace %+v", answer.Result, answer.Trace)
	}

	app.st.Routing["ru-msk-1"] = state.Routing{Groups: state.Groups{Split: []state.Rule{
		{Match: state.Match{Sets: []string{"geo"}}, Action: state.Action{Kind: state.ActionDirect}},
		{Action: state.Action{Kind: state.ActionLink, Link: "auto"}},
	}}}

	if err := json.Unmarshal([]byte(app.RouteTest("ru-msk-1", "split", "example.ru")), &answer); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(answer.Result, "cannot decide") {
		t.Fatalf("a geosite rule was decided on the phone: %q", answer.Result)
	}

	// a disabled rule is reported and skipped
	off := false
	app.st.Routing["ru-msk-1"] = state.Routing{Groups: state.Groups{Split: []state.Rule{
		{Match: state.Match{Domains: []string{"example.ru"}}, Action: state.Action{Kind: state.ActionBlock}, Enabled: &off},
		{Action: state.Action{Kind: state.ActionDirect}},
	}}}

	if err := json.Unmarshal([]byte(app.RouteTest("ru-msk-1", "split", "example.ru")), &answer); err != nil {
		t.Fatal(err)
	}

	if answer.Result != "direct" || len(answer.Trace) == 0 || answer.Trace[0].Verdict != "off" {
		t.Fatalf("disabled rule: %q %+v", answer.Result, answer.Trace)
	}
}

// TestCertsAndMetricsNeedTheAgent: both methods sign a request before anything
// reaches a host, so without a signer they must fail instead of dialling.
func TestCertsAndMetricsNeedTheAgent(t *testing.T) {
	app := openFixture(t)

	old := signer
	signer = nil

	t.Cleanup(func() { signer = old })

	if _, err := app.Certs("ru-msk-1"); err == nil {
		t.Fatal("Certs dialled without a signer")
	}

	if _, err := app.Metrics("ru-msk-1"); err == nil {
		t.Fatal("Metrics dialled without a signer")
	}
}

// TestToggleInbound: ws is not an entry's only way in, so it can be turned off;
// reality cannot, because the fixture's link and users depend on it.
func TestToggleInbound(t *testing.T) {
	app := openFixture(t)

	// every fixture user carries both reality and ws, so the switch refuses
	if _, err := app.ToggleInbound("ru-msk-1", "ws", false); err == nil {
		t.Fatal("an inbound in use was turned off")
	}

	if _, err := app.ToggleInbound("ru-msk-1", "reality", false); err == nil {
		t.Fatal("an inbound the users need was turned off")
	}

	if _, err := app.ToggleInbound("ru-msk-1", "quic", true); err == nil {
		t.Fatal("an unknown inbound was toggled")
	}
}

// TestInboundsList: the screen needs the switch state, not only the protocols a
// user could be given; both come from the same map.
func TestInboundsList(t *testing.T) {
	app := openFixture(t)

	var inb []InboundInfo
	if err := json.Unmarshal([]byte(app.Inbounds("ru-msk-1")), &inb); err != nil {
		t.Fatal(err)
	}

	if len(inb) == 0 {
		t.Fatal("no inbounds")
	}

	for i, one := range inb {
		if i > 0 && inb[i-1].Name > one.Name {
			t.Fatalf("not sorted: %+v", inb)
		}
	}

	if got := app.Inbounds("ghost"); got != "[]" {
		t.Fatalf("an unknown host answered %q", got)
	}
}
