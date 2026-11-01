package cli

import (
	"sort"
	"strings"
	"testing"

	ucli "github.com/urfave/cli/v3"
)

// TestCommandTree pins the surface: a command that disappears by accident
// breaks scripts and agents, so the list is explicit.
func TestCommandTree(t *testing.T) {
	want := []string{"alerts", "apply", "check", "host", "init", "key", "metrics", "plan", "probe", "render", "state", "user", "versions"}
	var got []string
	for _, c := range App().Commands {
		if c.Name == "help" || c.Name == "completion" {
			continue
		}
		got = append(got, c.Name)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("commands changed:\n got: %v\nwant: %v", got, want)
	}
}

// TestEveryCommandActs: a command without an action and without subcommands is
// a dead end in the help output.
func TestEveryCommandActs(t *testing.T) {
	var walk func(c *ucli.Command)
	walk = func(c *ucli.Command) {
		if c.Name == "help" || c.Name == "completion" {
			return
		}
		if c.Action == nil && len(c.Commands) == 0 {
			t.Errorf("%s has neither an action nor subcommands", c.Name)
		}
		for _, sub := range c.Commands {
			walk(sub)
		}
	}
	for _, c := range App().Commands {
		walk(c)
	}
}

// TestJSONEverywhere: section 16 promises a machine-readable answer from every
// command, so every leaf must accept --json.
func TestJSONEverywhere(t *testing.T) {
	var walk func(c *ucli.Command)
	walk = func(c *ucli.Command) {
		if len(c.Commands) > 0 {
			for _, sub := range c.Commands {
				walk(sub)
			}
			return
		}
		for _, f := range c.Flags {
			if names := f.Names(); containsName(names, "json") {
				return
			}
		}
		t.Errorf("%s does not accept --json", c.Name)
	}
	for _, c := range App().Commands {
		walk(c)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
