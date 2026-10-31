package main

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/state"
	"github.com/danilrwx/vpnctl-go/validate"
)

func main() {
	st, err := state.Load(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	errs := validate.State(st)
	if len(errs) == 0 {
		fmt.Printf("✓ state is valid: %d hosts, %d links, %d users\n",
			len(st.Hosts), len(st.Links), len(st.Users))
		return
	}
	for _, e := range errs {
		fmt.Println("✗", e)
	}
	os.Exit(1)
}
