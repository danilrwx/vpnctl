package main

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/gitsync"
	"github.com/danilrwx/vpnctl-go/ops"
	"github.com/danilrwx/vpnctl-go/state"
)

func main() {
	dir := os.Args[1]
	st, err := state.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	msg, err := ops.UserAdd(st, "probe-user", []string{"split"}, []string{"reality", "ws"}, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := state.SaveUsers(dir, st.Users); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := state.SaveSecrets(dir, st.Secrets); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	repo, err := gitsync.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := repo.Commit(msg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h, _ := repo.Head()
	fmt.Printf("%s · head=%s\n", msg, h)
}
