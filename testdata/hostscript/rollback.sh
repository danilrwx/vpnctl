set +e
if [ -f /var/lib/vpnctl/backup/abc1234/.new ]; then xargs -r rm -f < /var/lib/vpnctl/backup/abc1234/.new; fi
(cd /var/lib/vpnctl/backup/abc1234 && find . -type f ! -name .new | while read f; do install -D -m 0600 "$f" "/${f#./}"; done)
systemctl enable -q caddy; systemctl reload caddy || systemctl restart caddy
nft -f /etc/nftables.conf
systemctl enable -q sing-box; systemctl restart sing-box
sysctl --system >/dev/null
systemctl daemon-reload
