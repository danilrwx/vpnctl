// Package ops mutates the state. Each operation returns a commit message; the
// caller writes, validates and commits.
package ops

import (
	"fmt"
	"sort"

	"github.com/danilrwx/vpnctl-go/secrets"
	"github.com/danilrwx/vpnctl-go/state"
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
