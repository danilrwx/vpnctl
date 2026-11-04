// Package ops mutates the state. Each operation returns a commit message; the
// caller writes, validates and commits.
package ops

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/danilrwx/vpnctl-go/internal/secrets"
	"github.com/danilrwx/vpnctl-go/internal/state"
)

// UserAdd creates a user with fresh secrets and returns the commit message.
func UserAdd(st *state.State, name string, modes, protocols []string, tg bool) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is empty")
	}

	if _, ok := st.Users[name]; ok {
		return "", fmt.Errorf("user %q already exists", name)
	}

	if len(modes) == 0 {
		return "", fmt.Errorf("user %q: no modes", name)
	}

	if len(protocols) == 0 {
		return "", fmt.Errorf("user %q: no protocols", name)
	}

	u := state.User{Modes: modes, Protocols: protocols, TG: tg}

	sec := state.UserSecrets{
		UUID:    secrets.UUID(),
		Hy2Pass: secrets.Password(),
		SubID:   secrets.SubID(),
	}
	if err := prepareUserAWG(st, name, &u, &sec); err != nil {
		return "", err
	}

	storeUser(st, name, u, sec)

	return fmt.Sprintf("users: + %s (%s)", name, modes[0]), nil
}

// UserRm removes a user and its secrets.
func UserRm(st *state.State, name string) (string, error) {
	if _, ok := st.Users[name]; !ok {
		return "", fmt.Errorf("user %q is not described", name)
	}

	delete(st.Users, name)
	delete(st.Secrets.Users, name)

	return fmt.Sprintf("users: - %s", name), nil
}

// SortedUsers returns user names in a stable order.
func SortedUsers(st *state.State) []string {
	out := make([]string, 0, len(st.Users))
	for n := range st.Users {
		out = append(out, n)
	}

	sort.Strings(out)

	return out
}

// UserPatch describes what to change in UserEdit; nil fields stay as they are.
type UserPatch struct {
	Modes     []string
	Protocols []string
	TG        *bool
	Expires   *string
	Note      *string
}

// UserEdit applies the patch and returns the commit message.
func UserEdit(st *state.State, name string, p UserPatch) (string, error) {
	u, ok := st.Users[name]
	if !ok {
		return "", fmt.Errorf("user %q is not described", name)
	}

	var what []string

	if p.Modes != nil {
		if len(p.Modes) == 0 {
			return "", fmt.Errorf("user %q: no modes", name)
		}

		u.Modes = p.Modes

		what = append(what, "modes")
	}

	if p.Protocols != nil {
		if len(p.Protocols) == 0 {
			return "", fmt.Errorf("user %q: no protocols", name)
		}

		u.Protocols = p.Protocols

		what = append(what, "protocols")
	}

	if p.TG != nil {
		u.TG = *p.TG

		what = append(what, "tg")
	}

	if p.Expires != nil {
		u.Expires = *p.Expires

		what = append(what, "expiry")
	}

	if p.Note != nil {
		u.Note = *p.Note

		what = append(what, "note")
	}

	if len(what) == 0 {
		return "", fmt.Errorf("nothing to change")
	}

	sec := st.Secrets.Users[name]
	if err := prepareUserAWG(st, name, &u, &sec); err != nil {
		return "", err
	}

	storeUser(st, name, u, sec)

	return fmt.Sprintf("users: %s: %s", name, strings.Join(what, ", ")), nil
}

func storeUser(st *state.State, name string, u state.User, sec state.UserSecrets) {
	if st.Users == nil {
		st.Users = map[string]state.User{}
	}

	if st.Secrets.Users == nil {
		st.Secrets.Users = map[string]state.UserSecrets{}
	}

	st.Users[name] = u
	st.Secrets.Users[name] = sec
}

// prepareUserAWG only changes local copies. Callers publish both together after
// every check and key generation succeeds, including when replacing a peer.
func prepareUserAWG(st *state.State, name string, u *state.User, sec *state.UserSecrets) error {
	if len(u.Modes) == 0 {
		return fmt.Errorf("user %q: no modes", name)
	}

	for _, mode := range u.Modes {
		if mode != "split" && mode != "full" {
			return fmt.Errorf("user %q: invalid mode %q", name, mode)
		}
	}

	if !containsStr(u.Protocols, "awg") {
		u.AWG = nil
		sec.AWG = nil
		sec.AWGPreshared = ""

		return nil
	}

	entry, ok := st.EntryOf(*u)
	if !ok {
		return fmt.Errorf("user %q: cannot select AWG entry", name)
	}

	h, ok := st.Hosts[entry]
	if !ok || h.Role != state.Entry || (h.Engine != "" && h.Engine != "sing-box") {
		return fmt.Errorf("entry %q: AWG requires a sing-box entry", entry)
	}

	i, ok := st.Inbounds[entry]["awg"]
	if !ok || !i.IsEnabled() || i.Port == 0 {
		return fmt.Errorf("entry %q: AWG inbound unavailable or disabled", entry)
	}

	if i.Subnets == nil {
		return fmt.Errorf("entry %q: AWG subnets missing", entry)
	}

	mode := u.Modes[0]
	if u.AWG != nil && containsStr(u.Modes, u.AWG.Mode) {
		mode = u.AWG.Mode
	}

	prefix, err := netip.ParsePrefix(i.Subnets.ForMode(mode))
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
		return fmt.Errorf("entry %q: invalid AWG %s subnet", entry, mode)
	}
	// The first two addresses are the network and gateway. The final
	// address is broadcast, even for tiny prefixes with no usable peers.
	used := map[netip.Addr]bool{}

	for other, user := range st.Users {
		if other != name && user.AWG != nil {
			ip, err := netip.ParseAddr(user.AWG.IP)
			if err == nil {
				used[ip] = true
			}
		}
	}

	usable := func(ip netip.Addr) bool {
		return ip.IsValid() && prefix.Contains(ip) && ip != prefix.Addr() && ip != prefix.Addr().Next() && prefix.Contains(ip.Next()) && !used[ip]
	}
	if u.AWG == nil || u.AWG.Mode != mode {
		var chosen netip.Addr

		for ip := prefix.Addr().Next().Next(); ip.IsValid() && prefix.Contains(ip) && prefix.Contains(ip.Next()); ip = ip.Next() {
			if usable(ip) {
				chosen = ip
				break
			}
		}

		if !chosen.IsValid() {
			return fmt.Errorf("entry %q: AWG %s subnet exhausted", entry, mode)
		}

		u.AWG = &state.Peer{Mode: mode, IP: chosen.String()}
	} else if ip, err := netip.ParseAddr(u.AWG.IP); err != nil || !usable(ip) {
		return fmt.Errorf("user %q: invalid or duplicate AWG peer address", name)
	}

	if sec.AWG == nil {
		priv, pub, err := secrets.AWGKeys()
		if err != nil {
			return err
		}

		sec.AWG = &state.KeyPair{Private: priv, Public: pub}
	}

	if sec.AWGPreshared == "" {
		sec.AWGPreshared = secrets.Preshared()
	}

	return nil
}

// UserRotate gives a user fresh credentials: a leaked link stops working after
// the next apply. Modes and protocols stay as they are.
func UserRotate(st *state.State, name string) (string, error) {
	u, ok := st.Users[name]
	if !ok {
		return "", fmt.Errorf("user %q is not described", name)
	}

	sec := st.Secrets.Users[name]
	sec.UUID = secrets.UUID()
	sec.Hy2Pass = secrets.Password()
	sec.SubID = secrets.SubID()
	st.Secrets.Users[name] = sec

	if u.AWG != nil {
		priv, pub, err := secrets.AWGKeys()
		if err != nil {
			return "", err
		}

		sec.AWG = &state.KeyPair{Private: priv, Public: pub}
		sec.AWGPreshared = secrets.Preshared()
		st.Secrets.Users[name] = sec
	}

	return fmt.Sprintf("users: %s: rotate credentials", name), nil
}

// UserRenew extends a user's term. The count starts from the later of today and
// the current date, so renewing early never shortens anything, and an account
// that already expired starts again from today. Renewing does not enable a user
// somebody turned off on purpose: the date and the switch are separate.
func UserRenew(st *state.State, name string, days int, until string, now time.Time) (string, error) {
	u, ok := st.Users[name]
	if !ok {
		return "", fmt.Errorf("user %q is not described", name)
	}

	today := now.UTC().Truncate(24 * time.Hour)
	base := today

	if u.Expires != "" {
		cur, err := time.Parse("2006-01-02", u.Expires)
		if err != nil {
			return "", fmt.Errorf("user %q: expiry %q is not YYYY-MM-DD", name, u.Expires)
		}

		if cur.After(base) {
			base = cur
		}
	}

	var next time.Time

	switch {
	case until != "" && days != 0:
		return "", fmt.Errorf("either --days or --until, not both")
	case until != "":
		d, err := time.Parse("2006-01-02", until)
		if err != nil {
			return "", fmt.Errorf("--until %q is not YYYY-MM-DD", until)
		}

		if !d.After(today) {
			return "", fmt.Errorf("--until %s is not after today %s", until, today.Format("2006-01-02"))
		}

		next = d
	case days > 0:
		next = base.AddDate(0, 0, days)
	default:
		return "", fmt.Errorf("--days or --until is required")
	}

	u.Expires = next.Format("2006-01-02")
	st.Users[name] = u

	return fmt.Sprintf("users: %s: renew to %s", name, u.Expires), nil
}
