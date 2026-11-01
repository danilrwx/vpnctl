package ops

import (
	"fmt"
	"regexp"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// topicName is what ntfy accepts as a topic: no dots, no slashes, no spaces.
var topicName = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// AlertsSet records where the entry sends notifications. An empty topic turns
// them off. The token belongs to the topic: a new topic takes a new token, so
// an empty token clears the old one.
func AlertsSet(st *state.State, ntfy, token string) (string, error) {
	topic, err := state.CheckNtfy(ntfy)
	if err != nil {
		return "", err
	}
	if topic == "" && token != "" {
		return "", fmt.Errorf("a token without a topic does nothing; drop it or set the topic")
	}
	if topic == "" {
		st.Alerts = state.Alerts{}
		st.Secrets.Alerts = state.AlertsSecret{}
		return "alerts: off", nil
	}
	st.Alerts = state.Alerts{Ntfy: topic}
	st.Secrets.Alerts.Token = token
	if token == "" {
		return fmt.Sprintf("alerts: %s (no token)", topic), nil
	}
	return fmt.Sprintf("alerts: %s", topic), nil
}

// AlertsServe points the probe at a notification server we run ourselves on an
// entry. The public name is derived from the host, so the two cannot drift.
func AlertsServe(st *state.State, host, topic, token string) (string, error) {
	h, ok := st.Hosts[host]
	if !ok {
		return "", fmt.Errorf("host %q is not described", host)
	}
	if h.Role != state.Entry {
		return "", fmt.Errorf("host %q is not an entry: notifications come from the entry", host)
	}
	if h.Domain == "" {
		return "", fmt.Errorf("host %q has no domain: there is nowhere to serve ntfy", host)
	}
	if !topicName.MatchString(topic) {
		return "", fmt.Errorf("topic %q must be 3-64 characters of a-z, A-Z, 0-9, _ or -", topic)
	}
	if _, ok := st.Versions["ntfy"]; !ok {
		return "", fmt.Errorf("versions.yaml has no ntfy pin")
	}
	url := fmt.Sprintf("https://%s/%s", st.NtfyName(host), topic)
	st.Alerts = state.Alerts{Ntfy: url, Serve: host}
	st.Secrets.Alerts.Token = token
	return fmt.Sprintf("alerts: %s served by %s", url, host), nil
}
