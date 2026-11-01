// Package cli is the command surface of vpnctl: every subcommand lives here so
// the same code serves the binary and, later, the tests.
package cli

import "errors"

// ErrSilent means the command already printed its diagnostics: only the exit code is left.
var ErrSilent = errors.New("")
