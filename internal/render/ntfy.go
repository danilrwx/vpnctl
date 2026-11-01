package render

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// NtfyPort is the loopback port of the notification server. It never listens on
// a public address: reality splices every non-VPN TLS client to Caddy, and Caddy
// terminates the certificate for the ntfy name.
const NtfyPort = 2586

// NtfyLocal is the loopback topic URL the probe posts to, so a broken ingress
// cannot silence the alert about the ingress being broken.
func NtfyLocal(topicURL string) string {
	i := strings.Index(topicURL, "://")
	if i < 0 {
		return ""
	}

	rest := topicURL[i+3:]

	j := strings.Index(rest, "/")
	if j < 0 {
		return ""
	}

	return fmt.Sprintf("http://127.0.0.1:%d%s", NtfyPort, rest[j:])
}

// ntfyServer is the config of the notification server on the entry.
func ntfyServer(st *state.State, host string) string {
	return fmt.Sprintf(`# vpnctl · %s · do not edit by hand: overwritten by apply
base-url: "https://%s"
listen-http: "127.0.0.1:%d"
cache-file: "/var/lib/ntfy/cache.db"
auth-file: "/var/lib/ntfy/auth.db"
cache-duration: "72h"
behind-proxy: true
web-root: "disable"
auth-default-access: "read-write"
log-level: "warn"
`, host, st.NtfyName(host), NtfyPort)
}

// ntfyUnit keeps the server alive across reboots with no writable paths other
// than its own state directory.
func ntfyUnit(host string) string {
	return fmt.Sprintf(`# vpnctl · %s
[Unit]
Description=ntfy notification server (%s)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/ntfy serve --config /etc/ntfy/server.yml
DynamicUser=yes
StateDirectory=ntfy
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=yes

[Install]
WantedBy=multi-user.target
`, host, host)
}
