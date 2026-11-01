package state

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// ExpiryEntry is one account as the agent sees it: the date it stops working,
// the identities it owns in the engine, the subscription to take down and the
// awg peer to drop.
type ExpiryEntry struct {
	Expires string `yaml:"expires"`
	// SubID is the directory under /var/www/sub that serves the user.
	SubID string `yaml:"sub_id,omitempty"`
	// AWGIP is the peer address in the awg subnet, empty without a peer.
	AWGIP  string   `yaml:"awg_ip,omitempty"`
	Emails []string `yaml:"emails"`
}

// Expiry is the rendered /etc/vpnctl/expiry: the plan of the agent's tick.
type Expiry struct {
	Users map[string]ExpiryEntry `yaml:"users"`
}

// Expired is the one rule about dates in the whole tree: an account works
// through the day before its date and not on the date itself. The renderer, the
// planner and the agent's tick all ask this function, so they cannot disagree.
func Expired(expires string, at time.Time) bool {
	return expires != "" && expires <= at.UTC().Format("2006-01-02")
}

// ParseExpiry reads the rendered expiry file.
func ParseExpiry(b []byte) (Expiry, error) {
	var e Expiry
	if err := yaml.Unmarshal(b, &e); err != nil {
		return Expiry{}, fmt.Errorf("parse the expiry file: %w", err)
	}
	return e, nil
}
