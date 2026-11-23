package ops

import (
	"fmt"
	"net/netip"

	"github.com/danilrwx/vpnctl-go/internal/secrets"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

func prepareUserWG(st *state.State, name string, u *state.User, sec *state.UserSecrets) error {
	if !containsStr(u.Protocols, "wg") {
		u.WG, sec.WG = nil, nil
		return nil
	}

	entry, ok := st.EntryOf(*u)

	in := st.Inbounds[entry]["wg"]
	if !ok || in.Port == 0 || !in.IsEnabled() || in.Subnets == nil {
		return fmt.Errorf("user %q: WireGuard inbound unavailable or disabled", name)
	}

	used := map[string]bool{}

	for other, user := range st.Users {
		if own, _ := st.EntryOf(user); own == entry && other != name {
			for _, ip := range user.WG {
				used[ip] = true
			}
		}
	}

	addresses := make(map[string]string, len(u.Modes))

	keys := make(map[string]state.WGKeys, len(u.Modes))
	for _, mode := range u.Modes {
		prefix, err := netip.ParsePrefix(in.Subnets.ForMode(mode))
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() > 30 {
			return fmt.Errorf("entry %q: invalid WireGuard %s subnet", entry, mode)
		}

		ip := u.WG[mode]
		if ip == "" {
			for candidate := prefix.Addr().Next().Next(); prefix.Contains(candidate.Next()); candidate = candidate.Next() {
				if !used[candidate.String()] {
					ip = candidate.String()
					break
				}
			}
		}

		addr, err := netip.ParseAddr(ip)
		if err != nil || !prefix.Contains(addr) || addr == prefix.Addr() || addr == prefix.Addr().Next() || !prefix.Contains(addr.Next()) || used[ip] {
			return fmt.Errorf("user %q: invalid, duplicate or unavailable WireGuard %s address", name, mode)
		}

		key := sec.WG[mode]
		if key.Private == "" {
			key, err = newWGKeys()
			if err != nil {
				return err
			}
		}

		addresses[mode], keys[mode], used[ip] = ip, key, true
	}

	u.WG, sec.WG = addresses, keys

	return nil
}

func newWGKeys() (state.WGKeys, error) {
	priv, pub, err := secrets.AWGKeys()
	if err != nil {
		return state.WGKeys{}, err
	}

	return state.WGKeys{KeyPair: state.KeyPair{Private: priv, Public: pub}, Preshared: secrets.Preshared()}, nil
}
