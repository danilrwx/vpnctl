package ops

import (
	"fmt"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// RuleInput is one routing rule as a form produces it: what it matches and what
// it does.
type RuleInput struct {
	Sets    []string
	GeoIP   []string
	Inbound []string
	Domains []string
	IPs     []string
	Action  string
	Note    string
}

// chain returns the rules of one chain and writes them back.
func chain(st *state.State, host, name string) ([]state.Rule, func([]state.Rule), error) {
	r, ok := st.Routing[host]
	if !ok {
		return nil, nil, fmt.Errorf("host %q has no routing", host)
	}

	save := func(rules []state.Rule) {
		switch name {
		case "all":
			r.Groups.All = rules
		case "split":
			r.Groups.Split = rules
		case "full":
			r.Groups.Full = rules
		}

		st.Routing[host] = r
	}

	switch name {
	case "all":
		return r.Groups.All, save, nil
	case "split":
		return r.Groups.Split, save, nil
	case "full":
		return r.Groups.Full, save, nil
	}

	return nil, nil, fmt.Errorf("chain %q: expected all, split or full", name)
}

// RuleAdd appends a rule to the end of a chain. The end is deliberate: the list
// is top-down and first match wins, so a new rule never quietly takes over a
// connection the operator already routed.
func RuleAdd(st *state.State, host, chainName string, in RuleInput) (string, error) {
	rules, save, err := chain(st, host, chainName)
	if err != nil {
		return "", err
	}

	action, err := state.ParseAction(in.Action)
	if err != nil {
		return "", err
	}

	if len(in.Sets) == 0 && len(in.GeoIP) == 0 && len(in.Inbound) == 0 && len(in.Domains) == 0 && len(in.IPs) == 0 {
		return "", fmt.Errorf("a rule with no match is the catch-all: the chain already has its last rule")
	}

	for _, s := range in.Sets {
		if _, ok := st.Sets[s]; !ok {
			return "", fmt.Errorf("set %q is not described", s)
		}
	}

	rules = append(rules, state.Rule{
		Match:  state.Match{Sets: in.Sets, GeoIP: in.GeoIP, Inbound: in.Inbound, Domains: in.Domains, IP: in.IPs},
		Action: action,
		Note:   in.Note,
	})
	save(rules)

	return fmt.Sprintf("routing: %s/%s: + %s", host, chainName, action.String()), nil
}

// RuleRemove drops one rule by its position in the chain.
func RuleRemove(st *state.State, host, chainName string, index int) (string, error) {
	rules, save, err := chain(st, host, chainName)
	if err != nil {
		return "", err
	}

	if index < 0 || index >= len(rules) {
		return "", fmt.Errorf("rule %d: the chain has %d", index, len(rules))
	}

	rules = append(rules[:index], rules[index+1:]...)
	save(rules)

	return fmt.Sprintf("routing: %s/%s: - rule %d", host, chainName, index+1), nil
}

// RuleMove swaps a rule with its neighbour. One step at a time is what a phone
// screen can show; dragging is a desktop gesture.
func RuleMove(st *state.State, host, chainName string, index int, up bool) (string, error) {
	rules, save, err := chain(st, host, chainName)
	if err != nil {
		return "", err
	}

	to := index - 1
	if !up {
		to = index + 1
	}

	if index < 0 || index >= len(rules) || to < 0 || to >= len(rules) {
		return "", fmt.Errorf("rule %d cannot move %s", index+1, map[bool]string{true: "up", false: "down"}[up])
	}

	rules[index], rules[to] = rules[to], rules[index]
	save(rules)

	return fmt.Sprintf("routing: %s/%s: rule %d ⇄ %d", host, chainName, index+1, to+1), nil
}

// RuleToggle enables or disables one rule. A disabled rule stays in place: the
// order is the decision, and deleting it loses that.
func RuleToggle(st *state.State, host, chainName string, index int, on bool) (string, error) {
	rules, save, err := chain(st, host, chainName)
	if err != nil {
		return "", err
	}

	if index < 0 || index >= len(rules) {
		return "", fmt.Errorf("rule %d: the chain has %d", index, len(rules))
	}

	rules[index].Enabled = &on
	save(rules)

	return fmt.Sprintf("routing: %s/%s: rule %d %s", host, chainName, index+1, map[bool]string{true: "on", false: "off"}[on]), nil
}
