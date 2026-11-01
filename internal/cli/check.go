package cli

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/internal/state"
	"github.com/danilrwx/vpnctl-go/internal/validate"
)

// checkResult is the machine-readable answer of check.
type checkResult struct {
	OK       bool     `json:"ok"`
	Hosts    int      `json:"hosts"`
	Links    int      `json:"links"`
	Users    int      `json:"users"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Check validates the state statically.
func Check(dir string, stub, warnings bool) error {
	st, err := state.Load(dir)
	if err != nil {
		return err
	}
	if stub {
		st.FillStubSecrets()
	}
	warns := []string{}
	if warnings {
		warns = validate.Warnings(st)
	}
	errs := validate.State(st)
	// the formatting rule is part of the state being valid: a hand edit that
	// keeps four spaces must fail here, not slip into history
	errs = append(errs, stateProblems(st)...)
	res := checkResult{OK: len(errs) == 0, Hosts: len(st.Hosts), Links: len(st.Links), Users: len(st.Users), Errors: errs, Warnings: warns}
	if len(errs) == 0 {
		return emit(res, func() {
			for _, w := range warns {
				fmt.Println("  !", w)
			}
			fmt.Printf("✓ state is valid: %d hosts, %d links, %d users\n", res.Hosts, res.Links, res.Users)
		})
	}
	if jsonOut {
		_ = emit(res, func() {})
		return ErrSilent
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  ✗", e)
	}
	fmt.Fprintf(os.Stderr, "Error: %d errors in the state\n", len(errs))
	return ErrSilent
}
