package render

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

const WGConfig = "/etc/wireguard/wg0.conf"

// Keep the IPv4 address bits so the two mode subnets map to distinct ULA prefixes.
func wgIPv6(ip netip.Addr) netip.Addr {
	address := [16]byte{0xfd, 0x70, 0xbf, 0x8f, 0x6b, 0x9e, 0, 1}
	v4 := ip.As4()
	copy(address[12:], v4[:])

	return netip.AddrFrom16(address)
}

func wgIPv6Subnet(subnet string) string {
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil || !prefix.Addr().Is4() {
		return ""
	}

	return netip.PrefixFrom(wgIPv6(prefix.Addr()), 96+prefix.Bits()).String()
}

// WGFilename stays within WireGuard's 15-character interface name limit.
func WGFilename(user, mode string) string {
	sum := sha256.Sum256([]byte(user))
	return fmt.Sprintf("wg-%x-%s.conf", sum[:3], mode)
}

func WGClient(st *state.State, entry, user, mode string, at time.Time) (string, error) {
	u := st.Users[user]
	own, _ := st.EntryOf(u)

	in := st.Inbounds[entry]["wg"]
	if own != entry || !u.IsEnabled() || state.Expired(u.Expires, at) || !containsStr(u.Protocols, "wg") || !containsStr(u.Modes, mode) || !in.IsEnabled() || in.Subnets == nil {
		return "", fmt.Errorf("user %q has no enabled WireGuard %s profile", user, mode)
	}

	subnet, err := netip.ParsePrefix(in.Subnets.ForMode(mode))

	ip, ipErr := netip.ParseAddr(u.WG[mode])
	if err != nil || ipErr != nil || !ip.Is4() || !subnet.Contains(ip) || !usableAWGAddr(ip, subnet) {
		return "", fmt.Errorf("user %q has an invalid WireGuard %s address", user, mode)
	}

	host, err := st.Host(entry)
	if err != nil {
		return "", err
	}

	server := host.Domain
	if server == "" {
		server = host.IP
	}

	keys := st.Secrets.Users[user].WG[mode]

	hostKey := st.Secrets.Hosts[entry].WG
	if keys.Private == "" || keys.Preshared == "" || hostKey == nil || hostKey.Public == "" || server == "" || in.Port < 1 || in.Port > 65535 {
		return "", fmt.Errorf("incomplete WireGuard profile for %q on %q", user, entry)
	}

	return fmt.Sprintf(`# vpnctl · %s · mode %s · entry %s
[Interface]
Address = %s/32, %s/128
PrivateKey = %s
DNS = %s

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = %s
PersistentKeepalive = 25
`, user, mode, entry, ip, wgIPv6(ip), keys.Private, Gateway(subnet.String()), hostKey.Public, keys.Preshared, net.JoinHostPort(server, strconv.Itoa(in.Port))), nil
}

func wireGuardServer(st *state.State, entry string, at time.Time, in state.Inbound) (string, error) {
	keys := st.Secrets.Hosts[entry].WG
	if in.Subnets == nil || keys == nil || keys.Private == "" {
		return "", fmt.Errorf("entry %q has incomplete WireGuard settings", entry)
	}

	addresses := []string{}

	for _, subnet := range []string{in.Subnets.Split, in.Subnets.Full} {
		gateway, err := netip.ParseAddr(Gateway(subnet))
		if err != nil || !gateway.Is4() {
			return "", fmt.Errorf("entry %q has an invalid WireGuard subnet", entry)
		}

		bits := prefixLen(subnet)
		addresses = append(addresses, netip.PrefixFrom(gateway, bits).String(), netip.PrefixFrom(wgIPv6(gateway), 96+bits).String())
	}

	var peers []map[string]any

	for _, acc := range UsersOf(st, entry, at) {
		if !containsStr(acc.U.Protocols, "wg") {
			continue
		}

		for _, mode := range acc.U.Modes {
			key := st.Secrets.Users[acc.Name].WG[mode]

			ip, err := netip.ParseAddr(acc.U.WG[mode])
			if key.Public == "" || key.Preshared == "" || err != nil || !ip.Is4() {
				return "", fmt.Errorf("incomplete WireGuard peer %q in mode %q", acc.Name, mode)
			}

			peers = append(peers, obj("name", acc.Name+" "+mode, "public_key", key.Public, "preshared", key.Preshared, "allowed_ips", []string{ip.String() + "/32", wgIPv6(ip).String() + "/128"}))
		}
	}

	// Keep WG's policy routes independent of awg0 so either service can restart.
	return Template(st, "awg.conf.j2", obj(
		"host", entry, "iface", "wg0", "purpose", "WireGuard clients", "plain", true,
		"addresses", addresses, "private_key", keys.Private, "listen_port", in.Port,
		"table_off", false, "peers", peers,
		"post_up", []string{"ip rule add fwmark 2 lookup 109", "ip route add local 0.0.0.0/0 dev lo table 109", "ip -6 rule add fwmark 2 lookup 109", "ip -6 route add local ::/0 dev lo table 109"},
		"post_down", []string{"ip rule del fwmark 2 lookup 109", "ip route del local 0.0.0.0/0 dev lo table 109", "ip -6 rule del fwmark 2 lookup 109 2>/dev/null || true", "ip -6 route del local ::/0 dev lo table 109 2>/dev/null || true"},
	))
}
