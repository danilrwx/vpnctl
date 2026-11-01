set -e
mkdir -p /var/lib/vpnctl/backup/abc1234
cp --parents -a '/etc/sing-box/config.json' /var/lib/vpnctl/backup/abc1234; install -D -m 0600 '/tmp/.vpnctl/etc/sing-box/config.json' '/etc/sing-box/config.json'
cp --parents -a '/etc/nftables.conf' /var/lib/vpnctl/backup/abc1234; install -D -m 0644 '/tmp/.vpnctl/etc/nftables.conf' '/etc/nftables.conf'
cp --parents -a '/etc/sysctl.d/90-vpnctl.conf' /var/lib/vpnctl/backup/abc1234; install -D -m 0644 '/tmp/.vpnctl/etc/sysctl.d/90-vpnctl.conf' '/etc/sysctl.d/90-vpnctl.conf'
echo '/etc/caddy/Caddyfile' >> /var/lib/vpnctl/backup/abc1234/.new; install -D -m 0644 '/tmp/.vpnctl/etc/caddy/Caddyfile' '/etc/caddy/Caddyfile'
cp --parents -a '/etc/sing-box/rules/old.json' /var/lib/vpnctl/backup/abc1234 2>/dev/null || true; rm -f '/etc/sing-box/rules/old.json'
cp --parents -a '/etc/systemd/system/mtg.service' /var/lib/vpnctl/backup/abc1234; install -D -m 0644 '/tmp/.vpnctl/etc/systemd/system/mtg.service' '/etc/systemd/system/mtg.service'
systemctl daemon-reload
sysctl --system >/dev/null
nft -f /etc/nftables.conf
systemctl enable -q caddy; systemctl reload caddy || systemctl restart caddy
systemctl enable -q sing-box; systemctl restart sing-box
systemctl enable -q mtg; systemctl restart mtg
rm -rf /tmp/.vpnctl
echo ok
