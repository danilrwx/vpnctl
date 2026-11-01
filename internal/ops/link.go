package ops

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// LinkSet adds or removes link protocols and sets the active one.
// Returns the commit message.
func LinkSet(st *state.State, from, to string, add, rm []string, active, policy string) (string, error) {
	idx := -1

	for i, l := range st.Links {
		if l.From == from && l.To == to {
			idx = i
			break
		}
	}

	if idx < 0 {
		return "", fmt.Errorf("there is no link %s->%s", from, to)
	}

	l := st.Links[idx]

	var changed []string

	for _, p := range rm {
		for i, have := range l.Protocols {
			if have == p {
				l.Protocols = append(l.Protocols[:i], l.Protocols[i+1:]...)
				changed = append(changed, "-"+p)

				break
			}
		}
	}

	for _, p := range add {
		if !containsStr(l.Protocols, p) {
			l.Protocols = append(l.Protocols, p)
			changed = append(changed, "+"+p)
		}
	}

	if active != "" {
		l.Active = active
		changed = append(changed, "active "+active)
	}

	if policy != "" {
		l.Policy = policy
		changed = append(changed, "policy "+policy)
	}

	if len(changed) == 0 {
		return "", fmt.Errorf("nothing to change")
	}

	if len(l.Protocols) == 0 {
		return "", fmt.Errorf("link %s->%s: no protocols left", from, to)
	}

	st.Links[idx] = l

	return fmt.Sprintf("links: %s->%s: %s", from, to, strings.Join(changed, ", ")), nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
}
