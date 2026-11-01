package render

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// expiryFile is the plan of the agent's tick: who stops working when, which
// identities in the engine belong to them, which subscription and which awg
// peer go with them. It lists only accounts that are still alive today.
func expiryFile(st *state.State, entry string, at time.Time) string {
	exp := state.Expiry{Users: map[string]state.ExpiryEntry{}}
	for _, acc := range UsersOf(st, entry, at) {
		if acc.U.Expires == "" {
			continue
		}
		e := state.ExpiryEntry{Expires: acc.U.Expires, SubID: st.Secrets.Users[acc.Name].SubID}
		if acc.U.AWG != nil && containsStr(acc.U.Protocols, "awg") {
			e.AWGIP = acc.U.AWG.IP
		}
		for _, m := range acc.U.Modes {
			for _, p := range acc.U.Protocols {
				if isEngineUser(p) {
					e.Emails = append(e.Emails, st.Email(acc.Name, m, p))
				}
			}
		}
		sort.Strings(e.Emails)
		exp.Users[acc.Name] = e
	}
	var b bytes.Buffer
	b.WriteString("# vpnctl · account expiry, read by vpnctl-agent tick\n")
	if len(exp.Users) == 0 {
		return b.String()
	}
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(exp); err != nil {
		return b.String()
	}
	if err := enc.Close(); err != nil {
		return b.String()
	}
	return b.String()
}

// tickUnit runs the agent's expiry tick every hour on the entry. The agent is
// installed by `host agent-install`, a separate step: without it the timer has
// nothing to run, and exiting 0 is better than a red unit nobody reads.
func tickUnit(st *state.State, host, engine string) string {
	awg := ""
	if _, ok := st.Inbounds[host]["awg"]; ok {
		awg = " --awg /etc/amnezia/amneziawg/awg0.conf"
	}
	return fmt.Sprintf(`# vpnctl · %s
[Unit]
Description=vpnctl expiry tick (%s)
After=network-online.target

[Service]
Type=oneshot
ExecStart=/bin/sh -c '[ -x /usr/local/bin/vpnctl-agent ] || exit 0; exec /usr/local/bin/vpnctl-agent tick --config %s --engine %s%s'
Nice=10
`, host, host, EngineConfigPath(engine), EngineBin(engine), awg)
}

// tickTimer is hourly and after boot: an account that expired while the host was
// down must not survive the next boot.
func tickTimer(host string) string {
	return fmt.Sprintf(`# vpnctl · %s
[Unit]
Description=vpnctl expiry tick timer (%s)

[Timer]
OnBootSec=10min
OnUnitActiveSec=1h
AccuracySec=1min
Unit=vpnctl-tick.service

[Install]
WantedBy=timers.target
`, host, host)
}
