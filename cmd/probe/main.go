package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/danilrwx/vpnctl-go/state"
)

func main() {
	st, err := state.Load(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	entry, err := st.Entry()
	if err != nil {
		return
	}
	names := make([]string, 0, len(st.Users))
	for n := range st.Users {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("entry=%s hosts=%d links=%d users=%d encrypted=%v\n", entry, len(st.Hosts), len(st.Links), len(names), st.Encrypted)
	for _, n := range names {
		u := st.Users[n]
		s := st.Secrets.Users[n]
		fmt.Printf("  %-16s modes=%v protos=%v tg=%v sub=%s\n", n, u.Modes, u.Protocols, u.TG, s.SubID)
	}
}
