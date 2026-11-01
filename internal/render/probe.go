package render

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// probeScript is the chain check the entry runs on a timer: 204 through the
// engine's socks inbound, and one notification when it is not.
func probeScript(st *state.State, host string) string {
	notify := ""
	if st.Alerts.Ntfy != "" {
		notify = `
# notification: the topic and its token come from the state, never from the script
if [ -s /etc/vpnctl/alerts.env ]; then
  . /etc/vpnctl/alerts.env
  curl -sS --max-time 10 -o /dev/null \
    -H "Authorization: Bearer ${VPNCTL_ALERTS_TOKEN}" \
    -H "Title: vpnctl ` + host + `" \
    -d "chain probe returned ${code:-no answer}" \
    "${VPNCTL_ALERTS_URL}" || true
fi
`
	}
	return fmt.Sprintf(`#!/bin/sh
# vpnctl · chain probe on %s · every minute by vpnctl-probe.timer
set -u
PROBE_URL="%s"
code=$(curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 --socks5-hostname 127.0.0.1:%d "$PROBE_URL" 2>/dev/null || true)
if [ "$code" = "204" ]; then
  printf 'ok 204\n'
  exit 0
fi
printf 'chain probe: %%s instead of 204\n' "${code:-no answer}"
engine=sing-box
[ -x /usr/local/bin/xray ] && engine=xray
systemctl is-active --quiet "$engine" || printf 'engine %%s is not running\n' "$engine"
%s
exit 1
`, host, ProbeURL, ProbePort, notify)
}

// probeUnit is the oneshot service the timer starts.
func probeUnit(host string) string {
	return fmt.Sprintf(`# vpnctl · %s
[Unit]
Description=vpnctl chain probe (%s)
After=network-online.target sing-box.service xray.service

[Service]
Type=oneshot
ExecStart=/bin/sh /etc/vpnctl/probe.sh
# the probe must not fight an apply for the chain
Nice=10
`, host, host)
}

// probeTimer runs the probe every minute and survives reboots.
func probeTimer(host string) string {
	return fmt.Sprintf(`# vpnctl · %s
[Unit]
Description=vpnctl chain probe timer (%s)

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
AccuracySec=10s
Unit=vpnctl-probe.service

[Install]
WantedBy=timers.target
`, host, host)
}

// alertsEnv is what the script sources: the topic and the token, 0600.
func alertsEnv(st *state.State) string {
	token := ""
	if st.Secrets.Alerts.Token != "" {
		token = st.Secrets.Alerts.Token
	}
	return fmt.Sprintf("VPNCTL_ALERTS_URL=%q\nVPNCTL_ALERTS_TOKEN=%q\n", st.Alerts.Ntfy, token)
}
