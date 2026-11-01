package main

import (
	"testing"

	"github.com/danilrwx/vpnctl-go/internal/cli"
)

// TestKnownAcceptsBuiltins: completion, help and version are not in the command
// tree — urfave adds the first two at run time and we add the third — but the
// guard sits before Run and used to reject them with exit 3.
func TestKnownAcceptsBuiltins(t *testing.T) {
	app := cli.App()
	for _, arg := range []string{"help", "completion", "version", "-h", "--help", "apply", "probe"} {
		if !known(app, arg) {
			t.Errorf("%q was rejected", arg)
		}
	}
	for _, arg := range []string{"nope", "comp"} {
		if known(app, arg) {
			t.Errorf("%q was accepted", arg)
		}
	}
}

// TestCompletionBypassesTheGuard: the shell asks about a prefix, so "appl" is a
// legitimate argument and must not be answered with exit 2.
func TestCompletionBypassesTheGuard(t *testing.T) {
	if !wantsCompletion([]string{"vpnctl", "appl", "--generate-shell-completion"}) {
		t.Fatal("a completion request was treated as a typo")
	}
	if wantsCompletion([]string{"vpnctl", "apply"}) {
		t.Fatal("a plain command was taken for a completion request")
	}
}
