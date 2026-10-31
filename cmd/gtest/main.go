package main

import (
	"fmt"
	"os"

	"github.com/danilrwx/vpnctl-go/gitsync"
)

func main() {
	repo, err := gitsync.Init(os.Args[1])
	if err != nil {
		fmt.Println("init:", err)
		return
	}
	if err := os.WriteFile(os.Args[1]+"/probe.txt", []byte("x\n"), 0o644); err != nil {
		fmt.Println(err)
		return
	}
	if err := repo.Commit("ops: probe commit"); err != nil {
		fmt.Println("commit:", err)
		return
	}
	h, _ := repo.Head()
	clean, _ := repo.Clean()
	fmt.Printf("head=%s clean=%v\n", h, clean)
}
