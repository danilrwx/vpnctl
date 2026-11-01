// Package ops mutates the state. Each operation returns a commit message; the
// caller writes, validates and commits.
package ops

import (
	"fmt"
	"sort"
	"strings"

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
	st.Users[name] = state.User{Modes: modes, Protocols: protocols, TG: tg}
	st.Secrets.Users[name] = state.UserSecrets{
		UUID:    secrets.UUID(),
		Hy2Pass: secrets.Password(),
		SubID:   secrets.SubID(),
	}
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
	st.Users[name] = u
	return fmt.Sprintf("users: %s: %s", name, strings.Join(what, ", ")), nil
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
