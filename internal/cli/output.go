package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	ucli "github.com/urfave/cli/v3"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

// jsonOut is set by the dispatcher for every command run with --json.
var jsonOut bool

// SetJSON switches the output format for the command about to run.
func SetJSON(on bool) { jsonOut = on }

// JSON reports whether the caller asked for machine-readable output.
func JSON() bool { return jsonOut }

// emit prints v as JSON, or calls text when --json is off.
func emit(v any, text func()) error {
	if !jsonOut {
		text()
		return nil
	}

	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))

	return nil
}

// acceptNewHostKey is set by --accept-host-key: the first contact with a host
// whose key is not in the state yet.
var acceptNewHostKey bool

// SetAcceptNewHostKey switches the first-contact policy for this run.
func SetAcceptNewHostKey(on bool) { acceptNewHostKey = on }

// loadState reads the state and applies the flags that are policy, not data.
func loadState(dir string) (*state.State, error) {
	st, err := state.Load(dir)
	if err != nil {
		return nil, err
	}

	st.AcceptNewHostKey = acceptNewHostKey

	return st, nil
}

// acceptHostKeyFlag is the opt-in for the first contact with a host.
func acceptHostKeyFlag() ucli.Flag {
	return &ucli.BoolFlag{Name: "accept-host-key", Usage: "allow the first contact with a host whose key is not recorded yet"}
}

// Exit codes, as section 16 of the design asks: a guard refusal is not the same
// as a failure, an agent must know to pull or ask a human.
const (
	ExitError = 1
	ExitUsage = 2
	ExitGuard = 3
)

// guardError marks a refusal by a guard rather than a failure.
type guardError struct{ err error }

func (e *guardError) Error() string { return e.err.Error() }
func (e *guardError) Unwrap() error { return e.err }

// usageError marks a bad invocation: section 16 gives it its own exit code.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Usage wraps an argument or flag error.
func Usage(err error) error {
	if err == nil {
		return nil
	}

	return &usageError{err}
}

// Guard wraps a refusal: not pushed, a host ahead of HEAD, drift, agent busy.
func Guard(err error) error {
	if err == nil {
		return nil
	}

	return &guardError{err}
}

// ExitCode is the process exit status for an error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	var u *usageError
	if errors.As(err, &u) {
		return ExitUsage
	}
	// urfave reports flag and command mistakes as plain errors
	if msg := err.Error(); strings.Contains(msg, "flag provided but not defined") ||
		strings.Contains(msg, "unknown command") || strings.Contains(msg, "Incorrect Usage") {
		return ExitUsage
	}

	var g *guardError
	if errors.As(err, &g) {
		return ExitGuard
	}

	if errors.Is(err, ErrSilent) {
		return ExitError
	}

	return ExitError
}

// errorJSON is the shape of a failure under --json.
type errorJSON struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ReportError writes the failure as JSON when the caller asked for it.
func ReportError(err error) {
	if !jsonOut {
		return
	}

	var body errorJSON

	body.Error.Code = "error"

	var u *usageError
	if errors.As(err, &u) {
		body.Error.Code = "usage"
	}

	var g *guardError
	if errors.As(err, &g) {
		body.Error.Code = "guard"
	}

	body.Error.Message = err.Error()
	b, _ := json.MarshalIndent(body, "", "  ")
	fmt.Fprintln(os.Stderr, string(b))
}
