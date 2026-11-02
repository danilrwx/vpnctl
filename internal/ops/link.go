package ops

import (
	"fmt"
	"strings"

	"github.com/danilrwx/vpnctl-go/internal/secrets"
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

// LinkAdd appends a link between two hosts. The render turns links into the
// exit chain, so the endpoints have to exist and differ, and the protocol list
// is the transports that can carry the chain.
func LinkAdd(st *state.State, from, to string, protocols []string, active, policy string) (string, error) {
	if from == to {
		return "", fmt.Errorf("a link from %s to itself", from)
	}

	if _, ok := st.Hosts[from]; !ok {
		return "", fmt.Errorf("host %q is not described", from)
	}

	if _, ok := st.Hosts[to]; !ok {
		return "", fmt.Errorf("host %q is not described", to)
	}

	for _, l := range st.Links {
		if l.From == from && l.To == to {
			return "", fmt.Errorf("the link %s->%s already exists", from, to)
		}
	}

	if len(protocols) == 0 {
		return "", fmt.Errorf("link %s->%s: no protocols", from, to)
	}

	for _, p := range protocols {
		if p != "awg" && p != "reality" && p != "hy2" {
			return "", fmt.Errorf("protocol %q: expected awg, reality or hy2", p)
		}
	}

	link := state.Link{From: from, To: to, Protocols: protocols, Active: active, Policy: policy}

	sec, err := linkSecrets(protocols)
	if err != nil {
		return "", err
	}

	st.Secrets.Links[link.Key()] = sec
	st.Links = append(st.Links, link)

	return fmt.Sprintf("links: + %s->%s (%s)", from, to, strings.Join(protocols, ",")), nil
}

// linkSecrets mints the key material each transport on the link needs. The
// command line never had to do this because the render refuses a link without
// it: a new link with an empty secrets entry validates as a broken state.
func linkSecrets(protocols []string) (state.LinkSecrets, error) {
	var out state.LinkSecrets

	for _, p := range protocols {
		switch p {
		case "awg":
			fromPriv, fromPub, err := secrets.AWGKeys()
			if err != nil {
				return out, err
			}

			toPriv, toPub, err := secrets.AWGKeys()
			if err != nil {
				return out, err
			}

			out.AWG = &state.LinkAWG{
				From:      state.KeyPair{Private: fromPriv, Public: fromPub},
				To:        state.KeyPair{Private: toPriv, Public: toPub},
				Preshared: secrets.Preshared(),
			}
		case "reality":
			priv, pub, sid, err := secrets.RealityKeys()
			if err != nil {
				return out, err
			}

			out.Reality = &state.LinkReality{
				UUID: secrets.UUID(),
				Keys: state.RealityKeys{Private: priv, Public: pub, ShortIDs: []string{sid}},
			}
		case "hy2":
			out.Hy2Pass = secrets.Password()
		}
	}

	return out, nil
}
