package state

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// CheckNtfy validates a notification topic and returns it without a trailing
// slash. An empty topic is valid and means notifications are off.
func CheckNtfy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("the notification topic is not a URL: %w", err)
	}
	switch {
	case u.Scheme != "https":
		return "", fmt.Errorf("the notification topic must be https, not %q", u.Scheme)
	case u.Host == "":
		return "", errors.New("the notification topic has no host")
	case u.User != nil:
		return "", errors.New("the notification topic carries credentials — the token belongs in secrets, not in the URL")
	case u.RawQuery != "" || u.Fragment != "":
		return "", errors.New("the notification topic has a query or a fragment, and ntfy would read it as part of the topic")
	case strings.Trim(u.Path, "/") == "":
		return "", errors.New("the notification topic has no name, e.g. https://ntfy.sh/vpnctl-xxxx")
	}
	return strings.TrimRight(raw, "/"), nil
}

// NtfyName is the public name of an entry that serves its own notifications:
// ntfy.<domain>. Empty when the host has no domain.
func (st *State) NtfyName(host string) string {
	h, ok := st.Hosts[host]
	if !ok || h.Domain == "" {
		return ""
	}
	return "ntfy." + h.Domain
}

// SaveAlerts writes alerts.yaml, or removes it when the topic is empty: a file
// that says nothing is noise in the history.
func SaveAlerts(dir string, a Alerts) error {
	path := filepath.Join(dir, "alerts.yaml")
	if a.Ntfy == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := marshal(func() map[string]any {
		m := map[string]any{"ntfy": a.Ntfy}
		if a.Serve != "" {
			m["serve"] = a.Serve
		}
		return m
	}())
	if err != nil {
		return fmt.Errorf("marshal alerts: %w", err)
	}
	return os.WriteFile(path, b, 0o644)
}
