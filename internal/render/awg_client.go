package render

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// AWGClient renders a user's enabled AWG profile, with the same overrides as RenderHost.
func AWGClient(st *state.State, entry, user string) (string, error) {
	u, ok := st.Users[user]
	if !ok || !u.IsEnabled() || !containsStr(u.Protocols, "awg") || u.AWG == nil {
		return "", fmt.Errorf("user %q has no enabled AWG profile", user)
	}

	if (u.AWG.Mode != "split" && u.AWG.Mode != "full") || !containsStr(u.Modes, u.AWG.Mode) {
		return "", fmt.Errorf("user %q has an invalid AWG mode", user)
	}

	own, ok := st.EntryOf(u)
	if !ok || own != entry {
		return "", fmt.Errorf("user %q does not belong to entry %q", user, entry)
	}

	h, err := st.Host(entry)
	if err != nil {
		return "", err
	}

	in, ok := st.Inbounds[entry]["awg"]
	if !ok || !in.IsEnabled() || in.Subnets == nil || in.Port <= 0 {
		return "", fmt.Errorf("entry %q has no enabled AWG inbound with subnets", entry)
	}

	subnet, err := netip.ParsePrefix(in.Subnets.ForMode(u.AWG.Mode))
	if err != nil {
		return "", fmt.Errorf("invalid AWG subnet for user %q", user)
	}

	ip, err := netip.ParseAddr(u.AWG.IP)
	if err != nil || !subnet.Contains(ip) {
		return "", fmt.Errorf("invalid AWG address for user %q", user)
	}

	sec := st.Secrets.Users[user]

	host := st.Secrets.Hosts[entry]
	if sec.AWG == nil || sec.AWG.Private == "" || sec.AWG.Public == "" || sec.AWGPreshared == "" || host.AWG == nil || host.AWG.Public == "" {
		return "", fmt.Errorf("missing AWG keys for user %q or entry %q", user, entry)
	}

	domain := h.Domain
	if domain == "" {
		domain = h.IP
	}

	if domain == "" {
		return "", fmt.Errorf("entry %q has no address", entry)
	}

	body, err := Template(st, "awg-client.conf.j2", map[string]any{
		"user": user, "mode": u.AWG.Mode, "domain": domain,
		"address": u.AWG.IP, "private_key": sec.AWG.Private,
		"dns": Gateway(subnet.String()), "server_public_key": host.AWG.Public,
		"preshared": sec.AWGPreshared, "port": in.Port,
	})
	if err != nil {
		return "", err
	}

	override := filepath.Join(st.Dir, "overrides", entry, "etc/vpnctl/profiles", user, "awg.conf")
	if data, err := os.ReadFile(override); err == nil {
		return string(data), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	return body, nil
}
