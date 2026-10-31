package main

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
)

func main() {
	st, err := state.Load(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	entry, _ := st.Entry()
	user := os.Args[2]
	sub, err := render.SubURL(st, entry, user, "split")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(sub)
	links, _ := render.Links(st, entry, user, "split")
	for _, l := range links {
		fmt.Println(" ", l)
	}
	if tg, ok := render.TGLink(st, entry); ok {
		fmt.Println(tg)
	}
}
