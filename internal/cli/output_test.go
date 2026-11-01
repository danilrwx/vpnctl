package cli

import (
	"errors"
	"testing"
)

func TestExitCodes(t *testing.T) {
	if ExitCode(nil) != 0 {
		t.Error("success must be 0")
	}

	if ExitCode(errors.New("boom")) != ExitError {
		t.Error("a failure must be 1")
	}

	if ExitCode(Guard(errors.New("not pushed"))) != ExitGuard {
		t.Error("a guard refusal must be 3")
	}

	if ExitCode(Usage(errors.New("bad flag"))) != ExitUsage {
		t.Error("a usage error must be 2")
	}

	if ExitCode(ErrSilent) != ExitError {
		t.Error("ErrSilent must be 1")
	}

	if got := Guard(nil); got != nil {
		t.Error("guard of nil must stay nil")
	}
}
