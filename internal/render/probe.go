package render

import (
	"encoding/json"
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// probeScript is the chain check the entry runs on a timer: 204 through the
// engine's socks inbound, and a notification when the answer changes.
//
// One message per transition, then at most one every half hour while the chain
// is still down: a failing minute every minute would be 1440 messages a day and
// the operator would mute the topic.
func probeScript(st *state.State, host, engine string) string {
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
  url=${VPNCTL_ALERTS_LOCAL:-}
  [ -n "$url" ] || url=${VPNCTL_ALERTS_URL:-}
  [ -n "$url" ] || return 0
  set -- -sS --max-time 10 -o /dev/null -H "Title: vpnctl ` + host + `" -H "Priority: $priority" -d "$text"
  [ -n "${VPNCTL_ALERTS_TOKEN:-}" ] && set -- "$@" -H "Authorization: Bearer ${VPNCTL_ALERTS_TOKEN}"
  curl "$@" "$url" || true
}
`
	}

	// mtg accepts a client and can still fail to carry it; its log is the only
	// place that says so, so a warning in the last two minutes is a broken path
	mtgCheck := ""
	if _, ok := st.Inbounds[host]["mtg"]; ok {
		mtgCheck = fmt.Sprintf(`# a restart of the engine closes the socks inbound for a second, and mtg
# rightly complains: that window is a deploy, not a fault
engine_age=$(( $(date +%%s) - $(date -d "$(systemctl show -p ActiveEnterTimestamp --value %s 2>/dev/null)" +%%s 2>/dev/null || echo 0) ))
mtg_warn=0
if [ "$engine_age" -gt 180 ]; then
  mtg_warn=$(journalctl -u mtg --since "-2min" --no-pager 2>/dev/null | grep -c '"level":"warn"' || true)
fi
case ${mtg_warn:-0} in
  0) ;;
  *) printf 'mtg warned %%s times in two minutes\n' "$mtg_warn"
     broken="$broken mtg"
     ;;
esac
`, engine)
	}

	// the host's own fronting name and one Telegram endpoint: the two paths that
	// broke while the chain itself was fine
	ownURL := "https://" + st.Hosts[host].Domain
	telegramURL := "https://pluto.web.telegram.org"

	if st.Hosts[host].Domain == "" {
		ownURL = ""
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
# what the chain is actually used for besides a 204: the fronting name of this
# host and a Telegram endpoint. Both failed silently once — the chain probe was
# green while the Telegram proxy was down for hours.
probe_socks() {
  name=$1
  url=$2
  scode=$(curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 --socks5-hostname 127.0.0.1:%d "$url" 2>/dev/null || true)
  case ${scode:-000} in
    000|"")
      printf 'socks path broken: %%s\n' "$name"
      broken="$broken $name"
      ;;
  esac
}
code=$(curl -sS -o /dev/null -w '%%{http_code}' --max-time 10 --socks5-hostname 127.0.0.1:%d "$PROBE_URL" 2>/dev/null || true)
broken=""
probe_socks own "%s"
probe_socks telegram "%s"
%s
if [ "$code" = "204" ] && [ -z "$broken" ]; then
  printf 'ok 204\n'
  if [ "$was" = "fail" ]; then notify default "chain probe: recovered"; fi
  mkdir -p /var/lib/vpnctl && printf '%%s ok\n' "$now" > "$STATE"
  exit 0
fi
if [ "$code" != "204" ]; then printf 'chain probe: %%s instead of 204\n' "${code:-no answer}"; fi
if [ -n "$broken" ]; then printf 'broken paths:%%s\n' "$broken"; fi
systemctl is-active --quiet %s || printf 'engine %s is not running\n' %s
if [ "$was" != "fail" ] || [ $((now - last)) -ge "$REMIND" ]; then
  notify high "chain probe returned ${code:-no answer}${broken:+; broken:$broken}"
  mkdir -p /var/lib/vpnctl && printf '%%s fail\n' "$now" > "$STATE"
fi
exit 1
`, host, ProbeURL, notify, ProbePort, ProbePort, ownURL, telegramURL, mtgCheck, engine, engine, engine)
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

// alertsEnv is what the script sources: the topic and the token, 0600. When the
// entry runs the server itself, the probe posts over loopback so a broken
// ingress cannot swallow the message about the ingress being broken.
func alertsEnv(st *state.State, host string) string {
	token := st.Secrets.Alerts.Token

	local := ""
	if st.Alerts.Serve == host && st.Alerts.Ntfy != "" {
		local = NtfyLocal(st.Alerts.Ntfy)
	}

	return fmt.Sprintf("VPNCTL_ALERTS_URL=%q\nVPNCTL_ALERTS_LOCAL=%q\nVPNCTL_ALERTS_TOKEN=%q\n",
		st.Alerts.Ntfy, local, token)
}

// probePlanJSON is what the entry measures: one cell per protocol of every link
// that starts here. It is rendered only on the entry, because only the entry
// sits on the near side of those edges.
func probePlanJSON(st *state.State, host string) string {
	plan := state.ProbePlan{Host: host, Links: []state.ProbeLink{}}
	for i := range st.Links {
		l := &st.Links[i]
		if l.From != host {
			continue
		}

		to, ok := st.Hosts[l.To]
		if !ok {
			continue
		}

		pl := state.ProbeLink{From: l.From, To: l.To, Addr: to.IP, Probes: []state.ProbeCell{}}
		for _, p := range l.Protocols {
			c := state.ProbeCell{Protocol: p, Port: 443}
			if inb, ok := st.Inbounds[l.To][p]; ok {
				if inb.Port != 0 {
					c.Port = inb.Port
				}

				if p == "ws" {
					c.Path = pathOr(inb.Path)
				}
			}

			switch p {
			case "reality":
				if inb, ok := st.Inbounds[l.To][p]; ok {
					c.SNI, _, _ = RealityTarget(st, l.To, inb)
				}
			case "hy2", "ws":
				c.SNI = l.To
			}

			pl.Probes = append(pl.Probes, c)
		}

		plan.Links = append(plan.Links, pl)
	}

	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "{}"
	}

	return string(b) + "\n"
}
