package ops

import (
	"strings"
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

func TestAlertsSet(t *testing.T) {
	st := fixture(t)

	msg, err := AlertsSet(st, "https://ntfy.sh/vpnctl-abc/", "tok")
	if err != nil {
		t.Fatal(err)
	}

	if msg != "alerts: https://ntfy.sh/vpnctl-abc" {
		t.Fatalf("message %q", msg)
	}

	if st.Alerts.Ntfy != "https://ntfy.sh/vpnctl-abc" || st.Secrets.Alerts.Token != "tok" {
		t.Fatalf("state %+v %+v", st.Alerts, st.Secrets.Alerts)
	}
	// a public topic needs no token, and a stale one would only break delivery
	if _, err := AlertsSet(st, "https://ntfy.sh/other", ""); err != nil {
		t.Fatal(err)
	}

	if st.Secrets.Alerts.Token != "" {
		t.Error("the old token survived a new topic")
	}
	// off clears both halves
	if _, err := AlertsSet(st, "", ""); err != nil {
		t.Fatal(err)
	}

	if st.Alerts.Ntfy != "" || st.Secrets.Alerts.Token != "" {
		t.Fatalf("alerts are still on: %+v %+v", st.Alerts, st.Secrets.Alerts)
	}
}

func TestAlertsSetRejects(t *testing.T) {
	st := fixture(t)
	if _, err := AlertsSet(st, "http://ntfy.sh/x", ""); err == nil {
		t.Error("a plain http topic was accepted")
	}

	if _, err := AlertsSet(st, "", "tok"); err == nil {
		t.Error("a token without a topic was accepted")
	}

	if st.Alerts.Ntfy != "" || st.Secrets.Alerts.Token != "" {
		t.Fatal("a rejected call changed the state")
	}
}

func TestAlertsServe(t *testing.T) {
	st := fixture(t)
	st.Versions["ntfy"] = state.Pin{Version: "2.36.0"}

	msg, err := AlertsServe(st, "a", "vpnctl-abc", "")
	if err != nil {
		t.Fatal(err)
	}

	if st.Alerts.Ntfy != "https://ntfy.e.example/vpnctl-abc" || st.Alerts.Serve != "a" {
		t.Fatalf("state %+v", st.Alerts)
	}

	if msg != "alerts: https://ntfy.e.example/vpnctl-abc served by a" {
		t.Fatalf("message %q", msg)
	}

	cases := []struct {
		name  string
		host  string
		topic string
		pin   bool
		want  string
	}{
		{"an unknown host", "ghost", "vpnctl-abc", true, "is not described"},
		{"an exit", "b", "vpnctl-abc", true, "is not an entry"},
		{"a topic with a slash", "a", "vpn/abc", true, "must be 3-64 characters"},
		{"a short topic", "a", "ab", true, "must be 3-64 characters"},
		{"no pin", "a", "vpnctl-abc", false, "no ntfy pin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := fixture(t)

			st.Versions = map[string]state.Pin{}
			if c.pin {
				st.Versions["ntfy"] = state.Pin{Version: "2.36.0"}
			}

			_, err := AlertsServe(st, c.host, c.topic, "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}

			if st.Alerts.Ntfy != "" || st.Alerts.Serve != "" {
				t.Fatal("a rejected call changed the state")
			}
		})
	}
}
