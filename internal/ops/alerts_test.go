package ops

import "testing"

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
