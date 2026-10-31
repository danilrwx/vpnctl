package main

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/render"
	"github.com/danilrwx/vpnctl-go/state"
)

func main() {
	dir := os.Args[1]
	st, err := state.Load(dir)
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
	repo, err := gitsync.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	head, _ := repo.Head()
	clean, _ := repo.Clean()
	fmt.Printf("head=%s clean=%v\n", head, clean)
}
