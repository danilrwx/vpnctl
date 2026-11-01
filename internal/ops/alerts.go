package ops

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

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
	st.Alerts.Ntfy = topic
	st.Secrets.Alerts.Token = token
	if token == "" {
		return fmt.Sprintf("alerts: %s (no token)", topic), nil
	}
	return fmt.Sprintf("alerts: %s", topic), nil
}
