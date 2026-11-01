package render

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// probeScript is the chain check the entry runs on a timer: 204 through the
// engine's socks inbound, and a notification when the answer changes.
//
// One message per transition, then at most one every half hour while the chain
// is still down: a failing minute every minute would be 1440 messages a day and
// the operator would mute the topic.
func probeScript(st *state.State, host string) string {
	// notify always exists: the call sites do not care whether a topic is set
	notify := `# notifications are off: alerts.yaml has no topic
notify() { return 0; }
`
	if st.Alerts.Ntfy != "" {
		notify = `# notification: the topic and its token come from the state, never from the script
notify() {
  priority=$1
  text=$2
  [ -s /etc/vpnctl/alerts.env ] || return 0
  . /etc/vpnctl/alerts.env
  [ -n "${VPNCTL_ALERTS_URL:-}" ] || return 0
  set -- -sS --max-time 10 -o /dev/null -H "Title: vpnctl ` + host + `" -H "Priority: $priority" -d "$text"
  [ -n "${VPNCTL_ALERTS_TOKEN:-}" ] && set -- "$@" -H "Authorization: Bearer ${VPNCTL_ALERTS_TOKEN}"
  curl "$@" "$VPNCTL_ALERTS_URL" || true
}
`
	}
	return fmt.Sprintf(`#!/bin/sh
# vpnctl · chain probe on %s · every minute by vpnctl-probe.timer
set -u
PROBE_URL="%s"
STATE=/var/lib/vpnctl/probe.state
REMIND=1800
now=$(date +%%s)
last=0
was=ok
if [ -r "$STATE" ]; then
  read -r last was < "$STATE" || true
fi
case ${last:-} in ''|*[!0-9]*) last=0 ;; esac
was=${was:-ok}
%s
code=$(curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 --socks5-hostname 127.0.0.1:%d "$PROBE_URL" 2>/dev/null || true)
if [ "$code" = "204" ]; then
  printf 'ok 204\n'
  if [ "$was" = "fail" ]; then notify default "chain probe: recovered"; fi
  mkdir -p /var/lib/vpnctl && printf '%%s ok\n' "$now" > "$STATE"
  exit 0
fi
printf 'chain probe: %%s instead of 204\n' "${code:-no answer}"
engine=sing-box
[ -x /usr/local/bin/xray ] && engine=xray
systemctl is-active --quiet "$engine" || printf 'engine %%s is not running\n' "$engine"
if [ "$was" != "fail" ] || [ $((now - last)) -ge "$REMIND" ]; then
  notify high "chain probe returned ${code:-no answer}"
  mkdir -p /var/lib/vpnctl && printf '%%s fail\n' "$now" > "$STATE"
fi
exit 1
`, host, ProbeURL, notify, ProbePort)
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
